package localai

import (
    "context"
 "database/sql"
    "errors"
    "fmt"
    "strings"
    "time"
)

// ColibriSwapState reports whole-model residency, distinct from Colibri's
// internal SSD/RAM/VRAM expert cache and OnePane's inference routing selection.
type ColibriSwapState struct {
    DeploymentID string `json:"deployment_id"`
    Status string `json:"status"`
    ActiveDeploymentIDs []string `json:"active_deployment_ids"`
    EvictedDeploymentIDs []string `json:"evicted_deployment_ids,omitempty"`
    Message string `json:"message,omitempty"`
}
type colibriSwapCandidate struct {
    ID string
    Status RuntimeInstanceStatus
    LastUsed int64
    Pinned bool
}

// The caller must enforce the supervisor residency lock. Refuse a competing
// busy, starting, draining or orphaned runtime BEFORE evicting anyone.
func selectColibriEvictions(candidates []colibriSwapCandidate, active map[string]int) ([]string,error) {
    out:=[]string{}
    for _,c:=range candidates {
        if c.Pinned {return nil,fmt.Errorf("Colibri model %s is pinned; unpin it before selecting another Colibri model",c.ID)}
        switch c.Status {
        case RuntimeHealthy:
            if active[c.ID]>0 {
                return nil,fmt.Errorf("Colibri model %s is serving an active request",c.ID)
            }
            out=append(out,c.ID)
        case RuntimeBusy,RuntimeStarting,RuntimeDraining,RuntimeOrphaned:
            return nil,fmt.Errorf("Colibri model %s is %s; wait for an idle model or resolve the orphaned process",c.ID,c.Status)
        case RuntimeFailed,RuntimeStopped:
            // Only reported here when a PID is independently observed
            // alive despite this stale persisted state.
            return nil,fmt.Errorf("Colibri model %s has a live process despite status %s; reconcile the process before swapping",c.ID,c.Status)
        default:
            return nil,fmt.Errorf("unexpected Colibri residency state %s",c.Status)
        }
    }
    // Only one resident Colibri is allowed per Node. Earlier installs or
    // recovery races can leave multiple healthy processes. Stopping more
    // than one sequentially risks evicting an innocent working model when
    // the later stop fails. Require human reconciliation instead.
    if len(out)>1{
        return nil,fmt.Errorf("multiple Colibri processes remain resident on this Node; stop or reconcile them individually before swapping")
    }
    return out,nil
}

func (s *RuntimeSupervisor) colibriResidents(ctx context.Context,nodeID,except string) ([]colibriSwapCandidate,error) {
    rows,err:=s.db.QueryContext(ctx, `SELECT i.deployment_id,i.status,COALESCE(i.last_seen_at,i.started_at,i.updated_at),
        CASE WHEN json_extract(d.runtime_config_json,'$.colibri_pinned')=1 THEN 1 ELSE 0 END,i.pid
        FROM local_runtime_instances i
        JOIN model_deployments d ON d.id=i.deployment_id
        WHERE i.node_id=? AND i.deployment_id<>?
          AND LOWER(COALESCE(d.runtime_name,''))='colibri'
          AND (i.status IN ('healthy','busy','starting','draining','orphaned')
            OR (i.status IN ('failed','stopped') AND COALESCE(i.pid,0)>1))
        ORDER BY COALESCE(i.last_seen_at,i.started_at,i.updated_at) DESC`,nodeID,except)
    if err!=nil{return nil,err}
    defer rows.Close()
    result:=[]colibriSwapCandidate{}
    for rows.Next(){
        var v colibriSwapCandidate
        var pid sql.NullInt64
        if err:=rows.Scan(&v.ID,&v.Status,&v.LastUsed,&v.Pinned,&pid);err!=nil{return nil,err}
        if v.Status==RuntimeFailed||v.Status==RuntimeStopped {
            // A recorded "failed" or "stopped" status does not free VRAM
            // when its old managed engine is still alive. Reject a new
            // Colibri residency rather than overcommitting constrained RAM.
            if !pid.Valid||pid.Int64<=1{continue}
            if s.processes==nil{
                return nil,fmt.Errorf("cannot verify process for stale Colibri model %s",v.ID)
            }
            if !s.processes.Alive(int(pid.Int64)){continue}
        }
        result=append(result,v)
    }
    return result,rows.Err()
}

