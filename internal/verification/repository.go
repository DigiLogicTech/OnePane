package verification

import (
	"context"

	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type repository interface {
	GetVerification(context.Context, string) (Verification, error)
	GetVerificationTx(context.Context, storage.Tx, string) (Verification, error)
	InsertVerification(context.Context, storage.Tx, Verification) error
	ResolveVerification(context.Context, storage.Tx, string, int64, Status, Status, *string, *int64, any, any) error
	TaskWorkspaceTx(context.Context, storage.Tx, string) (string, error)
	OperationWorkspaceTx(context.Context, storage.Tx, string) (string, error)
	VerifierEligibleTx(context.Context, storage.Tx, string, string) (bool, error)

	GetCheckpoint(context.Context, string) (Checkpoint, error)
	LatestValidCheckpoint(context.Context, string) (Checkpoint, error)
	InsertCheckpoint(context.Context, storage.Tx, Checkpoint) error
	SupersedeValidCheckpoints(context.Context, storage.Tx, string, string, int64) ([]string, error)
	InvalidateCheckpoint(context.Context, storage.Tx, string, CheckpointStatus, CheckpointStatus, int64) error
	TaskHasUnknownMutationTx(context.Context, storage.Tx, string) (bool, error)
}
