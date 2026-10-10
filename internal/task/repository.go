package task

import (
	"context"

	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type repository interface {
	Get(context.Context, string) (Task, error)
	List(context.Context, string, int) ([]Task, error)
	ListArchived(context.Context, string, int) ([]Task, error)
	GetForUpdate(context.Context, storage.Tx, string) (Task, error)
	Insert(context.Context, storage.Tx, Task) error
	SetArchived(context.Context, storage.Tx, string, int64, *int64, int64) error
	Transition(context.Context, storage.Tx, transitionRecord) error
	NextAttemptNumber(context.Context, storage.Tx, string) (int64, error)
	InsertAttempt(context.Context, storage.Tx, Attempt) error
	ActiveAttempt(context.Context, storage.Tx, string) (*Attempt, error)
	HardDependenciesSatisfied(context.Context, storage.Tx, string) (bool, error)
	TransitionAttempt(context.Context, storage.Tx, string, AttemptState, AttemptState, int64) error
	ValidCompletionEvidence(context.Context, storage.Tx, string, string) (bool, error)
}

type transitionRecord struct {
	TaskID            string
	ExpectedRevision  int64
	From              State
	To                State
	UpdatedAt         int64
	ReadyAt           *int64
	CancelRequestedAt *int64
	Result            []byte
}
