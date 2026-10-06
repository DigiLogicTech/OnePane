package agentruntime

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

type Service struct {
	repo                repository
	invocations         invocationRepository
	tx                  storage.Transactor
	events              event.Store
	ids                 id.Generator
	clock               clock.Clock
	registry            *Registry
	artifacts           *artifact.Service
	budgets             *budget.Service
	transports          *RuntimeTransportRegistry
	secrets             SecretResolver
	localNodeID         string
	credentialValidator interface {
		ValidateAgentRuntimeCredentialRef(context.Context, string, string) error
	}
}

func NewService(db *sql.DB, tx storage.Transactor, registry *Registry, clk clock.Clock) *Service {
	return &Service{repo: newSQLRepository(db), invocations: newSQLInvocationRepository(db), tx: tx, events: event.Store{}, ids: id.Generator{}, clock: clk, registry: registry}
}

func (s *Service) ConfigureExecution(artifacts *artifact.Service, transports *RuntimeTransportRegistry, secrets SecretResolver, localNodeID string) error {
	if artifacts == nil || transports == nil || strings.TrimSpace(localNodeID) == "" {
		return ErrRuntimeExecutionUnavailable
	}
	s.artifacts = artifacts
	s.transports = transports
	s.secrets = secrets
	s.localNodeID = strings.TrimSpace(localNodeID)
	return nil
}
func (s *Service) ConfigureBudget(b *budget.Service) error {
	if b == nil {
		return ErrRuntimeExecutionUnavailable
	}
	s.budgets = b
	return nil
}

func (s *Service) SetCredentialValidator(v interface {
	ValidateAgentRuntimeCredentialRef(context.Context, string, string) error
}) {
	s.credentialValidator = v
}

func (s *Service) Get(ctx context.Context, id string) (Connection, error) {
	if strings.TrimSpace(id) == "" {
		return Connection{}, fmt.Errorf("%w: connection id required", ErrInvalidCommand)
	}
	return s.repo.Get(ctx, id)
}

