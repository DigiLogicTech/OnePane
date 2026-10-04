package inference

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/artifact"
	"github.com/DigiLogicTech/OnePane/internal/budget"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/id"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type RuntimeScheduleRequest struct {
	WorkspaceID        string
	TaskID             *string
	InferenceRequestID string
	Deployment         ModelDeployment
	Priority           string
}

type RuntimeScheduleLease interface {
	ReportBoundary(context.Context, string) error
	Release(context.Context, error)
}

type RuntimeCoordinator interface {
	Acquire(context.Context, RuntimeScheduleRequest) (RuntimeScheduleLease, error)
}

type Service struct {
	repo               repository
	requests           requestRepository
	tx                 storage.Transactor
	events             event.Store
	ids                id.Generator
	clock              clock.Clock
	artifacts          *artifact.Service
	budgets            *budget.Service
	transports         *TransportRegistry
	secrets            SecretResolver
	localNodeID        string
	runtimeCoordinator RuntimeCoordinator
}

func NewService(db *sql.DB, tx storage.Transactor, clk clock.Clock) *Service {
	return &Service{repo: newSQLRepository(db), requests: newSQLRequestRepository(db), tx: tx, events: event.Store{}, ids: id.Generator{}, clock: clk}
}

func (s *Service) ConfigureExecution(artifacts *artifact.Service, transports *TransportRegistry, secrets SecretResolver, localNodeID string) error {
	if artifacts == nil || transports == nil || strings.TrimSpace(localNodeID) == "" {
		return fmt.Errorf("%w: artifact service, transport registry and local node are required", ErrExecutionUnavailable)
	}
	s.artifacts = artifacts
	s.transports = transports
	s.secrets = secrets
	s.localNodeID = strings.TrimSpace(localNodeID)
	return nil
}

func (s *Service) ConfigureRuntimeCoordinator(c RuntimeCoordinator) error {
	if c == nil {
		return fmt.Errorf("%w: runtime coordinator is required", ErrExecutionUnavailable)
	}
	s.runtimeCoordinator = c
	return nil
}

func (s *Service) ConfigureBudget(b *budget.Service) error {
	if b == nil {
		return fmt.Errorf("%w: budget service is required", ErrExecutionUnavailable)
	}
	s.budgets = b
	return nil
}

func (s *Service) Provider(ctx context.Context, id string) (ProviderConnection, error) {
	if strings.TrimSpace(id) == "" {
		return ProviderConnection{}, fmt.Errorf("%w: provider id required", ErrInvalidCommand)
	}
	return s.repo.Provider(ctx, id)
}
func (s *Service) Providers(ctx context.Context, workspaceID *string) ([]ProviderConnection, error) {
	if workspaceID != nil {
		v := strings.TrimSpace(*workspaceID)
		if v == "" {
			workspaceID = nil
		} else {
			workspaceID = &v
		}
	}
	return s.repo.Providers(ctx, workspaceID)
}
func (s *Service) Model(ctx context.Context, id string) (Model, error) {
	if strings.TrimSpace(id) == "" {
		return Model{}, fmt.Errorf("%w: model id required", ErrInvalidCommand)
	}
	return s.repo.Model(ctx, id)
}
func (s *Service) Deployment(ctx context.Context, id string) (ModelDeployment, error) {
	if strings.TrimSpace(id) == "" {
		return ModelDeployment{}, fmt.Errorf("%w: deployment id required", ErrInvalidCommand)
	}
	return s.repo.Deployment(ctx, id)
}

