package localai

import (
	"context"

	"github.com/DigiLogicTech/OnePane/internal/teamgate"
)

func (s *Service) AuthorizeLocalTaskExecution(ctx context.Context, workspaceID string, taskID *string) error {
	if taskID == nil {
		return nil
	}
	_, err := teamgate.Require(ctx, s.db, workspaceID, *taskID)
	return err
}
