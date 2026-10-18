package agentworker

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
 "sync"

	"github.com/DigiLogicTech/OnePane/internal/agentruntime"
	"github.com/DigiLogicTech/OnePane/internal/artifact"
	"github.com/DigiLogicTech/OnePane/internal/authority"
	"github.com/DigiLogicTech/OnePane/internal/budget"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/featurepolicy"
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

type createdAdmissionPosition struct {
 priority int
 createdAt int64
 taskID string
 valid bool
}

type activeQueuePosition struct {
 updatedAt int64
 runID string
 valid bool
}

type readyQueuePosition struct {
 priority int
 at int64
 taskID string
 valid bool
}

type Service struct {
 // Serialize complete scheduler cycles, including slow inference and
 // proposal application. Merely CASing the dispatch revision cannot
 // prevent a second Tick from starting another step while a previous
 // inference request is still in flight.
 workerCycleMu sync.Mutex
 // An unresolved dispatch must never be retried if recovery persistence failed.
 // Access is serialized by workerCycleMu, including startup recovery.
 dispatchReconciliationFailed bool
 // One-slot scheduler calls alternate Active and READY work when both are
 // present. The mutex protects only the preference, never executes a step.
 tickFairnessMu sync.Mutex
 nextSingleSlotReady bool
 // This process-local position bounds wait reconciliation CPU/SQLite per
 // Tick without altering durable run/Attempt identity or authority.
 waitingReconcileMu sync.Mutex
 waitingReconcileCursor string
 // A malformed early READY Task cannot permanently occupy the available
 // queue slots or use an unbounded number of SQLite reads on a small host.
 readyQueueMu sync.Mutex
 readyQueuePosition readyQueuePosition
 // Active Worker decode must not repeatedly traverse an unlimited corrupt
 // prefix on an older CPU. This bounded rotating cursor is process-local.
 activeQueueMu sync.Mutex
 activeQueuePosition activeQueuePosition
 // A long queue of failing CREATED Tasks must never monopolise the CPU on
 // older hosts. This process-local cursor rotates through that queue over
 // consecutive Ticks, and the mutex serializes concurrent scheduler calls.
 createdAdmissionMu sync.Mutex
 createdAdmissionPosition createdAdmissionPosition
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
 // A config change during a model/tool step could change future context
 // limits and authority expectations midway through one execution cycle.
 // Require an idle Worker rather than mutate settings concurrently.
 if !s.workerCycleMu.TryLock(){return ErrWorkerCycleInProgress}
 defer s.workerCycleMu.Unlock()
 s.cfg=cfg
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
	var currentStatus string
	if err := s.db.QueryRowContext(ctx, "SELECT status FROM workspaces WHERE id=?", workspaceID).Scan(&currentStatus); err != nil {
		return fmt.Errorf("load Workspace %s status: %w", workspaceID, err)
	}
	if currentStatus != "active" {
		return fmt.Errorf("Workspace %s is not active (state=%s)", workspaceID, currentStatus)
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
 // Startup crash recovery must not interrupt a live scheduler step in
 // this process. Refuse concurrent recovery rather than guess outcome.
 if !s.workerCycleMu.TryLock(){return 0,ErrWorkerCycleInProgress}
 defer s.workerCycleMu.Unlock()
 // Startup may encounter thousands of interrupted model jobs. Process
 // bounded keyset pages, and close the SQLite cursor before opening any
 // recovery write transaction. OFFSET would skip work as status changes.
 const pageSize=256
 type x struct {
  run,task string
  rev,startedAt int64
 }
 recovered:=0
 problems := boundedWorkerRecoveryErrors{category:"lost running Worker recoveries"}
 var lastStartedAt int64
 var lastRunID string
 firstPage:=true
 for{
  rows,err:=s.db.QueryContext(ctx,`SELECT r.id,t.id,t.revision,r.started_at
    FROM agent_worker_runs r JOIN tasks t ON t.id=r.task_id
    WHERE r.status='running'
      AND (?=1 OR (r.started_at,r.id)>(?,?))
    ORDER BY r.started_at,r.id LIMIT ?`,
    firstPage,lastStartedAt,lastRunID,pageSize)
  if err!=nil{return recovered,problems.result(err)}
  xs:=make([]x,0,pageSize)
  for rows.Next(){
   var v x
   if err=rows.Scan(&v.run,&v.task,&v.rev,&v.startedAt);err!=nil{break}
   xs=append(xs,v)
  }
  if err==nil{err=rows.Err()}
  closeErr:=rows.Close()
  if err!=nil||closeErr!=nil{
   return recovered,problems.result(err,closeErr)
  }
  if len(xs)==0{break}
  last:=xs[len(xs)-1]
  lastStartedAt,lastRunID=last.startedAt,last.run
  firstPage=false
  for _,v:=range xs {
  // A crash is NOT evidence an external mutation failed or can be retried.
  // Hold the Task/Attempt transition, recovery audit/outbox and run
  // interruption in one durable commit. A revision race or missing attempt
  // rolls back *all* records, leaving an explicit recovery error.
  err:=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx) error{
   // Recheck the exact active original Attempt rather than allowing
   // the Task recovery primitive to select another Attempt.
   var status, attemptStatus string
   var attemptPrincipal sql.NullString
   var activeCount int
   err:=tx.QueryRowContext(ctx,`SELECT r.status,a.status,a.worker_principal_id,
     (SELECT COUNT(*) FROM task_attempts other
       WHERE other.task_id=r.task_id
         AND other.status IN ('created','queued','running','waiting'))
     FROM agent_worker_runs r
     JOIN tasks t ON t.id=r.task_id AND t.workspace_id=r.workspace_id
     JOIN task_attempts a ON a.id=r.attempt_id AND a.task_id=t.id
     WHERE r.id=? AND r.task_id=? AND t.revision=?`,
     v.run,v.task,v.rev).Scan(&status,&attemptStatus,&attemptPrincipal,&activeCount)
   if err!=nil{return fmt.Errorf("Worker recovery identity unavailable: %w",err)}
   if status!=string(RunRunning)||activeCount!=1||
     (attemptStatus!="created"&&attemptStatus!="queued"&&
      attemptStatus!="running"&&attemptStatus!="waiting")||
     !attemptPrincipal.Valid||attemptPrincipal.String!=WorkerPrincipal{
     return ErrInvalidWorkerState
   }
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
   problems.add(fmt.Errorf("run %s: %w",v.run,err))
   continue
  }
  recovered++
  }
 }
 // Historical partially persisted admissions and externally corrupted
 // records may have no live Agent Worker row. Reconcile those separately,
 // after known in-flight runs, without touching other Task executors.
 orphans,orphanErr:=s.recoverOrphanedAgentAttempts(ctx)
 if orphanErr!=nil{problems.add(orphanErr)}
 recoveryErr:=problems.result()
 if recoveryErr==nil{s.dispatchReconciliationFailed=false}
 return recovered+orphans,recoveryErr
}

