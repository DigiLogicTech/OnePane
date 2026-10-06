package agentruntime

import (
	"context"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type repository interface {
	Get(context.Context, string) (Connection, error)
	GetTx(context.Context, storage.Tx, string) (Connection, error)
	Insert(context.Context, storage.Tx, Connection) error
	UpdateStatus(context.Context, storage.Tx, Connection, Status, int64) error
	WorkspaceStatusTx(context.Context, storage.Tx, string) (string, error)
	NodeTrustTx(context.Context, storage.Tx, string) (string, bool, error)
}
