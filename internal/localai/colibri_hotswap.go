package localai

import (
    "context"
    "errors"
    "fmt"
    "strings"
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
        default:
            return nil,fmt.Errorf("unexpected Colibri residency state %s",c.Status)
        }
    }
    return out,nil
}

func (s *RuntimeSupervisor) colibriResidents(ctx context.Context,nodeID,except string) ([]colibriSwapCandidate,error) {
    rows,err:=s.db.QueryContext(ctx, `SELECT i.deployment_id,i.status,COALESCE(i.last_seen_at,i.started_at,i.updated_at),CASE WHEN json_extract(d.runtime_config_json,'$.colibri_pinned')=1 THEN 1 ELSE 0 END
        FROM local_runtime_instances i
        JOIN model_deployments d ON d.id=i.deployment_id
        WHERE i.node_id=? AND i.deployment_id<>?
          AND LOWER(COALESCE(d.runtime_name,''))='colibri'
          AND i.status IN ('healthy','busy','starting','draining','orphaned')
        ORDER BY COALESCE(i.last_seen_at,i.started_at,i.updated_at) DESC`,nodeID,except)
    if err!=nil{return nil,err}
    defer rows.Close()
    result:=[]colibriSwapCandidate{}
    for rows.Next(){
        var v colibriSwapCandidate
        if err:=rows.Scan(&v.ID,&v.Status,&v.LastUsed,&v.Pinned);err!=nil{return nil,err}
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
        if err:=s.Stop(ctx,id);err!=nil{
            return stopped,fmt.Errorf("cannot safely evict Colibri model %s: %w",id,err)
        }
        stopped=append(stopped,id)
    }
    return stopped,nil
}

func (s *RuntimeSupervisor) ColibriHotSwap(ctx context.Context,deploymentID string) (ColibriSwapState,error) {
    state:=ColibriSwapState{DeploymentID:deploymentID,ActiveDeploymentIDs:[]string{}}
    if strings.TrimSpace(deploymentID)==""{return state,errors.New("Colibri deployment ID is required")}
    s.residencyMu.Lock()
    defer s.residencyMu.Unlock()

    cfg,_,nodeID,err:=s.resolve(ctx,deploymentID)
    if err!=nil{return state,err}
    if !strings.EqualFold(cfg.RuntimeBackend,"colibri"){
        return state,errors.New("hot swap requires a managed Colibri deployment")
    }
    previous,err:=s.colibriResidents(ctx,nodeID,deploymentID)
    if err!=nil{return state,err}
    // The actual evictions happen inside startLocked, under the same lock.
    inst,launchErr:=s.startLocked(ctx,deploymentID)
    after,queryErr:=s.colibriResidents(ctx,nodeID,deploymentID)
    if queryErr!=nil {
        if launchErr!=nil{return state,fmt.Errorf("Colibri launch failed: %w; residency inspection failed: %v",launchErr,queryErr)}
        return state,queryErr
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
        if failed,instanceErr:=s.Instance(ctx,deploymentID);instanceErr==nil && failed.PID>1 && s.processes.Alive(failed.PID) {
            if cleanupErr:=s.Stop(ctx,deploymentID);cleanupErr!=nil {
                return state,fmt.Errorf("Colibri swap failed: %w; replacement process could not be safely drained (%v); rollback withheld",launchErr,cleanupErr)
            }
        }
        // Only roll back an instance that was actually stopped. Never
        // attempt another model switch when an active/busy model blocked us.
        if len(state.EvictedDeploymentIDs)>0{
            prev:=state.EvictedDeploymentIDs[0]
            if _,restoreErr:=s.startLocked(ctx,prev);restoreErr!=nil{
                return state,fmt.Errorf("Colibri swap failed: %w; rollback to %s also failed: %v",launchErr,prev,restoreErr)
            }
            return state,fmt.Errorf("Colibri swap failed: %w; restored previous model %s",launchErr,prev)
        }
        return state,launchErr
    }
    state.Status=string(inst.Status)
    state.ActiveDeploymentIDs=[]string{deploymentID}
    state.Message="Colibri model is resident and ready. Chat routing remains independently configurable."
    return state,nil
}

func (s *Service) ColibriHotSwap(ctx context.Context,deploymentID string) (ColibriSwapState,error) {
    if s==nil||s.supervisor==nil{return ColibriSwapState{},errors.New("local runtime supervisor unavailable")}
    return s.supervisor.ColibriHotSwap(ctx,deploymentID)
}

func (s *Service) ColibriHotSwapStatus(ctx context.Context,deploymentID string) (ColibriSwapState,error) {
    if _,err:=s.colibriTierSnapshot(ctx,deploymentID);err!=nil{return ColibriSwapState{},err}
    state:=ColibriSwapState{DeploymentID:deploymentID,Status:"stopped",ActiveDeploymentIDs:[]string{}}
    var nodeID string
    if err:=s.db.QueryRowContext(ctx,"SELECT node_id FROM model_deployments WHERE id=?",deploymentID).Scan(&nodeID);err!=nil{return state,err}
    residents,err:=s.supervisor.colibriResidents(ctx,nodeID,"")
    if err!=nil{return state,err}
    for _,r:=range residents{
        if r.Status==RuntimeHealthy||r.Status==RuntimeBusy||r.Status==RuntimeStarting {
            state.ActiveDeploymentIDs=append(state.ActiveDeploymentIDs,r.ID)
        }
        if r.ID==deploymentID{state.Status=string(r.Status)}
    }
    return state,nil
}
