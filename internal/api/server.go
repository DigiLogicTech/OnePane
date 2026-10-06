package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sort"
	"strings"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/agentruntime"
	"github.com/DigiLogicTech/OnePane/internal/assurance"
	"github.com/DigiLogicTech/OnePane/internal/botruntime"
	"github.com/DigiLogicTech/OnePane/internal/buildinfo"
	"github.com/DigiLogicTech/OnePane/internal/chatcommands"
	"github.com/DigiLogicTech/OnePane/internal/config"
	"github.com/DigiLogicTech/OnePane/internal/connectors"
	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/gateway"
	"github.com/DigiLogicTech/OnePane/internal/inference"
	"github.com/DigiLogicTech/OnePane/internal/localai"
	"github.com/DigiLogicTech/OnePane/internal/nodefederation"
	"github.com/DigiLogicTech/OnePane/internal/projectworkspace"
	"github.com/DigiLogicTech/OnePane/internal/provideronboarding"
	"github.com/DigiLogicTech/OnePane/internal/routine"
	"github.com/DigiLogicTech/OnePane/internal/scheduler"
	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/team"
	"github.com/DigiLogicTech/OnePane/internal/vault"
	"github.com/DigiLogicTech/OnePane/internal/webauth"
	"gopkg.in/yaml.v3"
)

type taskService interface {
	Create(context.Context, task.CreateCommand) (task.Task, error)
	List(context.Context, string, int) ([]task.Task, error)
}

type routineService interface {
	List(context.Context, string) ([]routine.Routine, error)
	Create(context.Context, routine.CreateCommand) (routine.Routine, error)
}

type chatCommandService interface {
	Handle(context.Context, string, string) (chatcommands.Result, error)
}

type projectService interface {
	Project(context.Context, string) (projectworkspace.Project, error)
	Projects(context.Context, string) ([]projectworkspace.Project, error)
	Runtime(context.Context, string) (projectworkspace.ProjectRuntime, error)
	RuntimeByProject(context.Context, string) (projectworkspace.ProjectRuntime, error)
	Change(context.Context, string) (projectworkspace.ChangeProposal, error)
	ListApplications(context.Context, string) ([]projectworkspace.Application, error)
	ListEndpoints(context.Context, string) ([]projectworkspace.Endpoint, error)
	ListChanges(context.Context, string) ([]projectworkspace.ChangeProposal, error)
	ListRoutineBindings(context.Context, string) ([]projectworkspace.RoutineBinding, error)
	CreateProject(context.Context, projectworkspace.CreateProjectCommand) (projectworkspace.Project, error)
	UpdateProjectPolicy(context.Context, projectworkspace.UpdateProjectPolicyCommand) (projectworkspace.Project, error)
	CreateRuntime(context.Context, projectworkspace.CreateRuntimeCommand) (projectworkspace.ProjectRuntime, error)
	SetRuntimeDesiredState(context.Context, projectworkspace.SetRuntimeDesiredStateCommand) (projectworkspace.ProjectRuntime, error)
	UpdateRuntimePolicy(context.Context, projectworkspace.UpdateRuntimePolicyCommand) (projectworkspace.ProjectRuntime, error)
	DeclareApplication(context.Context, projectworkspace.DeclareApplicationCommand) (projectworkspace.Application, error)
	DeclareEndpoint(context.Context, projectworkspace.DeclareEndpointCommand) (projectworkspace.Endpoint, error)
	ProposeChange(context.Context, projectworkspace.ProposeChangeCommand) (projectworkspace.ChangeProposal, error)
	ReviewChange(context.Context, projectworkspace.ReviewChangeCommand) (projectworkspace.ChangeProposal, error)
	BindRoutine(context.Context, projectworkspace.BindRoutineCommand) (projectworkspace.RoutineBinding, error)
}

type eventReader interface {
	After(context.Context, string, int64, int) ([]event.Stored, error)
}

type ingressRouteResolver interface {
	ResolveIngressRoute(context.Context, string) (projectworkspace.IngressRoute, error)
}

type localAIService interface {
	DetectAndPersist(context.Context, string) (localai.HardwareProfile, error)
	Recommendations(context.Context, string, localai.RecommendRequest) ([]localai.Recommendation, error)
	QueueOneClickInstall(context.Context, localai.OneClickInstallRequest) (localai.InstallJob, error)
	InstallJob(context.Context, string) (localai.InstallJob, error)
	Catalog() *localai.CatalogService
	SpecSheet(context.Context, string) (localai.ModelSpecSheet, error)
	StartTestbed(context.Context, string, *string, string) (localai.TestbedSession, error)
	RunTestbedTurn(context.Context, string, localai.TestbedTurnCommand) (localai.TestbedTurn, error)
	CompleteTestbed(context.Context, string) error
	TestbedSession(context.Context, string) (localai.TestbedSession, error)
	ListTestbedTurns(context.Context, string) ([]localai.TestbedTurn, error)
	AdmitModel(context.Context, string, localai.AdmissionCommand) (localai.ModelSpecSheet, error)
	ManagedDeploymentWorkspace(context.Context, string) (*string, error)
	ManagedDeployments(context.Context, string) ([]localai.ManagedDeploymentSummary, error)
	ComputePolicy(context.Context, string) (localai.ComputePolicy, error)
	SetComputePolicy(context.Context, localai.ComputePolicyCommand) (localai.ComputePolicy, error)
	RegisterColibriFolder(context.Context, localai.RegisterColibriCommand) (inference.ModelDeployment, error)
	ConfigureModelPool(string) error
	ManagedComponents(context.Context) (map[string]localai.ManagedComponent, error)
	RequestComponentAction(context.Context, string, string, *string) (localai.ComponentJob, error)
	ComponentJob(context.Context, string) (localai.ComponentJob, error)
	ManageComponent(context.Context, string, string) (localai.ManagedComponent, error)
}

type nodeFederationService interface {
	Nodes(context.Context) ([]nodefederation.NodeView, error)
	Pairings(context.Context) ([]nodefederation.Pairing, error)
	BeginPair(context.Context, string) (nodefederation.Pairing, string, error)
	ConfirmPair(context.Context, string, string) (nodefederation.Pairing, error)
	Revoke(context.Context, string) error
	Manifest(context.Context, string) (nodefederation.CapabilityManifest, error)
	CanOperate(context.Context, string) error
	ModelManagementGrant(context.Context, string) (nodefederation.ModelManagementGrant, error)
	SetModelManagementGrant(context.Context, string, string, bool) (nodefederation.ModelManagementGrant, error)
	RemoteModelManagementStatus(context.Context, string) (nodefederation.ModelManagementGrant, error)
	RemoteModelRecommendations(context.Context, string, nodefederation.RemoteModelRecommendRequest) (localai.HardwareProfile, []localai.Recommendation, error)
	RemoteInstallModel(context.Context, string, nodefederation.RemoteModelInstallRequest) (localai.InstallJob, error)
	RemoteInstallJob(context.Context, string, string) (localai.InstallJob, error)
	RemoteModelSpecSheet(context.Context, string, string) (localai.ModelSpecSheet, error)
	RemoteStartModelTestbed(context.Context, string, string, string) (localai.TestbedSession, error)
	RemoteRunModelTestbedTurn(context.Context, string, string, localai.TestbedTurnCommand) (localai.TestbedTurn, error)
	RemoteModelTestbedSession(context.Context, string, string) (localai.TestbedSession, error)
	RemoteModelTestbedTurns(context.Context, string, string) ([]localai.TestbedTurn, error)
	RemoteCompleteModelTestbed(context.Context, string, string) error
	RemoteAdmitModel(context.Context, string, string, localai.AdmissionCommand) (localai.ModelSpecSheet, error)
}

type schedulerCandidateService interface {
	Candidates(context.Context, string, string, string) ([]scheduler.Candidate, error)
}

type providerOnboardingService interface {
	Connection(context.Context, string) (inference.ProviderConnection, error)
	RevokeProvider(context.Context, string, *string) (inference.ProviderConnection, error)
	Connections(context.Context, *string) ([]inference.ProviderConnection, error)
	ProbeOmniRoute(context.Context, string, *string) (provideronboarding.OmniRouteProbe, error)
	AddOmniRoute(context.Context, provideronboarding.OmniRouteCommand) (provideronboarding.OmniRouteResult, error)
	ProbeProvider(context.Context, provideronboarding.PresetID, string, *string) (provideronboarding.ProviderProbe, error)
	AddProvider(context.Context, provideronboarding.ProviderCommand) (provideronboarding.ProviderResult, error)
}

type vaultService interface {
	CreateProviderCredential(context.Context, vault.ProviderCredentialCommand) (vault.SecretRecord, error)
	CreatePluginCredential(context.Context, vault.PluginCredentialCommand) (vault.SecretRecord, error)
	CreateAgentRuntimeCredential(context.Context, vault.AgentRuntimeCredentialCommand) (vault.SecretRecord, error)
	CreateGatewayCredential(context.Context, vault.GatewayCredentialCommand) (vault.SecretRecord, error)
	CreateBotCredential(context.Context, vault.BotCredentialCommand) (vault.SecretRecord, error)
	ListWorkspace(context.Context, string) ([]vault.SecretRecord, error)
}

type agentRuntimeService interface {
	Get(context.Context, string) (agentruntime.Connection, error)
	RegisterPreset(context.Context, agentruntime.RegisterPresetCommand) (agentruntime.Connection, error)
	SetStatus(context.Context, agentruntime.SetStatusCommand) (agentruntime.Connection, error)
}

type botRuntimeService interface {
	CreateConnection(context.Context, botruntime.CreateConnectionCommand) (botruntime.Connection, error)
	Connection(context.Context, string) (botruntime.Connection, error)
	ListConnections(context.Context, string) ([]botruntime.Connection, error)
	CreateBot(context.Context, botruntime.CreateBotCommand) (botruntime.Bot, error)
	Bot(context.Context, string) (botruntime.Bot, error)
	ListBots(context.Context, string) ([]botruntime.Bot, error)
	CreateSession(context.Context, botruntime.CreateSessionCommand) (botruntime.Session, error)
	Session(context.Context, string) (botruntime.Session, error)
	ListSessions(context.Context, string) ([]botruntime.Session, error)
	ListMessages(context.Context, string, int) ([]botruntime.Message, error)
	SendMessage(context.Context, botruntime.SendMessageCommand) (botruntime.SendResult, error)
}

type previewSessionMinter interface {
	MintEndpointGrant(endpointID, principalID, credentialID string) (string, int64, error)
}

type assuranceService interface {
	Workspace(context.Context, string) (string, error)
	Accept(context.Context, assurance.AcceptanceCommand) error
}

type teamService interface {
	CreateTeam(context.Context, team.CreateTeamCommand) (team.Team, error)
	Team(context.Context, string) (team.Team, error)
	ListTeams(context.Context, string) ([]team.Team, error)
	AddMember(context.Context, team.AddMemberCommand) (team.Member, error)
	ListMembers(context.Context, string) ([]team.Member, error)
	UpdateConfiguration(context.Context, team.UpdateConfigurationCommand) (team.Team, error)
	StartSession(context.Context, team.StartSessionCommand) (team.Session, error)
	Session(context.Context, string) (team.Session, error)
	SessionByTask(context.Context, string) (team.Session, error)
	PostMessage(context.Context, team.PostMessageCommand) (team.Message, error)
	ListMessages(context.Context, string, int) ([]team.Message, error)
	RequestRound(context.Context, team.RequestRoundCommand) ([]team.TurnRequest, error)
	ProposePlan(context.Context, team.ProposePlanCommand) (team.Plan, error)
	ListPlans(context.Context, string) ([]team.Plan, error)
	AcceptPlan(context.Context, team.AcceptPlanCommand) (team.Session, error)
	RaiseObjection(context.Context, team.RaiseObjectionCommand) (team.Objection, error)
	ResolveObjection(context.Context, team.ResolveObjectionCommand) (team.Objection, error)
	Objection(context.Context, string) (team.Objection, error)
	ListObjections(context.Context, string) ([]team.Objection, error)
	ListDecisions(context.Context, string) ([]team.Decision, error)
	ListTurns(context.Context, string) ([]team.TurnRequest, error)
	ReopenDeliberation(context.Context, team.ReopenCommand) (team.Session, error)
}

