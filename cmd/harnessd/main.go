package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/api"
	"github.com/DigiLogicTech/OnePane/internal/bootstrap"
	"github.com/DigiLogicTech/OnePane/internal/chatcommands"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/config"
	"github.com/DigiLogicTech/OnePane/internal/daemonpoll"
	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/ingress"
	"github.com/DigiLogicTech/OnePane/internal/nodefederation"
	"github.com/DigiLogicTech/OnePane/internal/processlock"
	"github.com/DigiLogicTech/OnePane/internal/sandboxrunner"
	"github.com/DigiLogicTech/OnePane/internal/serviceinstall"
	"github.com/DigiLogicTech/OnePane/internal/startupevidence"
	"github.com/DigiLogicTech/OnePane/internal/webui"
)

func main() {
	var configPath, offlineRoot string
	var previewSystemdStorage, configureSystemdStorage bool
	var printSystemdUpgradeSettings bool
	var verifySystemdUpgradePID int
	var upgradeConfigSHA256 string
	var initializeSystemdStorage bool
	var initialDataDir, initialModelPool string
	flag.StringVar(&configPath, "config", "", "path to bootstrap YAML configuration")
	flag.StringVar(&offlineRoot, "startup-evidence-root", "", "read only: print sanitized offline startup evidence for an explicit data root")
	flag.BoolVar(&previewSystemdStorage, "preview-systemd-storage", false, "read only: print the systemd storage boundary for an explicit configuration")
	flag.BoolVar(&printSystemdUpgradeSettings, "print-systemd-upgrade-settings", false, "read only: print versioned Linux service data root, health port and config hash without starting OnePane")
	flag.IntVar(&verifySystemdUpgradePID, "verify-systemd-upgrade-pid", 0, "read only: verify the selected Linux service PID, executable, config, owned listener and build readiness")
	flag.StringVar(&upgradeConfigSHA256, "upgrade-config-sha256", "", "expected preserved configuration SHA-256 for explicit service readiness verification")
	flag.BoolVar(&configureSystemdStorage, "configure-systemd-storage", false, "Linux root only: provision dedicated storage and write its systemd drop-in; does not start OnePane")
	flag.BoolVar(&initializeSystemdStorage, "initialize-systemd-storage", false, "Linux root only: create an absent bootstrap configuration; preserve existing configuration")
	flag.StringVar(&initialDataDir, "initial-data-dir", "", "data root for a newly initialized systemd configuration")
	flag.StringVar(&initialModelPool, "initial-model-pool", "", "model pool for a newly initialized systemd configuration")
	flag.Parse()
	verificationRequested := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "verify-systemd-upgrade-pid" || f.Name == "upgrade-config-sha256" {
			verificationRequested = true
		}
	})
	if verificationRequested {
		if verifySystemdUpgradePID < 1 || upgradeConfigSHA256 == "" || configPath == "" || offlineRoot != "" || initializeSystemdStorage || previewSystemdStorage || configureSystemdStorage || printSystemdUpgradeSettings || initialDataDir != "" || initialModelPool != "" || flag.NArg() != 0 {
			fmt.Fprintln(os.Stderr, "Readiness verification requires only an explicit config, service PID and configuration hash")
			os.Exit(2)
		}
		if err := serviceinstall.VerifySystemdUpgrade(configPath, verifySystemdUpgradePID, upgradeConfigSHA256); err != nil {
			fmt.Fprintln(os.Stderr, "Selected service process readiness is unverified")
			os.Exit(1)
		}
		fmt.Println("Selected service process, image, configuration and owned loopback readiness verified")
		return
	}
	if initializeSystemdStorage {
		if configPath == "" || offlineRoot != "" || previewSystemdStorage || configureSystemdStorage || printSystemdUpgradeSettings || initialDataDir == "" || initialModelPool == "" || flag.NArg() != 0 {
			fmt.Fprintln(os.Stderr, "Initialization requires only explicit configuration, initial data and model paths")
			os.Exit(2)
		}
		if err := serviceinstall.InitializeSystemdConfiguration(configPath, initialDataDir, initialModelPool); err != nil {
			fmt.Fprintln(os.Stderr, "Systemd configuration initialization unavailable:", err)
			os.Exit(1)
		}
		return
	}
	if initialDataDir != "" || initialModelPool != "" {
		fmt.Fprintln(os.Stderr, "Initial storage paths require -initialize-systemd-storage")
		os.Exit(2)
	}
	if previewSystemdStorage || configureSystemdStorage || printSystemdUpgradeSettings {
		modes := 0
		for _, selected := range []bool{previewSystemdStorage, configureSystemdStorage, printSystemdUpgradeSettings} {
			if selected {
				modes++
			}
		}
		if configPath == "" || offlineRoot != "" || modes != 1 || flag.NArg() != 0 {
			fmt.Fprintln(os.Stderr, "Select one systemd storage mode and an explicit -config path")
			os.Exit(2)
		}
		var plan serviceinstall.StoragePlan
		var err error
		if configureSystemdStorage {
			plan, err = serviceinstall.ConfigureSystemdStorage(configPath)
		} else {
			plan, err = serviceinstall.PreviewSystemdStorage(configPath)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "Systemd storage configuration unavailable:", err)
			os.Exit(1)
		}
		if printSystemdUpgradeSettings {
			fmt.Print(plan.UpgradeSettings())
		} else {
			fmt.Print(plan.DropIn())
		}
		return
	}

	// Capture the SCM launch credential then remove it from the backend's
	// environment BEFORE bootstrap or any model/tool subprocess can inherit it.
	serviceShutdownToken := consumeServiceLaunchToken()

	// This mode does not open config, SQLite, start listeners or use a network.
	// Local filesystem access is governed by OS permissions on the data root.
	if offlineRoot != "" {
		rows, err := startupevidence.Read(offlineRoot)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Offline startup evidence unavailable or access denied")
			os.Exit(1)
		}
		_ = json.NewEncoder(os.Stdout).Encode(rows)
		return
	}
	// A service manager may supply this path for evidence of config failures
	// that happen before the YAML data root can be loaded.
	evidenceRoot := strings.TrimSpace(os.Getenv("ONEPANE_STARTUP_EVIDENCE_ROOT"))
	record := func(stage startupevidence.Stage, outcome startupevidence.Outcome) {
		if evidenceRoot != "" {
			_ = startupevidence.Record(evidenceRoot, startupevidence.Backend, stage, outcome)
		}
	}
	record(startupevidence.Config, startupevidence.Begin)
	log.Printf("harnessd startup: loading configuration")
	cfg, err := config.Load(configPath)
	if err != nil {
		record(startupevidence.Config, startupevidence.Failed)
		log.Fatalf("configuration: %v", err)
	}
	if evidenceRoot == "" {
		evidenceRoot = cfg.Storage.DataDir
	}
	record(startupevidence.Config, startupevidence.OK)

	// Single active backend per canonical data root. The kernel holds the
	// ownership lock for the whole daemon lifetime, including migrations,
	// restart recovery, slow inference and shutdown. A second backend must
	// not run recovery against another live worker using this SQLite state.
	instance, err := processlock.Acquire(cfg.Storage.DataDir)
	if err != nil {
		record(startupevidence.Bootstrap, startupevidence.Failed)
		log.Fatalf("harnessd data-root ownership: %v", err)
	}
	defer func() {
		if err := instance.Close(); err != nil {
			log.Printf("release harnessd data-root ownership: %v", err)
		}
	}()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	log.Printf("harnessd startup: entering bootstrap")
	record(startupevidence.Bootstrap, startupevidence.Begin)
	runtime, err := bootstrap.Open(ctx, cfg)
	if err != nil {
		record(startupevidence.Bootstrap, startupevidence.Failed)
		log.Fatalf("bootstrap: %v", err)
	}
	record(startupevidence.Bootstrap, startupevidence.OK)
	record(startupevidence.State, startupevidence.Begin)
	log.Printf("harnessd startup: bootstrap ready; preparing API routes")
	defer runtime.DB.Close()
	// A previous service crash may have stranded private OCI credential files
	// without another sandbox command to trigger the per-runtime cleanup.
	// Never clean global/SYSTEM temp or user Project/Workspace data.
	if err := sandboxrunner.CleanupStaleRuntimeCredentialFiles(cfg.Storage.DataDir); err != nil {
		log.Printf("private sandbox credential cleanup needs attention: %v", err)
	}

	state, err := runtime.System.Get(ctx)
	if err != nil {
		record(startupevidence.State, startupevidence.Failed)
		log.Fatalf("system state: %v", err)
	}
	n, err := runtime.Nodes.Local(ctx)
	if err != nil {
		record(startupevidence.State, startupevidence.Failed)
		log.Fatalf("local node: %v", err)
	}
	record(startupevidence.State, startupevidence.OK)

	bearerAuth := api.NewBearerAuthorizer(runtime.DB.SQL(), clock.Real{})
	auth := api.NewHybridAuthorizer(bearerAuth, runtime.WebAuth)
	apiServer := api.NewServer(runtime.ProjectWorkspaces, event.NewReader(runtime.DB.SQL()), auth)
	apiServer.SetWatchdog(runtime.Watchdog)
	apiServer.SetObservedHealth(
		func(context.Context) (string, string) { return runtime.AgentWorker.ObservedHealth() },
		func(context.Context) (string, string) { return runtime.Assurance.ObservedHealth() },
		runtime.LocalAI.ObservedRuntimeHealth,
	)
	activity := daemonpoll.NewSignal()
	runtime.Tasks.SetActivityWake(activity.Nudge)
	apiServer.SetAttentionDB(runtime.DB.SQL())
	apiServer.SetLibraryArtifacts(runtime.Artifacts)
	apiServer.SetWebAuth(runtime.WebAuth, strings.HasPrefix(config.APIOrigin(cfg), "https://"))
	apiServer.SetVault(runtime.Vault)
	apiServer.SetProviderOnboarding(runtime.ProviderOnboarding)
	apiServer.SetScheduler(runtime.Scheduler)
	apiServer.SetAgentRuntimes(runtime.AgentRuntimes)
	apiServer.SetGateway(runtime.Gateway)
	apiServer.SetTeam(runtime.Team)
	apiServer.SetBots(runtime.Bots)
	apiServer.SetFederation(runtime.Federation)
	apiServer.SetTasks(runtime.Tasks)
	apiServer.SetRoutines(runtime.Routines)
	chatCommandService := chatcommands.NewService(chatcommands.SQLStore{DB: runtime.DB.SQL()})
	apiServer.SetChatCommands(chatCommandService)
	apiServer.SetAssistant(runtime.Assistant)
	apiServer.SetProjectOrchestrator(runtime.ProjectOrchestrator)
	apiServer.SetAgentProfiles(runtime.AgentProfiles)
	apiServer.SetSkills(runtime.Skills)
	apiServer.SetProviderOAuth(runtime.ProviderOAuth)
	apiServer.SetLocalAI(runtime.LocalAI, n.ID)
	apiServer.SetAssurance(runtime.Assurance)
	modelPoolPath := strings.TrimSpace(cfg.LocalAI.ModelPoolPath)
	if modelPoolPath == "" {
		modelPoolPath = filepath.Join(cfg.Storage.DataDir, "models", "managed")
	}
	apiServer.SetRuntimeConfig(configPath, modelPoolPath)
	apiServer.SetWebUI(webui.Handler())
	previewSessions, err := ingress.NewSessions(config.PreviewOrigin(cfg), clock.Real{})
	if err != nil {
		log.Fatalf("preview sessions: %v", err)
	}
	apiServer.SetPreviewSessions(previewSessions)
	previewServer := ingress.NewServer(runtime.ProjectWorkspaces, ingress.NewDBAccessChecker(runtime.DB.SQL(), clock.Real{}), previewSessions)
	httpServer := &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           apiServer.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      0, // SSE responses are intentionally long-lived.
		IdleTimeout:       60 * time.Second,
	}
	// Windows SCM readiness and shutdown must prove ownership of THIS
	// service-launched backend, not merely some listener on the same port.
	// No privileged endpoints exist for ordinary non-service launches.
	if len(serviceShutdownToken) >= 32 {
		httpServer.Handler = serviceControlHandler(httpServer.Handler, serviceShutdownToken, cancel)
	}
	previewHTTPServer := &http.Server{
		Addr:              cfg.Server.PreviewListen,
		Handler:           previewServer.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       0, // App requests may stream; proxy-level bounds still apply.
		WriteTimeout:      0,
		IdleTimeout:       60 * time.Second,
	}
	var federationHTTPServer *http.Server
	if runtime.Federation != nil {
		fedServer := nodefederation.NewServer(runtime.Federation, runtime.Inference, runtime.LocalAI)
		federationHTTPServer = &http.Server{
			Addr: cfg.Federation.Listen, Handler: fedServer.Handler(), TLSConfig: fedServer.TLSConfig(),
			ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second,
		}
	}
	log.Printf("harnessd startup: recovering model download/install jobs")
	if err := runtime.LocalAI.RecoverInstallJobs(ctx); err != nil {
		log.Printf("recover local AI install jobs: %v", err)
	}
	if strings.TrimSpace(cfg.LocalAI.CatalogURL) != "" {
		if rec, err := runtime.LocalAI.RefreshCatalog(ctx, cfg.LocalAI.CatalogURL, nil); err != nil {
			log.Printf("refresh local AI catalog: %v", err)
		} else {
			log.Printf("local AI catalog active: version=%s key=%s", rec.CatalogVersion, rec.KeyID)
		}
	}

	if strings.TrimSpace(cfg.LocalAI.CatalogURL) != "" {
		go func() {
			ticker := time.NewTicker(6 * time.Hour)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if rec, err := runtime.LocalAI.RefreshCatalog(ctx, cfg.LocalAI.CatalogURL, nil); err != nil && ctx.Err() == nil {
						log.Printf("refresh local AI catalog: %v", err)
					} else if err == nil {
						log.Printf("local AI catalog refreshed: version=%s", rec.CatalogVersion)
					}
				}
			}
		}()
	}

	go func() {
		err := runtime.Watchdog.Run(ctx, 5*time.Second, func() map[string]any {
			snap := apiServer.ObservedHealth(ctx)
			return map[string]any{
				"status": snap.Status, "reason": snap.Reason, "component": "harnessd",
				"worker": snap.Worker, "assurance": snap.Assurance, "runtime": snap.Runtime,
			}
		})
		if err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("watchdog stopped: %v; autonomous mutation will fail closed", err)
		}
	}()

	// Local AI installs are durable jobs. The worker advances only jobs whose
	// catalog artifacts were signature-verified and pinned at approval time.
	go daemonpoll.Run(ctx, 3*time.Second, 30*time.Second, activity.C(), func(ctx context.Context) (bool, error) {
		jobs, err := runtime.LocalAI.TickInstallJobs(ctx, 1)
		if err != nil && ctx.Err() == nil {
			log.Printf("local AI install worker: %v", err)
			return true, err
		}
		for _, job := range jobs {
			if job.Status == "failed" && job.FailureReason != nil {
				log.Printf("local AI install job %s failed: %s", job.ID, *job.FailureReason)
			}
		}
		return len(jobs) > 0, nil
	})

	// Managed local models are loaded lazily and may be unloaded after an idle
	// period to free VRAM/RAM. A later inference request starts them again.
	if cfg.LocalAI.IdleUnloadMinutes > 0 {
		idle := time.Duration(cfg.LocalAI.IdleUnloadMinutes) * time.Minute
		go daemonpoll.Run(ctx, time.Minute, 5*time.Minute, nil, func(ctx context.Context) (bool, error) {
			n, err := runtime.LocalAI.ReapIdleManagedRuntimes(ctx, idle, 10)
			if err != nil && ctx.Err() == nil {
				log.Printf("local AI idle reaper: %v", err)
				return true, err
			}
			if n > 0 {
				log.Printf("local AI idle reaper stopped %d runtime(s)", n)
			}
			return n > 0, nil
		})
	}

	// Routine scheduling is deterministic control-plane work. Occurrences become
	// normal Tasks; this loop never executes project/app commands directly.
	go daemonpoll.Run(ctx, 30*time.Second, 5*time.Minute, activity.C(), func(ctx context.Context) (bool, error) {
		occurrences, err := runtime.Routines.Tick(ctx, time.Now().UTC())
		if err != nil && ctx.Err() == nil {
			log.Printf("routine scheduler tick: %v", err)
			return true, err
		}
		return len(occurrences) > 0, nil
	})

	// Level 5 receipt-only review is deliberately cheaper than inference:
	// one bounded SQLite cadence pass each minute, with no external probes,
	// model residency, device changes or automatic remediation. A human must
	// approve exact source permits before this loop has any eligible work.
	go daemonpoll.Run(ctx, time.Minute, 5*time.Minute, activity.C(), func(ctx context.Context) (bool, error) {
		results, err := runtime.ProjectWorkspaces.TickDueGoalEvidenceReviews(ctx, 8)
		if err != nil && ctx.Err() == nil {
			log.Printf("goal receipt-review tick: %v", err)
			return true, err
		}
		if len(results) > 0 {
			log.Printf("goal receipt-review tick: recorded %d metadata assessments (not monitored health)", len(results))
		}
		return len(results) > 0, nil
	})

	// Independent Level 5 LAN observation: this loop is inert unless a
	// human specifically approved a short-lived recurring TCP grant that
	// references an exact-IP manual probe permit for a canonical Workspace.
	// Never infer network authority from source names or receipt review.
	// At most ONE 100–1500ms handshake per minute tick; SQLite deduplicates
	// each source/cadence window across restarts and competing workers.
	go daemonpoll.Run(ctx, time.Minute, 5*time.Minute, nil, func(ctx context.Context) (bool, error) {
		results, err := runtime.ProjectWorkspaces.TickDueTCPRecurringProbes(ctx, 1)
		if err != nil && ctx.Err() == nil {
			log.Printf("consented tcp observation tick: %v", err)
			return true, err
		}
		if len(results) > 0 {
			log.Printf("consented tcp observation tick: %d source handshake attempt(s); security health UNVERIFIED", len(results))
		}
		return len(results) > 0, nil
	})

	// Budget reservations are durable but time-bounded. Expiring abandoned
	// reservations returns capacity without relying on a caller to clean up.
	go daemonpoll.Run(ctx, time.Minute, 5*time.Minute, nil, func(ctx context.Context) (bool, error) {
		n, err := runtime.Budgets.ExpireDue(ctx, 200)
		if err != nil && ctx.Err() == nil {
			log.Printf("budget expiry: %v", err)
			return true, err
		}
		if n > 0 {
			log.Printf("budget expiry released %d reservation(s)", n)
		}
		return n > 0, nil
	})

	// Team Mode deliberation processes explicit human-requested rounds before or
	// during execution. It performs reasoning only: no tool proposal is permitted.
	go daemonpoll.Run(ctx, 2*time.Second, 20*time.Second, activity.C(), func(ctx context.Context) (bool, error) {
		results, err := runtime.TeamWorker.Tick(ctx, 16)
		if err != nil && ctx.Err() == nil {
			log.Printf("team deliberation tick: %v", err)
			return true, err
		}
		for _, result := range results {
			if result.Error != "" {
				log.Printf("team turn %s member %s status=%s: %s", result.TurnID, result.MemberID, result.Status, result.Error)
			}
		}
		return len(results) > 0, nil
	})

	// Cancellation maintenance runs independently of slow inference. It settles
	// only pristine requests; dispatched work retains its recovery boundary.
	go daemonpoll.Run(ctx, 2*time.Second, 20*time.Second, activity.C(), func(ctx context.Context) (bool, error) {
		busy := false
		if n, err := runtime.Tasks.CaptureRecoveryCases(ctx, 4); err != nil && ctx.Err() == nil {
			log.Printf("task recovery intake: %v", err)
			busy = true
		} else if n.Captured > 0 {
			busy = true
		}
		if n, err := runtime.Tasks.DiscoverRecoveryCases(ctx, 4); err != nil && ctx.Err() == nil {
			log.Printf("historical task recovery intake: %v", err)
			busy = true
		} else if n.Captured > 0 {
			busy = true
		}
		if n, err := runtime.Tasks.SettleUnstartedCancellations(ctx, 4); err != nil && ctx.Err() == nil {
			log.Printf("unstarted task cancellation: %v", err)
			busy = true
		} else if n > 0 {
			busy = true
		}
		if n, err := runtime.Team.SettleUnstartedCancellations(ctx, 4); err != nil && ctx.Err() == nil {
			log.Printf("unstarted Team cancellation: %v", err)
			busy = true
		} else if n > 0 {
			busy = true
		}
		return busy, nil
	})

	// General autonomous agent execution consumes ordinary READY Tasks. Each
	// attempt is durable and bounded; reasoning may route to a ModelDeployment or
	// an eligible external agent runtime, while all tool side effects remain
	// behind CapabilityLease/ToolGateway/OperationCoordinator enforcement.
	go daemonpoll.Run(ctx, 2*time.Second, 20*time.Second, activity.C(), func(ctx context.Context) (bool, error) {
		results, err := runtime.AgentWorker.Tick(ctx, 10)
		if err != nil && ctx.Err() == nil {
			log.Printf("agent worker tick: %v", err)
			return true, err
		}
		for _, result := range results {
			if result.Error != "" && result.Status != "retry_reasoning" && result.Status != "escalating" {
				log.Printf("agent task %s run %s status=%s: %s", result.TaskID, result.RunID, result.Status, result.Error)
			}
		}
		return len(results) > 0, nil
	})

	// Independent assurance evaluates pending V1-V5 completion evidence. It
	// never treats model agreement as verification: deterministic checks, direct
	// observations, independent integration paths and bound human acceptance are
	// required to earn the corresponding levels.
	go daemonpoll.Run(ctx, 2*time.Second, 20*time.Second, activity.C(), func(ctx context.Context) (bool, error) {
		results, err := runtime.Assurance.Tick(ctx, 20)
		if ctx.Err() == nil {
			runtime.Assurance.NoteTick(err)
		}
		if err != nil && ctx.Err() == nil {
			log.Printf("assurance worker tick: %v", err)
			return true, err
		}
		for _, result := range results {
			if result.Status == "failed" || result.Status == "inconclusive" {
				log.Printf("assurance verification %s status=%s: %s", result.VerificationID, result.Status, result.Message)
			}
		}
		return len(results) > 0, nil
	})

	// Messaging gateway delivery is a durable control-plane worker. It materializes
	// notification rules from the append-only Event Ledger and sends each delivery
	// through CapabilityLease -> OperationCoordinator -> gateway.send -> V1 receipt
	// verification. Unknown transport outcomes are never blindly replayed.
	go daemonpoll.Run(ctx, 2*time.Second, 20*time.Second, activity.C(), func(ctx context.Context) (bool, error) {
		results, err := runtime.Gateway.Tick(ctx, 20)
		if err != nil && ctx.Err() == nil {
			log.Printf("gateway delivery tick: %v", err)
			return true, err
		}
		for _, result := range results {
			if result.Status == "failed" || result.Status == "unknown" {
				msg := ""
				if result.LastError != nil {
					msg = *result.LastError
				}
				log.Printf("gateway delivery %s status=%s: %s", result.ID, result.Status, msg)
			}
		}
		return len(results) > 0, nil
	})

	// Routine execution is a separate deterministic worker. It only consumes
	// materialized project app-command Tasks and still goes through ToolGateway,
	// CapabilityLease, Observation, Verification and Checkpoint.
	go daemonpoll.Run(ctx, 5*time.Second, 30*time.Second, activity.C(), func(ctx context.Context) (bool, error) {
		results, err := runtime.RoutineWorker.Tick(ctx, 20)
		if err != nil && ctx.Err() == nil {
			log.Printf("routine worker tick: %v", err)
			return true, err
		}
		for _, result := range results {
			if result.Status != "complete" {
				log.Printf("routine occurrence %s task %s failed: %s", result.OccurrenceID, result.TaskID, result.Error)
			}
		}
		return len(results) > 0, nil
	})

	if runtime.Federation != nil {
		if cfg.Federation.DiscoveryEnabled {
			go func() {
				d := nodefederation.NewDiscovery(runtime.Federation, cfg.Federation.DiscoveryMulticast, mustPort(cfg.Federation.Listen))
				if err := d.Run(ctx); err != nil && !errors.Is(err, context.Canceled) && ctx.Err() == nil {
					log.Printf("node LAN discovery stopped: %v", err)
				}
			}()
		}
		go func() {
			var receiptPrune federationReceiptPruneSchedule
			run := func() {
				// Retry transient SQLite failures after a bounded delay rather
				// than suppressing transport-cache hygiene for an entire day.
				if err := receiptPrune.RunIfDue(time.Now(), func() (int, error) {
					return runtime.Federation.PruneCachedInferenceResponses(ctx, nodefederation.DefaultInboundReceiptRetention)
				}); err != nil && ctx.Err() == nil {
					log.Printf("federation cached response retention: %v", err)
				}
				if err := runtime.Federation.HeartbeatPeers(ctx); err != nil && ctx.Err() == nil {
					log.Printf("node federation heartbeat: %v", err)
				}
				if n, err := runtime.Federation.ExpireStale(ctx); err != nil && ctx.Err() == nil {
					log.Printf("node federation stale check: %v", err)
				} else if n > 0 {
					log.Printf("node federation marked %d peer(s) unavailable", n)
				}
			}
			run()
			ticker := time.NewTicker(time.Duration(cfg.Federation.HeartbeatSeconds) * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					run()
				}
			}
		}()
	}

	type serverResult struct {
		name string
		err  error
	}
	serverCount := 2
	if federationHTTPServer != nil {
		serverCount++
	}
	record(startupevidence.API, startupevidence.Begin)
	serverErr := make(chan serverResult, serverCount)
	go func() {
		err := httpServer.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- serverResult{name: "control-plane", err: err}
			return
		}
		serverErr <- serverResult{name: "control-plane"}
	}()
	go func() {
		err := previewHTTPServer.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- serverResult{name: "preview", err: err}
			return
		}
		serverErr <- serverResult{name: "preview"}
	}()
	if federationHTTPServer != nil {
		go func() {
			err := federationHTTPServer.ListenAndServeTLS("", "")
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				serverErr <- serverResult{name: "federation", err: err}
				return
			}
			serverErr <- serverResult{name: "federation"}
		}()
	}

	fmt.Printf("harnessd initialized: mode=%s revision=%d node=%s api=%s preview=%s federation=%t\n", state.Mode, state.Revision, n.ID, cfg.Server.Listen, cfg.Server.PreviewListen, federationHTTPServer != nil)

	select {
	case <-ctx.Done():
		record(startupevidence.API, startupevidence.Stopped)
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 48*time.Second)
		defer shutdownCancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("http shutdown: %v", err)
		}
		if err := previewHTTPServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("preview http shutdown: %v", err)
		}
		if federationHTTPServer != nil {
			if err := federationHTTPServer.Shutdown(shutdownCtx); err != nil {
				log.Printf("federation http shutdown: %v", err)
			}
		}
		if err := runtime.LocalAI.ShutdownManagedRuntimes(shutdownCtx); err != nil {
			log.Printf("managed local AI shutdown: %v", err)
		}
		if err := runtime.LocalAI.StopManagedLLMFit(shutdownCtx); err != nil {
			log.Printf("managed llmfit shutdown: %v", err)
		}
		for i := 0; i < serverCount; i++ {
			<-serverErr
		}
	case result := <-serverErr:
		if result.err != nil {
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			_ = httpServer.Shutdown(shutdownCtx)
			_ = previewHTTPServer.Shutdown(shutdownCtx)
			if federationHTTPServer != nil {
				_ = federationHTTPServer.Shutdown(shutdownCtx)
			}
			_ = runtime.LocalAI.ShutdownManagedRuntimes(shutdownCtx)
			_ = runtime.LocalAI.StopManagedLLMFit(shutdownCtx)
			record(startupevidence.API, startupevidence.Failed)
			log.Fatalf("%s http server: %v", result.name, result.err)
		}
	}
}

func mustPort(addr string) int {
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		return 18443
	}
	n, err := strconv.Atoi(p)
	if err != nil || n < 1 || n > 65535 {
		return 18443
	}
	return n
}