func (s *Service) RegisterProvider(ctx context.Context, cmd RegisterProviderCommand) (ProviderConnection, error) {
	if strings.TrimSpace(cmd.Provider) == "" || strings.TrimSpace(cmd.DisplayName) == "" || strings.TrimSpace(cmd.AuthType) == "" {
		return ProviderConnection{}, fmt.Errorf("%w: provider, display name and auth type are required", ErrInvalidCommand)
	}
	canonical, err := normalizeProviderConnectionJSON(cmd.ConnectionJSON)
	if err != nil {
		return ProviderConnection{}, fmt.Errorf("%w: connection_json: %v", ErrInvalidCommand, err)
	}
	idv, err := s.ids.New("provider")
	if err != nil {
		return ProviderConnection{}, err
	}
	evt, err := s.ids.New("evt")
	if err != nil {
		return ProviderConnection{}, err
	}
	now := s.clock.UnixMilli()
	p := ProviderConnection{ID: idv, WorkspaceID: cmd.WorkspaceID, Provider: strings.TrimSpace(cmd.Provider), DisplayName: strings.TrimSpace(cmd.DisplayName), AuthType: strings.TrimSpace(cmd.AuthType), SecretRef: cmd.SecretRef, Status: ProviderUnavailable, ConnectionJSON: canonical, Revision: 1, CreatedAt: now, UpdatedAt: now}
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if p.WorkspaceID != nil {
			status, err := s.repo.WorkspaceStatusTx(ctx, tx, *p.WorkspaceID)
			if err != nil {
				return fmt.Errorf("resolve provider workspace: %w", err)
			}
			if status != "active" {
				return ErrWorkspaceInactive
			}
		}
		if err := s.repo.InsertProvider(ctx, tx, p); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"provider_connection_id": p.ID, "provider": p.Provider, "status": p.Status, "revision": p.Revision, "workspace_id": p.WorkspaceID})
		return s.events.Append(ctx, tx, event.Event{ID: evt, WorkspaceID: p.WorkspaceID, Type: "provider_connection.registered", AggregateType: "provider_connection", AggregateID: p.ID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now})
	})
	if err != nil {
		return ProviderConnection{}, err
	}
	return s.repo.Provider(ctx, p.ID)
}

func (s *Service) SetProviderStatus(ctx context.Context, cmd SetProviderStatusCommand) (ProviderConnection, error) {
	if strings.TrimSpace(cmd.ConnectionID) == "" || cmd.ExpectedRevision < 1 || !ValidProviderStatus(cmd.Status) {
		return ProviderConnection{}, fmt.Errorf("%w: provider id, revision and valid status required", ErrInvalidCommand)
	}
	evt, err := s.ids.New("evt")
	if err != nil {
		return ProviderConnection{}, err
	}
	now := s.clock.UnixMilli()
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		p, err := s.repo.ProviderTx(ctx, tx, cmd.ConnectionID)
		if err != nil {
			return err
		}
		if p.Revision != cmd.ExpectedRevision {
			return ErrRevisionConflict
		}
		if p.Status == ProviderRevoked {
			return ErrProviderRevoked
		}
		if p.Status == cmd.Status {
			return fmt.Errorf("%w: status unchanged", ErrInvalidTransition)
		}
		if cmd.Status == ProviderRateLimited && cmd.RetryAfter == nil {
			return fmt.Errorf("%w: rate_limited requires retry_after", ErrInvalidCommand)
		}
		retry := cmd.RetryAfter
		if cmd.Status != ProviderRateLimited {
			retry = nil
		}
		if err := s.repo.UpdateProviderStatus(ctx, tx, p, cmd.Status, retry, now); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"provider_connection_id": p.ID, "from": p.Status, "to": cmd.Status, "retry_after": retry, "reason": strings.TrimSpace(cmd.Reason), "revision": p.Revision + 1})
		return s.events.Append(ctx, tx, event.Event{ID: evt, WorkspaceID: p.WorkspaceID, Type: "provider_connection.status_changed", AggregateType: "provider_connection", AggregateID: p.ID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now})
	})
	if err != nil {
		return ProviderConnection{}, err
	}
	return s.repo.Provider(ctx, cmd.ConnectionID)
}

