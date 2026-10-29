package recoveryinventory

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

const WorkerJournalObservationVersion = "onepane.worker-journal-slice/v1"
const WorkerJournalMaxSteps = 256
const WorkerJournalMaxBytes = 256 << 10

var ErrWorkerJournalUnavailable = errors.New("scoped Worker journal observation unavailable")
var ErrWorkerJournalLimit = errors.New("Worker journal observation exceeds bounds")

// Scope is supplied by a trusted caller within its original observation
// transaction. It is an exact identity comparison, not caller authentication.
// Nullable Task context is explicit; nil never acts as a wildcard.
type WorkerJournalScope struct {
	WorkspaceID, TaskID, AttemptID, RunID            string
	ProjectID, ProjectWorkspaceID, ArtifactSessionID *string
}

type WorkerJournalStepObservation struct {
	ID               string
	Number           int64
	Kind             string
	Status           string
	RowSHA256        string
	ResultReferences WorkerStepReferences
}

type WorkerJournalObservation struct {
	Version     string
	Scope       WorkerJournalScope
	Fingerprint string
	Steps       []WorkerJournalStepObservation
}

type workerJournalRunRow struct {
	ID, WorkspaceID, TaskID, AttemptID, PrincipalID, Status, Role, Capability, Protocol string
	MaxSteps, StepCount, MaxReplans, ReplanCount, MaxEscalations, EscalationCount       int64
	RoutePolicy, Continuation                                                           string
	LastCandidateKind, LastCandidateID, LastError                                       *string
	Revision, StartedAt, UpdatedAt                                                      int64
	CompletedAt                                                                         *int64
	TaskRevision                                                                        int64
	TaskState, AttemptState                                                             string
	TaskProject, TaskWorkspace, TaskSession, AttemptPrincipal                           *string
}

type workerJournalStepRow struct {
	ID, RunID                                                       string
	Number                                                          int64
	Kind, Status                                                    string
	CandidateKind, CandidateID, ProposalType, RequestRef, ResultRef *string
	Detail                                                          string
	StartedAt                                                       int64
	CompletedAt                                                     *int64
}

