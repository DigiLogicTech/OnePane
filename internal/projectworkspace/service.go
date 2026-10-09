package projectworkspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/id"
	"github.com/DigiLogicTech/OnePane/internal/outbox"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type Service struct {
	db          *sql.DB
	repo        repository
	tx          storage.Transactor
	events      event.Store
	outbox      outbox.Store
	ids         id.Generator
	clock       clock.Clock
	projectRoot string
	storageMu   sync.Mutex
}

func NewService(db *sql.DB, tx storage.Transactor, clk clock.Clock) *Service {
	return &Service{db: db, repo: newSQLRepository(db), tx: tx, events: event.Store{}, outbox: outbox.Store{}, ids: id.Generator{}, clock: clk}
}

func (s *Service) Project(ctx context.Context, id string) (Project, error) {
	if strings.TrimSpace(id) == "" {
		return Project{}, ErrInvalidCommand
	}
	return s.repo.Project(ctx, id)
}
func (s *Service) Projects(ctx context.Context, workspaceID string) ([]Project, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, ErrInvalidCommand
	}
	return s.repo.Projects(ctx, workspaceID)
}
func (s *Service) Runtime(ctx context.Context, id string) (ProjectRuntime, error) {
	if strings.TrimSpace(id) == "" {
		return ProjectRuntime{}, ErrInvalidCommand
	}
	return s.repo.Runtime(ctx, id)
}
func (s *Service) RuntimeByProject(ctx context.Context, id string) (ProjectRuntime, error) {
	if strings.TrimSpace(id) == "" {
		return ProjectRuntime{}, ErrInvalidCommand
	}
	return s.repo.RuntimeByProject(ctx, id)
}
func (s *Service) RuntimeByProjectWorkspace(ctx context.Context,projectID,workspaceID string) (ProjectRuntime,error){
 if strings.TrimSpace(projectID)==""||strings.TrimSpace(workspaceID)==""{return ProjectRuntime{},ErrInvalidCommand}
 return s.repo.RuntimeByProjectWorkspace(ctx,projectID,workspaceID)
}
func (s *Service) Application(ctx context.Context, id string) (Application, error) {
	if strings.TrimSpace(id) == "" {
		return Application{}, ErrInvalidCommand
	}
	return s.repo.Application(ctx, id)
}
func (s *Service) Change(ctx context.Context, id string) (ChangeProposal, error) {
	if strings.TrimSpace(id) == "" {
		return ChangeProposal{}, ErrInvalidCommand
	}
	return s.repo.Change(ctx, id)
}

func (s *Service) ListApplications(ctx context.Context, runtimeID string) ([]Application, error) {
	if strings.TrimSpace(runtimeID) == "" {
		return nil, ErrInvalidCommand
	}
	return s.repo.ListApplications(ctx, runtimeID)
}
func (s *Service) ListEndpoints(ctx context.Context, runtimeID string) ([]Endpoint, error) {
	if strings.TrimSpace(runtimeID) == "" {
		return nil, ErrInvalidCommand
	}
	return s.repo.ListEndpoints(ctx, runtimeID)
}
func (s *Service) Endpoint(ctx context.Context, id string) (Endpoint, error) {
	if strings.TrimSpace(id) == "" {
		return Endpoint{}, ErrInvalidCommand
	}
	return s.repo.Endpoint(ctx, id)
}
func (s *Service) EndpointRoute(ctx context.Context, endpointID string) (EndpointRoute, error) {
	if strings.TrimSpace(endpointID) == "" {
		return EndpointRoute{}, ErrInvalidCommand
	}
	return s.repo.EndpointRoute(ctx, endpointID)
}
func (s *Service) ResolveIngressRoute(ctx context.Context, endpointID string) (IngressRoute, error) {
	if strings.TrimSpace(endpointID) == "" {
		return IngressRoute{}, ErrInvalidCommand
	}
	x, err := s.repo.IngressRoute(ctx, endpointID)
	if err != nil {
		return IngressRoute{}, err
	}
	if x.Route.HostIP != "127.0.0.1" || x.Route.HostPort < 1 || x.Route.HostPort > 65535 || x.Route.TransportProtocol != "tcp" || x.Route.Status != "verified" {
		return IngressRoute{}, ErrInvalidTransition
	}
	return x, nil
}
func (s *Service) ListEndpointRoutes(ctx context.Context, runtimeID string) ([]EndpointRoute, error) {
	if strings.TrimSpace(runtimeID) == "" {
		return nil, ErrInvalidCommand
	}
	return s.repo.ListEndpointRoutes(ctx, runtimeID)
}
func (s *Service) ListChanges(ctx context.Context, projectID string) ([]ChangeProposal, error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrInvalidCommand
	}
	return s.repo.ListChanges(ctx, projectID)
}
func (s *Service) RoutineBinding(ctx context.Context, id string) (RoutineBinding, error) {
	if strings.TrimSpace(id) == "" {
		return RoutineBinding{}, ErrInvalidCommand
	}
	return s.repo.RoutineBinding(ctx, id)
}

func (s *Service) TaskWorkspace(ctx context.Context, taskID string) (string, error) {
	if strings.TrimSpace(taskID) == "" {
		return "", ErrInvalidCommand
	}
	workspace, _, err := s.repo.TaskProjectWorkspace(ctx, taskID)
	return workspace, err
}

func (s *Service) ValidateTaskProject(ctx context.Context, taskID, projectID string) error {
	if strings.TrimSpace(taskID) == "" || strings.TrimSpace(projectID) == "" {
		return ErrInvalidCommand
	}
	_, project, err := s.repo.TaskProjectWorkspace(ctx, taskID)
	if err != nil {
		return err
	}
	if project == nil || *project != projectID {
		return ErrCrossWorkspace
	}
	return nil
}