type gatewayService interface {
	CreateConnection(context.Context, string, string, string, string, *string, json.RawMessage, string) (gateway.Connection, error)
	CreateTarget(context.Context, string, string, string, string, *string, json.RawMessage) (gateway.Target, error)
	CreateRule(context.Context, gateway.Rule) (gateway.Rule, error)
	ListConnections(context.Context, string) ([]gateway.Connection, error)
	ListTargets(context.Context, string) ([]gateway.Target, error)
	ListRules(context.Context, string) ([]gateway.Rule, error)
	Enqueue(context.Context, string, string, string, string, string) (gateway.Delivery, error)
	NotifyRoutineTarget(context.Context, string, string, string) (gateway.Rule, error)
	RoutineWorkspace(context.Context, string) (string, error)
}

type Server struct {
	mux             *http.ServeMux
	projects        projectService
	events          eventReader
	auth            Authorizer
	ingressRoutes   ingressRouteResolver
	previewSessions previewSessionMinter
	webAuth         *webauth.Service
	secureCookies   bool
	vault           vaultService
	webUI           http.Handler
	localAI         localAIService
	localNodeID     string
	providers       providerOnboardingService
	scheduler       schedulerCandidateService
	assurance       assuranceService
	agentRuntimes   agentRuntimeService
	gateway         gatewayService
	team            teamService
	bots            botRuntimeService
	federation      nodeFederationService
	tasks           taskService
	routines        routineService
	chatCommands    chatCommandService
	assistant       assistantService
	projectOrchestrator projectOrchestratorService
	agentProfiles   agentProfileService
	skills          skillCatalogService
	providerOAuth   providerOAuthService
	configPath      string
	modelPoolPath   string
}

func NewServer(projects projectService, events eventReader, auth Authorizer) *Server {
	s := &Server{mux: http.NewServeMux(), projects: projects, events: events, auth: auth}
	if routes, ok := any(projects).(ingressRouteResolver); ok {
		s.ingressRoutes = routes
	}
	s.routes()
	return s
}
func (s *Server) SetPreviewSessions(m previewSessionMinter) { s.previewSessions = m }
func (s *Server) SetWebAuth(a *webauth.Service, secureCookies bool) {
	s.webAuth = a
	s.secureCookies = secureCookies
}
func (s *Server) SetVault(v vaultService) { s.vault = v }
func (s *Server) SetWebUI(h http.Handler) { s.webUI = h }
func (s *Server) SetLocalAI(v localAIService, localNodeID string) {
	s.localAI = v
	s.localNodeID = localNodeID
}
func (s *Server) SetProviderOnboarding(v providerOnboardingService) { s.providers = v }
func (s *Server) SetScheduler(v schedulerCandidateService)          { s.scheduler = v }
func (s *Server) SetAssurance(v assuranceService)                   { s.assurance = v }
func (s *Server) SetAgentRuntimes(v agentRuntimeService)            { s.agentRuntimes = v }
func (s *Server) SetGateway(v gatewayService)                       { s.gateway = v }
func (s *Server) SetTeam(v teamService)                             { s.team = v }
func (s *Server) SetBots(v botRuntimeService)                       { s.bots = v }
func (s *Server) SetFederation(v nodeFederationService)             { s.federation = v }
func (s *Server) SetTasks(v taskService)                            { s.tasks = v }
func (s *Server) SetRoutines(v routineService)                      { s.routines = v }
func (s *Server) SetChatCommands(v chatCommandService)              { s.chatCommands = v }
func (s *Server) SetAssistant(v assistantService)                    { s.assistant = v }
func (s *Server) SetProjectOrchestrator(v projectOrchestratorService) { s.projectOrchestrator = v }
func (s *Server) SetAgentProfiles(v agentProfileService)             { s.agentProfiles = v }
func (s *Server) SetSkills(v skillCatalogService)                     { s.skills = v }
func (s *Server) SetProviderOAuth(v providerOAuthService)             { s.providerOAuth = v }
func (s *Server) SetRuntimeConfig(path, modelPoolPath string) {
	s.configPath = strings.TrimSpace(path)
	s.modelPoolPath = strings.TrimSpace(modelPoolPath)
}
func (s *Server) Handler() http.Handler { return s.securityHeaders(s.mux) }