// ObserveWorkerJournalSlice reads only inside the caller's live SQL transaction.
// It is NOT the complete recovery inventory: target rows, reverse provenance,
// continuation semantics, other Workers/Attempts and effects remain uncollected.
// Unsupported result shapes remain explicit observations, never absence proofs.
// There is no persistent write, API, daemon, phase/admission change or adapter.
func ObserveWorkerJournalSlice(ctx context.Context, tx *sql.Tx, scope WorkerJournalScope) (WorkerJournalObservation, error) {
	empty := WorkerJournalObservation{}
	validID := func(s string) bool {
		return len(s) > 0 && len(s) <= 160 && utf8.ValidString(s) && strings.TrimSpace(s) == s && !strings.ContainsRune(s, 0)
	}
	if tx == nil {
		return empty, ErrWorkerJournalUnavailable
	}
	for _, id := range []string{scope.WorkspaceID, scope.TaskID, scope.AttemptID, scope.RunID} {
		if !validID(id) {
			return empty, ErrWorkerJournalUnavailable
		}
	}
	for _, id := range []*string{scope.ProjectID, scope.ProjectWorkspaceID, scope.ArtifactSessionID} {
		if id != nil && !validID(*id) {
			return empty, ErrWorkerJournalUnavailable
		}
	}
	copyID := func(id *string) *string {
		if id == nil {
			return nil
		}
		value := *id
		return &value
	}
	scope.ProjectID = copyID(scope.ProjectID)
	scope.ProjectWorkspaceID = copyID(scope.ProjectWorkspaceID)
	scope.ArtifactSessionID = copyID(scope.ArtifactSessionID)
	if err := ValidateTaskAttemptSchema(ctx, tx); err != nil {
		return empty, err
	}
	if err := ValidateIndirectOwnershipSchema(ctx, tx); err != nil {
		return empty, err
	}
	if err := validateWorkerJournalStepIndex(ctx, tx); err != nil {
		return empty, err
	}
	var run workerJournalRunRow
	// SQL guards precede materialization of every variable-length run field.
	// Oversized rows are rejected, not observed through a truncated projection.
	err := tx.QueryRowContext(ctx, `SELECT r.id,r.workspace_id,r.task_id,r.attempt_id,r.worker_principal_id,r.status,r.role_name,r.capability_id,r.protocol_level,
 r.max_steps,r.step_count,r.max_replans,r.replan_count,r.max_escalations,r.escalation_count,
 r.route_policy_json,r.continuation_json,r.last_candidate_kind,r.last_candidate_id,r.last_error,
 r.revision,r.started_at,r.updated_at,r.completed_at,t.revision,t.state,a.status,t.project_id,t.project_workspace_id,t.artifact_session_id,a.worker_principal_id
 FROM main.agent_worker_runs r JOIN main.tasks t ON t.id=r.task_id AND t.workspace_id=r.workspace_id
 JOIN main.task_attempts a ON a.id=r.attempt_id AND a.task_id=t.id
 WHERE r.id=? AND r.workspace_id=? AND r.task_id=? AND r.attempt_id=?
 AND t.project_id IS ? AND t.project_workspace_id IS ? AND t.artifact_session_id IS ?
 AND (t.project_id IS NULL OR EXISTS(SELECT 1 FROM main.projects p WHERE p.id=t.project_id AND p.workspace_id=t.workspace_id))
 AND (t.project_workspace_id IS NULL OR EXISTS(SELECT 1 FROM main.project_workspaces pw WHERE pw.id=t.project_workspace_id AND pw.project_id=t.project_id))
 AND (t.artifact_session_id IS NULL OR EXISTS(SELECT 1 FROM main.artifact_sessions ss WHERE ss.id=t.artifact_session_id AND ss.workspace_id=t.workspace_id AND ss.project_id IS t.project_id))
 AND a.worker_principal_id=r.worker_principal_id
 AND length(CAST(r.id AS BLOB))<=160 AND length(CAST(r.workspace_id AS BLOB))<=160
 AND length(CAST(r.task_id AS BLOB))<=160 AND length(CAST(r.attempt_id AS BLOB))<=160
 AND length(CAST(r.worker_principal_id AS BLOB))<=512 AND length(CAST(r.role_name AS BLOB))<=512
 AND length(CAST(r.capability_id AS BLOB))<=512 AND length(CAST(r.status AS BLOB))<=64
 AND length(CAST(r.protocol_level AS BLOB))<=64 AND length(CAST(t.state AS BLOB))<=64 AND length(CAST(a.status AS BLOB))<=64
 AND length(CAST(r.route_policy_json AS BLOB))<=65536 AND length(CAST(r.continuation_json AS BLOB))<=65536
 AND (r.last_candidate_kind IS NULL OR length(CAST(r.last_candidate_kind AS BLOB))<=64)
 AND (r.last_candidate_id IS NULL OR length(CAST(r.last_candidate_id AS BLOB))<=512)
 AND (r.last_error IS NULL OR length(CAST(r.last_error AS BLOB))<=2048)`,
		scope.RunID, scope.WorkspaceID, scope.TaskID, scope.AttemptID, scope.ProjectID, scope.ProjectWorkspaceID, scope.ArtifactSessionID).Scan(
		&run.ID, &run.WorkspaceID, &run.TaskID, &run.AttemptID, &run.PrincipalID, &run.Status, &run.Role, &run.Capability, &run.Protocol,
		&run.MaxSteps, &run.StepCount, &run.MaxReplans, &run.ReplanCount, &run.MaxEscalations, &run.EscalationCount,
		&run.RoutePolicy, &run.Continuation, &run.LastCandidateKind, &run.LastCandidateID, &run.LastError,
		&run.Revision, &run.StartedAt, &run.UpdatedAt, &run.CompletedAt, &run.TaskRevision, &run.TaskState, &run.AttemptState,
		&run.TaskProject, &run.TaskWorkspace, &run.TaskSession, &run.AttemptPrincipal)
	if errors.Is(err, sql.ErrNoRows) {
		return empty, ErrWorkerJournalUnavailable
	}
	if err != nil {
		return empty, err
	}
	validStrings := func(values ...string) bool {
		for _, value := range values {
			if !utf8.ValidString(value) {
				return false
			}
		}
		return true
	}
	validOptional := func(values ...*string) bool {
		for _, value := range values {
			if value != nil && !utf8.ValidString(*value) {
				return false
			}
		}
		return true
	}
	if !validStrings(run.ID, run.WorkspaceID, run.TaskID, run.AttemptID, run.PrincipalID, run.Status, run.Role, run.Capability, run.Protocol, run.RoutePolicy, run.Continuation, run.TaskState, run.AttemptState) || !validOptional(run.LastCandidateKind, run.LastCandidateID, run.LastError, run.TaskProject, run.TaskWorkspace, run.TaskSession, run.AttemptPrincipal) {
		return empty, ErrWorkerJournalUnavailable
	}
	// Fingerprints bind all observed stored fields, including unrecognized
	// continuation data, nullable fields and metadata without revision columns.
	// JSON is stored as a string to preserve its exact original encoding.
	h := sha256.New()
	_, _ = h.Write([]byte(WorkerJournalObservationVersion + "\x00"))
	used := 0
	appendRecord := func(value any) (string, error) {
		raw, e := json.Marshal(value)
		if e != nil {
			return "", e
		}
		if len(raw) > WorkerJournalMaxBytes-used {
			return "", ErrWorkerJournalLimit
		}
		used += len(raw)
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(raw)))
		_, _ = h.Write(size[:])
		_, _ = h.Write(raw)
		digest := sha256.Sum256(raw)
		return "sha256:" + hex.EncodeToString(digest[:]), nil
	}
	// Structural fixtures are part of this slice's version identity, not a
	// substitute for the actual same-transaction validation above.
	direct, err := workerJournalContractHash(taskAttemptSchemaJSON)
	if err != nil {
		return empty, err
	}
	indirect, err := workerJournalContractHash(indirectOwnershipSchemaJSON)
	if err != nil {
		return empty, err
	}
	_, err = appendRecord(struct {
		Direct, Indirect [32]byte
		ResultParser     string
	}{direct, indirect, WorkerStepReferenceParserVersion})
	if err != nil {
		return empty, err
	}
	_, err = appendRecord(run)
	if err != nil {
		return empty, err
	}
	// Index-ordered limit-plus-one bounds raw rows before any semantic filter.
	// A malformed/oversized early step cannot disappear from a partial result.
	rows, err := tx.QueryContext(ctx, `SELECT
 CASE WHEN length(CAST(id AS BLOB))<=160 THEN id ELSE '' END,
 CASE WHEN length(CAST(run_id AS BLOB))<=160 THEN run_id ELSE '' END,step_number,
 CASE WHEN length(CAST(step_kind AS BLOB))<=64 THEN step_kind ELSE '' END,
 CASE WHEN length(CAST(status AS BLOB))<=64 THEN status ELSE '' END,
 CASE WHEN length(CAST(candidate_kind AS BLOB))<=64 THEN candidate_kind ELSE NULL END,
 CASE WHEN length(CAST(candidate_id AS BLOB))<=512 THEN candidate_id ELSE NULL END,
 CASE WHEN length(CAST(proposal_type AS BLOB))<=64 THEN proposal_type ELSE NULL END,
 CASE WHEN length(CAST(request_ref AS BLOB))<=512 THEN request_ref ELSE NULL END,
 CASE WHEN length(CAST(result_ref AS BLOB))<=512 THEN result_ref ELSE NULL END,
 CASE WHEN length(CAST(detail_json AS BLOB))<=65536 THEN detail_json ELSE '' END,started_at,completed_at,
 length(CAST(id AS BLOB))<=160 AND length(CAST(run_id AS BLOB))<=160
 AND length(CAST(step_kind AS BLOB))<=64 AND length(CAST(status AS BLOB))<=64
 AND (candidate_kind IS NULL OR length(CAST(candidate_kind AS BLOB))<=64)
 AND (candidate_id IS NULL OR length(CAST(candidate_id AS BLOB))<=512)
 AND (proposal_type IS NULL OR length(CAST(proposal_type AS BLOB))<=64)
 AND (request_ref IS NULL OR length(CAST(request_ref AS BLOB))<=512)
 AND (result_ref IS NULL OR length(CAST(result_ref AS BLOB))<=512)
 AND length(CAST(detail_json AS BLOB))<=65536
 FROM main.agent_worker_steps INDEXED BY idx_agent_worker_steps_run WHERE run_id=? ORDER BY step_number LIMIT 257`, scope.RunID)
	if err != nil {
		return empty, err
	}
	defer rows.Close()
	out := WorkerJournalObservation{Version: WorkerJournalObservationVersion, Scope: scope, Steps: []WorkerJournalStepObservation{}}
	for rows.Next() {
		var step workerJournalStepRow
		var bounded bool
		if err = rows.Scan(&step.ID, &step.RunID, &step.Number, &step.Kind, &step.Status, &step.CandidateKind, &step.CandidateID, &step.ProposalType, &step.RequestRef, &step.ResultRef, &step.Detail, &step.StartedAt, &step.CompletedAt, &bounded); err != nil {
			return empty, err
		}
		if !bounded || len(out.Steps) >= WorkerJournalMaxSteps {
			return empty, ErrWorkerJournalLimit
		}
		if step.Number != int64(len(out.Steps)+1) || step.RunID != scope.RunID || !validID(step.ID) {
			return empty, ErrWorkerJournalUnavailable
		}
		if !validStrings(step.ID, step.RunID, step.Kind, step.Status, step.Detail) || !validOptional(step.CandidateKind, step.CandidateID, step.ProposalType, step.RequestRef, step.ResultRef) {
			return empty, ErrWorkerJournalUnavailable
		}
		rowHash, e := appendRecord(step)
		if e != nil {
			return empty, e
		}
		out.Steps = append(out.Steps, WorkerJournalStepObservation{ID: step.ID, Number: step.Number, Kind: step.Kind, Status: step.Status, RowSHA256: rowHash, ResultReferences: ClassifyWorkerStepReferences(step.Kind, step.Status, step.ResultRef, []byte(step.Detail))})
	}
	if err = rows.Err(); err != nil {
		return empty, err
	}
	if err = rows.Close(); err != nil {
		return empty, err
	}
	out.Fingerprint = "sha256:" + hex.EncodeToString(h.Sum(nil))
	return out, nil
}

