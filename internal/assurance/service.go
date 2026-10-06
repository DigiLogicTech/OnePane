package assurance

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/artifact"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/id"
	"github.com/DigiLogicTech/OnePane/internal/observation"
	"github.com/DigiLogicTech/OnePane/internal/operation"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	"github.com/DigiLogicTech/OnePane/internal/storage"
	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/verification"
)

type workerBridge interface {
	AssurancePassed(context.Context, string, string, string) error
	AssuranceRetry(context.Context, string, string, json.RawMessage) error
}

type Service struct {
	db           *sql.DB
	tx           storage.Transactor
	clock        clock.Clock
	ids          id.Generator
	events       event.Store
	verification *verification.Service
	tasks        *task.Service
	artifacts    *artifact.Service
	observations *observation.Service
	operations   *operation.Coordinator
	workers      workerBridge
}

func New(db *sql.DB, tx storage.Transactor, clk clock.Clock, ver *verification.Service, tasks *task.Service, artifacts *artifact.Service, observations *observation.Service, operations *operation.Coordinator) *Service {
	return &Service{db: db, tx: tx, clock: clk, ids: id.Generator{}, events: event.Store{}, verification: ver, tasks: tasks, artifacts: artifacts, observations: observations, operations: operations}
}

func (s *Service) SetWorkerBridge(w workerBridge) { s.workers = w }

func (s *Service) EnsureSystemPrincipal(ctx context.Context) error {
	var status string
	err := s.db.QueryRowContext(ctx, `SELECT status FROM principals WHERE id=?`, VerifierPrincipal).Scan(&status)
	if err == nil {
		if status != "active" {
			return fmt.Errorf("assurance verifier principal is %s", status)
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	now := s.clock.UnixMilli()
	actor := VerifierPrincipal
	return s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES(?,'system','Assurance Verifier','active',1,?,?)`, VerifierPrincipal, now, now); err != nil {
			return err
		}
		eid, _ := s.ids.New("evt")
		p, _ := json.Marshal(map[string]any{"principal_id": VerifierPrincipal, "purpose": "independent_v1_v5_assurance"})
		return s.events.Append(ctx, tx, event.Event{ID: eid, Type: "system_principal.registered", AggregateType: "principal", AggregateID: VerifierPrincipal, ActorPrincipalID: &actor, Payload: p, OccurredAt: now})
	})
}

func (s *Service) ensureWorkspace(ctx context.Context, workspaceID string) error {
	if err := s.EnsureSystemPrincipal(ctx); err != nil {
		return err
	}
	var status string
	err := s.db.QueryRowContext(ctx, `SELECT status FROM workspace_memberships WHERE workspace_id=? AND principal_id=?`, workspaceID, VerifierPrincipal).Scan(&status)
	if err == nil {
		if status != "active" {
			return fmt.Errorf("assurance workspace membership is %s", status)
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	now := s.clock.UnixMilli()
	actor := VerifierPrincipal
	return s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		var ws string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM workspaces WHERE id=?`, workspaceID).Scan(&ws); err != nil {
			return err
		}
		if ws != "active" {
			return fmt.Errorf("workspace is %s", ws)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES(?,?,'active',?,?)`, workspaceID, VerifierPrincipal, now, now); err != nil {
			return err
		}
		eid, _ := s.ids.New("evt")
		p, _ := json.Marshal(map[string]any{"workspace_id": workspaceID, "principal_id": VerifierPrincipal, "purpose": "independent_assurance"})
		return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: &workspaceID, Type: "workspace.system_membership_added", AggregateType: "workspace_membership", AggregateID: workspaceID + ":" + VerifierPrincipal, ActorPrincipalID: &actor, Payload: p, OccurredAt: now})
	})
}

func (s *Service) RecoverInterrupted(ctx context.Context) (int, error) {
	now := s.clock.UnixMilli()
	res, err := s.db.ExecContext(ctx, `UPDATE assurance_runs SET status='interrupted',failure_reason='daemon restart during assurance evaluation',revision=revision+1,updated_at=?,completed_at=? WHERE status='running'`, now, now)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	// Interrupted deterministic evaluations are safe to requeue; no external
	// side effect is performed by the assurance evaluator itself.
	if n > 0 {
		if _, err := s.db.ExecContext(ctx, `UPDATE assurance_runs SET status='queued',failure_reason=NULL,completed_at=NULL,revision=revision+1,updated_at=? WHERE status='interrupted'`, now); err != nil {
			return int(n), err
		}
	}
	return int(n), nil
}

func (s *Service) Tick(ctx context.Context, limit int) ([]TickResult, error) {
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx, `SELECT v.id FROM verifications v LEFT JOIN assurance_runs ar ON ar.verification_id=v.id WHERE json_extract(v.spec_json,'$.assurance_version')=1 AND ((v.status='pending' AND (ar.id IS NULL OR ar.status IN ('queued','waiting_evidence','waiting_human'))) OR (v.status IN ('pass','fail','inconclusive') AND ar.status IN ('queued','running','interrupted','waiting_evidence','waiting_human'))) ORDER BY v.started_at,v.id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var idv string
		if err := rows.Scan(&idv); err != nil {
			return nil, err
		}
		ids = append(ids, idv)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]TickResult, 0, len(ids))
	for _, idv := range ids {
		r, e := s.evaluateOne(ctx, idv)
		if e != nil {
			return out, e
		}
		out = append(out, r)
	}
	return out, nil
}

