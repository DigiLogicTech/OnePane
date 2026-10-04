package bootstrap

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/assistant"
	"github.com/DigiLogicTech/OnePane/internal/agentprofile"
	"github.com/DigiLogicTech/OnePane/internal/agentruntime"
	"github.com/DigiLogicTech/OnePane/internal/agentworker"
	"github.com/DigiLogicTech/OnePane/internal/approval"
	"github.com/DigiLogicTech/OnePane/internal/artifact"
	"github.com/DigiLogicTech/OnePane/internal/assurance"
	"github.com/DigiLogicTech/OnePane/internal/authority"
	"github.com/DigiLogicTech/OnePane/internal/botruntime"
	"github.com/DigiLogicTech/OnePane/internal/budget"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/config"
	"github.com/DigiLogicTech/OnePane/internal/connectors"
	"github.com/DigiLogicTech/OnePane/internal/gateway"
	"github.com/DigiLogicTech/OnePane/internal/inference"
	"github.com/DigiLogicTech/OnePane/internal/localai"
	"github.com/DigiLogicTech/OnePane/internal/node"
	"github.com/DigiLogicTech/OnePane/internal/nodefederation"
	"github.com/DigiLogicTech/OnePane/internal/observation"
	"github.com/DigiLogicTech/OnePane/internal/operation"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	"github.com/DigiLogicTech/OnePane/internal/projectorchestrator"
	"github.com/DigiLogicTech/OnePane/internal/projectroutine"
	"github.com/DigiLogicTech/OnePane/internal/projectruntime"
	"github.com/DigiLogicTech/OnePane/internal/projectworkspace"
	"github.com/DigiLogicTech/OnePane/internal/provideronboarding"
	"github.com/DigiLogicTech/OnePane/internal/resourcecoord"
	"github.com/DigiLogicTech/OnePane/internal/routine"
	"github.com/DigiLogicTech/OnePane/internal/routineworker"
	"github.com/DigiLogicTech/OnePane/internal/runtimecoord"
	"github.com/DigiLogicTech/OnePane/internal/sandboxrunner"
	"github.com/DigiLogicTech/OnePane/internal/scheduler"
	"github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
	"github.com/DigiLogicTech/OnePane/internal/system"
	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/team"
	"github.com/DigiLogicTech/OnePane/internal/teamworker"
	"github.com/DigiLogicTech/OnePane/internal/tool"
	"github.com/DigiLogicTech/OnePane/internal/vault"
	"github.com/DigiLogicTech/OnePane/internal/verification"
	"github.com/DigiLogicTech/OnePane/internal/verticalslice"
	"github.com/DigiLogicTech/OnePane/internal/watchdog"
	"github.com/DigiLogicTech/OnePane/internal/webauth"
)

type Runtime struct {
	DB                  *sqlite.DB
	System              *system.Service
	Nodes               *node.Service
	Tasks               *task.Service
	Authority           *authority.Service
	Budgets             *budget.Service
	Approvals           *approval.Service
	Policy              *policy.Engine
	Artifacts           *artifact.Service
	Observations        *observation.Service
	ToolGateway         *tool.Gateway
	Verification        *verification.Service
	Watchdog            *watchdog.Service
	Vault               *vault.Service
	Inference           *inference.Service
	LocalAI             *localai.Service
	AgentProfiles       *agentprofile.Service
	AgentRuntimes       *agentruntime.Service
	AgentWorker         *agentworker.Service
	Assurance           *assurance.Service
	Scheduler           *scheduler.Service
	RuntimeCoordinator  *runtimecoord.Service
	ResourceCoordinator *resourcecoord.Service
	ProviderOnboarding  *provideronboarding.Service
	Assistant           *assistant.Service
	ProjectOrchestrator *projectorchestrator.Service
	ProjectWorkspaces   *projectworkspace.Service
	ProjectRuntime      *projectruntime.Reconciler
	ProjectRoutine      *projectroutine.Executor
	Routines            *routine.Service
	RoutineWorker       *routineworker.Service
	Operations          *operation.Coordinator
	ReadOnlySlice       *verticalslice.ReadOnlyRunner
	WebAuth             *webauth.Service
	Gateway             *gateway.Service
	Team                *team.Service
	TeamWorker          *teamworker.Service
	Bots                *botruntime.Service
	Federation          *nodefederation.Service
}

