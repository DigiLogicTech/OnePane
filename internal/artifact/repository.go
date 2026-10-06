package artifact

import (
	"context"

	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type repository interface {
	Get(context.Context, string) (Artifact, error)
	Insert(context.Context, storage.Tx, Artifact) error
	WorkspaceStatus(context.Context, storage.Tx, string) (string, error)
	ProjectWorkspace(context.Context, storage.Tx, string) (string, error)
	PrincipalEligible(context.Context, storage.Tx, string, string) (bool, error)
}