func (s *Service) Register(ctx context.Context, cmd RegisterCommand) (Connection, error) {
	if strings.TrimSpace(cmd.RuntimeKind) == "" || strings.TrimSpace(cmd.DisplayName) == "" || strings.TrimSpace(cmd.AdapterName) == "" || strings.TrimSpace(cmd.AdapterVersion) == "" || strings.TrimSpace(cmd.AuthType) == "" || !ValidTrustState(cmd.TrustState) || cmd.TrustState == TrustTrustedAdapter || !ValidOperatingMode(cmd.OperatingMode) {
		return Connection{}, fmt.Errorf("%w: runtime identity, adapter, auth, trust and mode are required", ErrInvalidCommand)
	}
	adapter, ok := s.registry.Resolve(cmd.AdapterName, cmd.AdapterVersion)
	if !ok {
		return Connection{}, ErrAdapterUnavailable
	}
	if !adapter.Supports(cmd.OperatingMode) {
		return Connection{}, fmt.Errorf("%w: adapter does not support mode %q", ErrInvalidCommand, cmd.OperatingMode)
	}
	endpoint, err := canonicalJSON(cmd.EndpointJSON, "{}")
	if err != nil {
		return Connection{}, fmt.Errorf("%w: endpoint_json: %v", ErrInvalidCommand, err)
	}
	if err := rejectSecretLikeJSON(endpoint); err != nil {
		return Connection{}, err
	}
	protocol, err := canonicalJSON(cmd.ProtocolJSON, `{"agent_protocol":"v1"}`)
	if err != nil {
		return Connection{}, fmt.Errorf("%w: protocol_json: %v", ErrInvalidCommand, err)
	}
	caps, err := canonicalJSON(cmd.CapabilitiesJSON, "{}")
	if err != nil {
		return Connection{}, fmt.Errorf("%w: capabilities_json: %v", ErrInvalidCommand, err)
	}
	data, err := normalizeDataPolicy(cmd.DataPolicyJSON)
	if err != nil {
		return Connection{}, fmt.Errorf("%w: data_policy_json: %v", ErrInvalidCommand, err)
	}
	idv, err := s.ids.New("agentrt")
	if err != nil {
		return Connection{}, err
	}
	evt, err := s.ids.New("evt")
	if err != nil {
		return Connection{}, err
	}
	now := s.clock.UnixMilli()
	c := Connection{ID: idv, WorkspaceID: cmd.WorkspaceID, NodeID: cmd.NodeID, RuntimeKind: strings.TrimSpace(cmd.RuntimeKind), DisplayName: strings.TrimSpace(cmd.DisplayName), AdapterName: adapter.Name, AdapterVersion: adapter.Version, EndpointJSON: endpoint, AuthType: strings.TrimSpace(cmd.AuthType), SecretRef: cmd.SecretRef, Status: StatusRegistered, TrustState: cmd.TrustState, OperatingMode: cmd.OperatingMode, ProtocolJSON: protocol, CapabilitiesJSON: caps, DataPolicyJSON: data, Revision: 1, CreatedAt: now, UpdatedAt: now}
	if cmd.OperatingMode == Unmanaged && cmd.TrustState == TrustTrustedAdapter {
		return Connection{}, fmt.Errorf("%w: unmanaged runtimes cannot be trusted adapters", ErrInvalidCommand)
	}
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if c.NodeID != nil {
			trust, local, err := s.repo.NodeTrustTx(ctx, tx, *c.NodeID)
			if err != nil {
				return fmt.Errorf("resolve runtime node: %w", err)
			}
			var dp DataPolicy
			if err := json.Unmarshal(c.DataPolicyJSON, &dp); err != nil {
				return err
			}
			if dp.DestinationKind == "origin_node" && !local {
				return fmt.Errorf("%w: origin_node runtime must bind the local node", ErrInvalidCommand)
			}
			if dp.DestinationKind == "trusted_node" && trust != "local" && trust != "paired" {
				return fmt.Errorf("%w: trusted_node runtime requires a local/paired node", ErrInvalidCommand)
			}
		}
		if c.WorkspaceID != nil {
			st, err := s.repo.WorkspaceStatusTx(ctx, tx, *c.WorkspaceID)
			if err != nil {
				return fmt.Errorf("resolve runtime workspace: %w", err)
			}
			if st != "active" {
				return ErrWorkspaceInactive
			}
		}
		if err := s.repo.Insert(ctx, tx, c); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"agent_runtime_connection_id": c.ID, "runtime_kind": c.RuntimeKind, "node_id": c.NodeID, "adapter": c.AdapterName + "@" + c.AdapterVersion, "operating_mode": c.OperatingMode, "trust_state": c.TrustState, "status": c.Status, "revision": c.Revision})
		return s.events.Append(ctx, tx, event.Event{ID: evt, WorkspaceID: c.WorkspaceID, Type: "agent_runtime.registered", AggregateType: "agent_runtime_connection", AggregateID: c.ID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now})
	})
	if err != nil {
		return Connection{}, err
	}
	return s.repo.Get(ctx, c.ID)
}

func (s *Service) SetStatus(ctx context.Context, cmd SetStatusCommand) (Connection, error) {
	if strings.TrimSpace(cmd.ConnectionID) == "" || cmd.ExpectedRevision < 1 || !ValidStatus(cmd.Status) {
		return Connection{}, fmt.Errorf("%w: connection, revision and valid status required", ErrInvalidCommand)
	}
	evt, err := s.ids.New("evt")
	if err != nil {
		return Connection{}, err
	}
	now := s.clock.UnixMilli()
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		c, err := s.repo.GetTx(ctx, tx, cmd.ConnectionID)
		if err != nil {
			return err
		}
		if c.Revision != cmd.ExpectedRevision {
			return ErrRevisionConflict
		}
		if !CanTransition(c.Status, cmd.Status) {
			return ErrInvalidTransition
		}
		if err := s.repo.UpdateStatus(ctx, tx, c, cmd.Status, now); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"agent_runtime_connection_id": c.ID, "from": c.Status, "to": cmd.Status, "reason": strings.TrimSpace(cmd.Reason), "revision": c.Revision + 1})
		return s.events.Append(ctx, tx, event.Event{ID: evt, WorkspaceID: c.WorkspaceID, Type: "agent_runtime.status_changed", AggregateType: "agent_runtime_connection", AggregateID: c.ID, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID, Payload: payload, OccurredAt: now})
	})
	if err != nil {
		return Connection{}, err
	}
	return s.repo.Get(ctx, cmd.ConnectionID)
}
