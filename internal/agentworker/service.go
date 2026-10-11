package agentworker

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/agentruntime"
	"github.com/DigiLogicTech/OnePane/internal/artifact"
	"github.com/DigiLogicTech/OnePane/internal/authority"
	"github.com/DigiLogicTech/OnePane/internal/budget"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/id"
	"github.com/DigiLogicTech/OnePane/internal/inference"
	"github.com/DigiLogicTech/OnePane/internal/observation"
	"github.com/DigiLogicTech/OnePane/internal/operation"
	"github.com/DigiLogicTech/OnePane/internal/scheduler"
	"github.com/DigiLogicTech/OnePane/internal/storage"
	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/tool"
	"github.com/DigiLogicTech/OnePane/internal/verification"
)

type Service struct {
	db           *sql.DB
	tx           storage.Transactor
	clock        clock.Clock
	ids          id.Generator
	events       event.Store
	cfg          Config
	localNodeID  string
	tasks        *task.Service
	authority    *authority.Service
	budgets      *budget.Service
	scheduler    *scheduler.Service
	inference    *inference.Service
	runtimes     *agentruntime.Service
	artifacts    *artifact.Service
	tools        *tool.Gateway
	operations   *operation.Coordinator
	observations *observation.Service
	verification *verification.Service
}

func New(db *sql.DB, tx storage.Transactor, clk clock.Clock, localNodeID string, tasks *task.Service, auth *authority.Service, budgets *budget.Service, sched *scheduler.Service, infer *inference.Service, runtimes *agentruntime.Service, artifacts *artifact.Service, tools *tool.Gateway, operations *operation.Coordinator, observations *observation.Service, verifications *verification.Service) *Service {
	return &Service{db: db, tx: tx, clock: clk, ids: id.Generator{}, events: event.Store{}, cfg: DefaultConfig(), localNodeID: localNodeID, tasks: tasks, authority: auth, budgets: budgets, scheduler: sched, inference: infer, runtimes: runtimes, artifacts: artifacts, tools: tools, operations: operations, observations: observations, verification: verifications}
}

func (s *Service) SetConfig(cfg Config) error {
	if cfg.MaxSteps < 1 || cfg.MaxSteps > 256 || cfg.MaxReplans < 0 || cfg.MaxReplans > 32 || cfg.MaxEscalations < 0 || cfg.MaxEscalations > 32 || cfg.ContextMaxBytes < 4096 || strings.TrimSpace(cfg.RoleName) == "" || strings.TrimSpace(cfg.CapabilityID) == "" {
		return fmt.Errorf("invalid agent worker config")
	}
	switch strings.ToUpper(cfg.ProtocolLevel) {
	case "L0", "L1", "L2", "L3":
	default:
		return fmt.Errorf("invalid protocol level")
	}
	s.cfg = cfg
	return nil
}

