package inference

import (
	"context"

	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type requestRepository interface {
	Get(context.Context, string) (InferenceRequest, error)
	Insert(context.Context, storage.Tx, InferenceRequest) error
	Transition(context.Context, storage.Tx, string, RequestStatus, RequestStatus, int64, *string, *string, *string, jsonFields) error
	PrincipalEligibleTx(context.Context, storage.Tx, string, string) (bool, error)
	TaskWorkspaceTx(context.Context, storage.Tx, string) (string, error)
}

type jsonFields struct {
	ResponseArtifactID *string
	UsageJSON          []byte
	ErrorCode          *string
	CompletedAt        *int64
}