func (s *Service) RegisterModel(ctx context.Context, cmd RegisterModelCommand) (Model, error) {
	if strings.TrimSpace(cmd.ModelRef) == "" || !ValidModelTrustState(cmd.TrustState) || cmd.TrustState == ModelTrusted {
		return Model{}, fmt.Errorf("%w: model_ref and valid trust state required", ErrInvalidCommand)
	}
	modalities, err := normalizeJSON(cmd.ModalitiesJSON, "[]")
	if err != nil {
		return Model{}, fmt.Errorf("%w: modalities_json: %v", ErrInvalidCommand, err)
	}
	metadata, err := normalizeJSON(cmd.StaticMetadataJSON, "{}")
	if err != nil {
		return Model{}, fmt.Errorf("%w: static_metadata_json: %v", ErrInvalidCommand, err)
	}
	idv, err := s.ids.New("model")
	if err != nil {
		return Model{}, err
	}
	evt, err := s.ids.New("evt")
	if err != nil {
		return Model{}, err
	}
	now := s.clock.UnixMilli()
	m := Model{ID: idv, ProviderName: cmd.ProviderName, ModelRef: strings.TrimSpace(cmd.ModelRef), Architecture: cmd.Architecture, RevisionRef: cmd.RevisionRef, WeightsHash: cmd.WeightsHash, Quantization: cmd.Quantization, ModalitiesJSON: modalities, StaticMetadataJSON: metadata, TrustState: cmd.TrustState, CreatedAt: now, UpdatedAt: now}
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if err := s.repo.InsertModel(ctx, tx, m); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"model_id": m.ID, "model_ref": m.ModelRef, "trust_state": m.TrustState})
		return s.events.Append(ctx, tx, event.Event{ID: evt, Type: "model.registered", AggregateType: "model", AggregateID: m.ID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now})
	})
	if err != nil {
		return Model{}, err
	}
	return s.repo.Model(ctx, m.ID)
}

func (s *Service) SetModelTrust(ctx context.Context, cmd SetModelTrustCommand) (Model, error) {
	if strings.TrimSpace(cmd.ModelID) == "" || !ValidModelTrustState(cmd.ExpectedTrustState) || !ValidModelTrustState(cmd.TrustState) || cmd.ExpectedTrustState == cmd.TrustState || cmd.TrustState == ModelTrusted {
		return Model{}, fmt.Errorf("%w: model id and distinct valid trust states required", ErrInvalidCommand)
	}
	if cmd.ExpectedTrustState == ModelRevoked {
		return Model{}, ErrModelRevoked
	}
	evt, err := s.ids.New("evt")
	if err != nil {
		return Model{}, err
	}
	now := s.clock.UnixMilli()
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		m, err := s.repo.ModelTx(ctx, tx, cmd.ModelID)
		if err != nil {
			return err
		}
		if m.TrustState != cmd.ExpectedTrustState {
			return ErrRevisionConflict
		}
		if m.TrustState == ModelRevoked {
			return ErrModelRevoked
		}
		if err := s.repo.UpdateModelTrust(ctx, tx, m, cmd.TrustState, now); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"model_id": m.ID, "from": m.TrustState, "to": cmd.TrustState, "reason": strings.TrimSpace(cmd.Reason)})
		return s.events.Append(ctx, tx, event.Event{ID: evt, Type: "model.trust_changed", AggregateType: "model", AggregateID: m.ID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now})
	})
	if err != nil {
		return Model{}, err
	}
	return s.repo.Model(ctx, cmd.ModelID)
}

