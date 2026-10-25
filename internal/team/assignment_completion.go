package team

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"strings"
	"unicode/utf8"
)

// AssignmentCompletion is a current, ledger-bound observation of one immutable
// accepted assignment. It grants neither execution nor Library content access.
// The comparable identity lets read-only consumers discard a collection whose
// accepted scope or completion evidence changed while it was being collected.
type AssignmentCompletion struct {
	SessionID          string `json:"session_id"`
	WorkspaceID        string `json:"workspace_id"`
	ParentTaskID       string `json:"parent_task_id"`
	TaskID             string `json:"task_id"`
	PlanID             string `json:"plan_id"`
	AssignmentKey      string `json:"assignment_key"`
	MemberID           string `json:"member_id"`
	ProjectID          string `json:"project_id"`
	ProjectWorkspaceID string `json:"project_workspace_id"`
	ArtifactSessionID  string `json:"artifact_session_id,omitempty"`
	TaskRevision       int64  `json:"task_revision"`
	CheckpointID       string `json:"checkpoint_id"`
	VerificationID     string `json:"verification_id"`
	VerificationLevel  string `json:"verification_level"`
}

// Share the entire completion protocol with the live projection. Accepted plan,
// parent, assignment key and member are obtained from the same SQL snapshot
// instead of being supplied from an earlier room read. This navigation reader
// additionally verifies the immutable root criteria seal and payload bounds;
// the legacy live projection does not perform that root criteria check.
var assignmentCompletionReadQuery = strings.Replace(
	strings.Replace(
		strings.Replace(liveAssignmentCompletionQuery,
			"SELECT c.id,v.id,v.achieved_level,t.completion_json,a.completion_sha256",
			`SELECT s.id,s.workspace_id,s.task_id,t.id,a.plan_id,a.assignment_key,a.member_id,
 t.project_id,t.project_workspace_id,COALESCE(t.artifact_session_id,''),t.revision,
 c.id,v.id,v.achieved_level,t.completion_json,a.completion_sha256,
 parent.completion_json,root.completion_sha256`, 1),
		" AND s.accepted_plan_id=?", "", 1),
	"WHERE a.session_id=? AND a.workspace_id=? AND a.parent_task_id=? AND a.task_id=? AND a.assignment_key=? AND a.member_id=?",
	`WHERE a.session_id=? AND a.task_id=?
 AND t.project_id IS NOT NULL AND t.project_id<>''
 AND t.project_workspace_id IS NOT NULL AND t.project_workspace_id<>''
 AND length(CAST(parent.completion_json AS BLOB))<=65536
 AND length(CAST(root.completion_sha256 AS BLOB))=64
 AND length(CAST(a.completion_sha256 AS BLOB))=64
 AND length(CAST(s.id AS BLOB))<=256 AND length(CAST(s.workspace_id AS BLOB))<=256
 AND length(CAST(s.task_id AS BLOB))<=256 AND length(CAST(t.id AS BLOB))<=256
 AND length(CAST(a.plan_id AS BLOB))<=256 AND length(CAST(a.assignment_key AS BLOB))<=256
 AND length(CAST(a.member_id AS BLOB))<=256 AND length(CAST(t.project_id AS BLOB))<=256
 AND length(CAST(t.project_workspace_id AS BLOB))<=256
 AND length(CAST(COALESCE(t.artifact_session_id,'') AS BLOB))<=256
 AND length(CAST(c.id AS BLOB))<=256 AND length(CAST(v.id AS BLOB))<=256`, 1)

// AssignmentCompletion does not load unrelated assignments, roster, Worker
// steps or result payloads. The shared protocol checks the exact latest
// succeeded Attempt, start/completion ledger, current revision and checkpoint.
func (s *Service) AssignmentCompletion(ctx context.Context, sessionID, taskID string) (AssignmentCompletion, error) {
	var out AssignmentCompletion
	if sessionID == "" || taskID == "" {
		return out, ErrInvalid
	}
	var completion, sealed, rootCompletion, rootSealed string
	err := s.db.QueryRowContext(ctx, assignmentCompletionReadQuery, sessionID, taskID).Scan(
		&out.SessionID, &out.WorkspaceID, &out.ParentTaskID, &out.TaskID, &out.PlanID,
		&out.AssignmentKey, &out.MemberID, &out.ProjectID, &out.ProjectWorkspaceID,
		&out.ArtifactSessionID, &out.TaskRevision, &out.CheckpointID,
		&out.VerificationID, &out.VerificationLevel, &completion, &sealed, &rootCompletion, &rootSealed)
	if err == sql.ErrNoRows {
		return AssignmentCompletion{}, ErrInvalid
	}
	if err != nil {
		return AssignmentCompletion{}, err
	}
	for _, value := range []string{out.SessionID, out.WorkspaceID, out.ParentTaskID, out.TaskID,
		out.PlanID, out.AssignmentKey, out.MemberID, out.ProjectID, out.ProjectWorkspaceID,
		out.ArtifactSessionID, out.CheckpointID, out.VerificationID, out.VerificationLevel} {
		if !utf8.ValidString(value) {
			return AssignmentCompletion{}, ErrInvalid
		}
	}
	sum := sha256.Sum256([]byte(completion))
	rootSum := sha256.Sum256([]byte(rootCompletion))
	if out.CheckpointID == "" || out.VerificationID == "" || hex.EncodeToString(sum[:]) != sealed || hex.EncodeToString(rootSum[:]) != rootSealed {
		return AssignmentCompletion{}, ErrInvalid
	}
	return out, nil
}