func (s *Service) ListRoutineBindings(ctx context.Context, projectID string) ([]RoutineBinding, error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, ErrInvalidCommand
	}
	return s.repo.ListRoutineBindings(ctx, projectID)
}

func (s *Service) CreateProject(ctx context.Context, cmd CreateProjectCommand) (Project, error) {
	if strings.TrimSpace(cmd.WorkspaceID) == "" || strings.TrimSpace(cmd.Name) == "" || strings.TrimSpace(cmd.CreatedBy) == "" {
		return Project{}, fmt.Errorf("%w: workspace, name and creator required", ErrInvalidCommand)
	}
	pol, err := canonicalJSON(cmd.ProjectPolicyJSON, "{}")
	if err != nil {
		return Project{}, fmt.Errorf("%w: policy: %v", ErrInvalidCommand, err)
	}
	idx, err := canonicalJSON(cmd.IndexingConfigJSON, "{}")
	if err != nil {
		return Project{}, fmt.Errorf("%w: indexing: %v", ErrInvalidCommand, err)
	}
	pid, err := s.ids.New("proj")
	if err != nil {
		return Project{}, err
	}
	eid, err := s.ids.New("evt")
	if err != nil {
		return Project{}, err
	}
	now := s.clock.UnixMilli()
	desc := strings.TrimSpace(cmd.Description)
	var dp *string
	if desc != "" {
		dp = &desc
	}
	p := Project{ID: pid, WorkspaceID: cmd.WorkspaceID, Name: strings.TrimSpace(cmd.Name), Description: dp, Status: "active", ProjectPolicyJSON: pol, IndexingConfigJSON: idx, Revision: 1, CreatedBy: cmd.CreatedBy, CreatedAt: now, UpdatedAt: now}
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if err := s.requireActor(ctx, tx, p.WorkspaceID, cmd.CreatedBy); err != nil {
			return err
		}
		if err := s.repo.InsertProject(ctx, tx, p); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"project_id": p.ID, "name": p.Name, "revision": 1})
		return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: &p.WorkspaceID, Type: "project.created", AggregateType: "project", AggregateID: p.ID, ActorPrincipalID: &cmd.CreatedBy, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now})
	})
	if err != nil {
		return Project{}, err
	}
	return s.repo.Project(ctx, p.ID)
}

func (s *Service) UpdateProjectPolicy(ctx context.Context, cmd UpdateProjectPolicyCommand) (Project, error) {
	if strings.TrimSpace(cmd.ProjectID) == "" || cmd.ExpectedRevision < 1 || strings.TrimSpace(cmd.ActorPrincipalID) == "" {
		return Project{}, ErrInvalidCommand
	}
	policy, err := canonicalJSON(cmd.ProjectPolicyJSON, "{}")
	if err != nil {
		return Project{}, fmt.Errorf("%w: policy: %v", ErrInvalidCommand, err)
	}
	eid, err := s.ids.New("evt")
	if err != nil {
		return Project{}, err
	}
	now := s.clock.UnixMilli()
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		p, err := s.repo.ProjectTx(ctx, tx, cmd.ProjectID)
		if err != nil {
			return err
		}
		if p.Revision != cmd.ExpectedRevision {
			return ErrRevisionConflict
		}
		if p.Status != "active" {
			return ErrProjectInactive
		}
		if err := s.requireActor(ctx, tx, p.WorkspaceID, cmd.ActorPrincipalID); err != nil {
			return err
		}
		if err := s.repo.UpdateProjectPolicy(ctx, tx, p, policy, now); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"project_id": p.ID, "revision": p.Revision + 1})
		actor := cmd.ActorPrincipalID
		return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: &p.WorkspaceID, Type: "project.policy_updated", AggregateType: "project", AggregateID: p.ID, ActorPrincipalID: &actor, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now})
	})
	if err != nil {
		return Project{}, err
	}
	return s.repo.Project(ctx, cmd.ProjectID)
}

type projectArchiveRepository interface {
	ArchiveProject(context.Context, storage.Tx, Project, int64) error
}

func (s *Service) ArchiveProject(ctx context.Context, cmd ArchiveProjectCommand) (Project, error) {
	if strings.TrimSpace(cmd.ProjectID) == "" || cmd.ExpectedRevision < 1 || strings.TrimSpace(cmd.ActorPrincipalID) == "" {
		return Project{}, ErrInvalidCommand
	}
	archiver, ok := s.repo.(projectArchiveRepository)
	if !ok {
		return Project{}, fmt.Errorf("project archive repository unavailable")
	}
	eid, err := s.ids.New("evt")
	if err != nil {
		return Project{}, err
	}
	now := s.clock.UnixMilli()
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		p, err := s.repo.ProjectTx(ctx, tx, cmd.ProjectID)
		if err != nil {
			return err
		}
		if p.Revision != cmd.ExpectedRevision {
			return ErrRevisionConflict
		}
		if p.Status != "active" {
			return ErrProjectInactive
		}
		if err := s.requireActor(ctx, tx, p.WorkspaceID, cmd.ActorPrincipalID); err != nil {
			return err
		}
		if err := archiver.ArchiveProject(ctx, tx, p, now); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"project_id": p.ID, "revision": p.Revision + 1, "status": "archived"})
		actor := cmd.ActorPrincipalID
		return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: &p.WorkspaceID, Type: "project.archived", AggregateType: "project", AggregateID: p.ID, ActorPrincipalID: &actor, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now})
	})
	if err != nil {
		return Project{}, err
	}
	return s.repo.Project(ctx, cmd.ProjectID)
}

