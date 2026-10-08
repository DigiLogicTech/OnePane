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
}

// The caller must enforce the supervisor residency lock. Refuse a competing
// busy, starting, draining or orphaned runtime BEFORE evicting anyone.
func selectColibriEvictions(candidates []colibriSwapCandidate, active map[string]int) ([]string,error) {
    out:=[]string{}
    for _,c:=range candidates {
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
    rows,err:=s.db.QueryContext(ctx, `SELECT i.deployment_id,i.status,COALESCE(i.last_seen_at,i.started_at,i.updated_at)
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
        if err:=rows.Scan(&v.ID,&v.Status,&v.LastUsed);err!=nil{return nil,err}
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
    inst,err:=s.startLocked(ctx,deploymentID)
    if err!=nil {
        // Best-effort rollback of the most recently used healthy predecessor.
        // Never replace another model if it has become busy.
        for _,p:=range previous {
            if p.Status!=RuntimeHealthy {continue}
            if _,restoreErr:=s.startLocked(ctx,p.ID);restoreErr!=nil {
                return state,fmt.Errorf("Colibri swap failed: %w; rollback to %s also failed: %v",err,p.ID,restoreErr)
            }
            return state,fmt.Errorf("Colibri swap failed: %w; restored previous model %s",err,p.ID)
        }
        return state,err
    }
    for _,p:=range previous {
        if p.Status==RuntimeHealthy && p.ID!=deploymentID {state.EvictedDeploymentIDs=append(state.EvictedDeploymentIDs,p.ID)}
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
    cfg,err:=s.colibriTierSnapshot(ctx,deploymentID)
    if err!=nil{return ColibriSwapState{},err}
    _=cfg
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