func (s *Service) Tick(ctx context.Context, limit int) ([]TickResult, error) {
 // Do not overlap read/route/inference/proposal cycles on the same Service.
 // The StepCount revision CAS is not an in-flight execution lease.
 if !s.workerCycleMu.TryLock(){return nil,ErrWorkerCycleInProgress}
 defer s.workerCycleMu.Unlock()
 if s.dispatchReconciliationFailed{return nil,ErrDispatchRequiresReconciliation}
 if limit<=0||limit>100 {limit=10}
 if err:=s.EnsureSystemPrincipals(ctx);err!=nil{return nil,err}

 // Both reconciliation and CREATED promotion retain bounded budgets and
 // independent failure reports. Neither may hide healthy runnable work.
 wakeErr:=s.syncResumedRuns(ctx)
 _,admissionErr:=s.admitCreated(ctx,limit)

 // Query READY eligibility read-only *before* consuming the whole step
 // quota with long-lived ACTIVE Workers. This guarantees a READY Task can
 // make progress even while active models remain queued for many Ticks.
 // The ordinary startRun CAS and Workspace/lease checks remain authoritative.
 ready,readyErr:=s.readyTasks(ctx,limit)
 activeQuota:=limit
 if len(ready)>0 {
  if limit==1{
   // With a single available step, alternate opportunities; malformed
   // or inadmissible READY Tasks cannot permanently starve active Workers.
   s.tickFairnessMu.Lock()
   preferReady:=s.nextSingleSlotReady
   s.nextSingleSlotReady=!s.nextSingleSlotReady
   s.tickFairnessMu.Unlock()
   if preferReady{activeQuota=0}
  }else{
   // Reserve one READY attempt while retaining at least one active slot.
   // If active work is sparse, all spare slots may still run READY Tasks.
   activeQuota=limit-1
  }
 }
 var runs []Run
 var activeErr error
 if activeQuota>0{runs,activeErr=s.activeRuns(ctx,activeQuota)}
 tickErrors:=errors.Join(wakeErr,admissionErr,readyErr,activeErr)
 out:=make([]TickResult,0,limit)
 for _,r:=range runs{
  out=append(out,s.step(ctx,r))
  if len(out)>=limit{return out,tickErrors}
 }
 for _,t:=range ready{
  if len(out)>=limit{break}
  r,res:=s.startRun(ctx,t)
  if res.Error!=""{
   out=append(out,res)
   continue
  }
  out=append(out,s.step(ctx,r))
 }
 return out,tickErrors
}