func (s *Service) evaluateOne(ctx context.Context, verificationID string) (TickResult, error) {
	v, err := s.verification.Get(ctx, verificationID)
	if err != nil {
		return TickResult{}, err
	}
	if v.Status != verification.StatusPending {
		run, e := s.getRunByVerification(ctx, v.ID)
		if e != nil {
			return TickResult{}, e
		}
		var spec Spec
		if e := json.Unmarshal(v.Spec, &spec); e != nil {
			return TickResult{}, e
		}
		return s.resumeResolved(ctx, v, spec, run)
	}
	if verification.LevelRank(v.RequiredLevel) < verification.LevelRank(policy.VerificationV1) {
		return TickResult{}, ErrInvalidSpec
	}
	if err := s.ensureWorkspace(ctx, v.WorkspaceID); err != nil {
		return TickResult{}, err
	}
	var spec Spec
	if err := json.Unmarshal(v.Spec, &spec); err != nil || spec.AssuranceVersion != 1 || len(spec.ProposedResult) == 0 || !json.Valid(spec.ProposedResult) {
		return s.resolveTerminal(ctx, v, Spec{}, policy.VerificationV0, verification.StatusInconclusive, ErrInvalidSpec.Error(), "", nil)
	}
	run, err := s.ensureRun(ctx, v, spec)
	if err != nil {
		return TickResult{}, err
	}
	if err := s.setRunStatus(ctx, run.ID, RunRunning, nil, nil, nil); err != nil {
		return TickResult{}, err
	}
	eval, evalErr := s.evaluateEvidence(ctx, v, spec)
	if evalErr != nil {
		status := verification.StatusFail
		runStatus := RunFailed
		if errors.Is(evalErr, ErrEvidenceMissing) || errors.Is(evalErr, ErrInvalidSpec) {
			status = verification.StatusInconclusive
			runStatus = RunInconclusive
		}
		return s.resolveTerminalWithRun(ctx, v, spec, eval.Achieved, status, runStatus, evalErr.Error(), eval.EvidenceHash, eval.Result, run.ID)
	}
	if v.RequiredLevel == policy.VerificationV5 && verification.LevelRank(eval.Achieved) >= verification.LevelRank(policy.VerificationV4) {
		accepted, rejected, err := s.acceptanceState(ctx, v.ID, eval.EvidenceHash)
		if err != nil {
			return TickResult{}, err
		}
		if rejected {
			return s.resolveTerminalWithRun(ctx, v, spec, policy.VerificationV4, verification.StatusFail, RunFailed, "human acceptance rejected", eval.EvidenceHash, eval.Result, run.ID)
		}
		if !accepted {
			lvl := eval.Achieved
			if err := s.setRunStatus(ctx, run.ID, RunWaitingHuman, &lvl, &eval.EvidenceHash, eval.Result); err != nil {
				return TickResult{}, err
			}
			return TickResult{VerificationID: v.ID, RunID: run.ID, Status: RunWaitingHuman, AchievedLevel: &lvl, Message: ErrHumanAcceptance.Error()}, nil
		}
		eval.Achieved = policy.VerificationV5
	}
	if verification.LevelRank(eval.Achieved) < verification.LevelRank(v.RequiredLevel) {
		return s.resolveTerminalWithRun(ctx, v, spec, eval.Achieved, verification.StatusInconclusive, RunInconclusive, fmt.Sprintf("achieved %s below required %s", eval.Achieved, v.RequiredLevel), eval.EvidenceHash, eval.Result, run.ID)
	}
	return s.resolvePass(ctx, v, spec, eval, run.ID)
}

