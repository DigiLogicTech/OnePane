package localai

import (
 "context"
 "errors"
 "fmt"
 "os"
 "path/filepath"
 "strings"
)

// CancelQueuedInstallJob cancels a durable job before its worker starts.
// Running downloads are intentionally not treated as cancelled: they require
// cooperative transport interruption and remain visible to the operator.
func (s *Service) CancelQueuedInstallJob(ctx context.Context, jobID, workspaceID string) (InstallJob,error) {
 j,err:=s.InstallJob(ctx,jobID)
 if err!=nil{return j,err}
 if j.WorkspaceID==nil || *j.WorkspaceID!=workspaceID {return j,errors.New("install job is not in the selected workspace")}
 if j.Status!=InstallJobQueued && j.Status!=InstallJobInterrupted {
  return j,fmt.Errorf("job is %s; only queued or interrupted jobs can be cancelled safely",j.Status)
 }
 now:=s.clock.UnixMilli()
 result,err:=s.db.ExecContext(ctx,`UPDATE local_ai_install_jobs SET status='cancelled',failure_reason='cancelled by operator',completed_at=?,revision=revision+1,updated_at=? WHERE id=? AND status IN ('queued','interrupted')`,now,now,jobID)
 if err!=nil{return j,err}
 n,_:=result.RowsAffected()
 if n!=1{return j,errors.New("install job started while cancelling; refresh its status")}
 _,_=s.db.ExecContext(ctx,`UPDATE local_model_install_plans SET status='cancelled',revision=revision+1,updated_at=? WHERE id=? AND status='approved'`,now,j.PlanID)
 return s.InstallJob(ctx,jobID)
}

// DeleteManagedDeployment removes only OnePane-owned weights under the managed
// model root. It preserves other deployments, external paths and project data.
func (s *Service) DeleteManagedDeployment(ctx context.Context,workspaceID,deploymentID,actor string) error {
 var row managedReconcileRow
 err:=s.db.QueryRowContext(ctx,`SELECT mm.id,mm.deployment_id,mm.node_id,mm.model_ref,COALESCE(m.quantization,''),mm.local_path,COALESCE(mm.observed_sha256,''),d.status,d.updated_at FROM managed_local_models mm JOIN model_deployments d ON d.id=mm.deployment_id JOIN models m ON m.id=mm.model_id JOIN local_model_install_plans p ON p.id=mm.plan_id WHERE mm.deployment_id=? AND p.workspace_id=? AND mm.status<>'removed'`,deploymentID,workspaceID).Scan(&row.ManagedID,&row.DeploymentID,&row.NodeID,&row.ModelRef,&row.Quantization,&row.LocalPath,&row.ObservedSHA,&row.Status,&row.UpdatedAt)
 if err!=nil{return err}
 var active string
 err=s.db.QueryRowContext(ctx,`SELECT status FROM local_runtime_instances WHERE deployment_id=?`,deploymentID).Scan(&active)
 if err==nil && (active=="starting"||active=="healthy"||active=="busy"||active=="draining"){return errors.New("model is resident or active; stop its runtime before deleting")}
 if err!=nil && !errors.Is(err,os.ErrNotExist) {
  // A missing SQLite row is expected for unloaded models.
  if !strings.Contains(err.Error(),"no rows in result set"){return err}
 }
 local:=filepath.Clean(row.LocalPath)
 root,err:=filepath.EvalSymlinks(s.modelRoot)
 if err!=nil{return fmt.Errorf("verify managed model root: %w",err)}
 checked:=local
 if _,err:=os.Lstat(local);err==nil {
  checked,err=filepath.EvalSymlinks(local)
  if err!=nil{return fmt.Errorf("verify model artifact: %w",err)}
 }else if !os.IsNotExist(err){return err}
 rel,err:=filepath.Rel(root,checked)
 if err!=nil || rel=="." || rel==".." || strings.HasPrefix(rel,".."+string(os.PathSeparator)) {
  return errors.New("refusing to delete a model outside the owned managed model pool")
 }
 var shared int
 if err:=s.db.QueryRowContext(ctx,`SELECT COUNT(1) FROM managed_local_models WHERE local_path=? AND deployment_id<>? AND status<>'removed'`,row.LocalPath,deploymentID).Scan(&shared);err!=nil{return err}
 if err:=s.disableManagedReconcileRow(ctx,row,actor,"operator deleted managed model");err!=nil{return err}
 if shared==0 {
  if err:=os.RemoveAll(local);err!=nil{return fmt.Errorf("model metadata disabled, but managed artifact removal failed: %w",err)}
 }
 return nil
}