func (s *Service) CreateRuntime(ctx context.Context, cmd CreateRuntimeCommand) (ProjectRuntime, error) {
	if strings.TrimSpace(cmd.ProjectID) == "" || strings.TrimSpace(cmd.CreatedBy) == "" {
		return ProjectRuntime{}, fmt.Errorf("%w: project and creator required", ErrInvalidCommand)
	}
	if cmd.IsolationMode == "" {
		cmd.IsolationMode = IsolationSandboxedContainer
	}
	if !validIsolation(cmd.IsolationMode) {
		return ProjectRuntime{}, fmt.Errorf("%w: isolation mode", ErrInvalidCommand)
	}
	if cmd.DesiredState == "" {
		cmd.DesiredState = RuntimeDesiredStopped
	}
	if !validRuntimeDesired(cmd.DesiredState) {
		return ProjectRuntime{}, fmt.Errorf("%w: desired state", ErrInvalidCommand)
	}
	rs, err := validateSafeSpec(cmd.RuntimeSpecJSON)
	if err != nil {
		return ProjectRuntime{}, err
	}
	rl, err := validateSafeSpec(cmd.ResourceLimitsJSON)
	if err != nil {
		return ProjectRuntime{}, err
	}
	if string(rl) == "{}" {
		rl = defaultResourceLimits()
	}
	eb, err := validateEnvironmentBindings(cmd.EnvironmentBindingsJSON)
	if err != nil {
		return ProjectRuntime{}, err
	}
	rid, err := s.ids.New("prun")
	if err != nil {
		return ProjectRuntime{}, err
	}
	eid, err := s.ids.New("evt")
	if err != nil {
		return ProjectRuntime{}, err
	}
	jobID, err := s.ids.New("job")
	if err != nil {
		return ProjectRuntime{}, err
	}
	now := s.clock.UnixMilli()
	r := ProjectRuntime{ID: rid, ProjectID: cmd.ProjectID, ProjectWorkspaceID: cmd.ProjectWorkspaceID, NodeID: cmd.NodeID, IsolationMode: cmd.IsolationMode, Backend: "sandbox_runner", DesiredState: cmd.DesiredState, Status: RuntimeDefined, RuntimeSpecJSON: rs, ResourceLimitsJSON: rl, NetworkPolicyJSON: defaultNetworkPolicy(), FilesystemPolicyJSON: defaultFilesystemPolicy(), EnvironmentBindingsJSON: eb, Revision: 1, CreatedBy: cmd.CreatedBy, CreatedAt: now, UpdatedAt: now}
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		p, err := s.repo.ProjectTx(ctx, tx, cmd.ProjectID)
		if err != nil {
			return err
		}
		if p.Status != "active" {
			return ErrProjectInactive
		}
		if err := s.requireActor(ctx, tx, p.WorkspaceID, cmd.CreatedBy); err != nil {
			return err
		}
		if cmd.ProjectWorkspaceID != nil {
			var count int
			err=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM project_workspaces WHERE id=? AND project_id=? AND status='active'`,*cmd.ProjectWorkspaceID,cmd.ProjectID).Scan(&count)
			if err!=nil{return err}
			if count!=1{return ErrCrossWorkspace}
		}
		if cmd.NodeID != nil {
			if strings.TrimSpace(*cmd.NodeID) == "" {
				return fmt.Errorf("%w: blank node id", ErrInvalidCommand)
			}
			ok, err := s.repo.NodeExistsTx(ctx, tx, *cmd.NodeID)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("%w: node not found", ErrInvalidCommand)
			}
		}
		if err := s.repo.InsertRuntime(ctx, tx, r); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"runtime_id": r.ID, "project_id": r.ProjectID, "isolation_mode": r.IsolationMode, "backend": r.Backend, "desired_state": r.DesiredState, "status": r.Status, "revision": 1})
		if err := s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: &p.WorkspaceID, Type: "project_runtime.created", AggregateType: "project_runtime", AggregateID: r.ID, ActorPrincipalID: &cmd.CreatedBy, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now}); err != nil {
			return err
		}
		if r.DesiredState == RuntimeDesiredRunning {
			jp, _ := json.Marshal(map[string]any{"project_runtime_id": r.ID, "reason": "desired_state"})
			return s.outbox.Enqueue(ctx, tx, outbox.Job{ID: jobID, WorkspaceID: &p.WorkspaceID, Type: "project_runtime.reconcile", Payload: jp, AvailableAt: now, MaxAttempts: 10, CreatedAt: now})
		}
		return nil
	})
	if err != nil {
		return ProjectRuntime{}, err
	}
	return s.repo.Runtime(ctx, r.ID)
}

func (s *Service) SetRuntimeDesiredState(ctx context.Context, cmd SetRuntimeDesiredStateCommand) (ProjectRuntime, error) {
	if strings.TrimSpace(cmd.RuntimeID) == "" || cmd.ExpectedRevision < 1 || strings.TrimSpace(cmd.ActorPrincipalID) == "" || !validRuntimeDesired(cmd.DesiredState) {
		return ProjectRuntime{}, ErrInvalidCommand
	}
	eid, _ := s.ids.New("evt")
	jid, _ := s.ids.New("job")
	now := s.clock.UnixMilli()
	err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		r, err := s.repo.RuntimeTx(ctx, tx, cmd.RuntimeID)
		if err != nil {
			return err
		}
		if r.Revision != cmd.ExpectedRevision {
			return ErrRevisionConflict
		}
		p, err := s.repo.ProjectTx(ctx, tx, r.ProjectID)
		if err != nil {
			return err
		}
		if err := s.requireActor(ctx, tx, p.WorkspaceID, cmd.ActorPrincipalID); err != nil {
			return err
		}
		if err := s.repo.UpdateRuntimeDesired(ctx, tx, r, cmd.DesiredState, now); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"from": r.DesiredState, "to": cmd.DesiredState, "observed_status": r.Status, "revision": r.Revision + 1})
		if err := s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: &p.WorkspaceID, Type: "project_runtime.desired_state_changed", AggregateType: "project_runtime", AggregateID: r.ID, ActorPrincipalID: &cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now}); err != nil {
			return err
		}
		jp, _ := json.Marshal(map[string]any{"project_runtime_id": r.ID, "reason": "desired_state"})
		return s.outbox.Enqueue(ctx, tx, outbox.Job{ID: jid, WorkspaceID: &p.WorkspaceID, Type: "project_runtime.reconcile", Payload: jp, AvailableAt: now, MaxAttempts: 10, CreatedAt: now})
	})
	if err != nil {
		return ProjectRuntime{}, err
	}
	return s.repo.Runtime(ctx, cmd.RuntimeID)
}

func normalizeRuntimeNetworkPolicy(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return defaultNetworkPolicy(), nil
	}
	v, err := validateSafeSpec(raw)
	if err != nil {
		return nil, err
	}
	var p struct {
		Mode    string            `json:"mode"`
		Ingress string            `json:"ingress"`
		Egress  []json.RawMessage `json:"egress"`
	}
	if err := json.Unmarshal(v, &p); err != nil {
		return nil, ErrInvalidCommand
	}
	if p.Ingress == "" {
		p.Ingress = "proxy_only"
	}
	if p.Ingress != "proxy_only" {
		return nil, fmt.Errorf("%w: runtime ingress must remain proxy_only", ErrInvalidCommand)
	}
	switch p.Mode {
	case "", "deny_by_default":
		if len(p.Egress) != 0 {
			return nil, fmt.Errorf("%w: deny_by_default cannot declare egress", ErrInvalidCommand)
		}
		return json.RawMessage(`{"mode":"deny_by_default","ingress":"proxy_only","egress":[]}`), nil
	case "external":
		// External is an explicit project-owner choice. The rootless backend
		// provides an ordinary dedicated bridge; fine-grained destination
		// filtering remains the responsibility of the later governed egress proxy.
		return json.RawMessage(`{"mode":"external","ingress":"proxy_only","egress":[{"kind":"external"}]}`), nil
	default:
		return nil, fmt.Errorf("%w: unsupported network mode", ErrInvalidCommand)
	}
}

func normalizeRuntimeFilesystemPolicy(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return defaultFilesystemPolicy(), nil
	}
	v, err := canonicalJSON(raw, "{}")
	if err != nil {
		return nil, fmt.Errorf("%w: invalid filesystem policy: %v", ErrInvalidCommand, err)
	}
	var p struct {
		Root             string `json:"root"`
		ProjectWorkspace struct {
			Mount string `json:"mount"`
			Mode  string `json:"mode"`
		} `json:"project_workspace"`
		HostMounts        []json.RawMessage `json:"host_mounts"`
		DockerSocket      bool              `json:"docker_socket"`
		DevicePassthrough bool              `json:"device_passthrough"`
		NoNewPrivileges   bool              `json:"no_new_privileges"`
	}
	if err := json.Unmarshal(v, &p); err != nil {
		return nil, ErrInvalidCommand
	}
	if p.Root != "ephemeral" || p.ProjectWorkspace.Mount != "/workspace" || p.ProjectWorkspace.Mode != "read_write" || len(p.HostMounts) != 0 || p.DockerSocket || p.DevicePassthrough || !p.NoNewPrivileges {
		return nil, fmt.Errorf("%w: filesystem policy must retain the isolated project workspace and cannot add host/device privilege", ErrInvalidCommand)
	}
	return v, nil
}

func (s *Service) UpdateRuntimePolicy(ctx context.Context, cmd UpdateRuntimePolicyCommand) (ProjectRuntime, error) {
	if strings.TrimSpace(cmd.RuntimeID) == "" || cmd.ExpectedRevision < 1 || strings.TrimSpace(cmd.ActorPrincipalID) == "" {
		return ProjectRuntime{}, ErrInvalidCommand
	}
	networkPolicy, err := normalizeRuntimeNetworkPolicy(cmd.NetworkPolicyJSON)
	if err != nil {
		return ProjectRuntime{}, err
	}
	filesystemPolicy, err := normalizeRuntimeFilesystemPolicy(cmd.FilesystemPolicyJSON)
	if err != nil {
		return ProjectRuntime{}, err
	}
	eid, _ := s.ids.New("evt")
	jid, _ := s.ids.New("job")
	now := s.clock.UnixMilli()
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		r, err := s.repo.RuntimeTx(ctx, tx, cmd.RuntimeID)
		if err != nil {
			return err
		}
		if r.Revision != cmd.ExpectedRevision {
			return ErrRevisionConflict
		}
		p, err := s.repo.ProjectTx(ctx, tx, r.ProjectID)
		if err != nil {
			return err
		}
		if err := s.requireActor(ctx, tx, p.WorkspaceID, cmd.ActorPrincipalID); err != nil {
			return err
		}
		if err := s.repo.UpdateRuntimePolicy(ctx, tx, r, networkPolicy, filesystemPolicy, now); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"runtime_id": r.ID, "network_policy": json.RawMessage(networkPolicy), "filesystem_policy": json.RawMessage(filesystemPolicy), "revision": r.Revision + 1})
		if err := s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: &p.WorkspaceID, Type: "project_runtime.policy_updated", AggregateType: "project_runtime", AggregateID: r.ID, ActorPrincipalID: &cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now}); err != nil {
			return err
		}
		jp, _ := json.Marshal(map[string]any{"project_runtime_id": r.ID, "reason": "policy_updated"})
		return s.outbox.Enqueue(ctx, tx, outbox.Job{ID: jid, WorkspaceID: &p.WorkspaceID, Type: "project_runtime.reconcile", Payload: jp, AvailableAt: now, MaxAttempts: 10, CreatedAt: now})
	})
	if err != nil {
		return ProjectRuntime{}, err
	}
	return s.repo.Runtime(ctx, cmd.RuntimeID)
}

func (s *Service) ApplyRuntimeObservation(ctx context.Context, cmd ApplyRuntimeObservationCommand) (ProjectRuntime, error) {
	if strings.TrimSpace(cmd.RuntimeID) == "" || cmd.ExpectedRevision < 1 || !validRuntimeStatus(cmd.Status) || strings.TrimSpace(cmd.ObservationID) == "" || strings.TrimSpace(cmd.VerificationID) == "" || strings.TrimSpace(cmd.ActorPrincipalID) == "" {
		return ProjectRuntime{}, ErrInvalidCommand
	}
	eid, _ := s.ids.New("evt")
	now := s.clock.UnixMilli()
	err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		r, err := s.repo.RuntimeTx(ctx, tx, cmd.RuntimeID)
		if err != nil {
			return err
		}
		if r.Revision != cmd.ExpectedRevision {
			return ErrRevisionConflict
		}
		p, err := s.repo.ProjectTx(ctx, tx, r.ProjectID)
		if err != nil {
			return err
		}
		if err := s.requireActor(ctx, tx, p.WorkspaceID, cmd.ActorPrincipalID); err != nil {
			return err
		}
		subject := "project_runtime:" + r.ID
		if err := s.repo.RequireVerifiedObservationTx(ctx, tx, p.WorkspaceID, subject, cmd.VerificationID, cmd.ObservationID); err != nil {
			return err
		}
		if err := s.repo.UpdateRuntimeObserved(ctx, tx, r, cmd.Status, now); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"runtime_id": r.ID, "from": r.Status, "to": cmd.Status, "observation_id": cmd.ObservationID, "verification_id": cmd.VerificationID, "revision": r.Revision + 1})
		return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: &p.WorkspaceID, Type: "project_runtime.observed", AggregateType: "project_runtime", AggregateID: r.ID, ActorPrincipalID: &cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now})
	})
	if err != nil {
		return ProjectRuntime{}, err
	}
	return s.repo.Runtime(ctx, cmd.RuntimeID)
}

func (s *Service) ApplyApplicationObservation(ctx context.Context, cmd ApplyApplicationObservationCommand) (Application, error) {
	if strings.TrimSpace(cmd.ApplicationID) == "" || cmd.ExpectedRevision < 1 || !validAppStatus(cmd.Status) || strings.TrimSpace(cmd.ObservationID) == "" || strings.TrimSpace(cmd.VerificationID) == "" || strings.TrimSpace(cmd.ActorPrincipalID) == "" {
		return Application{}, ErrInvalidCommand
	}
	eid, _ := s.ids.New("evt")
	now := s.clock.UnixMilli()
	err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		a, err := s.repo.ApplicationTx(ctx, tx, cmd.ApplicationID)
		if err != nil {
			return err
		}
		if a.Revision != cmd.ExpectedRevision {
			return ErrRevisionConflict
		}
		r, err := s.repo.RuntimeTx(ctx, tx, a.ProjectRuntimeID)
		if err != nil {
			return err
		}
		p, err := s.repo.ProjectTx(ctx, tx, r.ProjectID)
		if err != nil {
			return err
		}
		if err := s.requireActor(ctx, tx, p.WorkspaceID, cmd.ActorPrincipalID); err != nil {
			return err
		}
		subject := "project_app:" + a.ID
		if err := s.repo.RequireVerifiedObservationTx(ctx, tx, p.WorkspaceID, subject, cmd.VerificationID, cmd.ObservationID); err != nil {
			return err
		}
		// Every new container observation invalidates previously derived dynamic
		// host-port routes. Reconciliation must republish them from this exact
		// verified observation before ingress becomes available again.
		if err := s.repo.MarkApplicationEndpointRoutesStale(ctx, tx, a.ID, now); err != nil {
			return err
		}
		if err := s.repo.MarkApplicationEndpointsUnready(ctx, tx, a.ID, now); err != nil {
			return err
		}
		if err := s.repo.UpdateApplicationObserved(ctx, tx, a, cmd.Status, now); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"application_id": a.ID, "from": a.Status, "to": cmd.Status, "observation_id": cmd.ObservationID, "verification_id": cmd.VerificationID, "revision": a.Revision + 1})
		return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: &p.WorkspaceID, Type: "project_application.observed", AggregateType: "project_application", AggregateID: a.ID, ActorPrincipalID: &cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now})
	})
	if err != nil {
		return Application{}, err
	}
	return s.repo.Application(ctx, cmd.ApplicationID)
}

func (s *Service) ApplyEndpointRoute(ctx context.Context, cmd ApplyEndpointRouteCommand) (EndpointRoute, error) {
	if strings.TrimSpace(cmd.EndpointID) == "" || strings.TrimSpace(cmd.ApplicationID) == "" || cmd.ApplicationRevision < 1 || cmd.HostIP != "127.0.0.1" || cmd.HostPort < 1 || cmd.HostPort > 65535 || strings.ToLower(strings.TrimSpace(cmd.TransportProtocol)) != "tcp" || strings.TrimSpace(cmd.ContainerSpecHash) == "" || strings.TrimSpace(cmd.ObservationID) == "" || strings.TrimSpace(cmd.VerificationID) == "" || strings.TrimSpace(cmd.ActorPrincipalID) == "" {
		return EndpointRoute{}, ErrInvalidCommand
	}
	now := s.clock.UnixMilli()
	eid, _ := s.ids.New("evt")
	err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		e, err := s.repo.EndpointTx(ctx, tx, cmd.EndpointID)
		if err != nil {
			return err
		}
		if e.ApplicationID == nil || *e.ApplicationID != cmd.ApplicationID || e.DesiredState != "enabled" {
			return ErrInvalidTransition
		}
		a, err := s.repo.ApplicationTx(ctx, tx, cmd.ApplicationID)
		if err != nil {
			return err
		}
		if a.ProjectRuntimeID != e.ProjectRuntimeID || a.Revision != cmd.ApplicationRevision || a.Status != AppRunning {
			return ErrInvalidTransition
		}
		r, err := s.repo.RuntimeTx(ctx, tx, e.ProjectRuntimeID)
		if err != nil {
			return err
		}
		p, err := s.repo.ProjectTx(ctx, tx, r.ProjectID)
		if err != nil {
			return err
		}
		if err := s.requireActor(ctx, tx, p.WorkspaceID, cmd.ActorPrincipalID); err != nil {
			return err
		}
		if err := s.repo.RequireVerifiedEndpointRouteTx(ctx, tx, p.WorkspaceID, a.ID, cmd.VerificationID, cmd.ObservationID, e.InternalPort, cmd.HostPort, cmd.ContainerSpecHash); err != nil {
			return err
		}
		x := EndpointRoute{EndpointID: e.ID, ProjectRuntimeID: r.ID, ApplicationID: a.ID, HostIP: "127.0.0.1", HostPort: cmd.HostPort, TransportProtocol: "tcp", ObservationID: cmd.ObservationID, VerificationID: cmd.VerificationID, ApplicationRevision: a.Revision, EndpointRevision: e.Revision, ContainerSpecHash: cmd.ContainerSpecHash, Status: "verified", UpdatedAt: now}
		if err := s.repo.UpsertEndpointRoute(ctx, tx, x); err != nil {
			return err
		}
		if err := s.repo.SetEndpointObservedRoute(ctx, tx, e.ID, now); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"endpoint_id": e.ID, "runtime_id": r.ID, "application_id": a.ID, "host_ip": x.HostIP, "host_port": x.HostPort, "observation_id": x.ObservationID, "verification_id": x.VerificationID, "container_spec_hash": x.ContainerSpecHash})
		return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: &p.WorkspaceID, Type: "project_endpoint.route_verified", AggregateType: "project_endpoint", AggregateID: e.ID, ActorPrincipalID: &cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now})
	})
	if err != nil {
		return EndpointRoute{}, err
	}
	return s.repo.EndpointRoute(ctx, cmd.EndpointID)
}

func (s *Service) DeclareApplication(ctx context.Context, cmd DeclareApplicationCommand) (Application, error) {
	if strings.TrimSpace(cmd.RuntimeID) == "" || strings.TrimSpace(cmd.Name) == "" || strings.TrimSpace(cmd.CreatedBy) == "" || !validAppSource(cmd.SourceKind) {
		return Application{}, ErrInvalidCommand
	}
	sourceRef, err := validateSourceRef(cmd.SourceKind, cmd.SourceRef)
	if err != nil {
		return Application{}, err
	}
	if cmd.DesiredState == "" {
		cmd.DesiredState = AppDesiredInstalled
	}
	if !validAppDesired(cmd.DesiredState) {
		return Application{}, ErrInvalidCommand
	}
	install, err := validateSafeSpec(cmd.InstallSpecJSON)
	if err != nil {
		return Application{}, err
	}
	runtime, err := validateSafeSpec(cmd.RuntimeSpecJSON)
	if err != nil {
		return Application{}, err
	}
	env, err := validateEnvironmentBindings(cmd.EnvironmentBindingsJSON)
	if err != nil {
		return Application{}, err
	}
	aid, _ := s.ids.New("papp")
	eid, _ := s.ids.New("evt")
	jid, _ := s.ids.New("job")
	now := s.clock.UnixMilli()
	a := Application{ID: aid, ProjectRuntimeID: cmd.RuntimeID, Name: strings.TrimSpace(cmd.Name), SourceKind: cmd.SourceKind, SourceRef: sourceRef, VersionRef: cmd.VersionRef, InstallSpecJSON: install, RuntimeSpecJSON: runtime, EnvironmentBindingsJSON: env, DesiredState: cmd.DesiredState, Status: AppDeclared, Trust: "untrusted_content", Revision: 1, CreatedBy: cmd.CreatedBy, CreatedAt: now, UpdatedAt: now}
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		r, err := s.repo.RuntimeTx(ctx, tx, cmd.RuntimeID)
		if err != nil {
			return err
		}
		p, err := s.repo.ProjectTx(ctx, tx, r.ProjectID)
		if err != nil {
			return err
		}
		if err := s.requireActor(ctx, tx, p.WorkspaceID, cmd.CreatedBy); err != nil {
			return err
		}
		if cmd.SourceKind == AppArtifact || cmd.SourceKind == AppCompose {
			workspaceID, projectID, err := s.repo.ArtifactProjectWorkspaceTx(ctx, tx, artifactIDFromSource(sourceRef))
			if err != nil {
				return fmt.Errorf("resolve application source artifact: %w", err)
			}
			if workspaceID != p.WorkspaceID || (projectID != nil && *projectID != p.ID) {
				return ErrCrossWorkspace
			}
		}
		if err := s.repo.InsertApplication(ctx, tx, a); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"application_id": a.ID, "runtime_id": a.ProjectRuntimeID, "source_kind": a.SourceKind, "source_ref": a.SourceRef, "trust": a.Trust, "desired_state": a.DesiredState})
		if err := s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: &p.WorkspaceID, Type: "project_application.declared", AggregateType: "project_application", AggregateID: a.ID, ActorPrincipalID: &cmd.CreatedBy, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now}); err != nil {
			return err
		}
		jp, _ := json.Marshal(map[string]any{"project_runtime_id": r.ID, "application_id": a.ID, "reason": "application_declared"})
		return s.outbox.Enqueue(ctx, tx, outbox.Job{ID: jid, WorkspaceID: &p.WorkspaceID, Type: "project_runtime.reconcile", Payload: jp, AvailableAt: now, MaxAttempts: 10, CreatedAt: now})
	})
	if err != nil {
		return Application{}, err
	}
	return s.repo.Application(ctx, a.ID)
}

func (s *Service) DeclareEndpoint(ctx context.Context, cmd DeclareEndpointCommand) (Endpoint, error) {
	if strings.TrimSpace(cmd.RuntimeID) == "" || strings.TrimSpace(cmd.Name) == "" || strings.TrimSpace(cmd.CreatedBy) == "" || cmd.InternalPort < 1 || cmd.InternalPort > 65535 || !validExposure(cmd.Exposure) {
		return Endpoint{}, ErrInvalidCommand
	}
	proto := strings.ToLower(strings.TrimSpace(cmd.Protocol))
	if proto != "http" && proto != "https" && proto != "tcp" {
		return Endpoint{}, ErrInvalidCommand
	}
	if cmd.PathPrefix != nil && !strings.HasPrefix(*cmd.PathPrefix, "/") {
		return Endpoint{}, fmt.Errorf("%w: path prefix must begin with /", ErrInvalidCommand)
	}
	idv, _ := s.ids.New("pend")
	eid, _ := s.ids.New("evt")
	now := s.clock.UnixMilli()
	e := Endpoint{ID: idv, ProjectRuntimeID: cmd.RuntimeID, ApplicationID: cmd.ApplicationID, Name: strings.TrimSpace(cmd.Name), Protocol: proto, InternalPort: cmd.InternalPort, Exposure: cmd.Exposure, PathPrefix: cmd.PathPrefix, DesiredState: "enabled", Status: "declared", Revision: 1, CreatedBy: cmd.CreatedBy, CreatedAt: now, UpdatedAt: now}
	err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		r, err := s.repo.RuntimeTx(ctx, tx, cmd.RuntimeID)
		if err != nil {
			return err
		}
		p, err := s.repo.ProjectTx(ctx, tx, r.ProjectID)
		if err != nil {
			return err
		}
		if err := s.requireActor(ctx, tx, p.WorkspaceID, cmd.CreatedBy); err != nil {
			return err
		}
		if cmd.ApplicationID != nil {
			a, err := s.repo.ApplicationTx(ctx, tx, *cmd.ApplicationID)
			if err != nil {
				return err
			}
			if a.ProjectRuntimeID != r.ID {
				return ErrCrossWorkspace
			}
		}
		if err := s.repo.InsertEndpoint(ctx, tx, e); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"endpoint_id": e.ID, "runtime_id": e.ProjectRuntimeID, "application_id": e.ApplicationID, "protocol": e.Protocol, "port": e.InternalPort, "exposure": e.Exposure, "status": e.Status})
		return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: &p.WorkspaceID, Type: "project_endpoint.declared", AggregateType: "project_endpoint", AggregateID: e.ID, ActorPrincipalID: &cmd.CreatedBy, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now})
	})
	if err != nil {
		return Endpoint{}, err
	}
	return e, nil
}

func (s *Service) ProposeChange(ctx context.Context, cmd ProposeChangeCommand) (ChangeProposal, error) {
	if strings.TrimSpace(cmd.ProjectID) == "" || strings.TrimSpace(cmd.Summary) == "" || strings.TrimSpace(cmd.ProposedBy) == "" || !validProposalKind(cmd.Kind) {
		return ChangeProposal{}, ErrInvalidCommand
	}
	meta, err := canonicalJSON(cmd.MetadataJSON, "{}")
	if err != nil {
		return ChangeProposal{}, ErrInvalidCommand
	}
	cid, _ := s.ids.New("chg")
	eid, _ := s.ids.New("evt")
	now := s.clock.UnixMilli()
	c := ChangeProposal{ID: cid, ProjectID: cmd.ProjectID, ProjectRuntimeID: cmd.RuntimeID, TaskID: cmd.TaskID, ProposalKind: cmd.Kind, Status: ProposalProposed, Summary: strings.TrimSpace(cmd.Summary), BaseRevision: cmd.BaseRevision, PatchArtifactID: cmd.PatchArtifactID, MetadataJSON: meta, Revision: 1, ProposedBy: cmd.ProposedBy, CreatedAt: now, UpdatedAt: now}
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		p, err := s.repo.ProjectTx(ctx, tx, cmd.ProjectID)
		if err != nil {
			return err
		}
		if err := s.requireActor(ctx, tx, p.WorkspaceID, cmd.ProposedBy); err != nil {
			return err
		}
		if cmd.RuntimeID != nil {
			r, err := s.repo.RuntimeTx(ctx, tx, *cmd.RuntimeID)
			if err != nil {
				return err
			}
			if r.ProjectID != p.ID {
				return ErrCrossWorkspace
			}
		}
		if cmd.TaskID != nil {
			w, project, err := s.repo.TaskProjectWorkspaceTx(ctx, tx, *cmd.TaskID)
			if err != nil {
				return err
			}
			if w != p.WorkspaceID || project == nil || *project != p.ID {
				return ErrCrossWorkspace
			}
		}
		if cmd.PatchArtifactID != nil {
			w, project, err := s.repo.ArtifactProjectWorkspaceTx(ctx, tx, *cmd.PatchArtifactID)
			if err != nil {
				return err
			}
			if w != p.WorkspaceID || (project != nil && *project != p.ID) {
				return ErrCrossWorkspace
			}
		}
		if err := s.repo.InsertChange(ctx, tx, c); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"proposal_id": c.ID, "kind": c.ProposalKind, "summary": c.Summary, "task_id": c.TaskID, "patch_artifact_id": c.PatchArtifactID})
		return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: &p.WorkspaceID, Type: "project_change.proposed", AggregateType: "project_change", AggregateID: c.ID, ActorPrincipalID: &cmd.ProposedBy, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now})
	})
	if err != nil {
		return ChangeProposal{}, err
	}
	return s.repo.Change(ctx, c.ID)
}

func (s *Service) ReviewChange(ctx context.Context, cmd ReviewChangeCommand) (ChangeProposal, error) {
	if strings.TrimSpace(cmd.ProposalID) == "" || cmd.ExpectedRevision < 1 || strings.TrimSpace(cmd.ReviewedBy) == "" || (cmd.Decision != ProposalApproved && cmd.Decision != ProposalRejected) {
		return ChangeProposal{}, ErrInvalidCommand
	}
	eid, _ := s.ids.New("evt")
	now := s.clock.UnixMilli()
	err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		c, err := s.repo.ChangeTx(ctx, tx, cmd.ProposalID)
		if err != nil {
			return err
		}
		if c.Revision != cmd.ExpectedRevision {
			return ErrRevisionConflict
		}
		if c.Status != ProposalProposed && c.Status != ProposalReviewing {
			return ErrInvalidTransition
		}
		p, err := s.repo.ProjectTx(ctx, tx, c.ProjectID)
		if err != nil {
			return err
		}
		if err := s.requireActor(ctx, tx, p.WorkspaceID, cmd.ReviewedBy); err != nil {
			return err
		}
		if c.ProposedBy == cmd.ReviewedBy {
			return fmt.Errorf("%w: proposer cannot self-approve/reject review gate", ErrInvalidCommand)
		}
		if err := s.repo.UpdateChangeStatus(ctx, tx, c, cmd.Decision, cmd.ReviewedBy, now); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"proposal_id": c.ID, "from": c.Status, "to": cmd.Decision, "revision": c.Revision + 1})
		return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: &p.WorkspaceID, Type: "project_change.reviewed", AggregateType: "project_change", AggregateID: c.ID, ActorPrincipalID: &cmd.ReviewedBy, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now})
	})
	if err != nil {
		return ChangeProposal{}, err
	}
	return s.repo.Change(ctx, cmd.ProposalID)
}

func (s *Service) BindRoutine(ctx context.Context, cmd BindRoutineCommand) (RoutineBinding, error) {
	if strings.TrimSpace(cmd.ProjectID) == "" || strings.TrimSpace(cmd.RuntimeID) == "" || strings.TrimSpace(cmd.RoutineID) == "" || strings.TrimSpace(cmd.ActionRef) == "" || strings.TrimSpace(cmd.CreatedBy) == "" || !validActionKind(cmd.ActionKind) {
		return RoutineBinding{}, ErrInvalidCommand
	}
	spec, err := validateSafeSpec(cmd.ActionSpecJSON)
	if err != nil {
		return RoutineBinding{}, err
	}
	if cmd.ActionKind == ActionAppCommand && cmd.ApplicationID == nil {
		return RoutineBinding{}, fmt.Errorf("%w: app_command requires application", ErrInvalidCommand)
	}
	bid, _ := s.ids.New("prb")
	eid, _ := s.ids.New("evt")
	now := s.clock.UnixMilli()
	b := RoutineBinding{ID: bid, ProjectID: cmd.ProjectID, ProjectRuntimeID: cmd.RuntimeID, ApplicationID: cmd.ApplicationID, RoutineID: cmd.RoutineID, ActionKind: cmd.ActionKind, ActionRef: strings.TrimSpace(cmd.ActionRef), ActionSpecJSON: spec, Status: "active", Revision: 1, CreatedBy: cmd.CreatedBy, CreatedAt: now, UpdatedAt: now}
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		p, err := s.repo.ProjectTx(ctx, tx, cmd.ProjectID)
		if err != nil {
			return err
		}
		if err := s.requireActor(ctx, tx, p.WorkspaceID, cmd.CreatedBy); err != nil {
			return err
		}
		r, err := s.repo.RuntimeTx(ctx, tx, cmd.RuntimeID)
		if err != nil {
			return err
		}
		if r.ProjectID != p.ID {
			return ErrCrossWorkspace
		}
		rw, err := s.repo.RoutineWorkspaceTx(ctx, tx, cmd.RoutineID)
		if err != nil {
			return err
		}
		if rw != p.WorkspaceID {
			return ErrCrossWorkspace
		}
		if cmd.ApplicationID != nil {
			a, err := s.repo.ApplicationTx(ctx, tx, *cmd.ApplicationID)
			if err != nil {
				return err
			}
			if a.ProjectRuntimeID != r.ID {
				return ErrCrossWorkspace
			}
		}
		if err := s.repo.InsertRoutineBinding(ctx, tx, b); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"binding_id": b.ID, "routine_id": b.RoutineID, "runtime_id": b.ProjectRuntimeID, "application_id": b.ApplicationID, "action_kind": b.ActionKind, "action_ref": b.ActionRef, "note": "routine execution must instantiate a normal project Task; binding grants no authority"})
		return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: &p.WorkspaceID, Type: "project_routine.bound", AggregateType: "project_routine_binding", AggregateID: b.ID, ActorPrincipalID: &cmd.CreatedBy, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now})
	})
	if err != nil {
		return RoutineBinding{}, err
	}
	return b, nil
}

func (s *Service) requireActor(ctx context.Context, tx storage.Tx, workspaceID, principalID string) error {
	st, err := s.repo.ActorStateTx(ctx, tx, workspaceID, principalID)
	if err != nil {
		return err
	}
	if st.WorkspaceStatus != "active" {
		return ErrWorkspaceInactive
	}
	if st.PrincipalStatus != "active" {
		return ErrPrincipalIneligible
	}
	switch st.PrincipalType {
	case "system", "recovery", "watchdog":
		return nil
	case "human", "agent", "service":
		if st.MembershipStatus == nil || *st.MembershipStatus != "active" {
			return ErrPrincipalIneligible
		}
		return nil
	default:
		return ErrPrincipalIneligible
	}
}