func (s *Server) routes() {
	s.mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})
	s.mux.HandleFunc("GET /v1/about", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"version": buildinfo.Version, "revision": buildinfo.Revision, "build_time": buildinfo.BuildTime})
	})
	s.mux.HandleFunc("GET /v1/setup/status", s.setupStatus)
	s.mux.HandleFunc("POST /v1/setup/admin", s.bootstrapAdmin)
	s.mux.HandleFunc("POST /v1/auth/login", s.login)
	s.mux.HandleFunc("POST /v1/auth/logout", s.logout)
	s.mux.HandleFunc("GET /v1/auth/me", s.me)
	s.mux.HandleFunc("GET /v1/assistant", s.assistantOverview)
	s.mux.HandleFunc("GET /v1/assistant/threads", s.listAssistantThreads)
	s.mux.HandleFunc("POST /v1/assistant/threads", s.createAssistantThread)
	s.mux.HandleFunc("GET /v1/assistant/threads/{threadID}/turns", s.listAssistantTurns)
	s.mux.HandleFunc("POST /v1/assistant/threads/{threadID}/turns", s.submitAssistantTurn)
	s.mux.HandleFunc("POST /v1/assistant/threads/{threadID}/scope", s.setAssistantScope)
	s.mux.HandleFunc("GET /v1/agent-profiles", s.listAgentProfiles)
	s.mux.HandleFunc("POST /v1/agent-profiles", s.createAgentProfile)
	s.mux.HandleFunc("GET /v1/agent-profiles/{profileID}", s.getAgentProfile)
	s.mux.HandleFunc("PATCH /v1/agent-profiles/{profileID}", s.updateAgentProfile)
	s.mux.HandleFunc("POST /v1/agent-profiles/{profileID}/archive", s.archiveAgentProfile)
	s.mux.HandleFunc("GET /v1/agent-sessions", s.listAgentSessions)
	s.mux.HandleFunc("GET /v1/tasks", s.listTasks)
	s.mux.HandleFunc("POST /v1/tasks", s.createTask)
	s.mux.HandleFunc("GET /v1/routines", s.listRoutines)
	s.mux.HandleFunc("POST /v1/routines", s.createRoutine)
	s.mux.HandleFunc("GET /v1/chat-commands", s.listChatCommands)
	s.mux.HandleFunc("POST /v1/chat-sessions/{sessionID}/commands", s.runChatCommand)
	s.mux.HandleFunc("GET /v1/settings/local-ai", s.getLocalAISettings)
	s.mux.HandleFunc("POST /v1/settings/local-ai", s.setLocalAISettings)
	s.mux.HandleFunc("GET /v1/local-ai/catalog", s.listLocalAICatalog)
	s.mux.HandleFunc("GET /v1/local-ai/components", s.listManagedComponents)
	s.mux.HandleFunc("POST /v1/local-ai/components/{componentID}/{action}", s.manageComponent)
	s.mux.HandleFunc("GET /v1/local-ai/component-jobs/{jobID}", s.getComponentJob)
	s.mux.HandleFunc("GET /v1/local-ai/deployments", s.listManagedLocalDeployments)
	s.mux.HandleFunc("GET /v1/local-ai/deployments/{deploymentID}/compute-policy", s.getDeploymentComputePolicy)
	s.mux.HandleFunc("PATCH /v1/local-ai/deployments/{deploymentID}/compute-policy", s.setDeploymentComputePolicy)
	s.mux.HandleFunc("POST /v1/local-ai/colibri/register", s.registerColibriFolder)
	s.mux.HandleFunc("GET /v1/vault/provider-credentials", s.listProviderCredentials)
	s.mux.HandleFunc("POST /v1/vault/provider-credentials", s.createProviderCredential)
	s.mux.HandleFunc("GET /v1/provider-presets", s.providerPresets)
	s.mux.HandleFunc("GET /v1/scheduler/candidates", s.listSchedulerCandidates)
	s.mux.HandleFunc("GET /v1/providers", s.listProviders)
	s.mux.HandleFunc("POST /v1/providers/probe", s.probeProvider)
	s.mux.HandleFunc("POST /v1/providers", s.addProvider)
	s.mux.HandleFunc("POST /v1/providers/{providerID}/revoke", s.revokeProvider)
	s.mux.HandleFunc("GET /v1/provider-oauth/configs", s.listOAuthConfigs)
	s.mux.HandleFunc("PUT /v1/provider-oauth/configs/{presetID}", s.saveOAuthConfig)
	s.mux.HandleFunc("POST /v1/provider-oauth/{presetID}/start", s.startProviderOAuth)
	s.mux.HandleFunc("GET /v1/provider-oauth/callback", s.completeProviderOAuth)
	s.mux.HandleFunc("GET /v1/provider-oauth/connections", s.listProviderOAuthConnections)
	s.mux.HandleFunc("POST /v1/provider-oauth/connections/{connectionID}/revoke", s.revokeProviderOAuth)
	s.mux.HandleFunc("POST /v1/providers/omniroute/probe", s.probeOmniRoute)
	s.mux.HandleFunc("POST /v1/providers/omniroute", s.addOmniRoute)
	s.mux.HandleFunc("GET /v1/agent-runtime-presets", s.agentRuntimePresets)
	s.mux.HandleFunc("POST /v1/agent-runtimes", s.addAgentRuntime)
	s.mux.HandleFunc("POST /v1/agent-runtimes/{connectionID}/status", s.setAgentRuntimeStatus)
	s.mux.HandleFunc("GET /v1/plugin-presets", s.pluginPresets)
	s.mux.HandleFunc("POST /v1/vault/plugin-credentials", s.createPluginCredential)
	s.mux.HandleFunc("POST /v1/vault/agent-runtime-credentials", s.createAgentRuntimeCredential)
	s.mux.HandleFunc("GET /v1/bot-presets", s.botPresets)
	s.mux.HandleFunc("POST /v1/vault/bot-credentials", s.createBotCredential)
	s.mux.HandleFunc("GET /v1/bot-connections", s.listBotConnections)
	s.mux.HandleFunc("POST /v1/bot-connections", s.createBotConnection)
	s.mux.HandleFunc("GET /v1/bots", s.listBots)
	s.mux.HandleFunc("POST /v1/bots", s.createBot)
	s.mux.HandleFunc("GET /v1/bots/{botID}/sessions", s.listBotSessions)
	s.mux.HandleFunc("POST /v1/bots/{botID}/sessions", s.createBotSession)
	s.mux.HandleFunc("GET /v1/bot-sessions/{sessionID}", s.getBotSession)
	s.mux.HandleFunc("GET /v1/bot-sessions/{sessionID}/messages", s.listBotMessages)
	s.mux.HandleFunc("POST /v1/bot-sessions/{sessionID}/messages", s.sendBotMessage)
	s.mux.HandleFunc("GET /v1/gateway-presets", s.gatewayPresets)
	s.mux.HandleFunc("POST /v1/vault/gateway-credentials", s.createGatewayCredential)
	s.mux.HandleFunc("GET /v1/gateways", s.listGateways)
	s.mux.HandleFunc("POST /v1/gateways", s.createGateway)
	s.mux.HandleFunc("GET /v1/gateway-targets", s.listGatewayTargets)
	s.mux.HandleFunc("POST /v1/gateway-targets", s.createGatewayTarget)
	s.mux.HandleFunc("GET /v1/notification-rules", s.listNotificationRules)
	s.mux.HandleFunc("POST /v1/notification-rules", s.createNotificationRule)
	s.mux.HandleFunc("POST /v1/notifications", s.createNotification)
	s.mux.HandleFunc("POST /v1/routines/{routineID}/notification-targets", s.createRoutineNotificationTarget)
	s.mux.HandleFunc("GET /v1/teams", s.listTeams)
	s.mux.HandleFunc("POST /v1/teams", s.createTeam)
	s.mux.HandleFunc("GET /v1/teams/{teamID}/members", s.listTeamMembers)
	s.mux.HandleFunc("POST /v1/teams/{teamID}/members", s.addTeamMember)
	s.mux.HandleFunc("PATCH /v1/teams/{teamID}/configuration", s.updateTeamConfiguration)
	s.mux.HandleFunc("GET /v1/team-presets", s.listTeamPresets)
	s.mux.HandleFunc("GET /v1/skills/packages", s.listSkillPackages)
	s.mux.HandleFunc("POST /v1/skills/packages/upload", s.uploadSkillPackage)
	s.mux.HandleFunc("POST /v1/skills/packages/{packageID}/install", s.installSkillPackage)
	s.mux.HandleFunc("PATCH /v1/skills/packages/{packageID}", s.setSkillPackageStatus)
	s.mux.HandleFunc("GET /v1/skills/tool-bundles", s.listToolBundles)
	s.mux.HandleFunc("GET /v1/skills/assignments", s.listSkillAssignments)
	s.mux.HandleFunc("POST /v1/skills/assignments", s.assignSkill)
	s.mux.HandleFunc("POST /v1/tasks/{taskID}/team-session", s.startTeamSession)
	s.mux.HandleFunc("GET /v1/tasks/{taskID}/team-session", s.getTaskTeamSession)
	s.mux.HandleFunc("GET /v1/team-sessions/{sessionID}", s.getTeamSession)
	s.mux.HandleFunc("GET /v1/team-sessions/{sessionID}/messages", s.listTeamMessages)
	s.mux.HandleFunc("POST /v1/team-sessions/{sessionID}/messages", s.postTeamMessage)
	s.mux.HandleFunc("POST /v1/team-sessions/{sessionID}/rounds", s.requestTeamRound)
	s.mux.HandleFunc("GET /v1/team-sessions/{sessionID}/turns", s.listTeamTurns)
	s.mux.HandleFunc("GET /v1/team-sessions/{sessionID}/plans", s.listTeamPlans)
	s.mux.HandleFunc("POST /v1/team-sessions/{sessionID}/plans", s.proposeTeamPlan)
	s.mux.HandleFunc("POST /v1/team-sessions/{sessionID}/plans/{planID}/accept", s.acceptTeamPlan)
	s.mux.HandleFunc("GET /v1/team-sessions/{sessionID}/objections", s.listTeamObjections)
	s.mux.HandleFunc("POST /v1/team-sessions/{sessionID}/objections", s.raiseTeamObjection)
	s.mux.HandleFunc("POST /v1/team-objections/{objectionID}/resolve", s.resolveTeamObjection)
	s.mux.HandleFunc("GET /v1/team-sessions/{sessionID}/decisions", s.listTeamDecisions)
	s.mux.HandleFunc("POST /v1/team-sessions/{sessionID}/reopen", s.reopenTeamDeliberation)
	s.mux.HandleFunc("GET /v1/nodes", s.listNodes)
	s.mux.HandleFunc("GET /v1/node-pairings", s.listNodePairings)
	s.mux.HandleFunc("POST /v1/nodes/{nodeID}/pair", s.beginNodePair)
	s.mux.HandleFunc("POST /v1/nodes/{nodeID}/pair/confirm", s.confirmNodePair)
	s.mux.HandleFunc("POST /v1/nodes/{nodeID}/revoke", s.revokeNode)
	s.mux.HandleFunc("GET /v1/nodes/{nodeID}/capabilities", s.nodeCapabilities)
	s.mux.HandleFunc("GET /v1/nodes/{nodeID}/model-management", s.getNodeModelManagement)
	s.mux.HandleFunc("POST /v1/nodes/{nodeID}/model-management", s.setNodeModelManagement)
	s.mux.HandleFunc("GET /v1/nodes/{nodeID}/remote-model-management", s.getRemoteNodeModelManagement)
	s.mux.HandleFunc("POST /v1/nodes/{nodeID}/models/recommendations", s.remoteNodeModelRecommendations)
	s.mux.HandleFunc("POST /v1/nodes/{nodeID}/models/install", s.remoteNodeModelInstall)
	s.mux.HandleFunc("GET /v1/nodes/{nodeID}/model-install-jobs/{jobID}", s.remoteNodeModelInstallJob)
	s.mux.HandleFunc("GET /v1/nodes/{nodeID}/models/{deploymentID}/spec-sheet", s.remoteNodeModelSpecSheet)
	s.mux.HandleFunc("POST /v1/nodes/{nodeID}/models/{deploymentID}/testbed/sessions", s.remoteNodeStartModelTestbed)
	s.mux.HandleFunc("GET /v1/nodes/{nodeID}/model-testbed/{sessionID}", s.remoteNodeGetModelTestbed)
	s.mux.HandleFunc("GET /v1/nodes/{nodeID}/model-testbed/{sessionID}/turns", s.remoteNodeListModelTestbedTurns)
	s.mux.HandleFunc("POST /v1/nodes/{nodeID}/model-testbed/{sessionID}/turns", s.remoteNodeRunModelTestbedTurn)
	s.mux.HandleFunc("POST /v1/nodes/{nodeID}/model-testbed/{sessionID}/complete", s.remoteNodeCompleteModelTestbed)
	s.mux.HandleFunc("POST /v1/nodes/{nodeID}/models/{deploymentID}/admission", s.remoteNodeAdmitModel)
	s.mux.HandleFunc("POST /v1/local-ai/detect", s.detectLocalAI)
	s.mux.HandleFunc("POST /v1/local-ai/recommendations", s.recommendLocalAI)
	s.mux.HandleFunc("POST /v1/local-ai/catalogs/import", s.importLocalAICatalog)
	s.mux.HandleFunc("POST /v1/local-ai/install-jobs", s.queueLocalAIInstall)
	s.mux.HandleFunc("GET /v1/local-ai/install-jobs/{jobID}", s.getLocalAIInstallJob)
	s.mux.HandleFunc("GET /v1/model-deployments/{deploymentID}/spec-sheet", s.getModelSpecSheet)
	s.mux.HandleFunc("POST /v1/model-deployments/{deploymentID}/testbed/sessions", s.startModelTestbed)
	s.mux.HandleFunc("GET /v1/model-testbed/{sessionID}", s.getModelTestbed)
	s.mux.HandleFunc("GET /v1/model-testbed/{sessionID}/turns", s.listModelTestbedTurns)
	s.mux.HandleFunc("POST /v1/model-testbed/{sessionID}/turns", s.runModelTestbedTurn)
	s.mux.HandleFunc("POST /v1/model-testbed/{sessionID}/complete", s.completeModelTestbed)
	s.mux.HandleFunc("POST /v1/model-deployments/{deploymentID}/admission", s.admitModelDeployment)
	s.mux.HandleFunc("GET /v1/projects", s.listProjects)
	s.mux.HandleFunc("POST /v1/projects", s.createProject)
	s.mux.HandleFunc("GET /v1/projects/{projectID}", s.getProject)
	s.mux.HandleFunc("GET /v1/projects/{projectID}/orchestrator", s.getProjectOrchestrator)
	s.mux.HandleFunc("GET /v1/projects/{projectID}/orchestrator/turns", s.listProjectOrchestratorTurns)
	s.mux.HandleFunc("POST /v1/projects/{projectID}/orchestrator/turns", s.submitProjectOrchestratorTurn)
	s.mux.HandleFunc("GET /v1/projects/{projectID}/orchestrator/handoffs", s.listProjectHandoffs)
	s.mux.HandleFunc("PATCH /v1/projects/{projectID}", s.updateProjectPolicy)
	s.mux.HandleFunc("POST /v1/projects/{projectID}/runtime", s.createRuntime)
	s.mux.HandleFunc("GET /v1/projects/{projectID}/runtime", s.getRuntimeByProject)
	s.mux.HandleFunc("POST /v1/project-runtimes/{runtimeID}/desired-state", s.setRuntimeDesired)
	s.mux.HandleFunc("PATCH /v1/project-runtimes/{runtimeID}/policy", s.updateRuntimePolicy)
	s.mux.HandleFunc("POST /v1/project-runtimes/{runtimeID}/applications", s.declareApplication)
	s.mux.HandleFunc("GET /v1/project-runtimes/{runtimeID}/applications", s.listApplications)
	s.mux.HandleFunc("POST /v1/project-runtimes/{runtimeID}/endpoints", s.declareEndpoint)
	s.mux.HandleFunc("GET /v1/project-runtimes/{runtimeID}/endpoints", s.listEndpoints)
	s.mux.HandleFunc("POST /v1/project-endpoints/{endpointID}/preview-session", s.createPreviewSession)
	s.mux.HandleFunc("POST /v1/projects/{projectID}/changes", s.proposeChange)
	s.mux.HandleFunc("GET /v1/projects/{projectID}/changes", s.listChanges)
	s.mux.HandleFunc("POST /v1/project-changes/{proposalID}/review", s.reviewChange)
	s.mux.HandleFunc("POST /v1/projects/{projectID}/routine-bindings", s.bindRoutine)
	s.mux.HandleFunc("GET /v1/projects/{projectID}/routine-bindings", s.listRoutineBindings)
	s.mux.HandleFunc("POST /v1/verifications/{verificationID}/acceptance", s.acceptVerification)
	s.mux.HandleFunc("GET /v1/events", s.listEvents)
	s.mux.HandleFunc("GET /v1/events/stream", s.streamEvents)
	s.mux.HandleFunc("GET /", s.serveWebUI)
}

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	workspaceID := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if !s.authorize(w, r, i, workspaceID, "events.read") {
		return
	}
	after := int64(0)
	if raw := strings.TrimSpace(r.URL.Query().Get("after")); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "after must be a non-negative integer")
			return
		}
		after = n
	}
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	var rows []event.Stored
	var err error
	if (r.URL.Query().Get("latest") == "1" || strings.EqualFold(r.URL.Query().Get("latest"), "true")) && after == 0 {
		if recent, ok := s.events.(interface {
			Recent(context.Context, string, int) ([]event.Stored, error)
		}); ok {
			rows, err = recent.Recent(r.Context(), workspaceID, limit)
		} else {
			rows, err = s.events.After(r.Context(), workspaceID, after, limit)
		}
	} else {
		rows, err = s.events.After(r.Context(), workspaceID, after, limit)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) acceptVerification(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if s.assurance == nil {
		writeError(w, http.StatusServiceUnavailable, "assurance unavailable")
		return
	}
	verificationID := strings.TrimSpace(r.PathValue("verificationID"))
	workspaceID, err := s.assurance.Workspace(r.Context(), verificationID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "verification not found")
			return
		}
		writeError(w, http.StatusBadRequest, "verification is not eligible for acceptance")
		return
	}
	if !s.authorize(w, r, i, workspaceID, "verification.accept") {
		return
	}
	var in struct {
		Decision string `json:"decision"`
		Note     string `json:"note"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	err = s.assurance.Accept(r.Context(), assurance.AcceptanceCommand{
		VerificationID: verificationID,
		PrincipalID:    i.PrincipalID,
		Decision:       strings.ToLower(strings.TrimSpace(in.Decision)),
		Note:           in.Note,
	})
	if err != nil {
		switch {
		case errors.Is(err, assurance.ErrAcceptanceIneligible):
			writeError(w, http.StatusForbidden, "principal is not eligible to accept this verification")
		case errors.Is(err, assurance.ErrHumanAcceptance), errors.Is(err, assurance.ErrEvidenceChanged):
			writeError(w, http.StatusConflict, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "acceptance failed")
		}
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"verification_id": verificationID, "decision": strings.ToLower(strings.TrimSpace(in.Decision))})
}

func (s *Server) createPreviewSession(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if s.ingressRoutes == nil || s.previewSessions == nil {
		writeError(w, http.StatusServiceUnavailable, "preview unavailable")
		return
	}
	route, err := s.ingressRoutes.ResolveIngressRoute(r.Context(), r.PathValue("endpointID"))
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	if !s.authorize(w, r, i, route.WorkspaceID, "project.read") {
		return
	}
	if route.Endpoint.Protocol != "http" {
		writeError(w, http.StatusUnprocessableEntity, "endpoint protocol is not previewable yet")
		return
	}
	previewURL, expiresAt, err := s.previewSessions.MintEndpointGrant(route.Endpoint.ID, i.PrincipalID, i.CredentialID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "preview session creation failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"endpoint_id": route.Endpoint.ID, "preview_url": previewURL, "expires_at": expiresAt})
}

func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) (Identity, bool) {
	i, err := s.auth.Authenticate(r)
	if err != nil {
		if errors.Is(err, ErrUnauthenticated) {
			writeError(w, http.StatusUnauthorized, "unauthenticated")
			return Identity{}, false
		}
		if errors.Is(err, ErrForbidden) {
			writeError(w, http.StatusForbidden, "forbidden")
			return Identity{}, false
		}
		writeError(w, http.StatusInternalServerError, "authentication failed")
		return Identity{}, false
	}
	return i, true
}
func (s *Server) authorize(w http.ResponseWriter, r *http.Request, i Identity, workspace, cap string) bool {
	if err := s.auth.AuthorizeWorkspace(r.Context(), i, workspace, cap); err != nil {
		if errors.Is(err, ErrForbidden) {
			writeError(w, http.StatusForbidden, "forbidden")
			return false
		}
		writeError(w, http.StatusInternalServerError, "authorization failed")
		return false
	}
	return true
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	workspaceID := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if !s.authorize(w, r, i, workspaceID, "project.read") {
		return
	}
	rows, err := s.projects.Projects(r.Context(), workspaceID)
	respondDomain(w, rows, err, http.StatusOK)
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var in struct {
		WorkspaceID        string          `json:"workspace_id"`
		Name               string          `json:"name"`
		Description        string          `json:"description"`
		ProjectPolicyJSON  json.RawMessage `json:"project_policy"`
		IndexingConfigJSON json.RawMessage `json:"indexing_config"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !s.authorize(w, r, i, in.WorkspaceID, "project.write") {
		return
	}
	p, err := s.projects.CreateProject(r.Context(), projectworkspace.CreateProjectCommand{WorkspaceID: in.WorkspaceID, Name: in.Name, Description: in.Description, CreatedBy: i.PrincipalID, ProjectPolicyJSON: in.ProjectPolicyJSON, IndexingConfigJSON: in.IndexingConfigJSON, RequestID: headerPtr(r, "X-Request-ID"), TraceID: headerPtr(r, "X-Trace-ID")})
	respondDomain(w, p, err, http.StatusCreated)
}
func (s *Server) updateProjectPolicy(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	p, err := s.projects.Project(r.Context(), r.PathValue("projectID"))
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	if !s.authorize(w, r, i, p.WorkspaceID, "project.write") {
		return
	}
	var in struct {
		ExpectedRevision int64           `json:"expected_revision"`
		ProjectPolicy    json.RawMessage `json:"project_policy"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.projects.UpdateProjectPolicy(r.Context(), projectworkspace.UpdateProjectPolicyCommand{ProjectID: p.ID, ExpectedRevision: in.ExpectedRevision, ProjectPolicyJSON: in.ProjectPolicy, ActorPrincipalID: i.PrincipalID, RequestID: headerPtr(r, "X-Request-ID"), TraceID: headerPtr(r, "X-Trace-ID")})
	respondDomain(w, out, err, http.StatusOK)
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	p, err := s.projects.Project(r.Context(), r.PathValue("projectID"))
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	if !s.authorize(w, r, i, p.WorkspaceID, "project.read") {
		return
	}
	writeJSON(w, http.StatusOK, p)
}
func (s *Server) createRuntime(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	p, err := s.projects.Project(r.Context(), r.PathValue("projectID"))
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	if !s.authorize(w, r, i, p.WorkspaceID, "project.write") {
		return
	}
	var in struct {
		NodeID                  *string                              `json:"node_id"`
		IsolationMode           projectworkspace.IsolationMode       `json:"isolation_mode"`
		DesiredState            projectworkspace.RuntimeDesiredState `json:"desired_state"`
		RuntimeSpecJSON         json.RawMessage                      `json:"runtime_spec"`
		ResourceLimitsJSON      json.RawMessage                      `json:"resource_limits"`
		EnvironmentBindingsJSON json.RawMessage                      `json:"environment_bindings"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	x, err := s.projects.CreateRuntime(r.Context(), projectworkspace.CreateRuntimeCommand{ProjectID: p.ID, NodeID: in.NodeID, IsolationMode: in.IsolationMode, DesiredState: in.DesiredState, RuntimeSpecJSON: in.RuntimeSpecJSON, ResourceLimitsJSON: in.ResourceLimitsJSON, EnvironmentBindingsJSON: in.EnvironmentBindingsJSON, CreatedBy: i.PrincipalID, RequestID: headerPtr(r, "X-Request-ID"), TraceID: headerPtr(r, "X-Trace-ID")})
	respondDomain(w, x, err, http.StatusCreated)
}
func (s *Server) getRuntimeByProject(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	p, err := s.projects.Project(r.Context(), r.PathValue("projectID"))
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	if !s.authorize(w, r, i, p.WorkspaceID, "project.read") {
		return
	}
	x, err := s.projects.RuntimeByProject(r.Context(), p.ID)
	respondDomain(w, x, err, http.StatusOK)
}
func (s *Server) setRuntimeDesired(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	x, err := s.projects.Runtime(r.Context(), r.PathValue("runtimeID"))
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	p, err := s.projects.Project(r.Context(), x.ProjectID)
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	if !s.authorize(w, r, i, p.WorkspaceID, "project.run") {
		return
	}
	var in struct {
		ExpectedRevision int64                                `json:"expected_revision"`
		DesiredState     projectworkspace.RuntimeDesiredState `json:"desired_state"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.projects.SetRuntimeDesiredState(r.Context(), projectworkspace.SetRuntimeDesiredStateCommand{RuntimeID: x.ID, ExpectedRevision: in.ExpectedRevision, DesiredState: in.DesiredState, ActorPrincipalID: i.PrincipalID, RequestID: headerPtr(r, "X-Request-ID"), TraceID: headerPtr(r, "X-Trace-ID")})
	respondDomain(w, out, err, http.StatusOK)
}
func (s *Server) updateRuntimePolicy(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	x, err := s.projects.Runtime(r.Context(), r.PathValue("runtimeID"))
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	p, err := s.projects.Project(r.Context(), x.ProjectID)
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	if !s.authorize(w, r, i, p.WorkspaceID, "project.write") {
		return
	}
	var in struct {
		ExpectedRevision     int64           `json:"expected_revision"`
		NetworkPolicyJSON    json.RawMessage `json:"network_policy"`
		FilesystemPolicyJSON json.RawMessage `json:"filesystem_policy"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.projects.UpdateRuntimePolicy(r.Context(), projectworkspace.UpdateRuntimePolicyCommand{RuntimeID: x.ID, ExpectedRevision: in.ExpectedRevision, NetworkPolicyJSON: in.NetworkPolicyJSON, FilesystemPolicyJSON: in.FilesystemPolicyJSON, ActorPrincipalID: i.PrincipalID, RequestID: headerPtr(r, "X-Request-ID"), TraceID: headerPtr(r, "X-Trace-ID")})
	respondDomain(w, out, err, http.StatusOK)
}

func (s *Server) declareApplication(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	x, err := s.projects.Runtime(r.Context(), r.PathValue("runtimeID"))
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	p, err := s.projects.Project(r.Context(), x.ProjectID)
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	if !s.authorize(w, r, i, p.WorkspaceID, "project.write") {
		return
	}
	var in struct {
		Name                    string                           `json:"name"`
		SourceKind              projectworkspace.AppSourceKind   `json:"source_kind"`
		SourceRef               string                           `json:"source_ref"`
		VersionRef              *string                          `json:"version_ref"`
		InstallSpecJSON         json.RawMessage                  `json:"install_spec"`
		RuntimeSpecJSON         json.RawMessage                  `json:"runtime_spec"`
		EnvironmentBindingsJSON json.RawMessage                  `json:"environment_bindings"`
		DesiredState            projectworkspace.AppDesiredState `json:"desired_state"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.projects.DeclareApplication(r.Context(), projectworkspace.DeclareApplicationCommand{RuntimeID: x.ID, Name: in.Name, SourceKind: in.SourceKind, SourceRef: in.SourceRef, VersionRef: in.VersionRef, InstallSpecJSON: in.InstallSpecJSON, RuntimeSpecJSON: in.RuntimeSpecJSON, EnvironmentBindingsJSON: in.EnvironmentBindingsJSON, DesiredState: in.DesiredState, CreatedBy: i.PrincipalID, RequestID: headerPtr(r, "X-Request-ID"), TraceID: headerPtr(r, "X-Trace-ID")})
	respondDomain(w, out, err, http.StatusCreated)
}
func (s *Server) listApplications(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	x, err := s.projects.Runtime(r.Context(), r.PathValue("runtimeID"))
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	p, err := s.projects.Project(r.Context(), x.ProjectID)
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	if !s.authorize(w, r, i, p.WorkspaceID, "project.read") {
		return
	}
	out, err := s.projects.ListApplications(r.Context(), x.ID)
	respondDomain(w, out, err, http.StatusOK)
}

func (s *Server) listEndpoints(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	x, err := s.projects.Runtime(r.Context(), r.PathValue("runtimeID"))
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	p, err := s.projects.Project(r.Context(), x.ProjectID)
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	if !s.authorize(w, r, i, p.WorkspaceID, "project.read") {
		return
	}
	out, err := s.projects.ListEndpoints(r.Context(), x.ID)
	respondDomain(w, out, err, http.StatusOK)
}

func (s *Server) declareEndpoint(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	x, err := s.projects.Runtime(r.Context(), r.PathValue("runtimeID"))
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	p, err := s.projects.Project(r.Context(), x.ProjectID)
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	if !s.authorize(w, r, i, p.WorkspaceID, "project.write") {
		return
	}
	var in struct {
		ApplicationID *string                           `json:"application_id"`
		Name          string                            `json:"name"`
		Protocol      string                            `json:"protocol"`
		InternalPort  int                               `json:"internal_port"`
		Exposure      projectworkspace.EndpointExposure `json:"exposure"`
		PathPrefix    *string                           `json:"path_prefix"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.projects.DeclareEndpoint(r.Context(), projectworkspace.DeclareEndpointCommand{RuntimeID: x.ID, ApplicationID: in.ApplicationID, Name: in.Name, Protocol: in.Protocol, InternalPort: in.InternalPort, Exposure: in.Exposure, PathPrefix: in.PathPrefix, CreatedBy: i.PrincipalID, RequestID: headerPtr(r, "X-Request-ID"), TraceID: headerPtr(r, "X-Trace-ID")})
	respondDomain(w, out, err, http.StatusCreated)
}
func (s *Server) proposeChange(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	p, err := s.projects.Project(r.Context(), r.PathValue("projectID"))
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	if !s.authorize(w, r, i, p.WorkspaceID, "project.write") {
		return
	}
	var in struct {
		RuntimeID       *string                       `json:"runtime_id"`
		TaskID          *string                       `json:"task_id"`
		Kind            projectworkspace.ProposalKind `json:"kind"`
		Summary         string                        `json:"summary"`
		BaseRevision    *int64                        `json:"base_revision"`
		PatchArtifactID *string                       `json:"patch_artifact_id"`
		MetadataJSON    json.RawMessage               `json:"metadata"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.projects.ProposeChange(r.Context(), projectworkspace.ProposeChangeCommand{ProjectID: p.ID, RuntimeID: in.RuntimeID, TaskID: in.TaskID, Kind: in.Kind, Summary: in.Summary, BaseRevision: in.BaseRevision, PatchArtifactID: in.PatchArtifactID, MetadataJSON: in.MetadataJSON, ProposedBy: i.PrincipalID, RequestID: headerPtr(r, "X-Request-ID"), TraceID: headerPtr(r, "X-Trace-ID")})
	respondDomain(w, out, err, http.StatusCreated)
}
func (s *Server) listChanges(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	p, err := s.projects.Project(r.Context(), r.PathValue("projectID"))
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	if !s.authorize(w, r, i, p.WorkspaceID, "project.read") {
		return
	}
	out, err := s.projects.ListChanges(r.Context(), p.ID)
	respondDomain(w, out, err, http.StatusOK)
}

func (s *Server) reviewChange(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	c, err := s.projects.Change(r.Context(), r.PathValue("proposalID"))
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	p, err := s.projects.Project(r.Context(), c.ProjectID)
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	if !s.authorize(w, r, i, p.WorkspaceID, "project.review") {
		return
	}
	var in struct {
		ExpectedRevision int64                           `json:"expected_revision"`
		Decision         projectworkspace.ProposalStatus `json:"decision"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.projects.ReviewChange(r.Context(), projectworkspace.ReviewChangeCommand{ProposalID: c.ID, ExpectedRevision: in.ExpectedRevision, Decision: in.Decision, ReviewedBy: i.PrincipalID, RequestID: headerPtr(r, "X-Request-ID"), TraceID: headerPtr(r, "X-Trace-ID")})
	respondDomain(w, out, err, http.StatusOK)
}
func (s *Server) bindRoutine(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	p, err := s.projects.Project(r.Context(), r.PathValue("projectID"))
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	if !s.authorize(w, r, i, p.WorkspaceID, "project.write") {
		return
	}
	var in struct {
		RuntimeID      string                      `json:"runtime_id"`
		ApplicationID  *string                     `json:"application_id"`
		RoutineID      string                      `json:"routine_id"`
		ActionKind     projectworkspace.ActionKind `json:"action_kind"`
		ActionRef      string                      `json:"action_ref"`
		ActionSpecJSON json.RawMessage             `json:"action_spec"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.projects.BindRoutine(r.Context(), projectworkspace.BindRoutineCommand{ProjectID: p.ID, RuntimeID: in.RuntimeID, ApplicationID: in.ApplicationID, RoutineID: in.RoutineID, ActionKind: in.ActionKind, ActionRef: in.ActionRef, ActionSpecJSON: in.ActionSpecJSON, CreatedBy: i.PrincipalID, RequestID: headerPtr(r, "X-Request-ID"), TraceID: headerPtr(r, "X-Trace-ID")})
	respondDomain(w, out, err, http.StatusCreated)
}

func (s *Server) listRoutineBindings(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	p, err := s.projects.Project(r.Context(), r.PathValue("projectID"))
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	if !s.authorize(w, r, i, p.WorkspaceID, "project.read") {
		return
	}
	out, err := s.projects.ListRoutineBindings(r.Context(), p.ID)
	respondDomain(w, out, err, http.StatusOK)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request")
		return false
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "request must contain one JSON value")
		return false
	}
	return true
}
func headerPtr(r *http.Request, name string) *string {
	v := strings.TrimSpace(r.Header.Get(name))
	if v == "" {
		return nil
	}
	return &v
}
func respondDomain(w http.ResponseWriter, v any, err error, success int) {
	if err == nil {
		if success == 0 {
			success = http.StatusOK
		}
		writeJSON(w, success, v)
		return
	}
	switch {
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, projectworkspace.ErrInvalidCommand), errors.Is(err, projectworkspace.ErrInvalidTransition):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, projectworkspace.ErrRevisionConflict):
		writeError(w, http.StatusConflict, "revision conflict")
	case errors.Is(err, projectworkspace.ErrPrincipalIneligible), errors.Is(err, projectworkspace.ErrCrossWorkspace), errors.Is(err, projectworkspace.ErrWorkspaceInactive):
		writeError(w, http.StatusForbidden, "forbidden")
	default:
		writeError(w, http.StatusInternalServerError, "request failed")
	}
}
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'; frame-src 'none'; object-src 'none'; base-uri 'none'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) setupStatus(w http.ResponseWriter, r *http.Request) {
	if s.webAuth == nil {
		writeError(w, http.StatusServiceUnavailable, "web authentication unavailable")
		return
	}
	st, err := s.webAuth.SetupStatus(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "setup status unavailable")
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) bootstrapAdmin(w http.ResponseWriter, r *http.Request) {
	if s.webAuth == nil {
		writeError(w, http.StatusServiceUnavailable, "web authentication unavailable")
		return
	}
	var in webauth.BootstrapAdminCommand
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.webAuth.BootstrapAdmin(r.Context(), in)
	if err != nil {
		switch {
		case errors.Is(err, webauth.ErrSetupComplete):
			writeError(w, http.StatusConflict, "first-run setup is already complete")
		default:
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	s.setSessionCookies(w, out.SessionToken, out.CSRFToken, out.ExpiresAt)
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if s.webAuth == nil {
		writeError(w, http.StatusServiceUnavailable, "web authentication unavailable")
		return
	}
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.webAuth.Login(r.Context(), in.Username, in.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	s.setSessionCookies(w, out.SessionToken, out.CSRFToken, out.ExpiresAt)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	_, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if c, err := r.Cookie(WebSessionCookie); err == nil && s.webAuth != nil {
		_ = s.webAuth.Logout(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: WebSessionCookie, Value: "", Path: "/", HttpOnly: true, Secure: s.secureCookies, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	http.SetCookie(w, &http.Cookie{Name: WebCSRFCookie, Value: "", Path: "/", HttpOnly: false, Secure: s.secureCookies, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	writeJSON(w, http.StatusOK, map[string]any{"status": "logged_out"})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"principal_id": i.PrincipalID, "principal_type": i.PrincipalType, "auth_method": i.AuthMethod, "workspaces": json.RawMessage(i.WorkspaceScope), "capabilities": json.RawMessage(i.CapabilityScope)})
}

func (s *Server) setSessionCookies(w http.ResponseWriter, token, csrf string, expiresAt int64) {
	exp := time.UnixMilli(expiresAt)
	http.SetCookie(w, &http.Cookie{Name: WebSessionCookie, Value: token, Path: "/", HttpOnly: true, Secure: s.secureCookies, SameSite: http.SameSiteStrictMode, Expires: exp})
	http.SetCookie(w, &http.Cookie{Name: WebCSRFCookie, Value: csrf, Path: "/", HttpOnly: false, Secure: s.secureCookies, SameSite: http.SameSiteStrictMode, Expires: exp})
}

func (s *Server) listProviderCredentials(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	workspace := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	if workspace == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if !s.authorize(w, r, i, workspace, "vault.read") {
		return
	}
	if s.vault == nil {
		writeError(w, http.StatusServiceUnavailable, "vault unavailable")
		return
	}
	rows, err := s.vault.ListWorkspace(r.Context(), workspace)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "vault read failed")
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) createProviderCredential(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var in struct {
		WorkspaceID      string                `json:"workspace_id"`
		Scope            vault.CredentialScope `json:"scope"`
		UpstreamProvider string                `json:"upstream_provider"`
		Kind             vault.CredentialKind  `json:"kind"`
		Value            string                `json:"value"`
		DisplayLabel     string                `json:"display_label"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !s.authorize(w, r, i, in.WorkspaceID, "vault.write") {
		return
	}
	if s.vault == nil {
		writeError(w, http.StatusServiceUnavailable, "vault unavailable")
		return
	}
	ws := in.WorkspaceID
	rec, err := s.vault.CreateProviderCredential(r.Context(), vault.ProviderCredentialCommand{WorkspaceID: &ws, Scope: in.Scope, UpstreamProvider: in.UpstreamProvider, Kind: in.Kind, Value: []byte(in.Value), DisplayLabel: in.DisplayLabel, CreatedBy: i.PrincipalID})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Never echo the submitted secret value.
	writeJSON(w, http.StatusCreated, rec)
}

func taskResponse(t task.Task) map[string]any {
	return map[string]any{
		"id": t.ID, "workspace_id": t.WorkspaceID, "project_id": t.ProjectID, "objective": t.Objective,
		"state": t.State, "scheduling_class": t.SchedulingClass, "priority": t.Priority, "revision": t.Revision,
		"ready_at": t.ReadyAt, "cancel_requested_at": t.CancelRequestedAt, "created_at": t.CreatedAt, "updated_at": t.UpdatedAt,
	}
}

func (s *Server) listRoutines(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	workspaceID := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if !s.authorize(w, r, i, workspaceID, "task.read") {
		return
	}
	if s.routines == nil {
		writeError(w, http.StatusServiceUnavailable, "routine service unavailable")
		return
	}
	rows, err := s.routines.List(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) createRoutine(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var in struct {
		WorkspaceID string          `json:"workspace_id"`
		Name        string          `json:"name"`
		Timezone    string          `json:"timezone"`
		Trigger     routine.Trigger `json:"trigger"`
		Policy      routine.Policy  `json:"policy"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.WorkspaceID) == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if !s.authorize(w, r, i, in.WorkspaceID, "task.write") {
		return
	}
	if s.routines == nil {
		writeError(w, http.StatusServiceUnavailable, "routine service unavailable")
		return
	}
	if strings.TrimSpace(in.Timezone) == "" {
		in.Timezone = "UTC"
	}
	out, err := s.routines.Create(r.Context(), routine.CreateCommand{
		WorkspaceID: in.WorkspaceID,
		Name:        in.Name,
		Timezone:    in.Timezone,
		CreatedBy:   i.PrincipalID,
		Trigger:     in.Trigger,
		Policy:      in.Policy,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if s.tasks == nil {
		writeError(w, http.StatusServiceUnavailable, "task service unavailable")
		return
	}
	workspaceID := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if !s.authorize(w, r, i, workspaceID, "task.read") {
		return
	}
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	rows, err := s.tasks.List(r.Context(), workspaceID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, t := range rows {
		out = append(out, taskResponse(t))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if s.tasks == nil {
		writeError(w, http.StatusServiceUnavailable, "task service unavailable")
		return
	}
	var in struct {
		WorkspaceID     string               `json:"workspace_id"`
		ProjectID       *string              `json:"project_id"`
		Objective       string               `json:"objective"`
		SchedulingClass task.SchedulingClass `json:"scheduling_class"`
		Priority        int                  `json:"priority"`
		Completion      json.RawMessage      `json:"completion"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !s.authorize(w, r, i, in.WorkspaceID, "task.write") {
		return
	}
	if in.SchedulingClass == "" {
		in.SchedulingClass = task.ClassUserInteractive
	}
	if len(in.Completion) == 0 {
		in.Completion = json.RawMessage(`{"type":"operator_review"}`)
	}
	actor := i.PrincipalID
	out, err := s.tasks.Create(r.Context(), task.CreateCommand{WorkspaceID: in.WorkspaceID, ProjectID: in.ProjectID, Objective: in.Objective, SchedulingClass: in.SchedulingClass, Priority: in.Priority, Completion: in.Completion, ActorPrincipalID: &actor, RequestID: headerPtr(r, "X-Request-ID"), TraceID: headerPtr(r, "X-Trace-ID")})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, taskResponse(out))
}

func (s *Server) listChatCommands(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authenticate(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, chatcommands.Commands())
}

func (s *Server) runChatCommand(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authenticate(w, r); !ok {
		return
	}
	if s.chatCommands == nil {
		writeError(w, http.StatusServiceUnavailable, "chat command service unavailable")
		return
	}
	var in struct {
		Input string `json:"input"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.chatCommands.Handle(r.Context(), strings.TrimSpace(r.PathValue("sessionID")), in.Input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getLocalAISettings(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	workspace := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	if workspace != "" && !s.authorize(w, r, i, workspace, "model.read") {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"model_pool_path": s.modelPoolPath})
}

func (s *Server) setLocalAISettings(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if s.localAI == nil {
		writeError(w, http.StatusServiceUnavailable, "local AI unavailable")
		return
	}
	var in struct {
		WorkspaceID   string `json:"workspace_id"`
		ModelPoolPath string `json:"model_pool_path"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !s.authorize(w, r, i, in.WorkspaceID, "model.write") {
		return
	}
	path := filepath.Clean(strings.TrimSpace(in.ModelPoolPath))
	if path == "." || !filepath.IsAbs(path) {
		writeError(w, http.StatusBadRequest, "model_pool_path must be an absolute path")
		return
	}
	if err := s.localAI.ConfigureModelPool(path); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.configPath != "" {
		cfg, err := config.Load(s.configPath)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not reload OnePane configuration")
			return
		}
		cfg.LocalAI.ModelPoolPath = path
		raw, err := yaml.Marshal(cfg)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not serialize OnePane configuration")
			return
		}
		if err := os.WriteFile(s.configPath, raw, 0o600); err != nil {
			writeError(w, http.StatusInternalServerError, "could not persist OnePane configuration")
			return
		}
	}
	s.modelPoolPath = path
	writeJSON(w, http.StatusOK, map[string]any{"model_pool_path": path, "restart_required": false})
}

func (s *Server) listLocalAICatalog(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authenticate(w, r); !ok { return }
	models := localai.BuiltinCatalog()
	catalogVersion := ""
	installable := map[string]map[string]bool{}
	if s.localAI != nil && s.localAI.Catalog() != nil {
		if rec, _, err := s.localAI.Catalog().Active(r.Context()); err == nil { catalogVersion = rec.CatalogVersion }
		if x, err := s.localAI.Catalog().Installability(r.Context()); err == nil { installable = x }
		if signed, err := s.localAI.Catalog().ModelSpecifications(r.Context()); err == nil && len(signed) > 0 {
			seen := map[string]bool{}
			for _, m := range models { seen[m.ModelRef] = true }
			for _, m := range signed {
				if !seen[m.ModelRef] { models = append(models, m); seen[m.ModelRef] = true }
			}
		}
	}
	rows := make([]map[string]any, 0, len(models))
	for _, m := range models {
		raw, _ := json.Marshal(m)
		row := map[string]any{}
		_ = json.Unmarshal(raw, &row)
		qs := []string{}
		for q, ok := range installable[m.ModelRef] { if ok { qs = append(qs, q) } }
		sort.Strings(qs)
		row["installable"] = len(qs) > 0
		row["installable_quantizations"] = qs
		row["catalog_version"] = catalogVersion
		if len(qs) > 0 {
			row["source"] = "trusted"
		} else {
			row["source"] = "advisory"
			row["install_reason"] = "No verified artifact is available in the active trusted catalogue."
		}
		rows = append(rows, row)
	}
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) listManagedLocalDeployments(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	workspaceID := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if !s.authorize(w, r, i, workspaceID, "model.read") {
		return
	}
	if s.localAI == nil {
		writeError(w, http.StatusServiceUnavailable, "local AI unavailable")
		return
	}
	out, err := s.localAI.ManagedDeployments(r.Context(), workspaceID)
	respondDomain(w, out, err, http.StatusOK)
}

func (s *Server) registerColibriFolder(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var in localai.RegisterColibriCommand
	if !decodeJSON(w, r, &in) {
		return
	}
	if !s.authorize(w, r, i, in.WorkspaceID, "model.write") {
		return
	}
	if s.localAI == nil {
		writeError(w, http.StatusServiceUnavailable, "local AI unavailable")
		return
	}
	in.ActorPrincipalID = i.PrincipalID
	out, err := s.localAI.RegisterColibriFolder(r.Context(), in)
	respondDomain(w, out, err, http.StatusCreated)
}

func (s *Server) serveWebUI(w http.ResponseWriter, r *http.Request) {
	if s.webUI == nil {
		http.NotFound(w, r)
		return
	}
	s.webUI.ServeHTTP(w, r)
}

func (s *Server) listSchedulerCandidates(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	workspaceID := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if !s.authorize(w, r, i, workspaceID, "model.read") {
		return
	}
	if s.scheduler == nil {
		writeError(w, http.StatusServiceUnavailable, "scheduler unavailable")
		return
	}
	capabilityID := strings.TrimSpace(r.URL.Query().Get("capability_id"))
	if capabilityID == "" {
		capabilityID = "inference.general"
	}
	roleName := strings.TrimSpace(r.URL.Query().Get("role_name"))
	if roleName == "" {
		roleName = "general"
	}
	rows, err := s.scheduler.Candidates(r.Context(), workspaceID, capabilityID, roleName)
	if err != nil {
		respondDomain(w, nil, err, http.StatusInternalServerError)
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, c := range rows {
		out = append(out, map[string]any{
			"id": c.ID, "kind": c.Kind, "display_name": c.DisplayName, "provider": c.Provider,
			"status": c.Status, "local": c.Local, "cost_class": c.CostClass,
			"qualification": c.Qualification, "schedulable": c.Schedulable,
			"role_names": c.RoleNames, "capability_ids": c.CapabilityIDs, "context_max": c.ContextMax,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) providerPresets(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authenticate(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, provideronboarding.Builtins())
}

func (s *Server) listProviders(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	workspaceID := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if !s.authorize(w, r, i, workspaceID, "provider.read") {
		return
	}
	if s.providers == nil {
		writeError(w, http.StatusServiceUnavailable, "provider onboarding unavailable")
		return
	}
	rows, err := s.providers.Connections(r.Context(), &workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, p := range rows {
		out = append(out, map[string]any{
			"id": p.ID, "workspace_id": p.WorkspaceID, "provider": p.Provider,
			"display_name": p.DisplayName, "auth_type": p.AuthType, "secret_ref": p.SecretRef,
			"status": p.Status, "connection": json.RawMessage(p.ConnectionJSON), "retry_after": p.RetryAfter,
			"revision": p.Revision, "created_at": p.CreatedAt, "updated_at": p.UpdatedAt,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) revokeProvider(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if s.providers == nil {
		writeError(w, http.StatusServiceUnavailable, "provider onboarding unavailable")
		return
	}
	id := strings.TrimSpace(r.PathValue("providerID"))
	p, err := s.providers.Connection(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "provider connection not found")
		return
	}
	if p.WorkspaceID == nil || !s.authorize(w, r, i, *p.WorkspaceID, "provider.write") {
		return
	}
	actor := i.PrincipalID
	out, err := s.providers.RevokeProvider(r.Context(), id, &actor)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) probeProvider(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var in struct {
		WorkspaceID string                      `json:"workspace_id"`
		PresetID    provideronboarding.PresetID `json:"preset_id"`
		BaseURL     string                      `json:"base_url"`
		SecretRef   *string                     `json:"secret_ref"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !s.authorize(w, r, i, in.WorkspaceID, "provider.read") {
		return
	}
	if s.providers == nil {
		writeError(w, http.StatusServiceUnavailable, "provider onboarding unavailable")
		return
	}
	out, err := s.providers.ProbeProvider(r.Context(), in.PresetID, in.BaseURL, in.SecretRef)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) addProvider(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var in struct {
		WorkspaceID          string                      `json:"workspace_id"`
		PresetID             provideronboarding.PresetID `json:"preset_id"`
		BaseURL              string                      `json:"base_url"`
		SecretRef            *string                     `json:"secret_ref"`
		ModelRef             string                      `json:"model_ref"`
		ActivateWithoutProbe bool                        `json:"activate_without_probe"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !s.authorize(w, r, i, in.WorkspaceID, "provider.write") {
		return
	}
	if s.providers == nil {
		writeError(w, http.StatusServiceUnavailable, "provider onboarding unavailable")
		return
	}
	ws, actor := in.WorkspaceID, i.PrincipalID
	out, err := s.providers.AddProvider(r.Context(), provideronboarding.ProviderCommand{WorkspaceID: &ws, PresetID: in.PresetID, BaseURL: in.BaseURL, SecretRef: in.SecretRef, ModelRef: in.ModelRef, ActivateWithoutProbe: in.ActivateWithoutProbe, ActorPrincipalID: &actor, RequestID: headerPtr(r, "X-Request-ID"), TraceID: headerPtr(r, "X-Trace-ID")})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) agentRuntimePresets(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authenticate(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, agentruntime.BuiltinPresets())
}

func (s *Server) addAgentRuntime(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var in struct {
		WorkspaceID string  `json:"workspace_id"`
		NodeID      *string `json:"node_id"`
		PresetID    string  `json:"preset_id"`
		DisplayName string  `json:"display_name"`
		BaseURL     string  `json:"base_url"`
		SecretRef   *string `json:"secret_ref"`
		AuthType    string  `json:"auth_type"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !s.authorize(w, r, i, in.WorkspaceID, "agent_runtime.write") {
		return
	}
	if s.agentRuntimes == nil {
		writeError(w, http.StatusServiceUnavailable, "agent runtime service unavailable")
		return
	}
	ws, actor := in.WorkspaceID, i.PrincipalID
	out, err := s.agentRuntimes.RegisterPreset(r.Context(), agentruntime.RegisterPresetCommand{WorkspaceID: &ws, NodeID: in.NodeID, PresetID: in.PresetID, DisplayName: in.DisplayName, BaseURL: in.BaseURL, SecretRef: in.SecretRef, AuthType: in.AuthType, ActorPrincipalID: &actor, RequestID: headerPtr(r, "X-Request-ID"), TraceID: headerPtr(r, "X-Trace-ID")})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) setAgentRuntimeStatus(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if s.agentRuntimes == nil {
		writeError(w, http.StatusServiceUnavailable, "agent runtime service unavailable")
		return
	}
	id := strings.TrimSpace(r.PathValue("connectionID"))
	c, err := s.agentRuntimes.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "agent runtime not found")
		return
	}
	if c.WorkspaceID == nil {
		writeError(w, http.StatusBadRequest, "global runtime status changes are not exposed through this API")
		return
	}
	if !s.authorize(w, r, i, *c.WorkspaceID, "agent_runtime.write") {
		return
	}
	var in struct {
		ExpectedRevision int64               `json:"expected_revision"`
		Status           agentruntime.Status `json:"status"`
		Reason           string              `json:"reason"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	actor := i.PrincipalID
	out, err := s.agentRuntimes.SetStatus(r.Context(), agentruntime.SetStatusCommand{ConnectionID: id, ExpectedRevision: in.ExpectedRevision, Status: in.Status, ActorPrincipalID: &actor, RequestID: headerPtr(r, "X-Request-ID"), TraceID: headerPtr(r, "X-Trace-ID"), Reason: in.Reason})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) pluginPresets(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authenticate(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, connectors.Builtins())
}

func (s *Server) createPluginCredential(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var in struct {
		WorkspaceID  string               `json:"workspace_id"`
		PluginID     string               `json:"plugin_id"`
		Kind         vault.CredentialKind `json:"kind"`
		Value        string               `json:"value"`
		DisplayLabel string               `json:"display_label"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !s.authorize(w, r, i, in.WorkspaceID, "vault.write") {
		return
	}
	if s.vault == nil {
		writeError(w, http.StatusServiceUnavailable, "vault unavailable")
		return
	}
	if _, ok := connectors.ByID(in.PluginID); !ok {
		writeError(w, http.StatusBadRequest, "unknown plugin_id")
		return
	}
	ws := in.WorkspaceID
	rec, err := s.vault.CreatePluginCredential(r.Context(), vault.PluginCredentialCommand{WorkspaceID: &ws, PluginID: in.PluginID, Kind: in.Kind, Value: []byte(in.Value), DisplayLabel: in.DisplayLabel, CreatedBy: i.PrincipalID})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, rec)
}

func (s *Server) createAgentRuntimeCredential(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var in struct {
		WorkspaceID  string               `json:"workspace_id"`
		RuntimeKind  string               `json:"runtime_kind"`
		Kind         vault.CredentialKind `json:"kind"`
		Value        string               `json:"value"`
		DisplayLabel string               `json:"display_label"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !s.authorize(w, r, i, in.WorkspaceID, "vault.write") {
		return
	}
	if s.vault == nil {
		writeError(w, http.StatusServiceUnavailable, "vault unavailable")
		return
	}
	if _, ok := agentruntime.PresetByID(in.RuntimeKind); !ok {
		writeError(w, http.StatusBadRequest, "unknown runtime_kind")
		return
	}
	ws := in.WorkspaceID
	rec, err := s.vault.CreateAgentRuntimeCredential(r.Context(), vault.AgentRuntimeCredentialCommand{WorkspaceID: &ws, RuntimeKind: in.RuntimeKind, Kind: in.Kind, Value: []byte(in.Value), DisplayLabel: in.DisplayLabel, CreatedBy: i.PrincipalID})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, rec)
}

func (s *Server) probeOmniRoute(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var in struct {
		WorkspaceID string  `json:"workspace_id"`
		BaseURL     string  `json:"base_url"`
		SecretRef   *string `json:"secret_ref"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !s.authorize(w, r, i, in.WorkspaceID, "provider.read") {
		return
	}
	if s.providers == nil {
		writeError(w, http.StatusServiceUnavailable, "provider onboarding unavailable")
		return
	}
	out, err := s.providers.ProbeOmniRoute(r.Context(), in.BaseURL, in.SecretRef)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) addOmniRoute(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var in struct {
		WorkspaceID           string  `json:"workspace_id"`
		BaseURL               string  `json:"base_url"`
		SecretRef             *string `json:"secret_ref"`
		RequireStrictZeroCost bool    `json:"require_strict_zero_cost"`
		DefaultModel          string  `json:"default_model"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !s.authorize(w, r, i, in.WorkspaceID, "provider.write") {
		return
	}
	if s.providers == nil {
		writeError(w, http.StatusServiceUnavailable, "provider onboarding unavailable")
		return
	}
	ws := in.WorkspaceID
	actor := i.PrincipalID
	out, err := s.providers.AddOmniRoute(r.Context(), provideronboarding.OmniRouteCommand{WorkspaceID: &ws, BaseURL: in.BaseURL, SecretRef: in.SecretRef, RequireStrictZeroCost: in.RequireStrictZeroCost, DefaultModel: in.DefaultModel, ActorPrincipalID: &actor, RequestID: headerPtr(r, "X-Request-ID"), TraceID: headerPtr(r, "X-Trace-ID")})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) detectLocalAI(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var in struct {
		WorkspaceID string `json:"workspace_id"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !s.authorize(w, r, i, in.WorkspaceID, "model.write") {
		return
	}
	if s.localAI == nil || strings.TrimSpace(s.localNodeID) == "" {
		writeError(w, http.StatusServiceUnavailable, "local AI unavailable")
		return
	}
	out, err := s.localAI.DetectAndPersist(r.Context(), s.localNodeID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) recommendLocalAI(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var in struct {
		WorkspaceID         string                `json:"workspace_id"`
		ProfileID           string                `json:"profile_id"`
		UseCase             localai.UseCase       `json:"use_case"`
		ContextTokens       int64                 `json:"context_tokens"`
		Limit               int                   `json:"limit"`
		MinimumFit          localai.FitLevel      `json:"minimum_fit"`
		StorageHeadroomPct  int                   `json:"storage_headroom_pct"`
		PreferGPU           bool                  `json:"prefer_gpu"`
		PlacementPreference localai.PlacementMode `json:"placement_preference,omitempty"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !s.authorize(w, r, i, in.WorkspaceID, "model.read") {
		return
	}
	if s.localAI == nil {
		writeError(w, http.StatusServiceUnavailable, "local AI unavailable")
		return
	}
	if in.ContextTokens == 0 {
		in.ContextTokens = 8192
	}
	if in.Limit == 0 {
		in.Limit = 5
	}
	if in.StorageHeadroomPct == 0 {
		in.StorageHeadroomPct = 20
	}
	out, err := s.localAI.Recommendations(r.Context(), in.ProfileID, localai.RecommendRequest{UseCase: in.UseCase, ContextTokens: in.ContextTokens, Limit: in.Limit, MinimumFit: in.MinimumFit, StorageHeadroomPct: in.StorageHeadroomPct, PreferGPU: in.PreferGPU, PlacementPreference: in.PlacementPreference})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) importLocalAICatalog(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var in struct {
		WorkspaceID string          `json:"workspace_id"`
		SourceURL   *string         `json:"source_url"`
		Envelope    json.RawMessage `json:"envelope"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !s.authorize(w, r, i, in.WorkspaceID, "model.write") {
		return
	}
	if s.localAI == nil || s.localAI.Catalog() == nil {
		writeError(w, http.StatusServiceUnavailable, "local AI catalog unavailable")
		return
	}
	if len(in.Envelope) == 0 {
		writeError(w, http.StatusBadRequest, "signed catalog envelope required")
		return
	}
	actor := i.PrincipalID
	out, err := s.localAI.Catalog().ImportSigned(r.Context(), in.Envelope, in.SourceURL, &actor)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) queueLocalAIInstall(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var in struct {
		WorkspaceID         string                `json:"workspace_id"`
		ProfileID           string                `json:"profile_id"`
		RoleName            string                `json:"role_name"`
		UseCase             localai.UseCase       `json:"use_case"`
		ContextTokens       int64                 `json:"context_tokens"`
		ModelRef            string                `json:"model_ref"`
		Quantization        string                `json:"quantization"`
		PreferGPU           bool                  `json:"prefer_gpu"`
		PlacementPreference localai.PlacementMode `json:"placement_preference,omitempty"`
		ComputePreference   string                `json:"compute_preference,omitempty"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !s.authorize(w, r, i, in.WorkspaceID, "model.write") {
		return
	}
	if s.localAI == nil {
		writeError(w, http.StatusServiceUnavailable, "local AI unavailable")
		return
	}
	out, err := s.localAI.QueueOneClickInstall(r.Context(), localai.OneClickInstallRequest{WorkspaceID: in.WorkspaceID, HardwareProfileID: in.ProfileID, RoleName: in.RoleName, UseCase: in.UseCase, ContextTokens: in.ContextTokens, ModelRef: in.ModelRef, Quantization: in.Quantization, PreferGPU: in.PreferGPU, PlacementPreference: in.PlacementPreference, ComputePreference: in.ComputePreference, RequestedBy: i.PrincipalID})
	if err != nil {
		msg := err.Error()
		low := strings.ToLower(msg)
		if strings.Contains(low, "catalog") || strings.Contains(low, "no rows in result set") || strings.Contains(low, "verified artifact") {
			msg = "Trusted Local AI catalogue is unavailable or does not contain a verified artifact for this model/quantization."
		}
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	writeJSON(w, http.StatusAccepted, out)
}

func (s *Server) getLocalAIInstallJob(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if s.localAI == nil {
		writeError(w, http.StatusServiceUnavailable, "local AI unavailable")
		return
	}
	out, err := s.localAI.InstallJob(r.Context(), r.PathValue("jobID"))
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	if out.WorkspaceID == nil || !s.authorize(w, r, i, *out.WorkspaceID, "model.read") {
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) authorizeManagedDeployment(w http.ResponseWriter, r *http.Request, identity Identity, deploymentID, permission string) bool {
	if s.localAI == nil {
		writeError(w, http.StatusServiceUnavailable, "local AI unavailable")
		return false
	}
	ws, err := s.localAI.ManagedDeploymentWorkspace(r.Context(), deploymentID)
	if err != nil {
		respondDomain(w, nil, err, 0)
		return false
	}
	if ws == nil {
		if s.federation == nil || s.federation.CanOperate(r.Context(), identity.PrincipalID) != nil {
			writeError(w, http.StatusForbidden, "node-scoped model administration requires Admin role")
			return false
		}
		return true
	}
	return s.authorize(w, r, identity, *ws, permission)
}

func (s *Server) getModelSpecSheet(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	dep := strings.TrimSpace(r.PathValue("deploymentID"))
	if !s.authorizeManagedDeployment(w, r, i, dep, "model.read") {
		return
	}
	x, err := s.localAI.SpecSheet(r.Context(), dep)
	respondDomain(w, x, err, http.StatusOK)
}
func (s *Server) startModelTestbed(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	dep := strings.TrimSpace(r.PathValue("deploymentID"))
	if !s.authorizeManagedDeployment(w, r, i, dep, "model.write") {
		return
	}
	var in struct {
		Notes string `json:"notes"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	actor := i.PrincipalID
	x, err := s.localAI.StartTestbed(r.Context(), dep, &actor, in.Notes)
	respondDomain(w, x, err, http.StatusCreated)
}

func (s *Server) getModelTestbed(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	sid := strings.TrimSpace(r.PathValue("sessionID"))
	sess, err := s.localAI.TestbedSession(r.Context(), sid)
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	if !s.authorizeManagedDeployment(w, r, i, sess.DeploymentID, "model.read") {
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) listModelTestbedTurns(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	sid := strings.TrimSpace(r.PathValue("sessionID"))
	sess, err := s.localAI.TestbedSession(r.Context(), sid)
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	if !s.authorizeManagedDeployment(w, r, i, sess.DeploymentID, "model.read") {
		return
	}
	turns, err := s.localAI.ListTestbedTurns(r.Context(), sid)
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"turns": turns})
}

func (s *Server) runModelTestbedTurn(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	sid := strings.TrimSpace(r.PathValue("sessionID"))
	sess, err := s.localAI.TestbedSession(r.Context(), sid)
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	if !s.authorizeManagedDeployment(w, r, i, sess.DeploymentID, "model.write") {
		return
	}
	var in localai.TestbedTurnCommand
	if !decodeJSON(w, r, &in) {
		return
	}
	x, err := s.localAI.RunTestbedTurn(r.Context(), sid, in)
	respondDomain(w, x, err, http.StatusOK)
}
func (s *Server) completeModelTestbed(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	sid := strings.TrimSpace(r.PathValue("sessionID"))
	sess, err := s.localAI.TestbedSession(r.Context(), sid)
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	if !s.authorizeManagedDeployment(w, r, i, sess.DeploymentID, "model.write") {
		return
	}
	if err := s.localAI.CompleteTestbed(r.Context(), sid); err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"completed": true})
}
func (s *Server) admitModelDeployment(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	dep := strings.TrimSpace(r.PathValue("deploymentID"))
	if !s.authorizeManagedDeployment(w, r, i, dep, "model.write") {
		return
	}
	var in localai.AdmissionCommand
	if !decodeJSON(w, r, &in) {
		return
	}
	in.ActorPrincipalID = i.PrincipalID
	x, err := s.localAI.AdmitModel(r.Context(), dep, in)
	respondDomain(w, x, err, http.StatusOK)
}

func (s *Server) gatewayPresets(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authenticate(w, r); !ok {
		return
	}
	writeJSON(w, http.StatusOK, gateway.Builtins())
}
func (s *Server) createGatewayCredential(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if s.vault == nil {
		writeError(w, http.StatusServiceUnavailable, "vault unavailable")
		return
	}
	var in struct {
		WorkspaceID  *string `json:"workspace_id"`
		GatewayID    string  `json:"gateway_id"`
		Kind         string  `json:"kind"`
		Value        string  `json:"value"`
		DisplayLabel string  `json:"display_label"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	ws := ""
	if in.WorkspaceID != nil {
		ws = *in.WorkspaceID
		if !s.authorize(w, r, i, ws, "gateway.write") {
			return
		}
	}
	rec, err := s.vault.CreateGatewayCredential(r.Context(), vault.GatewayCredentialCommand{WorkspaceID: in.WorkspaceID, GatewayID: in.GatewayID, Kind: in.Kind, Value: []byte(in.Value), DisplayLabel: in.DisplayLabel, CreatedBy: i.PrincipalID})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": rec.ID, "secret_ref": "vault:" + rec.ID, "logical_name": rec.LogicalName, "workspace_id": ws})
}
func (s *Server) createGateway(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if s.gateway == nil {
		writeError(w, http.StatusServiceUnavailable, "gateway unavailable")
		return
	}
	var in struct {
		WorkspaceID   string          `json:"workspace_id"`
		PresetID      string          `json:"preset_id"`
		DisplayName   string          `json:"display_name"`
		DeliveryMode  string          `json:"delivery_mode"`
		CredentialRef *string         `json:"credential_ref"`
		Config        json.RawMessage `json:"config"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !s.authorize(w, r, i, in.WorkspaceID, "gateway.write") {
		return
	}
	x, err := s.gateway.CreateConnection(r.Context(), in.WorkspaceID, in.PresetID, in.DisplayName, in.DeliveryMode, in.CredentialRef, in.Config, i.PrincipalID)
	respondDomain(w, x, err, http.StatusCreated)
}
func (s *Server) listGateways(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if s.gateway == nil {
		writeError(w, http.StatusServiceUnavailable, "gateway unavailable")
		return
	}
	ws := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	if !s.authorize(w, r, i, ws, "gateway.read") {
		return
	}
	x, err := s.gateway.ListConnections(r.Context(), ws)
	respondDomain(w, x, err, http.StatusOK)
}
func (s *Server) createGatewayTarget(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if s.gateway == nil {
		writeError(w, http.StatusServiceUnavailable, "gateway unavailable")
		return
	}
	var in struct {
		WorkspaceID  string          `json:"workspace_id"`
		ConnectionID string          `json:"connection_id"`
		Name         string          `json:"name"`
		Address      string          `json:"address"`
		ThreadRef    *string         `json:"thread_ref"`
		Config       json.RawMessage `json:"config"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !s.authorize(w, r, i, in.WorkspaceID, "gateway.write") {
		return
	}
	x, err := s.gateway.CreateTarget(r.Context(), in.WorkspaceID, in.ConnectionID, in.Name, in.Address, in.ThreadRef, in.Config)
	respondDomain(w, x, err, http.StatusCreated)
}
func (s *Server) listGatewayTargets(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if s.gateway == nil {
		writeError(w, http.StatusServiceUnavailable, "gateway unavailable")
		return
	}
	ws := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	if !s.authorize(w, r, i, ws, "gateway.read") {
		return
	}
	x, err := s.gateway.ListTargets(r.Context(), ws)
	respondDomain(w, x, err, http.StatusOK)
}
func (s *Server) createNotificationRule(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if s.gateway == nil {
		writeError(w, http.StatusServiceUnavailable, "gateway unavailable")
		return
	}
	var in gateway.Rule
	if !decodeJSON(w, r, &in) {
		return
	}
	in.CreatedBy = i.PrincipalID
	if !s.authorize(w, r, i, in.WorkspaceID, "gateway.write") {
		return
	}
	x, err := s.gateway.CreateRule(r.Context(), in)
	respondDomain(w, x, err, http.StatusCreated)
}
func (s *Server) listNotificationRules(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if s.gateway == nil {
		writeError(w, http.StatusServiceUnavailable, "gateway unavailable")
		return
	}
	ws := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	if !s.authorize(w, r, i, ws, "gateway.read") {
		return
	}
	x, err := s.gateway.ListRules(r.Context(), ws)
	respondDomain(w, x, err, http.StatusOK)
}
func (s *Server) createNotification(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if s.gateway == nil {
		writeError(w, http.StatusServiceUnavailable, "gateway unavailable")
		return
	}
	var in struct {
		WorkspaceID    string `json:"workspace_id"`
		TargetID       string `json:"target_id"`
		IdempotencyKey string `json:"idempotency_key"`
		Title          string `json:"title"`
		Text           string `json:"text"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !s.authorize(w, r, i, in.WorkspaceID, "gateway.write") {
		return
	}
	x, err := s.gateway.Enqueue(r.Context(), in.WorkspaceID, in.TargetID, in.IdempotencyKey, in.Title, in.Text)
	respondDomain(w, x, err, http.StatusAccepted)
}
func (s *Server) createRoutineNotificationTarget(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if s.gateway == nil {
		writeError(w, http.StatusServiceUnavailable, "gateway unavailable")
		return
	}
	var in struct {
		TargetID string `json:"target_id"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	ws, err := s.gateway.RoutineWorkspace(r.Context(), r.PathValue("routineID"))
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	if !s.authorize(w, r, i, ws, "gateway.write") {
		return
	}
	x, err := s.gateway.NotifyRoutineTarget(r.Context(), r.PathValue("routineID"), in.TargetID, i.PrincipalID)
	respondDomain(w, x, err, http.StatusCreated)
}

func (s *Server) requireNodeOperator(w http.ResponseWriter, r *http.Request) (Identity, bool) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return Identity{}, false
	}
	if s.federation == nil {
		writeError(w, http.StatusServiceUnavailable, "node federation unavailable")
		return Identity{}, false
	}
	if err := s.federation.CanOperate(r.Context(), i.PrincipalID); err != nil {
		writeError(w, http.StatusForbidden, "node administration requires Admin role")
		return Identity{}, false
	}
	return i, true
}
func (s *Server) listNodes(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireNodeOperator(w, r); !ok {
		return
	}
	nodes, err := s.federation.Nodes(r.Context())
	if err != nil {
		writeError(w, 500, "list nodes failed")
		return
	}
	writeJSON(w, 200, map[string]any{"nodes": nodes})
}
func (s *Server) listNodePairings(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireNodeOperator(w, r); !ok {
		return
	}
	xs, err := s.federation.Pairings(r.Context())
	if err != nil {
		writeError(w, 500, "list node pairings failed")
		return
	}
	writeJSON(w, 200, map[string]any{"pairings": xs})
}
func (s *Server) beginNodePair(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireNodeOperator(w, r); !ok {
		return
	}
	p, code, err := s.federation.BeginPair(r.Context(), strings.TrimSpace(r.PathValue("nodeID")))
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"pairing": p, "pairing_code": code, "instruction": "Confirm this same code on both OnePane nodes"})
}
func (s *Server) confirmNodePair(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireNodeOperator(w, r); !ok {
		return
	}
	var in struct {
		Code string `json:"code"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	p, err := s.federation.ConfirmPair(r.Context(), strings.TrimSpace(r.PathValue("nodeID")), strings.TrimSpace(in.Code))
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"pairing": p})
}
func (s *Server) revokeNode(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireNodeOperator(w, r); !ok {
		return
	}
	if err := s.federation.Revoke(r.Context(), strings.TrimSpace(r.PathValue("nodeID"))); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"revoked": true})
}
func (s *Server) nodeCapabilities(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireNodeOperator(w, r); !ok {
		return
	}
	m, err := s.federation.Manifest(r.Context(), strings.TrimSpace(r.PathValue("nodeID")))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, 404, "node capability manifest not available")
			return
		}
		writeError(w, 500, "read node capabilities failed")
		return
	}
	writeJSON(w, 200, m)
}

func (s *Server) getNodeModelManagement(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireNodeOperator(w, r); !ok {
		return
	}
	g, err := s.federation.ModelManagementGrant(r.Context(), strings.TrimSpace(r.PathValue("nodeID")))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, 200, map[string]any{"peer_node_id": strings.TrimSpace(r.PathValue("nodeID")), "enabled": false, "allow_catalog_install": true})
			return
		}
		writeError(w, 500, "read node model-management grant failed")
		return
	}
	writeJSON(w, 200, g)
}

func (s *Server) setNodeModelManagement(w http.ResponseWriter, r *http.Request) {
	i, ok := s.requireNodeOperator(w, r)
	if !ok {
		return
	}
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	g, err := s.federation.SetModelManagementGrant(r.Context(), i.PrincipalID, strings.TrimSpace(r.PathValue("nodeID")), in.Enabled)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, 200, g)
}

func (s *Server) remoteNodeModelRecommendations(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireNodeOperator(w, r); !ok {
		return
	}
	var in nodefederation.RemoteModelRecommendRequest
	if !decodeJSON(w, r, &in) {
		return
	}
	profile, recs, err := s.federation.RemoteModelRecommendations(r.Context(), strings.TrimSpace(r.PathValue("nodeID")), in)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"hardware_profile": profile, "recommendations": recs})
}

func (s *Server) remoteNodeModelInstall(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireNodeOperator(w, r); !ok {
		return
	}
	var in nodefederation.RemoteModelInstallRequest
	if !decodeJSON(w, r, &in) {
		return
	}
	job, err := s.federation.RemoteInstallModel(r.Context(), strings.TrimSpace(r.PathValue("nodeID")), in)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) remoteNodeModelInstallJob(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireNodeOperator(w, r); !ok {
		return
	}
	job, err := s.federation.RemoteInstallJob(r.Context(), strings.TrimSpace(r.PathValue("nodeID")), strings.TrimSpace(r.PathValue("jobID")))
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, 200, job)
}

func (s *Server) remoteNodeModelSpecSheet(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireNodeOperator(w, r); !ok {
		return
	}
	x, err := s.federation.RemoteModelSpecSheet(r.Context(), strings.TrimSpace(r.PathValue("nodeID")), strings.TrimSpace(r.PathValue("deploymentID")))
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, x)
}

func (s *Server) remoteNodeStartModelTestbed(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireNodeOperator(w, r); !ok {
		return
	}
	var in struct {
		Notes string `json:"notes"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	x, err := s.federation.RemoteStartModelTestbed(r.Context(), strings.TrimSpace(r.PathValue("nodeID")), strings.TrimSpace(r.PathValue("deploymentID")), in.Notes)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, x)
}

func (s *Server) remoteNodeGetModelTestbed(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireNodeOperator(w, r); !ok {
		return
	}
	x, err := s.federation.RemoteModelTestbedSession(r.Context(), strings.TrimSpace(r.PathValue("nodeID")), strings.TrimSpace(r.PathValue("sessionID")))
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, x)
}

func (s *Server) remoteNodeListModelTestbedTurns(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireNodeOperator(w, r); !ok {
		return
	}
	x, err := s.federation.RemoteModelTestbedTurns(r.Context(), strings.TrimSpace(r.PathValue("nodeID")), strings.TrimSpace(r.PathValue("sessionID")))
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"turns": x})
}

func (s *Server) remoteNodeRunModelTestbedTurn(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireNodeOperator(w, r); !ok {
		return
	}
	var in localai.TestbedTurnCommand
	if !decodeJSON(w, r, &in) {
		return
	}
	x, err := s.federation.RemoteRunModelTestbedTurn(r.Context(), strings.TrimSpace(r.PathValue("nodeID")), strings.TrimSpace(r.PathValue("sessionID")), in)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, x)
}

func (s *Server) remoteNodeCompleteModelTestbed(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireNodeOperator(w, r); !ok {
		return
	}
	if err := s.federation.RemoteCompleteModelTestbed(r.Context(), strings.TrimSpace(r.PathValue("nodeID")), strings.TrimSpace(r.PathValue("sessionID"))); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"completed": true})
}

func (s *Server) remoteNodeAdmitModel(w http.ResponseWriter, r *http.Request) {
	i, ok := s.requireNodeOperator(w, r)
	if !ok {
		return
	}
	var in localai.AdmissionCommand
	if !decodeJSON(w, r, &in) {
		return
	}
	// The target records its federated model-manager principal as the durable actor;
	// retain the initiating human identity in the admission notes for origin audit.
	if strings.TrimSpace(in.Notes) == "" {
		in.Notes = "requested by origin principal " + i.PrincipalID
	} else {
		in.Notes += " (requested by origin principal " + i.PrincipalID + ")"
	}
	x, err := s.federation.RemoteAdmitModel(r.Context(), strings.TrimSpace(r.PathValue("nodeID")), strings.TrimSpace(r.PathValue("deploymentID")), in)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, x)
}

func (s *Server) getRemoteNodeModelManagement(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireNodeOperator(w, r); !ok {
		return
	}
	g, err := s.federation.RemoteModelManagementStatus(r.Context(), strings.TrimSpace(r.PathValue("nodeID")))
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, 200, g)
}