// Embed/checkouts may use LF or CRLF. Ignore JSON formatting in the structural
// contract identity, while retaining exact bytes of every stored row payload.
func workerJournalContractHash(raw []byte) ([32]byte, error) {
	var canonical bytes.Buffer
	if err := json.Compact(&canonical, raw); err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(canonical.Bytes()), nil
}

func validateWorkerJournalStepIndex(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT seq,CASE WHEN length(CAST(name AS BLOB))<=512 THEN name ELSE '' END,"unique",origin,partial FROM pragma_index_list('agent_worker_steps','main') LIMIT 33`)
	if err != nil {
		return err
	}
	found := false
	count := 0
	for rows.Next() {
		count++
		if count > 32 {
			err = ErrWorkerJournalUnavailable
			break
		}
		var seq, unique, partial int
		var name, origin string
		if err = rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			break
		}
		if name == "idx_agent_worker_steps_run" {
			found = unique == 0 && origin == "c" && partial == 0
		}
	}
	if err == nil {
		err = rows.Err()
	}
	closeErr := rows.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if !found {
		return ErrWorkerJournalUnavailable
	}
	rows, err = tx.QueryContext(ctx, `SELECT seqno,cid,CASE WHEN length(CAST(name AS BLOB))<=256 THEN name ELSE NULL END,"desc",CASE WHEN length(CAST(coll AS BLOB))<=64 THEN coll ELSE NULL END,"key" FROM pragma_index_xinfo('idx_agent_worker_steps_run','main') LIMIT 4`)
	if err != nil {
		return err
	}
	defer rows.Close()
	position := 0
	for rows.Next() {
		var seq, cid, descending, key int
		var name, collation sql.NullString
		if err = rows.Scan(&seq, &cid, &name, &descending, &collation, &key); err != nil {
			return err
		}
		if key == 0 {
			continue
		}
		if position >= 2 || seq != position || cid < 0 || !name.Valid || name.String != []string{"run_id", "step_number"}[position] || descending != 0 || !collation.Valid || collation.String != "BINARY" {
			return ErrWorkerJournalUnavailable
		}
		position++
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	if position != 2 {
		return ErrWorkerJournalUnavailable
	}
	return nil
}