type evaluation struct {
	Achieved     policy.VerificationLevel
	EvidenceHash string
	Result       json.RawMessage
}

type observedEvidence struct {
	obs  observation.Observation
	role string
}

func (s *Service) evaluateEvidence(ctx context.Context, v verification.Verification, spec Spec) (evaluation, error) {
	result := map[string]any{"checks": []any{}, "artifacts": []any{}, "observations": []any{}}
	achieved := policy.VerificationV0
	layers := map[string]bool{}
	if len(spec.Criteria.ResultChecks) > 0 {
		if err := checkJSON(spec.ProposedResult, spec.Criteria.ResultChecks); err != nil {
			return evaluation{Achieved: achieved}, fmt.Errorf("%w: result: %v", ErrEvidenceMismatch, err)
		}
		achieved = policy.VerificationV1
		layers["result"] = true
		result["checks"] = append(result["checks"].([]any), map[string]any{"kind": "result_json", "status": "pass", "count": len(spec.Criteria.ResultChecks)})
	}

	usedArtifacts := map[string]bool{}
	artifactTokens := []string{}
	for i, req := range spec.Criteria.Artifacts {
		matched := ""
		for _, aid := range spec.Evidence.ArtifactIDs {
			if usedArtifacts[aid] {
				continue
			}
			a, err := s.artifacts.Get(ctx, aid)
			if err != nil {
				continue
			}
			if a.WorkspaceID != v.WorkspaceID || a.Status != artifact.StatusActive {
				continue
			}
			if req.MediaType != "" && a.MediaType != req.MediaType {
				continue
			}
			if req.ContentHash != "" && a.ContentHash != req.ContentHash {
				continue
			}
			if err := s.artifacts.VerifyContent(ctx, aid); err != nil {
				continue
			}
			usedArtifacts[aid] = true
			matched = aid
			artifactTokens = append(artifactTokens, aid+":"+a.ContentHash)
			break
		}
		if matched == "" {
			return evaluation{Achieved: achieved}, fmt.Errorf("%w: artifact requirement %d", ErrEvidenceMissing, i)
		}
		layers["artifact"] = true
		if verification.LevelRank(achieved) < 1 {
			achieved = policy.VerificationV1
		}
		result["artifacts"] = append(result["artifacts"].([]any), map[string]any{"requirement": i, "artifact_id": matched, "status": "pass"})
	}

	minObservedAt, err := s.minimumEvidenceTime(ctx, v)
	if err != nil {
		return evaluation{Achieved: achieved}, err
	}
	usedObs := map[string]bool{}
	var matched []observedEvidence
	observationTokens := []string{}
	for i, req := range spec.Criteria.Observations {
		role := strings.ToLower(strings.TrimSpace(req.Role))
		if role != "direct" && role != "integration" {
			return evaluation{Achieved: achieved}, fmt.Errorf("%w: observation role %q", ErrInvalidSpec, req.Role)
		}
		if req.SubjectRef == "" && req.ObservationType == "" && req.ProbeToolID == "" && req.AdapterID == "" && len(req.ValueChecks) == 0 {
			return evaluation{Achieved: achieved}, fmt.Errorf("%w: empty observation requirement", ErrInvalidSpec)
		}
		var found *observation.Observation
		for _, oid := range spec.Evidence.ObservationIDs {
			if usedObs[oid] {
				continue
			}
			o, e := s.observations.Get(ctx, oid)
			if e != nil {
				continue
			}
			if o.WorkspaceID != v.WorkspaceID || (minObservedAt > 0 && o.ObservedAt < minObservedAt) {
				continue
			}
			if req.SubjectRef != "" && o.SubjectRef != req.SubjectRef {
				continue
			}
			if req.ObservationType != "" && o.ObservationType != req.ObservationType {
				continue
			}
			if req.ProbeToolID != "" && o.ProbeToolID != req.ProbeToolID {
				continue
			}
			if req.AdapterID != "" && (o.AdapterID == nil || *o.AdapterID != req.AdapterID) {
				continue
			}
			if e := s.observations.VerifyIntegrity(ctx, oid); e != nil {
				continue
			}
			if e := checkJSON(o.Value, req.ValueChecks); e != nil {
				continue
			}
			copy := o
			found = &copy
			break
		}
		if found == nil {
			return evaluation{Achieved: achieved}, fmt.Errorf("%w: %s observation requirement %d", ErrEvidenceMissing, role, i)
		}
		usedObs[found.ID] = true
		matched = append(matched, observedEvidence{obs: *found, role: role})
		observationTokens = append(observationTokens, found.ID+":"+found.IntegrityHash)
		layers[role] = true
		result["observations"] = append(result["observations"].([]any), map[string]any{"requirement": i, "observation_id": found.ID, "role": role, "status": "pass"})
	}

	directPaths := map[string]bool{}
	integrationPaths := map[string]bool{}
	sources := map[string]bool{}
	workerPrincipal := "system:agent-worker"
	if spec.WorkerRunID != "" {
		var p string
		if s.db.QueryRowContext(ctx, `SELECT worker_principal_id FROM agent_worker_runs WHERE id=?`, spec.WorkerRunID).Scan(&p) == nil && p != "" {
			workerPrincipal = p
		}
	}
	for _, m := range matched {
		path := observationPath(m.obs)
		if m.role == "direct" {
			directPaths[path] = true
		} else {
			integrationPaths[path] = true
		}
		if m.obs.SourcePrincipalID != nil {
			sources[*m.obs.SourcePrincipalID] = true
		}
	}
	independentSource := false
	for _, m := range matched {
		if m.role == "integration" && m.obs.SourcePrincipalID != nil && *m.obs.SourcePrincipalID != workerPrincipal {
			independentSource = true
			break
		}
	}
	hasV1 := verification.LevelRank(achieved) >= verification.LevelRank(policy.VerificationV1)
	achieved = deriveLevel(hasV1, directPaths, integrationPaths, sources, layers, independentSource)

	sort.Strings(artifactTokens)
	sort.Strings(observationTokens)
	canonical, _ := json.Marshal(map[string]any{"verification_id": v.ID, "proposed_result": json.RawMessage(spec.ProposedResult), "artifacts": artifactTokens, "observations": observationTokens, "achieved": achieved})
	sum := sha256.Sum256(canonical)
	evidenceHash := "sha256:" + hex.EncodeToString(sum[:])
	result["achieved_level"] = achieved
	result["evidence_hash"] = evidenceHash
	result["layers"] = layers
	raw, _ := json.Marshal(result)
	return evaluation{Achieved: achieved, EvidenceHash: evidenceHash, Result: raw}, nil
}