// admitCreated pages independently eligible CREATED Tasks by stable priority
// cursor until LIMIT successful admissions are found. The old LIMIT-before-
// transition behavior repeatedly selected the same corrupt high-priority
// records, starving all healthy lower-priority Tasks on every Tick.
// Failed Tasks are never silently marked READY, and their IDs remain visible.
// admitCreated admits up to limit successful Tasks without probing every
// corrupt CREATED Task on the same scheduler tick. The stable priority
// cursor survives subsequent calls within this Service process, so healthy
// work after an unhealthy page is not starved. A restart safely resets to the
// beginning, but never silently promotes or discards an uncertain Task.
func (s *Service) admitCreated(ctx context.Context, limit int) (int,error) {
 if limit<=0 {return 0,nil}
 const pageSize=64
 const maxInspectedPerTick=128
 s.createdAdmissionMu.Lock()
 defer s.createdAdmissionMu.Unlock()

 first:=!s.createdAdmissionPosition.valid
 lastPriority:=s.createdAdmissionPosition.priority
 lastCreated:=s.createdAdmissionPosition.createdAt
 lastID:=s.createdAdmissionPosition.taskID
 admitted:=0
 inspected:=0
 var problems []error
 overflow:=0
 actor:=AuthorityPrincipal
 for admitted<limit && inspected<maxInspectedPerTick {
  rows,err:=s.db.QueryContext(ctx,`SELECT t.id,t.revision,t.priority,t.created_at
   FROM tasks t WHERE t.state='created'
   AND NOT EXISTS(SELECT 1 FROM task_execution_profiles ep
    LEFT JOIN team_sessions ts ON ts.id=ep.team_session_id
    WHERE ep.task_id=t.id AND ep.execution_mode='team'
    AND (ts.accepted_plan_id IS NULL OR ts.status NOT IN ('plan_accepted','executing')))
   AND NOT EXISTS(SELECT 1 FROM routine_occurrences ro
    JOIN project_routine_bindings b ON b.routine_id=ro.routine_id
    AND b.status='active' AND b.action_kind='app_command'
    WHERE ro.task_id=t.id)
   AND NOT EXISTS(SELECT 1 FROM task_dependencies d
    JOIN tasks dep ON dep.id=d.depends_on_task_id
    WHERE d.task_id=t.id AND d.dependency_type='hard'
     AND (dep.state<>'complete' OR dep.archived_at IS NOT NULL))
   AND (?=1 OR t.priority<? OR
    (t.priority=? AND (t.created_at>? OR
     (t.created_at=? AND t.id>?))))
   ORDER BY t.priority DESC,t.created_at,t.id LIMIT ?`,
   first,lastPriority,lastPriority,lastCreated,lastCreated,lastID,pageSize)
  if err!=nil{return admitted,errors.Join(append(problems,err)...)}
  type item struct{id string;revision int64;priority int;created int64}
  page:=make([]item,0,pageSize)
  for rows.Next(){
   var candidate item
   if err=rows.Scan(&candidate.id,&candidate.revision,
    &candidate.priority,&candidate.created);err!=nil{break}
   page=append(page,candidate)
  }
  if err==nil{err=rows.Err()}
  closeErr:=rows.Close()
  if err==nil{err=closeErr}
  if err!=nil{return admitted,errors.Join(append(problems,err)...)}
  if len(page)==0 {
   // At the saved end from a previous tick, wrap once to inspect newer
   // high-priority Tasks rather than wasting a scheduler cycle.
   s.createdAdmissionPosition.valid=false
   if !first && inspected==0 {
    first=true
    lastPriority=0
    lastCreated=0
    lastID=""
    continue
   }
   break
  }
  first=false
  for _,candidate:=range page {
   inspected++
   lastPriority,lastCreated,lastID=candidate.priority,candidate.created,candidate.id
   s.createdAdmissionPosition=createdAdmissionPosition{
    priority:lastPriority,createdAt:lastCreated,taskID:lastID,valid:true}
   _,promoteErr:=s.tasks.MarkReady(ctx,task.TransitionCommand{
    TaskID:candidate.id,ExpectedRevision:candidate.revision,
    ActorPrincipalID:&actor,Reason:"autonomous worker admission"})
   if promoteErr==nil {
    admitted++
   } else if !task.IsRevisionConflict(promoteErr) {
    if len(problems)<32 {
     problems=append(problems,fmt.Errorf("admit created Task %s: %w",
      candidate.id,promoteErr))
    } else {overflow++}
   }
   if admitted>=limit || inspected>=maxInspectedPerTick {break}
  }
  if len(page)<pageSize && admitted<limit {
   // Retain the cursor if the successful-admission quota stopped us
   // midway through a short page; those candidates remain eligible.
   s.createdAdmissionPosition.valid=false
   break
  }
 }
 if overflow>0 {
  problems=append(problems,fmt.Errorf(
   "%d additional failed CREATED Task admissions require operator review",overflow))
 }
 return admitted,errors.Join(problems...)
}

