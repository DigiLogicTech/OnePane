package agentruntime

import (
	"context"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type invocationRepository interface {
	Get(context.Context, string) (Invocation, error)
	GetTx(context.Context, storage.Tx, string) (Invocation, error)
	Insert(context.Context, storage.Tx, Invocation) error
	Transition(context.Context, storage.Tx, Invocation, InvocationStatus, *string, *string, *int64, int64) error
	PrincipalEligibleTx(context.Context, storage.Tx, string, string) (bool, error)
	TaskWorkspaceTx(context.Context, storage.Tx, string) (string, error)
	AttemptTaskTx(context.Context, storage.Tx, string) (string, error)
}
