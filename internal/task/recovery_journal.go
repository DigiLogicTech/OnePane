package task

import (
	"context"
	"database/sql"
	"errors"

	"github.com/DigiLogicTech/OnePane/internal/recoveryinventory"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

var ErrRecoveryJournalUnavailable = errors.New("stored recovery journal unavailable")

// This is an explicit read-only diagnostic, not an inventory, decision or
// independent postcondition receipt. Raw payloads and candidate target IDs are
// intentionally excluded even when they occur in an owned journal.
type RecoveryJournalStep struct {
	ID                  string `json:"id"`
	Number              int64  `json:"number"`
	Kind                string `json:"kind"`
	Status              string `json:"status"`
	MetadataSHA256      string `json:"metadata_sha256"`
	ReferenceRecognized bool   `json:"reference_recognized"`
}

type RecoveryJournalView struct {
	CaseID             string                `json:"case_id"`
	TaskID             string                `json:"task_id"`
	AttemptID          string                `json:"attempt_id"`
	Phase              string                `json:"phase"`
	TaskRevision       int64                 `json:"task_revision"`
	Availability       string                `json:"availability"`
	RunID              string                `json:"run_id,omitempty"`
	ObservationVersion string                `json:"observation_version,omitempty"`
	SnapshotSHA256     string                `json:"snapshot_sha256,omitempty"`
	Steps              []RecoveryJournalStep `json:"steps"`
}

func (s *Service) ObserveRecoveryJournal(ctx context.Context, workspace, taskID, caseID string) (RecoveryJournalView, error) {
	empty := RecoveryJournalView{}
	if workspace == "" || taskID == "" || caseID == "" || len(workspace) > 160 || len(taskID) > 160 || len(caseID) > 160 {
		return empty, ErrInvalidCommand
	}
	var out RecoveryJournalView
	err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		raw, ok := tx.(*sql.Tx)
		if !ok {
			return ErrRecoveryJournalUnavailable
		}
		var scope recoveryinventory.WorkerJournalScope
		scope.WorkspaceID, scope.TaskID = workspace, taskID
		out = RecoveryJournalView{CaseID: caseID, TaskID: taskID, Steps: []RecoveryJournalStep{}}
		err := tx.QueryRowContext(ctx, `SELECT c.attempt_id,c.phase,t.revision,t.project_id,t.project_workspace_id,t.artifact_session_id
 FROM main.task_recovery_cases c JOIN main.tasks t ON t.id=c.task_id AND t.workspace_id=c.workspace_id
 JOIN main.task_attempts a ON a.id=c.attempt_id AND a.task_id=t.id AND a.status='interrupted'
 WHERE c.id=? AND c.workspace_id=? AND c.task_id=? AND c.phase='awaiting_inventory'
 AND length(CAST(c.attempt_id AS BLOB))<=160
 AND (t.project_id IS NULL OR length(CAST(t.project_id AS BLOB))<=160)
 AND (t.project_workspace_id IS NULL OR length(CAST(t.project_workspace_id AS BLOB))<=160)
 AND (t.artifact_session_id IS NULL OR length(CAST(t.artifact_session_id AS BLOB))<=160)`, caseID, workspace, taskID).Scan(
			&out.AttemptID, &out.Phase, &out.TaskRevision, &scope.ProjectID, &scope.ProjectWorkspaceID, &scope.ArtifactSessionID)
		if err != nil {
			return err
		}
		scope.AttemptID = out.AttemptID
		var runWorkspace, runTask string
		err = tx.QueryRowContext(ctx, `SELECT CASE WHEN length(CAST(id AS BLOB))<=160 THEN id ELSE '' END,
 CASE WHEN length(CAST(workspace_id AS BLOB))<=160 THEN workspace_id ELSE '' END,
 CASE WHEN length(CAST(task_id AS BLOB))<=160 THEN task_id ELSE '' END FROM main.agent_worker_runs WHERE attempt_id=?`, scope.AttemptID).Scan(&scope.RunID, &runWorkspace, &runTask)
		if errors.Is(err, sql.ErrNoRows) {
			out.Availability = "not_recorded"
			return nil
		}
		if err != nil {
			return err
		}
		if scope.RunID == "" || runWorkspace != workspace || runTask != taskID {
			return ErrRecoveryJournalUnavailable
		}
		journal, err := recoveryinventory.ObserveWorkerJournalSlice(ctx, raw, scope)
		if err != nil {
			return err
		}
		out.Availability = "recorded"
		out.RunID = scope.RunID
		out.ObservationVersion = journal.Version
		out.SnapshotSHA256 = journal.Fingerprint
		for _, step := range journal.Steps {
			if !knownRecoveryJournalKind(step.Kind) || !knownRecoveryJournalStatus(step.Status) {
				return ErrRecoveryJournalUnavailable
			}
			out.Steps = append(out.Steps, RecoveryJournalStep{ID: step.ID, Number: step.Number, Kind: step.Kind, Status: step.Status, MetadataSHA256: step.RowSHA256, ReferenceRecognized: step.ResultReferences.State == "known_candidate"})
		}
		return nil
	})
	if err != nil {
		return empty, err
	}
	return out, nil
}

func knownRecoveryJournalKind(value string) bool {
	switch value {
	case "route", "model", "agent_runtime", "tool", "operation", "delegate", "replan", "complete", "human", "escalate", "wait", "fail", "verification":
		return true
	}
	return false
}
func knownRecoveryJournalStatus(value string) bool {
	switch value {
	case "started", "succeeded", "failed", "waiting", "unknown":
		return true
	}
	return false
}