func deriveLevel(hasV1 bool, directPaths, integrationPaths, sources, layers map[string]bool, independentIntegrationSource bool) policy.VerificationLevel {
	achieved := policy.VerificationV0
	if hasV1 {
		achieved = policy.VerificationV1
	}
	if len(directPaths) > 0 {
		achieved = policy.VerificationV2
	}
	if len(directPaths) > 0 && len(integrationPaths) > 0 && independentIntegrationSource {
		independentPath := false
		for p := range integrationPaths {
			if !directPaths[p] {
				independentPath = true
				break
			}
		}
		if independentPath {
			achieved = policy.VerificationV3
		}
	}
	if achieved == policy.VerificationV3 {
		paths := map[string]bool{}
		for p := range directPaths {
			paths[p] = true
		}
		for p := range integrationPaths {
			paths[p] = true
		}
		if len(paths) >= 2 && len(sources) >= 2 && len(layers) >= 3 {
			achieved = policy.VerificationV4
		}
	}
	return achieved
}

func observationPath(o observation.Observation) string {
	adapter := ""
	version := ""
	if o.AdapterID != nil {
		adapter = *o.AdapterID
	}
	if o.AdapterVersion != nil {
		version = *o.AdapterVersion
	}
	return o.ProbeToolID + "@" + o.ProbeToolVersion + "|" + adapter + "@" + version
}