func (s *Service) RegisterDeployment(ctx context.Context, cmd RegisterDeploymentCommand) (ModelDeployment, error) {
	if strings.TrimSpace(cmd.ModelID) == "" || (cmd.NodeID == nil && cmd.ProviderConnectionID == nil) {
		return ModelDeployment{}, fmt.Errorf("%w: model and node/provider binding required", ErrInvalidCommand)
	}
	if cmd.ContextMaxReported != nil && *cmd.ContextMaxReported <= 0 {
		return ModelDeployment{}, fmt.Errorf("%w: context max must be positive", ErrInvalidCommand)
	}
	config, err := normalizeJSON(cmd.RuntimeConfigJSON, "{}")
	if err != nil {
		return ModelDeployment{}, fmt.Errorf("%w: runtime_config_json: %v", ErrInvalidCommand, err)
	}
	if err := rejectSecretLikeJSON(config); err != nil {
		return ModelDeployment{}, err
	}
	idv, err := s.ids.New("deployment")
	if err != nil {
		return ModelDeployment{}, err
	}
	evt, err := s.ids.New("evt")
	if err != nil {
		return ModelDeployment{}, err
	}
	now := s.clock.UnixMilli()
	d := ModelDeployment{ID: idv, ModelID: cmd.ModelID, NodeID: cmd.NodeID, ProviderConnectionID: cmd.ProviderConnectionID, RuntimeName: cmd.RuntimeName, RuntimeVersion: cmd.RuntimeVersion, RuntimeConfigJSON: config, Status: DeploymentDiscovered, ContextMaxReported: cmd.ContextMaxReported, DeploymentFingerprint: deploymentFingerprint(cmd, config), Revision: 1, DiscoveredAt: now, UpdatedAt: now}
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		m, err := s.repo.ModelTx(ctx, tx, cmd.ModelID)
		if err != nil {
			return fmt.Errorf("resolve model: %w", err)
		}
		if m.TrustState == ModelRevoked {
			return ErrModelRevoked
		}
		if cmd.NodeID != nil {
			ok, err := s.repo.NodeExistsTx(ctx, tx, *cmd.NodeID)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("%w: node does not exist", ErrInvalidCommand)
			}
		}
		if cmd.ProviderConnectionID != nil {
			p, err := s.repo.ProviderTx(ctx, tx, *cmd.ProviderConnectionID)
			if err != nil {
				return fmt.Errorf("resolve provider: %w", err)
			}
			if p.Status == ProviderRevoked {
				return ErrProviderRevoked
			}
		}
		if err := s.repo.InsertDeployment(ctx, tx, d); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"deployment_id": d.ID, "model_id": d.ModelID, "node_id": d.NodeID, "provider_connection_id": d.ProviderConnectionID, "fingerprint": d.DeploymentFingerprint, "status": d.Status, "revision": d.Revision})
		return s.events.Append(ctx, tx, event.Event{ID: evt, Type: "model_deployment.registered", AggregateType: "model_deployment", AggregateID: d.ID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now})
	})
	if err != nil {
		return ModelDeployment{}, err
	}
	return s.repo.Deployment(ctx, d.ID)
}

func (s *Service) SetDeploymentStatus(ctx context.Context, cmd SetDeploymentStatusCommand) (ModelDeployment, error) {
	if strings.TrimSpace(cmd.DeploymentID) == "" || cmd.ExpectedRevision < 1 || !ValidDeploymentStatus(cmd.Status) {
		return ModelDeployment{}, fmt.Errorf("%w: deployment id, revision and valid status required", ErrInvalidCommand)
	}
	if cmd.ResidencyState != nil && !ValidResidencyState(*cmd.ResidencyState) {
		return ModelDeployment{}, fmt.Errorf("%w: invalid residency state", ErrInvalidCommand)
	}
	evt, err := s.ids.New("evt")
	if err != nil {
		return ModelDeployment{}, err
	}
	now := s.clock.UnixMilli()
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		d, err := s.repo.DeploymentTx(ctx, tx, cmd.DeploymentID)
		if err != nil {
			return err
		}
		if d.Revision != cmd.ExpectedRevision {
			return ErrRevisionConflict
		}
		if !CanDeploymentTransition(d.Status, cmd.Status) {
			return ErrInvalidTransition
		}
		m, err := s.repo.ModelTx(ctx, tx, d.ModelID)
		if err != nil {
			return err
		}
		if m.TrustState == ModelRevoked {
			return ErrModelRevoked
		}
		if cmd.Status == DeploymentReady && m.TrustState == ModelQuarantined {
			return ErrModelQuarantined
		}
		if d.ProviderConnectionID != nil {
			p, err := s.repo.ProviderTx(ctx, tx, *d.ProviderConnectionID)
			if err != nil {
				return err
			}
			if p.Status == ProviderRevoked {
				return ErrProviderRevoked
			}
			if cmd.Status == DeploymentReady && p.Status != ProviderConnected && p.Status != ProviderDegraded {
				return fmt.Errorf("%w: provider is not usable", ErrInvalidTransition)
			}
		}
		if err := s.repo.UpdateDeploymentStatus(ctx, tx, d, cmd.Status, cmd.ResidencyState, now); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"deployment_id": d.ID, "from": d.Status, "to": cmd.Status, "residency_state": cmd.ResidencyState, "reason": strings.TrimSpace(cmd.Reason), "revision": d.Revision + 1})
		return s.events.Append(ctx, tx, event.Event{ID: evt, Type: "model_deployment.status_changed", AggregateType: "model_deployment", AggregateID: d.ID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now})
	})
	if err != nil {
		return ModelDeployment{}, err
	}
	return s.repo.Deployment(ctx, cmd.DeploymentID)
}
