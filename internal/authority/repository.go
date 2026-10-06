package authority

import (
	"context"

	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type repository interface {
	Get(context.Context, string) (Lease, error)
	GetForUpdate(context.Context, storage.Tx, string) (Lease, error)
	Insert(context.Context, storage.Tx, Lease) error
	Transition(context.Context, storage.Tx, string, int64, Status, Status) error
	ConsumeAt(context.Context, storage.Tx, Lease, int64, Status, int64) error
	Subject(context.Context, string, string) (Subject, error)
	SubjectTx(context.Context, storage.Tx, string, string) (Subject, error)
	TaskWorkspace(context.Context, string) (string, error)
	TaskWorkspaceTx(context.Context, storage.Tx, string) (string, error)
}