func (s *Service) minimumEvidenceTime(ctx context.Context, v verification.Verification) (int64, error) {
	var minimum int64
	if v.TaskID != nil {
		var t sql.NullInt64
		if err := s.db.QueryRowContext(ctx, `SELECT MAX(started_at) FROM task_attempts WHERE task_id=?`, *v.TaskID).Scan(&t); err != nil {
			return 0, err
		}
		if t.Valid && t.Int64 > minimum {
			minimum = t.Int64
		}
	}
	if v.OperationID != nil {
		var t sql.NullInt64
		if err := s.db.QueryRowContext(ctx, `SELECT MAX(occurred_at) FROM events WHERE aggregate_type='operation' AND aggregate_id=? AND event_type='operation.executing'`, *v.OperationID).Scan(&t); err != nil {
			return 0, err
		}
		if t.Valid && t.Int64 > minimum {
			minimum = t.Int64
		}
	}
	return minimum, nil
}

func (s *Service) ensureRun(ctx context.Context, v verification.Verification, spec Spec) (Run, error) {
	if r, err := s.getRunByVerification(ctx, v.ID); err == nil {
		return r, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Run{}, err
	}
	idv, _ := s.ids.New("assurance")
	now := s.clock.UnixMilli()
	var worker *string
	if strings.TrimSpace(spec.WorkerRunID) != "" {
		x := spec.WorkerRunID
		worker = &x
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO assurance_runs(id,verification_id,workspace_id,task_id,operation_id,worker_run_id,status,required_level,result_json,revision,started_at,updated_at) VALUES(?,?,?,?,?,?,'queued',?,'{}',1,?,?)`, idv, v.ID, v.WorkspaceID, v.TaskID, v.OperationID, worker, v.RequiredLevel, now, now)
	if err != nil {
		return Run{}, err
	}
	return s.getRunByVerification(ctx, v.ID)
}

func (s *Service) getRunByVerification(ctx context.Context, verificationID string) (Run, error) {
	var r Run
	var taskID, opID, worker, ach, hash, reason sql.NullString
	var completed sql.NullInt64
	var result string
	err := s.db.QueryRowContext(ctx, `SELECT id,verification_id,workspace_id,task_id,operation_id,worker_run_id,status,required_level,achieved_level,evidence_hash,result_json,failure_reason,revision,started_at,updated_at,completed_at FROM assurance_runs WHERE verification_id=?`, verificationID).Scan(&r.ID, &r.VerificationID, &r.WorkspaceID, &taskID, &opID, &worker, &r.Status, &r.RequiredLevel, &ach, &hash, &result, &reason, &r.Revision, &r.StartedAt, &r.UpdatedAt, &completed)
	if taskID.Valid {
		x := taskID.String
		r.TaskID = &x
	}
	if opID.Valid {
		x := opID.String
		r.OperationID = &x
	}
	if worker.Valid {
		x := worker.String
		r.WorkerRunID = &x
	}
	if ach.Valid {
		x := policy.VerificationLevel(ach.String)
		r.AchievedLevel = &x
	}
	if hash.Valid {
		x := hash.String
		r.EvidenceHash = &x
	}
	if reason.Valid {
		x := reason.String
		r.FailureReason = &x
	}
	if completed.Valid {
		x := completed.Int64
		r.CompletedAt = &x
	}
	r.Result = json.RawMessage(result)
	return r, err
}

func (s *Service) setRunStatus(ctx context.Context, idv string, status RunStatus, achieved *policy.VerificationLevel, evidenceHash *string, result json.RawMessage) error {
	now := s.clock.UnixMilli()
	var a, h, r any
	if achieved != nil {
		a = string(*achieved)
	}
	if evidenceHash != nil {
		h = *evidenceHash
	}
	if len(result) > 0 {
		r = string(result)
	}
	completed := any(nil)
	if status == RunPassed || status == RunFailed || status == RunInconclusive {
		completed = now
	}
	res, err := s.db.ExecContext(ctx, `UPDATE assurance_runs SET status=?,achieved_level=COALESCE(?,achieved_level),evidence_hash=COALESCE(?,evidence_hash),result_json=COALESCE(?,result_json),completed_at=?,revision=revision+1,updated_at=? WHERE id=? AND status NOT IN ('passed','failed','inconclusive')`, status, a, h, r, completed, now, idv)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return fmt.Errorf("assurance run state conflict")
	}
	return nil
}

func (s *Service) resolvePass(ctx context.Context, v verification.Verification, spec Spec, eval evaluation, runID string) (TickResult, error) {
	actor := VerifierPrincipal
	level := eval.Achieved
	resolved, err := s.verification.Resolve(ctx, verification.ResolveCommand{VerificationID: v.ID, ExpectedRevision: v.Revision, Status: verification.StatusPass, AchievedLevel: &level, Result: eval.Result, VerifiedBy: VerifierPrincipal, ActorPrincipalID: &actor})
	if err != nil {
		return TickResult{}, err
	}
	if v.OperationID != nil && s.operations != nil {
		op, err := s.operations.Get(ctx, *v.OperationID)
		if err != nil {
			return TickResult{}, err
		}
		if op.State == operation.StateObserving {
			if _, err = s.operations.CommitVerified(ctx, operation.CommitCommand{OperationID: op.ID, ExpectedRevision: op.Revision, VerificationID: resolved.ID, ActorPrincipalID: &actor}); err != nil {
				return TickResult{}, err
			}
		}
	}
	checkpointID := ""
	if v.TaskID != nil {
		cpState, _ := json.Marshal(map[string]any{"verification_id": resolved.ID, "assurance_run_id": runID, "evidence_hash": eval.EvidenceHash, "result": json.RawMessage(spec.ProposedResult)})
		cp, err := s.verification.CreateCheckpoint(ctx, verification.CheckpointCommand{WorkspaceID: v.WorkspaceID, TaskID: *v.TaskID, VerificationID: resolved.ID, State: cpState, ActorPrincipalID: &actor})
		if err != nil {
			return TickResult{}, err
		}
		checkpointID = cp.ID
		t, err := s.tasks.Get(ctx, *v.TaskID)
		if err != nil {
			return TickResult{}, err
		}
		if _, err = s.tasks.CompleteVerified(ctx, task.CompleteCommand{TaskID: t.ID, ExpectedRevision: t.Revision, CheckpointID: cp.ID, Result: spec.ProposedResult, ActorPrincipalID: &actor}); err != nil {
			return TickResult{}, err
		}
	}
	if spec.WorkerRunID != "" && s.workers != nil {
		if err := s.workers.AssurancePassed(ctx, spec.WorkerRunID, v.ID, checkpointID); err != nil {
			return TickResult{}, err
		}
	}
	if err := s.setRunStatus(ctx, runID, RunPassed, &level, &eval.EvidenceHash, eval.Result); err != nil {
		return TickResult{}, err
	}
	return TickResult{VerificationID: v.ID, RunID: runID, Status: RunPassed, AchievedLevel: &level, Message: "verification passed"}, nil
}

func (s *Service) resolveTerminal(ctx context.Context, v verification.Verification, spec Spec, achieved policy.VerificationLevel, status verification.Status, reason, evidenceHash string, result json.RawMessage) (TickResult, error) {
	r, err := s.ensureRun(ctx, v, spec)
	if err != nil {
		return TickResult{}, err
	}
	rs := RunFailed
	if status == verification.StatusInconclusive {
		rs = RunInconclusive
	}
	return s.resolveTerminalWithRun(ctx, v, spec, achieved, status, rs, reason, evidenceHash, result, r.ID)
}
func (s *Service) resolveTerminalWithRun(ctx context.Context, v verification.Verification, spec Spec, achieved policy.VerificationLevel, status verification.Status, runStatus RunStatus, reason, evidenceHash string, result json.RawMessage, runID string) (TickResult, error) {
	actor := VerifierPrincipal
	if len(result) == 0 {
		result, _ = json.Marshal(map[string]any{"reason": reason})
	}
	var lvl *policy.VerificationLevel
	if achieved != policy.VerificationV0 {
		a := achieved
		lvl = &a
	}
	resolved, err := s.verification.Resolve(ctx, verification.ResolveCommand{VerificationID: v.ID, ExpectedRevision: v.Revision, Status: status, AchievedLevel: lvl, Result: result, VerifiedBy: VerifierPrincipal, ActorPrincipalID: &actor})
	if err != nil {
		return TickResult{}, err
	}
	_ = resolved
	var hashPtr *string
	if evidenceHash != "" {
		hashPtr = &evidenceHash
	}
	if spec.WorkerRunID != "" && s.workers != nil {
		if err := s.workers.AssuranceRetry(ctx, spec.WorkerRunID, v.ID, result); err != nil {
			return TickResult{}, err
		}
	}
	if err := s.setRunStatus(ctx, runID, runStatus, lvl, hashPtr, result); err != nil {
		return TickResult{}, err
	}
	return TickResult{VerificationID: v.ID, RunID: runID, Status: runStatus, AchievedLevel: lvl, Message: reason}, nil
}

func (s *Service) resumeResolved(ctx context.Context, v verification.Verification, spec Spec, run Run) (TickResult, error) {
	actor := VerifierPrincipal
	switch v.Status {
	case verification.StatusPass:
		eval, err := s.evaluateEvidence(ctx, v, spec)
		if err != nil {
			return TickResult{}, err
		}
		if v.RequiredLevel == policy.VerificationV5 {
			accepted, _, err := s.acceptanceState(ctx, v.ID, eval.EvidenceHash)
			if err != nil {
				return TickResult{}, err
			}
			if !accepted {
				return TickResult{}, ErrHumanAcceptance
			}
			eval.Achieved = policy.VerificationV5
		}
		if v.OperationID != nil && s.operations != nil {
			op, err := s.operations.Get(ctx, *v.OperationID)
			if err != nil {
				return TickResult{}, err
			}
			if op.State == operation.StateObserving {
				if _, err := s.operations.CommitVerified(ctx, operation.CommitCommand{OperationID: op.ID, ExpectedRevision: op.Revision, VerificationID: v.ID, ActorPrincipalID: &actor}); err != nil {
					return TickResult{}, err
				}
			} else if op.State != operation.StateCommitted {
				return TickResult{}, fmt.Errorf("assurance finalization: operation is %s", op.State)
			}
		}
		checkpointID := ""
		if v.TaskID != nil {
			var existing string
			err := s.db.QueryRowContext(ctx, `SELECT id FROM checkpoints WHERE verification_id=? AND task_id=? AND status='valid' ORDER BY created_at DESC LIMIT 1`, v.ID, *v.TaskID).Scan(&existing)
			if errors.Is(err, sql.ErrNoRows) {
				cpState, _ := json.Marshal(map[string]any{"verification_id": v.ID, "assurance_run_id": run.ID, "evidence_hash": eval.EvidenceHash, "result": json.RawMessage(spec.ProposedResult)})
				cp, e := s.verification.CreateCheckpoint(ctx, verification.CheckpointCommand{WorkspaceID: v.WorkspaceID, TaskID: *v.TaskID, VerificationID: v.ID, State: cpState, ActorPrincipalID: &actor})
				if e != nil {
					return TickResult{}, e
				}
				existing = cp.ID
			} else if err != nil {
				return TickResult{}, err
			}
			checkpointID = existing
			t, e := s.tasks.Get(ctx, *v.TaskID)
			if e != nil {
				return TickResult{}, e
			}
			if t.State == task.StateVerifying {
				if _, e := s.tasks.CompleteVerified(ctx, task.CompleteCommand{TaskID: t.ID, ExpectedRevision: t.Revision, CheckpointID: existing, Result: spec.ProposedResult, ActorPrincipalID: &actor}); e != nil {
					return TickResult{}, e
				}
			} else if t.State != task.StateComplete {
				return TickResult{}, fmt.Errorf("assurance finalization: task is %s", t.State)
			}
		}
		level := policy.VerificationV1
		if v.AchievedLevel != nil {
			level = *v.AchievedLevel
		}
		if spec.WorkerRunID != "" && s.workers != nil {
			if err := s.workers.AssurancePassed(ctx, spec.WorkerRunID, v.ID, checkpointID); err != nil {
				return TickResult{}, err
			}
		}
		if err := s.setRunStatus(ctx, run.ID, RunPassed, &level, &eval.EvidenceHash, eval.Result); err != nil {
			return TickResult{}, err
		}
		return TickResult{VerificationID: v.ID, RunID: run.ID, Status: RunPassed, AchievedLevel: &level, Message: "verification passed"}, nil
	case verification.StatusFail, verification.StatusInconclusive:
		result := v.Result
		if len(result) == 0 {
			result = json.RawMessage(`{}`)
		}
		if spec.WorkerRunID != "" && s.workers != nil {
			if err := s.workers.AssuranceRetry(ctx, spec.WorkerRunID, v.ID, result); err != nil {
				return TickResult{}, err
			}
		}
		status := RunFailed
		if v.Status == verification.StatusInconclusive {
			status = RunInconclusive
		}
		if err := s.setRunStatus(ctx, run.ID, status, v.AchievedLevel, run.EvidenceHash, result); err != nil {
			return TickResult{}, err
		}
		return TickResult{VerificationID: v.ID, RunID: run.ID, Status: status, AchievedLevel: v.AchievedLevel, Message: "verification finalized"}, nil
	default:
		return TickResult{}, fmt.Errorf("cannot resume verification status %s", v.Status)
	}
}

func (s *Service) acceptanceState(ctx context.Context, verificationID, evidenceHash string) (accepted, rejected bool, err error) {
	rows, err := s.db.QueryContext(ctx, `SELECT decision,evidence_hash FROM verification_acceptances WHERE verification_id=? ORDER BY created_at DESC`, verificationID)
	if err != nil {
		return false, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var d, h string
		if err := rows.Scan(&d, &h); err != nil {
			return false, false, err
		}
		if h != evidenceHash {
			continue
		}
		if d == "accepted" {
			return true, false, nil
		}
		if d == "rejected" {
			return false, true, nil
		}
	}
	return false, false, rows.Err()
}

func (s *Service) Workspace(ctx context.Context, verificationID string) (string, error) {
	if strings.TrimSpace(verificationID) == "" {
		return "", ErrHumanAcceptance
	}
	v, err := s.verification.Get(ctx, verificationID)
	if err != nil {
		return "", err
	}
	return v.WorkspaceID, nil
}

func (s *Service) Accept(ctx context.Context, cmd AcceptanceCommand) error {
	if strings.TrimSpace(cmd.VerificationID) == "" || strings.TrimSpace(cmd.PrincipalID) == "" || (cmd.Decision != "accepted" && cmd.Decision != "rejected") {
		return ErrHumanAcceptance
	}
	v, err := s.verification.Get(ctx, cmd.VerificationID)
	if err != nil {
		return err
	}
	if v.RequiredLevel != policy.VerificationV5 || v.Status != verification.StatusPending {
		return ErrHumanAcceptance
	}
	r, err := s.getRunByVerification(ctx, v.ID)
	if err != nil {
		return err
	}
	if r.Status != RunWaitingHuman || r.EvidenceHash == nil {
		return ErrHumanAcceptance
	}
	var eligible int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM principals p JOIN workspace_memberships wm ON wm.principal_id=p.id AND wm.workspace_id=? AND wm.status='active' JOIN principal_roles pr ON pr.principal_id=p.id AND pr.workspace_id=? JOIN roles r ON r.id=pr.role_id WHERE p.id=? AND p.principal_type='human' AND p.status='active' AND r.role_class='human' AND r.name IN ('Approver','Admin')`, v.WorkspaceID, v.WorkspaceID, cmd.PrincipalID).Scan(&eligible); err != nil {
		return err
	}
	if eligible < 1 {
		return ErrAcceptanceIneligible
	}
	idv, _ := s.ids.New("acceptance")
	now := s.clock.UnixMilli()
	note := strings.TrimSpace(cmd.Note)
	if len(note) > 2048 {
		note = note[:2048]
	}
	return s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO verification_acceptances(id,verification_id,workspace_id,principal_id,decision,evidence_hash,note,created_at) VALUES(?,?,?,?,?,?,?,?)`, idv, v.ID, v.WorkspaceID, cmd.PrincipalID, cmd.Decision, *r.EvidenceHash, note, now); err != nil {
			return err
		}
		eid, _ := s.ids.New("evt")
		p, _ := json.Marshal(map[string]any{"verification_id": v.ID, "decision": cmd.Decision, "evidence_hash": *r.EvidenceHash})
		return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: &v.WorkspaceID, Type: "verification.human_" + cmd.Decision, AggregateType: "verification", AggregateID: v.ID, ActorPrincipalID: &cmd.PrincipalID, Payload: p, OccurredAt: now})
	})
}
