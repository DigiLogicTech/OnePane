package observation

import (
	"context"

	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type repository interface {
	Get(context.Context, string) (Observation, error)
	Latest(context.Context, string, string, string) (Observation, error)
	Insert(context.Context, storage.Tx, Observation) error
	WorkspaceStatus(context.Context, storage.Tx, string) (string, error)
	SourceEligible(context.Context, storage.Tx, string, string) (bool, error)
}
