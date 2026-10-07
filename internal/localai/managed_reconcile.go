package localai

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/inference"
)

type ManagedDeploymentReconcileReport struct {
	Scanned int `json:"scanned"`
	Kept int `json:"kept"`
	RemovedStale int `json:"removed_stale"`
	RemovedDuplicates int `json:"removed_duplicates"`
}

type managedReconcileRow struct {
	ManagedID string
	DeploymentID string
	NodeID string
	ModelRef string
	Quantization string
	LocalPath string
	ObservedSHA string
	Status string
	UpdatedAt int64
}

func deploymentHealthRank(status string) int {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "ready": return 6
	case "qualifying": return 5
	case "degraded": return 4
	case "discovered": return 3
	case "draining": return 2
	case "unavailable": return 1
	default: return 0
	}
}

func (s *Service) disableManagedReconcileRow(ctx context.Context, row managedReconcileRow, actor, reason string) error {
	_ = s.supervisor.Stop(ctx, row.DeploymentID)
	dep, err := s.inference.Deployment(ctx, row.DeploymentID)
	if err == nil && dep.Status != inference.DeploymentDisabled {
		a := strings.TrimSpace(actor)
		var actorPtr *string
		if a != "" { actorPtr = &a }
		if _, e := s.inference.SetDeploymentStatus(ctx, inference.SetDeploymentStatusCommand{DeploymentID:dep.ID,ExpectedRevision:dep.Revision,Status:inference.DeploymentDisabled,ActorPrincipalID:actorPtr,Reason:reason}); e != nil { return e }
	}
	now := s.clock.UnixMilli()
	_, err = s.db.ExecContext(ctx, "UPDATE managed_local_models SET status='removed',revision=revision+1,updated_at=? WHERE id=?", now, row.ManagedID)
	return err
}

func (s *Service) ReconcileManagedDeployments(ctx context.Context, workspaceID, actor string) (ManagedDeploymentReconcileReport, error) {
	var report ManagedDeploymentReconcileReport
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" { return report, fmt.Errorf("workspace_id is required") }
	rows, err := s.db.QueryContext(ctx, "SELECT mm.id,mm.deployment_id,mm.node_id,mm.model_ref,COALESCE(m.quantization,''),mm.local_path,COALESCE(mm.observed_sha256,''),d.status,d.updated_at FROM managed_local_models mm JOIN model_deployments d ON d.id=mm.deployment_id JOIN models m ON m.id=mm.model_id JOIN local_model_install_plans p ON p.id=mm.plan_id WHERE p.workspace_id=? AND mm.status<>'removed' ORDER BY d.updated_at DESC", workspaceID)
	if err != nil { return report, err }
	defer rows.Close()
	var all []managedReconcileRow
	for rows.Next() {
		var x managedReconcileRow
		if err := rows.Scan(&x.ManagedID,&x.DeploymentID,&x.NodeID,&x.ModelRef,&x.Quantization,&x.LocalPath,&x.ObservedSHA,&x.Status,&x.UpdatedAt); err != nil { return report, err }
		all = append(all,x)
	}
	if err := rows.Err(); err != nil { return report, err }
	report.Scanned = len(all)

	live := make([]managedReconcileRow,0,len(all))
	for _, row := range all {
		p := strings.TrimSpace(row.LocalPath)
		if p == "" {
			if err := s.disableManagedReconcileRow(ctx,row,actor,"managed artifact path is empty during rescan"); err != nil { return report,err }
			report.RemovedStale++
			continue
		}
		if _, err := os.Stat(filepath.Clean(p)); err != nil {
			if os.IsNotExist(err) {
				if err := s.disableManagedReconcileRow(ctx,row,actor,"managed artifact is missing from disk"); err != nil { return report,err }
				report.RemovedStale++
				continue
			}
			return report,err
		}
		live=append(live,row)
	}

	groups:=map[string][]managedReconcileRow{}
	for _,row:=range live{
		identity:=strings.ToLower(strings.TrimSpace(row.ObservedSHA))
		if identity==""{identity=strings.ToLower(filepath.Clean(row.LocalPath))}
		key:=strings.ToLower(row.NodeID)+"|"+strings.ToLower(strings.TrimSpace(row.ModelRef))+"|"+strings.ToLower(strings.TrimSpace(row.Quantization))+"|"+identity
		groups[key]=append(groups[key],row)
	}
	for _,group:=range groups{
		if len(group)==1{report.Kept++;continue}
		best:=0
		for i:=1;i<len(group);i++{
			ri,rbest:=deploymentHealthRank(group[i].Status),deploymentHealthRank(group[best].Status)
			if ri>rbest||(ri==rbest&&group[i].UpdatedAt>group[best].UpdatedAt){best=i}
		}
		report.Kept++
		for i,row:=range group{
			if i==best{continue}
			if err:=s.disableManagedReconcileRow(ctx,row,actor,"duplicate managed deployment reconciled; healthiest deployment retained");err!=nil{return report,err}
			report.RemovedDuplicates++
		}
	}
	return report,nil
}