func Open(ctx context.Context, cfg config.Config) (*Runtime, error) {
	if err := ensureDataDir(cfg.Storage.DataDir); err != nil {
		return nil, err
	}

	dbPath := filepath.Join(cfg.Storage.DataDir, "state", "harness.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return nil, fmt.Errorf("create state directory: %w", err)
	}

	db, err := sqlite.Open(dbPath)
	if err != nil {
		return nil, err
	}
	if err := db.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate database: %w", err)
	}

	clk := clock.Real{}
	systemService := system.NewService(db.SQL(), db, clk)
	if _, err := systemService.EnsureBootstrap(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("bootstrap system state: %w", err)
	}

	fingerprint, err := node.EnsureIdentitySeed(cfg.Storage.DataDir)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	nodeService := node.NewService(db.SQL(), db, clk)
	nodeName, _ := os.Hostname()
	if strings.TrimSpace(nodeName) == "" {
		nodeName = "local"
	}
	localNode, err := nodeService.EnsureLocal(ctx, nodeName, fingerprint)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("bootstrap local node: %w", err)
	}

	var federationService *nodefederation.Service
	if cfg.Federation.Enabled {
		fedIdentity, err := nodefederation.EnsureIdentity(cfg.Storage.DataDir, localNode.ID)
		if err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("initialize federation identity: %w", err)
		}
		federationService = nodefederation.NewService(db.SQL(), db, clk, nodefederation.NodeView{
			ID: localNode.ID, Name: localNode.Name, Local: true, IdentityFingerprint: localNode.IdentityFingerprint,
			TrustState: localNode.TrustState, TrustZone: localNode.TrustZone, Revision: localNode.Revision,
		}, fedIdentity, config.FederationAdvertiseURL(cfg), time.Duration(cfg.Federation.StaleSeconds)*time.Second)
		if err := federationService.ActivateLocalCapabilities(ctx); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("activate local federation capabilities: %w", err)
		}
	}

	taskService := task.NewService(db.SQL(), db, clk)
	teamService := team.NewService(db.SQL(), db, clk, taskService)
	taskService.SetAdmissionGuard(teamService)
	authorityService := authority.NewService(db.SQL(), db, clk)
	watchdogService := watchdog.NewService(db.SQL(), db, clk, "control-plane", 15*time.Second)
	if err := watchdogService.Heartbeat(ctx, map[string]any{"status": "bootstrapping"}); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize watchdog heartbeat: %w", err)
	}
	policyEngine := policy.NewEngine(authorityService, systemService, clk)
	policyEngine.SetWatchdog(watchdogService)
	artifactStore, err := artifact.NewLocalStore(filepath.Join(cfg.Storage.DataDir, "artifacts"))
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open artifact store: %w", err)
	}
	artifactService := artifact.NewService(db.SQL(), db, artifactStore, clk)
	observationService := observation.NewService(db.SQL(), db, clk)
	toolRegistry := tool.NewRegistry()
	if err := tool.RegisterSynthetic(toolRegistry); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("register built-in tools: %w", err)
	}
	sandboxAdapter := sandboxrunner.NewAdapter(cfg.Storage.DataDir, sandboxrunner.NewCLIEngine())
	if err := sandboxrunner.Register(toolRegistry, sandboxAdapter); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("register sandbox-runner tools: %w", err)
	}
	toolGateway := tool.NewGateway(db.SQL(), db, policyEngine, authorityService, toolRegistry, clk)
	toolGateway.EnableSandboxAdapter(sandboxrunner.AdapterID, sandboxrunner.AdapterVersion)
	mutationGate := operation.NewMutationGate(db.SQL())
	toolGateway.SetMutationPermitValidator(mutationGate)
	resourceCoordinator := resourcecoord.NewService(db.SQL(), db, clk)
	operationCoordinator := operation.NewCoordinator(db.SQL(), db, policyEngine, toolGateway, mutationGate, clk)
	operationCoordinator.SetGlobalResourceCoordinator(resourceCoordinator)
	approvalService := approval.NewService(db.SQL(), db, clk)
	operationCoordinator.SetApprovalConsumer(approvalService)
	if _, err := operationCoordinator.RecoverInterrupted(ctx, nil, nil, nil); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("recover interrupted operations: %w", err)
	}
	verificationService := verification.NewService(db.SQL(), db, clk)
	budgetService := budget.NewService(db.SQL(), db, clk)
	inferenceService := inference.NewService(db.SQL(), db, clk)
	if err := inferenceService.ConfigureBudget(budgetService); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("configure inference budget enforcement: %w", err)
	}
	masterKey, err := vault.EnsureMasterKey(cfg.Storage.DataDir)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize vault master key: %w", err)
	}
	vaultService, err := vault.NewService(db.SQL(), db, clk, masterKey)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize vault: %w", err)
	}
	webAuthService := webauth.NewService(db.SQL(), db, clk)
	gatewayService := gateway.NewService(db.SQL(), db, clk, authorityService, operationCoordinator, verificationService)
	if err := gatewayService.EnsureSystemPrincipals(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("bootstrap messaging gateway principals: %w", err)
	}
	if _, err := gatewayService.RecoverInterrupted(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("recover interrupted gateway deliveries: %w", err)
	}
	if err := connectors.RegisterBuiltins(toolRegistry, vaultService); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("register SaaS connector tools: %w", err)
	}
	if err := gateway.Register(toolRegistry, db.SQL(), vaultService); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("register messaging gateway tool: %w", err)
	}
	sandboxAdapter.SetSecretResolver(vaultService)
	secretResolver := vaultService
	transportRegistry := inference.NewTransportRegistry()
	if err := inference.RegisterBuiltinTransports(transportRegistry); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("register inference transports: %w", err)
	}
	if federationService != nil {
		if err := transportRegistry.Register("remote-node", nodefederation.RemoteTransport{Service: federationService}); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("register remote-node inference transport: %w", err)
		}
	}
	if err := inferenceService.ConfigureExecution(artifactService, transportRegistry, secretResolver, localNode.ID); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("configure inference execution: %w", err)
	}
	agentRuntimeRegistry := agentruntime.NewRegistry()
	if err := agentruntime.RegisterBuiltinAdapters(agentRuntimeRegistry); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("register agent runtime adapters: %w", err)
	}
	agentRuntimeService := agentruntime.NewService(db.SQL(), db, agentRuntimeRegistry, clk)
	agentRuntimeService.SetCredentialValidator(vaultService)
	if err := agentRuntimeService.ConfigureBudget(budgetService); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("configure agent runtime budget enforcement: %w", err)
	}
	agentRuntimeTransports := agentruntime.NewRuntimeTransportRegistry()
	if err := agentruntime.RegisterBuiltinRuntimeTransports(agentRuntimeTransports); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("register agent runtime transports: %w", err)
	}
	if err := agentRuntimeService.ConfigureExecution(artifactService, agentRuntimeTransports, secretResolver, localNode.ID); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("configure agent runtime execution: %w", err)
	}
	botRuntimeService := botruntime.NewService(db.SQL(), db, clk, vaultService)
	runtimeCoordinator := runtimecoord.NewService(db.SQL(), clk)
	if err := inferenceService.ConfigureRuntimeCoordinator(runtimeCoordinator); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("configure global model runtime coordinator: %w", err)
	}
	schedulerService := scheduler.NewService(db.SQL(), db, clk)
	providerOnboardingService := provideronboarding.New(inferenceService, secretResolver)
	catalogTrust := localai.NewCatalogTrustStore()
	for keyID, encoded := range cfg.LocalAI.CatalogTrustKeys {
		raw, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(raw) != ed25519.PublicKeySize {
			_ = db.Close()
			return nil, fmt.Errorf("decode local AI catalog trust key %s", keyID)
		}
		if err := catalogTrust.Add(keyID, ed25519.PublicKey(raw)); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("register local AI catalog trust key %s: %w", keyID, err)
		}
	}
	localAIService := localai.NewService(db.SQL(), db, clk, inferenceService, cfg.Storage.DataDir, catalogTrust)
	if err := localAIService.ConfigureModelPool(cfg.LocalAI.ModelPoolPath); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("configure local AI model pool: %w", err)
	}
	if err := localAIService.ConfigureResidencyHeadroom(cfg.LocalAI.ResidencyHeadroomPct); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("configure local AI residency headroom: %w", err)
	}
	if err := localAIService.ConfigureLLMFit(cfg.LocalAI.LLMFitURL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("configure llmfit advisory source: %w", err)
	}
	if err := transportRegistry.Register("llamacpp", inference.LocalOpenAITransport{Resolver: localAIService}); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("register managed local inference transport: %w", err)
	}
	if err := transportRegistry.Register("colibri", inference.LocalOpenAITransport{Resolver: localAIService}); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("register Colibri local inference transport: %w", err)
	}
	if err := localAIService.RecoverManagedRuntimes(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("recover managed local runtimes: %w", err)
	}
	if err := localAIService.RecoverManagedComponents(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("recover managed local components: %w", err)
	}
	projectWorkspaceService := projectworkspace.NewService(db.SQL(), db, clk)
	projectRoot := strings.TrimSpace(cfg.Storage.ProjectRoot)
	if projectRoot == "" {
		projectRoot = filepath.Join(cfg.Storage.DataDir, "projects")
	}
	if err := projectWorkspaceService.ConfigureProjectStorageRoot(projectRoot); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("configure Project storage root: %w", err)
	}
	agentProfileService := agentprofile.NewService(db.SQL(), db, clk)
	projectOrchestratorService := projectorchestrator.NewService(db.SQL(), db, clk, schedulerService, inferenceService, artifactService, taskService, teamService)
	assistantService := assistant.NewService(db.SQL(), clk, schedulerService, inferenceService, artifactService, projectOrchestratorService)
	projectRuntimeReconciler := projectruntime.New(projectWorkspaceService, operationCoordinator, toolGateway, observationService, verificationService)
	projectRoutineExecutor := projectroutine.New(projectWorkspaceService, toolGateway)
	routineService := routine.NewService(db.SQL(), db, clk, taskService)
	routineWorkerService := routineworker.New(db.SQL(), db, clk, taskService, authorityService, projectRoutineExecutor, observationService, verificationService, routineService)
	if err := routineWorkerService.EnsureSystemPrincipals(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("bootstrap routine worker principals: %w", err)
	}
	if _, err := routineWorkerService.RecoverLostAttempts(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("recover interrupted routine worker attempts: %w", err)
	}
	agentWorkerService := agentworker.New(db.SQL(), db, clk, localNode.ID, taskService, authorityService, budgetService, schedulerService, inferenceService, agentRuntimeService, artifactService, toolGateway, operationCoordinator, observationService, verificationService)
	teamWorkerService := teamworker.New(db.SQL(), db, clk, teamService, schedulerService, inferenceService, agentRuntimeService, artifactService, budgetService)
	if err := teamWorkerService.EnsureSystemPrincipal(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("bootstrap team worker principal: %w", err)
	}
	if err := agentWorkerService.EnsureSystemPrincipals(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("bootstrap agent worker principals: %w", err)
	}
	if _, err := agentWorkerService.RecoverLostRuns(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("recover interrupted agent worker runs: %w", err)
	}
	assuranceService := assurance.New(db.SQL(), db, clk, verificationService, taskService, artifactService, observationService, operationCoordinator)
	assuranceService.SetWorkerBridge(agentWorkerService)
	if err := assuranceService.EnsureSystemPrincipal(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("bootstrap assurance verifier: %w", err)
	}
	if _, err := assuranceService.RecoverInterrupted(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("recover interrupted assurance runs: %w", err)
	}
	readOnlySlice := verticalslice.NewReadOnlyRunner(taskService, authorityService, toolGateway, observationService, verificationService)

	return &Runtime{
		DB: db, System: systemService, Nodes: nodeService, Tasks: taskService,
		Authority: authorityService, Budgets: budgetService, Approvals: approvalService, Policy: policyEngine, Artifacts: artifactService, Observations: observationService,
		ToolGateway: toolGateway, Verification: verificationService, Watchdog: watchdogService, Vault: vaultService, Inference: inferenceService, LocalAI: localAIService, AgentProfiles: agentProfileService, AgentRuntimes: agentRuntimeService, AgentWorker: agentWorkerService, Assurance: assuranceService,
		Scheduler: schedulerService, RuntimeCoordinator: runtimeCoordinator, ResourceCoordinator: resourceCoordinator, ProviderOnboarding: providerOnboardingService, Assistant: assistantService, ProjectOrchestrator: projectOrchestratorService, ProjectWorkspaces: projectWorkspaceService,
		ProjectRuntime: projectRuntimeReconciler, ProjectRoutine: projectRoutineExecutor, Routines: routineService, RoutineWorker: routineWorkerService, Operations: operationCoordinator, ReadOnlySlice: readOnlySlice, WebAuth: webAuthService, Gateway: gatewayService, Team: teamService, TeamWorker: teamWorkerService, Bots: botRuntimeService, Federation: federationService,
	}, nil
}

func ensureDataDir(path string) error {
	if path == "" {
		return fmt.Errorf("data directory is required")
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat data directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("data path is not a directory")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("data directory %s is group/world writable (%o)", path, info.Mode().Perm())
	}
	return nil
}