// Called by startLocked, including inference Acquire. The outer residency
// mutex blocks new acquisitions while another model is being evicted.
// activityMu excludes direct SetBusy transitions until Stop completes.
func (s *RuntimeSupervisor) prepareColibriSwapLocked(ctx context.Context,nodeID,target string) ([]string,error) {
    candidates,err:=s.colibriResidents(ctx,nodeID,target)
    if err!=nil{return nil,err}
    s.activityMu.Lock()
    defer s.activityMu.Unlock()
    evict,err:=selectColibriEvictions(candidates,s.activeRequests)
    if err!=nil{return nil,err}
    stopped:=[]string{}
    for _,id:=range evict {
        if err:=s.stopWithActivityLock(ctx,id);err!=nil{
            return stopped,fmt.Errorf("cannot safely evict Colibri model %s: %w",id,err)
        }
        stopped=append(stopped,id)
    }
    return stopped,nil
}

// A failed Colibri launch may be reported after the caller cancels while
// the managed Python/engine process continues to hold RAM/VRAM. Recovery must
// use a bounded context independent of that cancelled request, just like
// inference lease release, or it cannot inspect and drain the replacement.
// The budget allows the existing Colibri graceful drain (35s on Unix) and
// a slow-model restore (up to 120s); it never grants a background job.
const colibriFailureRecoveryLimit = 165*time.Second

func colibriFailureRecoveryContext(parent context.Context)(context.Context,context.CancelFunc){
 return context.WithTimeout(context.WithoutCancel(parent),colibriFailureRecoveryLimit)
}

// Called with residencyMu already held. Uncertain database inspection or an
// unrecorded PID in "starting" means the new model may still be running.
// Never restart the predecessor in that state and overcommit a small host.
func (s *RuntimeSupervisor) reconcileFailedColibriReplacement(ctx context.Context,target string)error{
 failed,err:=s.Instance(ctx,target)
 if errors.Is(err,sql.ErrNoRows){return nil}
 if err!=nil{return fmt.Errorf("replacement residency inspection unavailable; rollback withheld: %w",err)}
 // A failed replacement can retain engine descendants after its
 // Colibri parent disappears. Neither a missing PID nor an intermediate
 // startup/drain state proves memory was released. Only an exact durable
 // pre-spawn failure or independently completed Stop permits restoration.
 switch failed.Status {
 case RuntimeStarting,RuntimeDraining,RuntimeOrphaned,RuntimeBusy,RuntimeHealthy:
  return fmt.Errorf("replacement remains %s with uncertain process ownership; rollback withheld pending operator reconciliation",failed.Status)
 case RuntimeFailed:
  if failed.PID<=1&&!confirmedUnlaunchedModelFailure(failed){
   return errors.New("replacement failed with unverified process ownership; rollback withheld pending operator reconciliation")
  }
 case RuntimeStopped:
  if failed.PID<=1&&failed.pidRecorded{
   return errors.New("replacement stopped with invalid recorded PID; rollback withheld pending operator reconciliation")
  }
 default:
  return errors.New("replacement has unrecognized process state; rollback withheld pending operator reconciliation")
 }
 if failed.PID>1{
  if s.processes==nil{
   return errors.New("replacement process liveness cannot be inspected; rollback withheld")
  }
  if s.processes.Alive(failed.PID){
   if err:=s.stopWhileResidencyLocked(ctx,target);err!=nil{
    return fmt.Errorf("replacement process could not be safely drained; rollback withheld: %w",err)
   }
   // A successful Stop result is not sufficient evidence that the original
   // process exited; never restart the old model while its PID is still live.
   if s.processes.Alive(failed.PID){
    return errors.New("replacement process remains alive after stop; rollback withheld")
   }
  }
 }
 return nil
}

