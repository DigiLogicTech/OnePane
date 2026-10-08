package localai

import (
	"context"
	"database/sql"
	"errors"
    "fmt"
    "os"
	"strings"
)

func (s *Service) enrichInstallJobIdentity(ctx context.Context, j *InstallJob) {
	if j == nil || strings.TrimSpace(j.PlanID) == "" {
		return
	}
	if p, err := s.Plan(ctx, j.PlanID); err == nil {
		j.ModelRef = p.ModelRef
		j.Quantization = p.Quantization
		j.NodeID = p.NodeID
	}
}

func (s *Service) ActiveInstallJobs(ctx context.Context, workspaceID string) ([]InstallJob, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, errors.New("workspace_id is required")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM local_ai_install_jobs WHERE workspace_id=? AND status IN ('queued','resolving','provisioning','starting','qualifying','interrupted') ORDER BY created_at`, workspaceID)
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
	out := make([]InstallJob, 0, len(ids))
	for _, idv := range ids {
		j, err := s.InstallJob(ctx, idv)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, nil
}

func (s *Service) findExistingInstall(ctx context.Context, workspaceID, nodeID, modelRef, quantization string) (*InstallJob, error) {
	var jobID string
	err := s.db.QueryRowContext(ctx, `SELECT j.id
		FROM local_ai_install_jobs j
		JOIN local_model_install_plans p ON p.id=j.plan_id
		WHERE j.workspace_id=? AND p.node_id=? AND lower(p.model_ref)=lower(?) AND lower(p.quantization)=lower(?)
		  AND j.status IN ('queued','resolving','provisioning','starting','qualifying','interrupted')
		ORDER BY j.created_at LIMIT 1`,
		workspaceID, nodeID, modelRef, quantization).Scan(&jobID)
	if err == nil {
		j, err := s.InstallJob(ctx, jobID)
		return &j, err
	}
	if err != sql.ErrNoRows {
		return nil, err
	}

	// A completed deployment for the same model/quantization is also a duplicate
	// install request. Return its most recent install job when possible so the
	// UI can reattach to the durable state rather than creating a new plan.
	err = s.db.QueryRowContext(ctx, `SELECT j.id
		FROM managed_local_models mm
		JOIN local_model_install_plans p ON p.id=mm.plan_id
		JOIN models m ON m.id=mm.model_id
		LEFT JOIN local_ai_install_jobs j ON j.plan_id=mm.plan_id
		WHERE p.workspace_id=? AND mm.node_id=? AND lower(mm.model_ref)=lower(?) AND lower(COALESCE(m.quantization,''))=lower(?)
		  AND mm.status<>'removed'
		ORDER BY CASE WHEN j.status='ready' THEN 0 ELSE 1 END, COALESCE(j.updated_at,mm.updated_at) DESC LIMIT 1`,
		workspaceID, nodeID, modelRef, quantization).Scan(&jobID)
	if err == nil && strings.TrimSpace(jobID) != "" {
        // Reattachment is only correct when the previously installed artifact
        // still exists. A missing file needs inventory reconciliation first.
        var path string
        pathErr:=s.db.QueryRowContext(ctx,`SELECT mm.local_path FROM managed_local_models mm
          JOIN local_model_install_plans p ON p.id=mm.plan_id
          JOIN models m ON m.id=mm.model_id
          WHERE p.workspace_id=? AND mm.node_id=? AND lower(mm.model_ref)=lower(?)
            AND lower(COALESCE(m.quantization,''))=lower(?) AND mm.status<>'removed'
          ORDER BY mm.updated_at DESC LIMIT 1`, workspaceID,nodeID,modelRef,quantization).Scan(&path)
        if pathErr!=nil{return nil,pathErr}
        if st,e:=os.Stat(path);e!=nil||st.IsDir(){
           return nil,fmt.Errorf("existing model artifact is missing or invalid; use Rescan Installed Models, then reinstall %s (%s)",modelRef,quantization)
        }
		j, err := s.InstallJob(ctx, jobID)
		return &j, err
	}
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	return nil, nil
}
