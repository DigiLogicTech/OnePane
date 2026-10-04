package projectworkspace

import (
	"context"
	"encoding/json"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type actorState struct {
	WorkspaceStatus, PrincipalStatus, PrincipalType string
	MembershipStatus                                *string
}
type repository interface {
	Project(context.Context, string) (Project, error)
	Projects(context.Context, string) ([]Project, error)
	ProjectTx(context.Context, storage.Tx, string) (Project, error)
	InsertProject(context.Context, storage.Tx, Project) error
	UpdateProjectPolicy(context.Context, storage.Tx, Project, json.RawMessage, int64) error
	Runtime(context.Context, string) (ProjectRuntime, error)
	RuntimeTx(context.Context, storage.Tx, string) (ProjectRuntime, error)
	RuntimeByProject(context.Context, string) (ProjectRuntime, error)
	InsertRuntime(context.Context, storage.Tx, ProjectRuntime) error
	UpdateRuntimeDesired(context.Context, storage.Tx, ProjectRuntime, RuntimeDesiredState, int64) error
	UpdateRuntimePolicy(context.Context, storage.Tx, ProjectRuntime, json.RawMessage, json.RawMessage, int64) error
	UpdateRuntimeObserved(context.Context, storage.Tx, ProjectRuntime, RuntimeStatus, int64) error
	Application(context.Context, string) (Application, error)
	ListApplications(context.Context, string) ([]Application, error)
	ApplicationTx(context.Context, storage.Tx, string) (Application, error)
	InsertApplication(context.Context, storage.Tx, Application) error
	UpdateApplicationObserved(context.Context, storage.Tx, Application, AppStatus, int64) error
	InsertEndpoint(context.Context, storage.Tx, Endpoint) error
	Endpoint(context.Context, string) (Endpoint, error)
	EndpointTx(context.Context, storage.Tx, string) (Endpoint, error)
	ListEndpoints(context.Context, string) ([]Endpoint, error)
	EndpointRoute(context.Context, string) (EndpointRoute, error)
	IngressRoute(context.Context, string) (IngressRoute, error)
	ListEndpointRoutes(context.Context, string) ([]EndpointRoute, error)
	UpsertEndpointRoute(context.Context, storage.Tx, EndpointRoute) error
	MarkApplicationEndpointRoutesStale(context.Context, storage.Tx, string, int64) error
	MarkApplicationEndpointsUnready(context.Context, storage.Tx, string, int64) error
	SetEndpointObservedRoute(context.Context, storage.Tx, string, int64) error
	Change(context.Context, string) (ChangeProposal, error)
	ListChanges(context.Context, string) ([]ChangeProposal, error)
	ChangeTx(context.Context, storage.Tx, string) (ChangeProposal, error)
	InsertChange(context.Context, storage.Tx, ChangeProposal) error
	UpdateChangeStatus(context.Context, storage.Tx, ChangeProposal, ProposalStatus, string, int64) error
	InsertRoutineBinding(context.Context, storage.Tx, RoutineBinding) error
	RoutineBinding(context.Context, string) (RoutineBinding, error)
	ListRoutineBindings(context.Context, string) ([]RoutineBinding, error)
	ActorStateTx(context.Context, storage.Tx, string, string) (actorState, error)
	NodeExistsTx(context.Context, storage.Tx, string) (bool, error)
	RoutineWorkspaceTx(context.Context, storage.Tx, string) (string, error)
	ArtifactProjectWorkspaceTx(context.Context, storage.Tx, string) (string, *string, error)
	TaskProjectWorkspace(context.Context, string) (string, *string, error)
	TaskProjectWorkspaceTx(context.Context, storage.Tx, string) (string, *string, error)
	RequireVerifiedObservationTx(context.Context, storage.Tx, string, string, string, string) error
	RequireVerifiedEndpointRouteTx(context.Context, storage.Tx, string, string, string, string, int, int, string) error
}