func (s *Service) EnsureSystemPrincipals(ctx context.Context) error {
	for _, p := range []struct{ id, name string }{{AuthorityPrincipal, "Agent Authority"}, {WorkerPrincipal, "Agent Worker"}, {VerifierPrincipal, "Agent Verifier"}} {
		var status string
		err := s.db.QueryRowContext(ctx, `SELECT status FROM principals WHERE id=?`, p.id).Scan(&status)
		if err == nil {
			if status != "active" {
				return fmt.Errorf("system principal %s is not active", p.id)
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		now := s.clock.UnixMilli()
		actor := p.id
		if err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
			if _, err := tx.ExecContext(ctx, `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES(?,'system',?,'active',1,?,?)`, p.id, p.name, now, now); err != nil {
				return err
			}
			eid, _ := s.ids.New("evt")
			payload, _ := json.Marshal(map[string]any{"principal_id": p.id, "principal_type": "system", "purpose": "autonomous_agent_worker"})
			return s.events.Append(ctx, tx, event.Event{ID: eid, Type: "system_principal.registered", AggregateType: "principal", AggregateID: p.id, ActorPrincipalID: &actor, Payload: payload, OccurredAt: now})
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ensureWorkspaceAccess(ctx context.Context, workspaceID string) error {
	if strings.TrimSpace(workspaceID) == "" {
		return fmt.Errorf("workspace id is required")
	}
	if err := s.EnsureSystemPrincipals(ctx); err != nil {
		return err
	}
	for _, principal := range []string{AuthorityPrincipal, WorkerPrincipal, VerifierPrincipal} {
		var status string
		err := s.db.QueryRowContext(ctx, `SELECT status FROM workspace_memberships WHERE workspace_id=? AND principal_id=?`, workspaceID, principal).Scan(&status)
		if err == nil {
			if status != "active" {
				return fmt.Errorf("system membership %s/%s is %s", workspaceID, principal, status)
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		now := s.clock.UnixMilli()
		actor := AuthorityPrincipal
		if err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
			var wsStatus string
			if err := tx.QueryRowContext(ctx, `SELECT status FROM workspaces WHERE id=?`, workspaceID).Scan(&wsStatus); err != nil {
				return err
			}
			if wsStatus != "active" {
				return fmt.Errorf("workspace %s is not active", workspaceID)
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES(?,?,'active',?,?)`, workspaceID, principal, now, now); err != nil {
				return err
			}
			eid, _ := s.ids.New("evt")
			payload, _ := json.Marshal(map[string]any{"workspace_id": workspaceID, "principal_id": principal, "purpose": "autonomous_agent_worker"})
			return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: &workspaceID, Type: "workspace.system_membership_added", AggregateType: "workspace_membership", AggregateID: workspaceID + ":" + principal, ActorPrincipalID: &actor, Payload: payload, OccurredAt: now})
		}); err != nil {
			return err
		}
	}
	return nil
}

// RecoverLostRuns is intentionally fail-closed. A previous worker may have
// consumed paid/subscription inference or initiated a gateway-mediated action;
// after restart we do not infer that an in-flight step never happened.
func (s *Service) RecoverLostRuns(ctx context.Context) (int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT r.id,t.id,t.revision FROM agent_worker_runs r JOIN tasks t ON t.id=r.task_id WHERE r.status='running' ORDER BY r.started_at,r.id`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	type x struct {
		run, task string
		rev       int64
	}
	var xs []x
	for rows.Next() {
		var v x
		if err := rows.Scan(&v.run, &v.task, &v.rev); err != nil {
			return 0, err
		}
		xs = append(xs, v)
	}
 if err:=rows.Err();err!=nil{return 0,err}
 n:=0
 var problems []error
 for _,v:=range xs {
  // A crash is NOT evidence an external mutation failed or can be retried.
  // Hold the Task/Attempt transition, recovery audit/outbox and run
  // interruption in one durable commit. A revision race or missing attempt
  // rolls back *all* records, leaving an explicit recovery error.
  err:=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx) error{
   var status string
   if err:=tx.QueryRowContext(ctx,`SELECT status FROM agent_worker_runs WHERE id=? AND task_id=?`,v.run,v.task).Scan(&status);err!=nil{return err}
   if status!=string(RunRunning){return fmt.Errorf("worker recovery run %s is no longer running",v.run)}
   // A completed model proposal may already have entered VERIFYING and
   // created a pending independent Verification before the Worker handoff
   // was durably recorded. This is NOT an in-flight inference or Tool effect.
   // Reattach its exact pending Verification atomically without cancelling
   // the original WAITING Attempt or losing verified evidence.
   rebound,boundErr:=s.reconcilePendingAssuranceHandoffTx(ctx,tx,v.run,v.task)
   if boundErr!=nil{return boundErr}
   if rebound{return nil}
   actor:=AuthorityPrincipal
   if err:=s.tasks.InterruptForRecoveryInTransaction(ctx,tx,task.TransitionCommand{
    TaskID:v.task,ExpectedRevision:v.rev,ActorPrincipalID:&actor,
    Reason:"agent_worker_restart_unknown_step_outcome",
   });err!=nil{return fmt.Errorf("interrupt Task %s for worker recovery: %w",v.task,err)}
   now:=s.clock.UnixMilli()
   result,err:=tx.ExecContext(ctx,`UPDATE agent_worker_runs SET status='interrupted',
    last_error='daemon restart during active worker step',revision=revision+1,
    updated_at=?,completed_at=? WHERE id=? AND task_id=? AND status='running'`,
    now,now,v.run,v.task)
   if err!=nil{return err}
   affected,err:=result.RowsAffected()
   if err!=nil{return err}
   if affected!=1{return fmt.Errorf("worker recovery run %s lost ownership",v.run)}
   return nil
  })
  if err!=nil{
   // Continue with independent Workers: a corrupt Task must never prevent
   // other lost attempts from being safely blocked and journalled.
   problems=append(problems,fmt.Errorf("run %s: %w",v.run,err))
   continue
  }
  n++
 }
 // Historical partially persisted admissions and externally corrupted
 // records may have no live Agent Worker row. Reconcile those separately,
 // after known in-flight runs, without touching other Task executors.
 orphans,orphanErr:=s.recoverOrphanedAgentAttempts(ctx)
 if orphanErr!=nil{problems=append(problems,orphanErr)}
 return n+orphans,errors.Join(problems...)
}

// reconcilePendingAssuranceHandoffTx reconstructs only an already-issued,
// uniquely scoped independent verification handoff. The model completion
// proposal must have committed Task VERIFYING, original Attempt WAITING and
// a pending V1+ Verification tied to this Worker before the crash. It never
// retries inference, publishes evidence, or substitutes a different Worker.
// If evidence is ambiguous, operator review is required, not auto-selection.
func (s *Service) reconcilePendingAssuranceHandoffTx(
 ctx context.Context,tx storage.Tx,runID,taskID string,
)(bool,error){
 if tx==nil||runID==""||taskID==""{return false,ErrInvalidWorkerState}
 rows,err:=tx.QueryContext(ctx,`SELECT v.id,v.required_level,r.attempt_id,
  r.workspace_id,r.revision
  FROM agent_worker_runs r
  JOIN tasks t ON t.id=r.task_id AND t.workspace_id=r.workspace_id
    AND t.state='verifying' AND t.archived_at IS NULL
  JOIN task_attempts a ON a.id=r.attempt_id AND a.task_id=t.id
    AND a.worker_principal_id=r.worker_principal_id AND a.status='waiting'
    AND json_valid(a.metadata_json)=1
    AND json_extract(a.metadata_json,'$.agent_worker')='v1'
  JOIN workspaces w ON w.id=t.workspace_id AND w.status='active'
  LEFT JOIN projects p ON p.id=t.project_id
  LEFT JOIN project_workspaces pw ON pw.id=t.project_workspace_id
  JOIN verifications v ON v.task_id=t.id AND v.workspace_id=t.workspace_id
   AND v.status='pending' AND v.required_level<>'V0'
   AND v.subject_ref='agent_worker_run:' || r.id
   AND json_valid(v.spec_json)=1
   AND json_extract(v.spec_json,'$.source')='agent_completion_proposal'
   AND json_extract(v.spec_json,'$.worker_run_id')=r.id
  WHERE r.id=? AND r.task_id=? AND r.status='running'
   AND r.worker_principal_id=?
   AND (t.project_id IS NULL OR
    (p.id=t.project_id AND p.workspace_id=t.workspace_id AND p.status='active'))
   AND (t.project_workspace_id IS NULL OR
    (t.project_id IS NOT NULL AND pw.id=t.project_workspace_id
     AND pw.project_id=t.project_id AND pw.status='active'))
  LIMIT 2`,runID,taskID,WorkerPrincipal)
 if err!=nil{return false,err}
 type binding struct{verification,level,attempt,workspace string;revision int64}
 var bindings []binding
 for rows.Next(){
  var b binding
  if err=rows.Scan(&b.verification,&b.level,&b.attempt,&b.workspace,&b.revision);err!=nil{break}
  bindings=append(bindings,b)
 }
 if err==nil{err=rows.Err()}
 closeErr:=rows.Close()
 if err!=nil{return false,err}
 if closeErr!=nil{return false,closeErr}
 if len(bindings)==0{return false,nil}
 if len(bindings)!=1{
  return false,fmt.Errorf("ambiguous pending independent assurance handoff for Worker %s: operator review required",runID)
 }
 b:=bindings[0]
 continuation,err:=json.Marshal(map[string]any{
  "verification_id":b.verification,"required_verification":b.level,
  "reconciled_after_restart":true,
 })
 if err!=nil{return false,err}
 now:=s.clock.UnixMilli()
 result,err:=tx.ExecContext(ctx,`UPDATE agent_worker_runs
  SET status='blocked',continuation_json=?,last_error=?,
    revision=revision+1,updated_at=?
  WHERE id=? AND task_id=? AND attempt_id=? AND workspace_id=?
    AND worker_principal_id=? AND status='running' AND revision=?`,
  string(continuation),"pending independent assurance restored after restart",
  now,runID,taskID,b.attempt,b.workspace,WorkerPrincipal,b.revision)
 if err!=nil{return false,err}
 affected,err:=result.RowsAffected()
 if err!=nil{return false,err}
 if affected!=1{return false,ErrInvalidWorkerState}
 err=s.journalInTransaction(ctx,tx,runID,"verification","waiting",
  nil,nil,strPtr("complete"),strPtr(b.verification),map[string]any{
   "verification_id":b.verification,"required_level":b.level,
   "action":"restart_reconcile_independent_assurance",
   "no_model_or_tool_replay":true,
  })
 if err!=nil{return false,err}
 return true,nil
}

func (s *Service) Tick(ctx context.Context, limit int) ([]TickResult, error) {
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	if err := s.EnsureSystemPrincipals(ctx); err != nil {
		return nil, err
	}
	// Continue independent work after a corrupt waiting continuation.
	// Return the actionable per-run error with any completed tick results.
	wakeErr:=s.syncResumedRuns(ctx)
	_,admissionErr:=s.admitCreated(ctx,limit)
	// Per-Task admissions are independent. A corrupt CREATED Task cannot
	// starve already running Workers or eligible siblings in this tick.
	runs,activeErr:=s.activeRuns(ctx,limit)
	// Preserve the exact corrupt Worker IDs for operator recovery, but do
	// not strand independent running/ready Tasks in the same tick.
	tickErrors:=errors.Join(wakeErr,admissionErr,activeErr)
	out:=make([]TickResult,0,limit)
	for _,r:=range runs{
		out=append(out,s.step(ctx,r))
		if len(out)>=limit{return out,tickErrors}
	}
	ready,readyErr:=s.readyTasks(ctx,limit-len(out))
	// A corrupt eligible Task is not authority to stall healthy READY Tasks.
	// Retain its ID in the Tick error while admitting independent siblings.
	tickErrors=errors.Join(tickErrors,readyErr)
	for _,t:=range ready{
		r,res:=s.startRun(ctx,t)
		if res.Error!=""{
			out=append(out,res)
			continue
		}
		out=append(out,s.step(ctx,r))
	}
	return out,tickErrors
}

func (s *Service) admitCreated(ctx context.Context, limit int) (int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT t.id,t.revision FROM tasks t
WHERE t.state='created'
AND NOT EXISTS (SELECT 1 FROM task_execution_profiles ep LEFT JOIN team_sessions ts ON ts.id=ep.team_session_id WHERE ep.task_id=t.id AND ep.execution_mode='team' AND (ts.accepted_plan_id IS NULL OR ts.status NOT IN ('plan_accepted','executing')))
AND NOT EXISTS (SELECT 1 FROM routine_occurrences ro JOIN project_routine_bindings b ON b.routine_id=ro.routine_id AND b.status='active' AND b.action_kind='app_command' WHERE ro.task_id=t.id)
AND NOT EXISTS (SELECT 1 FROM task_dependencies d JOIN tasks dep ON dep.id=d.depends_on_task_id WHERE d.task_id=t.id AND d.dependency_type='hard' AND (dep.state<>'complete' OR dep.archived_at IS NOT NULL))
ORDER BY t.priority DESC,t.created_at,t.id LIMIT ?`, limit)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	type x struct {
		id  string
		rev int64
	}
	var xs []x
	for rows.Next() {
		var v x
		if err := rows.Scan(&v.id, &v.rev); err != nil {
			return 0, err
		}
		xs = append(xs, v)
	}
	n := 0
	actor := AuthorityPrincipal
	var problems []error
	for _, v := range xs {
		if _, err := s.tasks.MarkReady(ctx, task.TransitionCommand{TaskID: v.id, ExpectedRevision: v.rev, ActorPrincipalID: &actor, Reason: "autonomous worker admission"}); err == nil {
			n++
		} else if !task.IsRevisionConflict(err) {
			// Do not convert a failure into success, and do not silently
			// discard its Task ID. Other Task admissions still proceed.
			problems=append(problems,fmt.Errorf("admit created Task %s: %w",v.id,err))
		}
	}
	return n,errors.Join(append(problems,rows.Err())...)
}

func (s *Service) syncResumedRuns(ctx context.Context) error {
	// Parent Tasks waiting on delegated children resume only after all hard
	// dependencies complete. Human/capability waits resume when another trusted
	// control-plane action moves the Task back to RUNNING.
	rows, err := s.db.QueryContext(ctx, `SELECT r.id,t.id,t.revision,t.state FROM agent_worker_runs r JOIN tasks t ON t.id=r.task_id WHERE r.status='waiting'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type x struct {
		run, task, state string
		rev              int64
	}
	var xs []x
	for rows.Next() {
		var v x
		if err := rows.Scan(&v.run, &v.task, &v.rev, &v.state); err != nil {
			return err
		}
		xs = append(xs, v)
	}
	var problems []error
	for _, v := range xs {
		// One corrupt Worker continuation must not prevent independent
		// waiting Tasks from being reconciled on the same daemon tick.
		if runErr:=func()error{
		if v.state == string(task.StateWaitingDependency) {
			var blocked int
			if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_dependencies d JOIN tasks dep ON dep.id=d.depends_on_task_id WHERE d.task_id=? AND d.dependency_type='hard' AND (dep.state<>'complete' OR dep.archived_at IS NOT NULL)`, v.task).Scan(&blocked); err != nil {
				return err
			}
			ready := blocked == 0
			// A mutation proposal may also wait on an Operation that is independently
			// observed/verified. If continuation names one, it must be COMMITTED before
			// the attempt resumes.
			var cont string
			if err := s.db.QueryRowContext(ctx, `SELECT continuation_json FROM agent_worker_runs WHERE id=?`, v.run).Scan(&cont); err != nil {
				return err
			}
			var c struct {
				OperationID string `json:"operation_id"`
			}
			if json.Unmarshal([]byte(cont), &c) != nil {
				return fmt.Errorf("invalid durable Agent Worker continuation for run %s",v.run)
			}
			// A model-resource wait is a real persisted suspension, not a
			// dependency-free Task that should resume on every worker tick.
			// Retain the same pinned model and local-only routing after wake.
			if hasModelWaitField(json.RawMessage(cont))&&decodeModelWait(json.RawMessage(cont))==nil {
                return fmt.Errorf("corrupt persisted local model wait for Worker run %s",v.run)
            }
			if modelWait:=decodeModelWait(json.RawMessage(cont));modelWait!=nil &&
				s.clock.UnixMilli()<modelWait.RetryAtMS {
				ready=false
			}
			if hasToolchainWaitField(json.RawMessage(cont))&&decodeToolchainWait(json.RawMessage(cont))==nil{
				return fmt.Errorf("corrupt persisted Workspace toolchain wait for Worker run %s",v.run)
			}
			if toolchainWait:=decodeToolchainWait(json.RawMessage(cont));toolchainWait!=nil{
				ready=false
				if s.clock.UnixMilli()>=toolchainWait.RetryAtMS{
					registered,checkErr:=isApprovedToolchainRegisteredRunning(ctx,s.db,toolchainWait)
					if checkErr!=nil{return fmt.Errorf("recheck persisted Workspace toolchain wait: %w",checkErr)}
					ready=blocked==0&&registered
                    if !ready {
                        // Advance the persisted deadline atomically: an
                        // unavailable approved runtime must not be rechecked
                        // on every Worker tick after the first expiry.
                        if err:=s.deferWorkspaceToolchainWait(ctx,v.run,cont,toolchainWait);err!=nil&&
                            !errors.Is(err,ErrInvalidWorkerState){
                            // A stale CAS means another Worker or operator
                            // already changed this wait; do not overwrite it.
                            return fmt.Errorf("defer approved Workspace runtime recheck: %w",err)
                        }
                    }
				}
			}
			if strings.TrimSpace(c.OperationID) != "" {
				var opState string
				if err := s.db.QueryRowContext(ctx, `SELECT state FROM operations WHERE id=?`, c.OperationID).Scan(&opState); err != nil {
					return err
				}
				ready = ready && opState == "committed"
			}
			if ready {
                // Task, existing Attempt, Worker and outbox either all wake
                // or all remain suspended after a failed resume.
                if err:=s.resumeWaitingWorkerAndAttempt(ctx,v.run,v.task,v.rev,cont);err!=nil &&
                    !task.IsRevisionConflict(err)&&!errors.Is(err,ErrInvalidWorkerState) {
                    return fmt.Errorf("resume waiting Worker/Task/Attempt: %w",err)
                }
            }
		}
		var state string
		if err := s.db.QueryRowContext(ctx, `SELECT state FROM tasks WHERE id=?`, v.task).Scan(&state); err != nil {
			return err
		}
		if state == string(task.StateRunning) {
            // Externally authorised Task resume: require its matching
            // original Attempt to be running before reconciling the Worker.
            if _,err:=s.db.ExecContext(ctx,`UPDATE agent_worker_runs
             SET status='running',revision=revision+1,updated_at=?
             WHERE id=? AND task_id=? AND status='waiting'
             AND EXISTS(SELECT 1 FROM tasks t WHERE t.id=agent_worker_runs.task_id
               AND t.workspace_id=agent_worker_runs.workspace_id AND t.state='running')
             AND EXISTS(SELECT 1 FROM task_attempts a WHERE a.id=agent_worker_runs.attempt_id
               AND a.task_id=agent_worker_runs.task_id AND a.status='running')`,
               s.clock.UnixMilli(),v.run,v.task);err!=nil{return err}
        }
		return nil
		}();runErr!=nil{
			problems=append(problems,fmt.Errorf("reconcile waiting Worker %s Task %s: %w",v.run,v.task,runErr))
		}
	}
	return errors.Join(append(problems,rows.Err())...)
}

// resumeWaitingWorkerAndAttempt is the atomic counterpart of
// suspendForResource. The Task event/outbox, Attempt and Worker resume commit
// together; stale revisions and journal failures roll back all transitions.
func (s *Service) resumeWaitingWorkerAndAttempt(
 ctx context.Context,runID,taskID string,expectedTaskRevision int64,expectedContinuation string,
) error {
 if s==nil||s.tx==nil||s.tasks==nil||runID==""||taskID==""||
    expectedTaskRevision<1||expectedContinuation=="" {return ErrInvalidWorkerState}
 return s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  var workspaceID,attemptID string
  var runRevision int64
  err:=tx.QueryRowContext(ctx,`SELECT workspace_id,attempt_id,revision
    FROM agent_worker_runs WHERE id=? AND task_id=? AND status='waiting'
    AND continuation_json=?`,runID,taskID,expectedContinuation).
    Scan(&workspaceID,&attemptID,&runRevision)
  if errors.Is(err,sql.ErrNoRows){return ErrInvalidWorkerState}
  if err!=nil{return err}
  var taskWorkspace string
  err=tx.QueryRowContext(ctx,`SELECT workspace_id FROM tasks WHERE id=?
   AND state='waiting_dependency' AND revision=?`,
   taskID,expectedTaskRevision).Scan(&taskWorkspace)
  if errors.Is(err,sql.ErrNoRows){return task.ErrRevisionConflict}
  if err!=nil{return err}
  if taskWorkspace!=workspaceID{return ErrInvalidWorkerState}
  changed,err:=tx.ExecContext(ctx,`UPDATE agent_worker_runs
   SET status='running',revision=revision+1,updated_at=?
   WHERE id=? AND task_id=? AND workspace_id=? AND attempt_id=?
   AND revision=? AND status='waiting' AND continuation_json=?`,
   s.clock.UnixMilli(),runID,taskID,workspaceID,attemptID,
   runRevision,expectedContinuation)
  if err!=nil{return err}
  affected,err:=changed.RowsAffected()
  if err!=nil{return err}
  if affected!=1{return ErrInvalidWorkerState}
  actor:=AuthorityPrincipal
  return s.tasks.ResumeWaitingAttemptInTransaction(ctx,tx,task.TransitionCommand{
   TaskID:taskID,ExpectedRevision:expectedTaskRevision,ActorPrincipalID:&actor,
   Reason:"worker dependencies/resources ready for retry",
  },attemptID)
 })
}

// activeRuns pages by a stable (updated_at,id) cursor instead of applying
// LIMIT before decoding. One corrupt early JSON row previously discarded
// every healthy row and starved other Tasks on every subsequent tick.
// Bounded-size pages preserve local-first memory use; a bad run is never
// executed or silently replaced, and its exact ID is retained as an error.
func (s *Service) activeRuns(ctx context.Context,limit int)([]Run,error){
	if limit<=0||limit>100{limit=10}
	const pageSize=64
	lastTime:=int64(0)
	lastID:=""
	first:=true
	out:=make([]Run,0,limit)
	var problems []error
	var moreFailures int
	for len(out)<limit{
		var rows *sql.Rows
		var err error
		if first{
			rows,err=s.db.QueryContext(ctx,`SELECT r.id,r.updated_at
			 FROM agent_worker_runs r WHERE r.status='running'
			 ORDER BY r.updated_at,r.id LIMIT ?`,pageSize)
			first=false
		}else{
			rows,err=s.db.QueryContext(ctx,`SELECT r.id,r.updated_at
			 FROM agent_worker_runs r WHERE r.status='running'
			 AND (r.updated_at>? OR (r.updated_at=? AND r.id>?))
			 ORDER BY r.updated_at,r.id LIMIT ?`,
			 lastTime,lastTime,lastID,pageSize)
		}
		if err!=nil{return out,errors.Join(append(problems,err)...)}
		type candidate struct{id string;timestamp int64}
		page:=make([]candidate,0,pageSize)
		for rows.Next(){
			var row candidate
			if scanErr:=rows.Scan(&row.id,&row.timestamp);scanErr!=nil{
				err=scanErr
				break
			}
			page=append(page,row)
		}
		if err==nil{err=rows.Err()}
		closeErr:=rows.Close()
		if err==nil{err=closeErr}
		if err!=nil{return out,errors.Join(append(problems,err)...)}
		if len(page)==0{break}
		for _,candidate:=range page{
			lastTime,lastID=candidate.timestamp,candidate.id
			run,getErr:=s.getRun(ctx,candidate.id)
			if getErr!=nil{
				// Cap diagnostic allocation even with thousands of damaged
				// rows; a count still flags additional failed identities.
				if len(problems)<32{
					problems=append(problems,fmt.Errorf("decode active Worker %s: %w",candidate.id,getErr))
				}else{moreFailures++}
				continue
			}
			if run.Status!=RunRunning{continue}
			out=append(out,run)
			if len(out)>=limit{break}
		}
		if len(page)<pageSize{break}
	}
	if moreFailures>0{
		problems=append(problems,fmt.Errorf("%d additional corrupt active Workers require operator review",moreFailures))
	}
	return out,errors.Join(problems...)
}

// readyTasks scans the READY queue in bounded pages and collects up to
// limit HEALTHY Tasks. Previously LIMIT applied before validating each Task;
// one damaged high-priority record caused every later Task to starve forever.
// Record corrupt IDs for operator repair, without admitting/substituting them.
func (s *Service) readyTasks(ctx context.Context,limit int)([]task.Task,error){
 if limit<=0{return nil,nil}
 const pageSize=64
 first:=1
 lastPriority:=0
 var lastTime int64
 lastID:=""
 out:=make([]task.Task,0,limit)
 var problems []error
 var overflow int
 for len(out)<limit{
  rows,err:=s.db.QueryContext(ctx,`SELECT t.id,t.priority,
   COALESCE(t.ready_at,t.created_at)
   FROM tasks t WHERE t.state='ready'
   AND NOT EXISTS(SELECT 1 FROM task_attempts a WHERE a.task_id=t.id
    AND a.status IN ('created','queued','running','waiting'))
   AND NOT EXISTS(SELECT 1 FROM routine_occurrences ro
    JOIN project_routine_bindings b ON b.routine_id=ro.routine_id
     AND b.status='active' AND b.action_kind='app_command'
    WHERE ro.task_id=t.id)
   AND NOT EXISTS(SELECT 1 FROM task_dependencies d
    JOIN tasks dep ON dep.id=d.depends_on_task_id
    WHERE d.task_id=t.id AND d.dependency_type='hard'
     AND (dep.state<>'complete' OR dep.archived_at IS NOT NULL))
   AND (?=1 OR t.priority<? OR
    (t.priority=? AND (COALESCE(t.ready_at,t.created_at)>? OR
     (COALESCE(t.ready_at,t.created_at)=? AND t.id>?))))
   ORDER BY t.priority DESC,COALESCE(t.ready_at,t.created_at),t.id
   LIMIT ?`,first,lastPriority,lastPriority,lastTime,lastTime,lastID,pageSize)
  if err!=nil{return out,errors.Join(append(problems,err)...)}
  type row struct{id string;priority int;at int64}
  page:=make([]row,0,pageSize)
  for rows.Next(){
   var v row
   if err=rows.Scan(&v.id,&v.priority,&v.at);err!=nil{break}
   page=append(page,v)
  }
  if err==nil{err=rows.Err()}
  closeErr:=rows.Close()
  if err==nil{err=closeErr}
  if err!=nil{return out,errors.Join(append(problems,err)...)}
  if len(page)==0{break}
  first=0
  for _,item:=range page{
   lastID,lastPriority,lastTime=item.id,item.priority,item.at
   t,loadErr:=s.tasks.Get(ctx,item.id)
   if loadErr!=nil{
    if len(problems)<32{
     problems=append(problems,fmt.Errorf("read eligible Task %s: %w",item.id,loadErr))
    }else{overflow++}
    continue
   }
   if t.State!=task.StateReady{continue}
   out=append(out,t)
   if len(out)>=limit{break}
  }
  if len(page)<pageSize{break}
 }
 if overflow>0{problems=append(problems,
  fmt.Errorf("%d additional corrupt READY Tasks require operator review",overflow))}
 return out,errors.Join(problems...)
}

const runSelect = `SELECT r.id,r.workspace_id,r.task_id,r.attempt_id,r.worker_principal_id,r.status,r.role_name,r.capability_id,r.protocol_level,r.max_steps,r.step_count,r.max_replans,r.replan_count,r.max_escalations,r.escalation_count,r.route_policy_json,r.continuation_json,r.last_candidate_kind,r.last_candidate_id,r.last_error,r.revision,r.started_at,r.updated_at,r.completed_at FROM agent_worker_runs r`

type rowScanner interface{ Scan(...any) error }

func scanRun(row rowScanner) (Run, error) {
	var r Run
	var lastKind, lastID, lastErr sql.NullString
	var completed sql.NullInt64
	var routeJSON, continuationJSON string
	// SQLite's TEXT columns scan into strings, not *json.RawMessage.
	// Reconstruct the exact persisted JSON after the successful scan.
	err := row.Scan(&r.ID, &r.WorkspaceID, &r.TaskID, &r.AttemptID, &r.WorkerPrincipalID, &r.Status, &r.RoleName, &r.CapabilityID, &r.ProtocolLevel, &r.MaxSteps, &r.StepCount, &r.MaxReplans, &r.ReplanCount, &r.MaxEscalations, &r.EscalationCount, &routeJSON, &continuationJSON, &lastKind, &lastID, &lastErr, &r.Revision, &r.StartedAt, &r.UpdatedAt, &completed)
	if err!=nil{return Run{},err}
	if !json.Valid([]byte(routeJSON)) || !json.Valid([]byte(continuationJSON)){
		return Run{},fmt.Errorf("persisted Agent Worker run contains invalid JSON")
	}
	r.RoutePolicy=json.RawMessage(routeJSON)
	r.Continuation=json.RawMessage(continuationJSON)
	if lastKind.Valid {
		v := lastKind.String
		r.LastCandidateKind = &v
	}
	if lastID.Valid {
		v := lastID.String
		r.LastCandidateID = &v
	}
	if lastErr.Valid {
		v := lastErr.String
		r.LastError = &v
	}
	if completed.Valid {
		v := completed.Int64
		r.CompletedAt = &v
	}
	return r, err
}

func (s *Service) startRun(ctx context.Context, t task.Task) (Run, TickResult) {
 res:=TickResult{TaskID:t.ID,Status:"failed"}
 if err:=s.ensureWorkspaceAccess(ctx,t.WorkspaceID);err!=nil{
  res.Error=err.Error();return Run{},res
 }
 worker:=WorkerPrincipal
 actor:=AuthorityPrincipal
 cmd:=task.StartCommand{
  TaskID:t.ID,ExpectedRevision:t.Revision,WorkerPrincipalID:&worker,
  ActorPrincipalID:&actor,Metadata:json.RawMessage(`{"agent_worker":"v1"}`),
 }
 // Preserve the ordinary admission policy before entering the write
 // transaction. StartInTransaction enforces the same Task CAS again.
 if err:=s.tasks.CheckStartAdmission(ctx,cmd);err!=nil{
  res.Error=err.Error();return Run{},res
 }
 rid,err:=s.ids.New("awrun")
 if err!=nil{res.Error=err.Error();return Run{},res}
 eventID,err:=s.ids.New("evt")
 if err!=nil{res.Error=err.Error();return Run{},res}
 now:=s.clock.UnixMilli()
 route:=defaultRoutePolicy()
 workspaceRouting:=routingPolicyFromCompletion(t.Completion)
 if workspaceRouting.Enabled!=nil{
  route.RoutingEnabled=*workspaceRouting.Enabled
  route.AllowDelegation=*workspaceRouting.Enabled
 }
 route.AllowRemote=effectiveRemoteModelAllowance(t,workspaceRouting,route.AllowRemote)
 placement,placementErr:=computePlacementForTask(t)
 if placementErr!=nil{res.Error=placementErr.Error();return Run{},res}
 route.ComputePreference=placement
 ids:=make([]string,0,1+len(workspaceRouting.FallbackCandidateIDs))
 if v:=strings.TrimSpace(workspaceRouting.CandidateID);v!=""{
  ids=append(ids,v)
 }
 if route.RoutingEnabled{
  for _,id:=range workspaceRouting.FallbackCandidateIDs{
   if id=strings.TrimSpace(id);id!=""&&!contains(ids,id){ids=append(ids,id)}
  }
 }
 route.IncludeCandidateIDs=ids
 maxEscalations:=s.cfg.MaxEscalations
 if !route.RoutingEnabled{maxEscalations=0}
 rp,err:=json.Marshal(route)
 if err!=nil{res.Error=err.Error();return Run{},res}
 var r Run
 // Previously Task.Start committed before Worker creation. Any DB failure
 // between those writes stranded a RUNNING Task and Attempt without a Worker
 // row; RecoverLostRuns could not discover the orphan. All admission writes
 // and both audit/outbox events must now commit together or not at all.
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  started,attempt,startErr:=s.tasks.StartInTransaction(ctx,tx,cmd)
  if startErr!=nil{return startErr}
  r=Run{
   ID:rid,WorkspaceID:started.WorkspaceID,TaskID:started.ID,
   AttemptID:attempt.ID,WorkerPrincipalID:worker,Status:RunRunning,
   RoleName:s.cfg.RoleName,CapabilityID:s.cfg.CapabilityID,
   ProtocolLevel:strings.ToUpper(s.cfg.ProtocolLevel),
   MaxSteps:s.cfg.MaxSteps,MaxReplans:s.cfg.MaxReplans,
   MaxEscalations:maxEscalations,RoutePolicy:rp,
   Continuation:json.RawMessage(`{}`),Revision:1,StartedAt:now,UpdatedAt:now,
  }
  if _,err:=tx.ExecContext(ctx,`INSERT INTO agent_worker_runs(id,workspace_id,task_id,attempt_id,worker_principal_id,status,role_name,capability_id,protocol_level,max_steps,step_count,max_replans,replan_count,max_escalations,escalation_count,route_policy_json,continuation_json,revision,started_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
   r.ID,r.WorkspaceID,r.TaskID,r.AttemptID,r.WorkerPrincipalID,
   r.Status,r.RoleName,r.CapabilityID,r.ProtocolLevel,r.MaxSteps,
   0,r.MaxReplans,0,r.MaxEscalations,0,string(r.RoutePolicy),
   string(r.Continuation),1,now,now);err!=nil{return err}
  p,err:=json.Marshal(map[string]any{
   "run_id":r.ID,"task_id":r.TaskID,"attempt_id":r.AttemptID,
   "max_steps":r.MaxSteps,
  })
  if err!=nil{return err}
  return s.events.Append(ctx,tx,event.Event{
   ID:eventID,WorkspaceID:&r.WorkspaceID,Type:"agent_worker.started",
   AggregateType:"agent_worker_run",AggregateID:r.ID,
   ActorPrincipalID:&actor,Payload:p,OccurredAt:now,
  })
 })
 if err!=nil{res.Error=err.Error();return Run{},res}
 res.RunID=r.ID
 res.Status="running"
 return r,res
}

func (s *Service) getRun(ctx context.Context, id string) (Run, error) {
	return scanRun(s.db.QueryRowContext(ctx, runSelect+` WHERE r.id=?`, id))
}