func (s *Service) syncResumedRuns(ctx context.Context) error {
	// Parent Tasks waiting on delegated children resume only after all hard
	// dependencies complete. Human/capability waits resume when another trusted
	// control-plane action moves the Task back to RUNNING.
	// Keep the queue reconciler bounded on low-memory hosts. A keyset
	// cursor is stable while successful wakeups remove rows from the waiting
	// set; OFFSET would skip still-waiting runs after each mutation.
	const pageSize=64
 const maxInspectedPerTick=128
 s.waitingReconcileMu.Lock()
 defer s.waitingReconcileMu.Unlock()
	type x struct {
		run, task, state string
		rev              int64
	}
	problems := boundedWorkerRecoveryErrors{category:"waiting Worker reconciliation failures"}
 lastRunID:=s.waitingReconcileCursor
 inspected:=0
 for inspected<maxInspectedPerTick{
  // Fully close the SQLite cursor before any per-Run write transaction.
  fetchLimit:=pageSize
  if remaining:=maxInspectedPerTick-inspected;remaining<fetchLimit{fetchLimit=remaining}
  if err:=ctx.Err();err!=nil{return problems.result(err)}
		rows,err:=s.db.QueryContext(ctx,`SELECT r.id,t.id,t.revision,t.state
		 FROM agent_worker_runs r JOIN tasks t ON t.id=r.task_id
		 WHERE r.status='waiting' AND r.id>?
		 ORDER BY r.id LIMIT ?`,lastRunID,fetchLimit)
		if err!=nil{return problems.result(err)}
		xs:=make([]x,0,pageSize)
		var scanErr error
		for rows.Next(){
			var v x
			if scanErr=rows.Scan(&v.run,&v.task,&v.rev,&v.state);scanErr!=nil{break}
			xs=append(xs,v)
		}
		if scanErr==nil{scanErr=rows.Err()}
		closeErr:=rows.Close()
		if scanErr!=nil||closeErr!=nil{
			return problems.result(scanErr,closeErr)
		}
  if len(xs)==0{
   s.waitingReconcileCursor=""
   if lastRunID!="" && inspected==0{
    // The previous tick ended on a full page at the end; try the
    // queue head once without consuming another whole queue per Tick.
    lastRunID=""
    continue
   }
   break
  }
		// Advance after processing each record, never past uninspected
  // continuations during a failure or scheduler budget boundary.
  for _, v := range xs {
   if err:=ctx.Err();err!=nil{return problems.result(err)}
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
			fields,valid:=readWorkerContinuationFields(json.RawMessage(cont))
			if !valid||json.Unmarshal([]byte(cont), &c)!=nil{
				return fmt.Errorf("invalid durable Agent Worker continuation for run %s",v.run)
			}
			// Only one of model, toolchain or operation may gate this Attempt.
			_,hasModel:=fields["model_wait"]
			_,hasToolchain:=fields["toolchain_wait"]
			_,hasOperation:=fields["operation_id"]
			if (hasModel&&hasToolchain)||((hasModel||hasToolchain)&&hasOperation){
				return fmt.Errorf("conflicting durable Worker wait conditions for run %s",v.run)
			}
			if hasOperation&&strings.TrimSpace(c.OperationID)==""{
				return fmt.Errorf("invalid persisted Operation wait for Worker run %s",v.run)
			}
			// A model-resource wait is a real persisted suspension, not a
			// dependency-free Task that should resume on every worker tick.
			// Retain the same pinned model and local-only routing after wake.
			// Parse only the wait category actually present. Most active
			// attempts have no resource wait; avoid repeatedly rescanning the
			// same JSON document on every tick of a low-power host.
			if hasModel{
				modelWait:=decodeModelWait(json.RawMessage(cont))
				if modelWait==nil{
					return fmt.Errorf("corrupt persisted local model wait for Worker run %s",v.run)
				}
				if s.clock.UnixMilli()<modelWait.RetryAtMS{ready=false}
			}
			if hasToolchain{
				toolchainWait:=decodeToolchainWait(json.RawMessage(cont))
				if toolchainWait==nil{
					return fmt.Errorf("corrupt persisted Workspace toolchain wait for Worker run %s",v.run)
				}
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
			problems.add(fmt.Errorf("reconcile waiting Worker %s Task %s: %w",v.run,v.task,runErr))
  }
   inspected++
   lastRunID=v.run
   s.waitingReconcileCursor=lastRunID
  }
  if len(xs)<fetchLimit{
   // A fully inspected short page reaches the tail. Revisit remaining
   // waiting approval/resource gates from the head on the next Tick.
   s.waitingReconcileCursor=""
   break
  }
	}
	return problems.result()
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
  // Resume is a new admission decision. A suspended Task cannot regain
  // model/tool execution after Project or Workspace autonomy was revoked.
  allowed,policyErr:=(featurepolicy.StoredScopes{DB:s.db}).AuthorizeTaskTx(ctx,tx,workspaceID,taskID,"tasks.autonomous")
  if policyErr!=nil{return fmt.Errorf("Worker resume scope unavailable: %w",policyErr)}
  if !allowed{return fmt.Errorf("autonomous Worker permission revoked before resume")}
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

// activeRuns bounds both decoded Workers returned and database candidates
// inspected. A damaged earlier Worker cannot starve healthy later Workers,
// even with >128 damaged rows and one SQLite connection. The cursor makes
// progress between Ticks but never alters stored state or retries effects.
func (s *Service) activeRuns(ctx context.Context,limit int)([]Run,error){
 if limit<=0||limit>100{limit=10}
 const pageSize=64
 const maxInspectedPerTick=128
 s.activeQueueMu.Lock()
 defer s.activeQueueMu.Unlock()
 lastTime:=s.activeQueuePosition.updatedAt
 lastID:=s.activeQueuePosition.runID
 first:=!s.activeQueuePosition.valid
 inspected:=0
 out:=make([]Run,0,limit)
 var problems []error
 moreFailures:=0
 for len(out)<limit && inspected<maxInspectedPerTick{
  fetchLimit:=pageSize
  if remain:=maxInspectedPerTick-inspected;remain<fetchLimit{fetchLimit=remain}
  var rows *sql.Rows
  var err error
  if first{
   rows,err=s.db.QueryContext(ctx,`SELECT r.id,r.updated_at
    FROM agent_worker_runs r WHERE r.status='running'
    ORDER BY r.updated_at,r.id LIMIT ?`,fetchLimit)
  }else{
   rows,err=s.db.QueryContext(ctx,`SELECT r.id,r.updated_at
    FROM agent_worker_runs r WHERE r.status='running'
    AND (r.updated_at>? OR (r.updated_at=? AND r.id>?))
    ORDER BY r.updated_at,r.id LIMIT ?`,
    lastTime,lastTime,lastID,fetchLimit)
  }
  if err!=nil{return out,errors.Join(append(problems,err)...)}
  type candidate struct{id string;timestamp int64}
  page:=make([]candidate,0,fetchLimit)
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
  if len(page)==0{
   s.activeQueuePosition.valid=false
   if !first && inspected==0{
    // Reach a saved tail after state changes: restart once at the head.
    first=true
    lastTime=0
    lastID=""
    continue
   }
   break
  }
  first=false
  for _,item:=range page{
   inspected++
   lastTime,lastID=item.timestamp,item.id
   s.activeQueuePosition=activeQueuePosition{
    updatedAt:lastTime,runID:lastID,valid:true}
   run,getErr:=s.getRun(ctx,item.id)
   if getErr!=nil{
    if len(problems)<32{
     problems=append(problems,fmt.Errorf("decode active Worker %s: %w",item.id,getErr))
    }else{moreFailures++}
   }else if run.Status==RunRunning{
    out=append(out,run)
   }
   if len(out)>=limit||inspected>=maxInspectedPerTick{break}
  }
  if len(page)<fetchLimit && len(out)<limit{
   // Only wrap after exhausting every inspected candidate. If selection
   // filled LIMIT midway through a short page, later records must retain
   // their turn on the next Tick.
   s.activeQueuePosition.valid=false
   break
  }
 }
 if moreFailures>0{
  problems=append(problems,fmt.Errorf("%d additional corrupt active Workers require operator review",moreFailures))
 }
 return out,errors.Join(problems...)
}

// readyTasks returns up to limit valid Tasks while reading at most 128
// candidate rows. Later ticks resume after damaged higher-priority rows;
// only a Task state transition in startRun may create a real Attempt.
func (s *Service) readyTasks(ctx context.Context,limit int)([]task.Task,error){
 if limit<=0{return nil,nil}
 const pageSize=64
 const maxInspectedPerTick=128
 s.readyQueueMu.Lock()
 defer s.readyQueueMu.Unlock()

 first:=!s.readyQueuePosition.valid
 lastPriority:=s.readyQueuePosition.priority
 lastTime:=s.readyQueuePosition.at
 lastID:=s.readyQueuePosition.taskID
 inspected:=0
 out:=make([]task.Task,0,limit)
 var problems []error
 overflow:=0
 for len(out)<limit && inspected<maxInspectedPerTick{
  fetchLimit:=pageSize
  if remain:=maxInspectedPerTick-inspected;remain<fetchLimit{fetchLimit=remain}
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
   LIMIT ?`,first,lastPriority,lastPriority,lastTime,lastTime,lastID,fetchLimit)
  if err!=nil{return out,errors.Join(append(problems,err)...)}
  type row struct{id string;priority int;at int64}
  page:=make([]row,0,fetchLimit)
  for rows.Next(){
   var v row
   if err=rows.Scan(&v.id,&v.priority,&v.at);err!=nil{break}
   page=append(page,v)
  }
  if err==nil{err=rows.Err()}
  closeErr:=rows.Close()
  if err==nil{err=closeErr}
  if err!=nil{return out,errors.Join(append(problems,err)...)}
  if len(page)==0{
   s.readyQueuePosition.valid=false
   if !first && inspected==0{
    // A now-empty suffix is not a reason to waste this Tick. Retry the
    // current queue from the head, at most once.
    first=true
    lastPriority=0
    lastTime=0
    lastID=""
    continue
   }
   break
  }
  first=false
  for _,item:=range page{
   inspected++
   lastID,lastPriority,lastTime=item.id,item.priority,item.at
   s.readyQueuePosition=readyQueuePosition{
    priority:lastPriority,at:lastTime,taskID:lastID,valid:true}
   candidate,loadErr:=s.tasks.Get(ctx,item.id)
   if loadErr!=nil{
    if len(problems)<32{
     problems=append(problems,fmt.Errorf("read eligible Task %s: %w",item.id,loadErr))
    }else{overflow++}
   }else if candidate.State==task.StateReady{
    out=append(out,candidate)
   }
   if len(out)>=limit||inspected>=maxInspectedPerTick{break}
  }
  if len(page)<fetchLimit && len(out)<limit{
   // A short page is not exhausted when LIMIT stops iteration early.
   // Preserve the cursor for the uninspected Tasks on the next Tick.
   s.readyQueuePosition.valid=false
   break
  }
 }
 if overflow>0{
  problems=append(problems,fmt.Errorf(
   "%d additional corrupt READY Tasks require operator review",overflow))
 }
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
 // Agent Worker admission must not turn a disabled autonomy policy into
 // a new Task/Attempt or a model dispatch.
 scopes:=featurepolicy.StoredScopes{DB:s.db}
 allowed,scopeErr:=scopes.AuthorizeTask(ctx,t.WorkspaceID,t.ID,"tasks.autonomous")
 if scopeErr!=nil{
  res.Error="stored Project or Workspace feature policy scope unavailable: "+scopeErr.Error()
  return Run{},res
 }
 if !allowed{
  res.Error="stored Project or Workspace feature policy denied autonomous Worker admission"
  return Run{},res
 }
 fallbackAllowed,fbErr:=scopes.AuthorizeTask(ctx,t.WorkspaceID,t.ID,"inference.fallback")
 if fbErr!=nil{res.Error="stored feature policy fallback scope unavailable";return Run{},res}
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
 if route.RoutingEnabled && fallbackAllowed{
  for _,id:=range workspaceRouting.FallbackCandidateIDs{
   if id=strings.TrimSpace(id);id!=""&&!contains(ids,id){ids=append(ids,id)}
  }
 }
 route.IncludeCandidateIDs=ids
 maxEscalations:=s.cfg.MaxEscalations
 if !route.RoutingEnabled || !fallbackAllowed{maxEscalations=0}
 rp,err:=json.Marshal(route)
 if err!=nil{res.Error=err.Error();return Run{},res}
 var r Run
 // Previously Task.Start committed before Worker creation. Any DB failure
 // between those writes stranded a RUNNING Task and Attempt without a Worker
 // row; RecoverLostRuns could not discover the orphan. All admission writes
 // and both audit/outbox events must now commit together or not at all.
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  var workspaceStatus string
  if err:=tx.QueryRowContext(ctx,`SELECT status FROM workspaces WHERE id=?`,t.WorkspaceID).Scan(&workspaceStatus);err!=nil{return fmt.Errorf("verify Worker Workspace %s state: %w",t.WorkspaceID,err)}
  if workspaceStatus!="active"{return fmt.Errorf("Workspace %s is not active (state=%s)",t.WorkspaceID,workspaceStatus)}
  granted,grantErr:=scopes.AuthorizeTaskTx(ctx,tx,t.WorkspaceID,t.ID,"tasks.autonomous")
  if grantErr!=nil{return fmt.Errorf("transactional Worker policy unavailable: %w",grantErr)}
  if !granted{return fmt.Errorf("autonomous Worker execution revoked before admission")}
  currentFallback,fbCheckErr:=scopes.AuthorizeTaskTx(ctx,tx,t.WorkspaceID,t.ID,"inference.fallback")
  if fbCheckErr!=nil{return fmt.Errorf("transactional fallback policy unavailable: %w",fbCheckErr)}
  if currentFallback!=fallbackAllowed{return fmt.Errorf("Worker routing policy changed during admission")}
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