// Explicit hot swaps and ordinary chat acquisition share one bounded,
// cancellation-safe Colibri swap/rollback path. A caller must never start a
// replacement after losing its previously resident model without applying
// the same verified compensation rules.
func (s *RuntimeSupervisor) ColibriHotSwap(ctx context.Context,deploymentID string) (ColibriSwapState,error) {
 s.residencyMu.Lock()
 defer s.residencyMu.Unlock()
 state,_,err:=s.colibriHotSwapLocked(ctx,deploymentID)
 return state,err
}

// Must be called with residencyMu held. On failure it never reports success
// merely because a compensating predecessor was restored. The caller can
// surface the specific failure without sending prompts to an uncertain PID.
func (s *RuntimeSupervisor) colibriHotSwapLocked(ctx context.Context,deploymentID string) (ColibriSwapState,RuntimeInstance,error) {
    state:=ColibriSwapState{DeploymentID:deploymentID,ActiveDeploymentIDs:[]string{}}
    if strings.TrimSpace(deploymentID)==""{return state,RuntimeInstance{},errors.New("Colibri deployment ID is required")}
    cfg,_,nodeID,err:=s.resolve(ctx,deploymentID)
    if err!=nil{return state,RuntimeInstance{},err}
    if !strings.EqualFold(cfg.RuntimeBackend,"colibri"){
        return state,RuntimeInstance{},errors.New("hot swap requires a managed Colibri deployment")
    }
    return s.colibriSwapOnNodeLocked(ctx,deploymentID,nodeID,s.startLocked)
}

// With residencyMu held, run the same stateful rollback for explicit swaps
// and inference acquisition. A starter is injectable for deterministic
// cancellation and restoration tests without launching a real GPU engine.
func (s *RuntimeSupervisor) colibriSwapOnNodeLocked(ctx context.Context,
 deploymentID,nodeID string,start func(context.Context,string)(RuntimeInstance,error)) (ColibriSwapState,RuntimeInstance,error){
 state:=ColibriSwapState{DeploymentID:deploymentID,ActiveDeploymentIDs:[]string{}}
 if start==nil{return state,RuntimeInstance{},errors.New("managed Colibri starter unavailable")}
 previous,err:=s.colibriResidents(ctx,nodeID,deploymentID)
 if err!=nil{return state,RuntimeInstance{},err}
 // The actual evictions happen inside the passed startLocked operation.
 inst,launchErr:=start(ctx,deploymentID)
    // Success can use the caller's context; failure recovery must not.
    // A cancelled request must never leave the replacement uninspected.
    recoveryCtx:=ctx
    if launchErr!=nil{
        var cancel context.CancelFunc
        recoveryCtx,cancel=colibriFailureRecoveryContext(ctx)
        defer cancel()
    }
    after,queryErr:=s.colibriResidents(recoveryCtx,nodeID,deploymentID)
    if queryErr!=nil {
        if launchErr!=nil{return state,RuntimeInstance{},fmt.Errorf("Colibri launch failed: %w; residency inspection failed: %v",launchErr,queryErr)}
        return state,RuntimeInstance{},queryErr
    }
    still:=map[string]bool{}
    for _,c:=range after {still[c.ID]=true}
    for _,p:=range previous {
        if p.Status==RuntimeHealthy && !still[p.ID]{
            state.EvictedDeploymentIDs=append(state.EvictedDeploymentIDs,p.ID)
        }
    }
    if launchErr!=nil {
        // A startup timeout may mark the replacement failed while its engine
        // is still alive. Never load the predecessor until verified teardown
        // has completed: doing so risks concurrent VRAM/RAM overcommit.
        if cleanupErr:=s.reconcileFailedColibriReplacement(recoveryCtx,deploymentID);cleanupErr!=nil{
            return state,RuntimeInstance{},fmt.Errorf("Colibri swap failed: %w; %v",launchErr,cleanupErr)
        }
        // Only roll back an instance that was actually stopped. Never
        // attempt another model switch when an active/busy model blocked us.
        if len(state.EvictedDeploymentIDs)>0{
            prev:=state.EvictedDeploymentIDs[0]
            if _,restoreErr:=start(recoveryCtx,prev);restoreErr!=nil{
                return state,RuntimeInstance{},fmt.Errorf("Colibri swap failed: %w; rollback to %s also failed: %v",launchErr,prev,restoreErr)
            }
            // Rollback is real residency, not a successful target launch.
            // Report the restored engine as active instead of still evicted,
            // while keeping the original replacement failure as the error.
            state.Status="failed"
            state.ActiveDeploymentIDs=[]string{prev}
            state.EvictedDeploymentIDs=nil
            state.Message="Replacement failed; the previous Colibri model was restored. The requested model did not become resident."
            return state,RuntimeInstance{},fmt.Errorf("Colibri swap failed: %w; restored previous model %s",launchErr,prev)
        }
        return state,RuntimeInstance{},launchErr
    }
    state.Status=string(inst.Status)
    state.ActiveDeploymentIDs=[]string{deploymentID}
    state.Message="Colibri model is resident and ready. Chat routing remains independently configurable."
    return state,inst,nil
}

