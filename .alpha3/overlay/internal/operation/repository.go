package operation

import (
	"context"

	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type repository interface {
	Get(context.Context, string) (Operation, error)
	GetTx(context.Context, storage.Tx, string) (Operation, error)
	ByIdempotency(context.Context, string, string) (Operation, error)
	ListByStates(context.Context, ...State) ([]Operation, error)
	Insert(context.Context, storage.Tx, Operation) error
	Transition(context.Context, storage.Tx, string, int64, State, State, int64) error
	SetAuthorization(context.Context, storage.Tx, string, int64, int64, string, int64, string, string, int64) error
	SetResourceLease(context.Context, storage.Tx, string, int64, string, *string, int64) error

	AcquireResourceLease(context.Context, storage.Tx, ResourceLease) error
	ResourceLeaseTx(context.Context, storage.Tx, string) (ResourceLease, error)
	ReleaseResourceLease(context.Context, storage.Tx, string, int64, int64) error
	RenewResourceLease(context.Context, storage.Tx, string, int64, int64) error
	ActiveResourceLeaseForResource(context.Context, storage.Tx, string, string) (*ResourceLease, error)
	HasUnknownOutcomeForResource(context.Context, string, string, string) (bool, error)
	StaleValidCheckpointsForTask(context.Context, storage.Tx, string, int64) ([]string, error)

	InsertReceipt(context.Context, storage.Tx, Receipt) error
	VerificationForOperation(context.Context, storage.Tx, string, string) (verificationRecord, error)
}

type verificationRecord struct {
	ID            string
	OperationID   *string
	RequiredLevel string
	AchievedLevel *string
	Status        string
}
