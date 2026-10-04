package inference

import (
	"context"

	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type repository interface {
	Provider(context.Context, string) (ProviderConnection, error)
	Providers(context.Context, *string) ([]ProviderConnection, error)
	ProviderTx(context.Context, storage.Tx, string) (ProviderConnection, error)
	InsertProvider(context.Context, storage.Tx, ProviderConnection) error
	UpdateProviderStatus(context.Context, storage.Tx, ProviderConnection, ProviderStatus, *int64, int64) error

	Model(context.Context, string) (Model, error)
	ModelTx(context.Context, storage.Tx, string) (Model, error)
	InsertModel(context.Context, storage.Tx, Model) error
	UpdateModelTrust(context.Context, storage.Tx, Model, ModelTrustState, int64) error

	Deployment(context.Context, string) (ModelDeployment, error)
	DeploymentTx(context.Context, storage.Tx, string) (ModelDeployment, error)
	InsertDeployment(context.Context, storage.Tx, ModelDeployment) error
	UpdateDeploymentStatus(context.Context, storage.Tx, ModelDeployment, DeploymentStatus, *ResidencyState, int64) error

	WorkspaceStatusTx(context.Context, storage.Tx, string) (string, error)
	NodeExistsTx(context.Context, storage.Tx, string) (bool, error)
	NodeTrust(context.Context, string) (string, error)
}