func (s *Service) ColibriHotSwap(ctx context.Context,deploymentID string) (ColibriSwapState,error) {
    if s==nil||s.supervisor==nil{return ColibriSwapState{},errors.New("local runtime supervisor unavailable")}
    return s.supervisor.ColibriHotSwap(ctx,deploymentID)
}

func (s *Service) ColibriHotSwapStatus(ctx context.Context,deploymentID string) (ColibriSwapState,error) {
    if s==nil||s.db==nil||s.supervisor==nil{return ColibriSwapState{},errors.New("local runtime supervisor unavailable")}
    if _,err:=s.colibriTierSnapshot(ctx,deploymentID);err!=nil{return ColibriSwapState{},err}
    state:=ColibriSwapState{DeploymentID:deploymentID,Status:"stopped",ActiveDeploymentIDs:[]string{}}
    var nodeID string
    if err:=s.db.QueryRowContext(ctx,"SELECT node_id FROM model_deployments WHERE id=?",deploymentID).Scan(&nodeID);err!=nil{return state,err}
    // A failed local engine is not resident, but must be visible to the
    // operator instead of being incorrectly displayed as "stopped".
    var persistedStatus string
    err:=s.db.QueryRowContext(ctx,`SELECT status FROM local_runtime_instances
      WHERE deployment_id=? AND node_id=?`,deploymentID,nodeID).Scan(&persistedStatus)
    if err!=nil && err!=sql.ErrNoRows{return state,err}
    if err==nil{
        state.Status=persistedStatus
        if persistedStatus=="failed"{
            state.Message="The managed Colibri process failed; inspect local runtime diagnostics before retrying."
        }
    }
    residents,err:=s.supervisor.colibriResidents(ctx,nodeID,"")
    if err!=nil{return state,err}
    for _,r:=range residents{
        if r.Status==RuntimeHealthy||r.Status==RuntimeBusy||r.Status==RuntimeStarting||
            r.Status==RuntimeFailed||r.Status==RuntimeStopped {
            state.ActiveDeploymentIDs=append(state.ActiveDeploymentIDs,r.ID)
        }
        if r.ID==deploymentID{
            state.Status=string(r.Status)
            if r.Status==RuntimeFailed||r.Status==RuntimeStopped{
                state.Message="Colibri process is still running despite its saved state; stop/reconcile the engine before another swap."
            }
        }
    }
    return state,nil
}
