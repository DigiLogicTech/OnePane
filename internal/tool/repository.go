package tool

import (
	"context"

	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type repository interface {
	Get(context.Context, string) (Invocation, error)
	Insert(context.Context, storage.Tx, Invocation) error
	Transition(context.Context, storage.Tx, Transition) error
	ValidateExecutionContext(context.Context, string, string, *string, *string) error
}
