package localai

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/inference"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type InstallJobStatus string

const (
	InstallJobQueued       InstallJobStatus = "queued"
	InstallJobResolving    InstallJobStatus = "resolving"
	InstallJobProvisioning InstallJobStatus = "provisioning"
	InstallJobStarting     InstallJobStatus = "starting"
	InstallJobQualifying   InstallJobStatus = "qualifying"
	InstallJobReady        InstallJobStatus = "ready"
	InstallJobFailed       InstallJobStatus = "failed"
	InstallJobCancelled    InstallJobStatus = "cancelled"
	InstallJobInterrupted  InstallJobStatus = "interrupted"
)

type InstallJob struct {
	ID                 string           `json:"id"`
	WorkspaceID        *string          `json:"workspace_id,omitempty"`
	PlanID             string           `json:"plan_id"`
	CatalogID          *string          `json:"catalog_id,omitempty"`
	DeploymentID       *string          `json:"deployment_id,omitempty"`
	QualificationRunID *string          `json:"qualification_run_id,omitempty"`
	Status             InstallJobStatus `json:"status"`
	AttemptCount       int64            `json:"attempt_count"`
	FailureReason      *string          `json:"failure_reason,omitempty"`
	RequestedBy        *string          `json:"requested_by,omitempty"`
	StartedAt          *int64           `json:"started_at,omitempty"`
	CompletedAt        *int64           `json:"completed_at,omitempty"`
	Revision           int64            `json:"revision"`
	CreatedAt          int64            `json:"created_at"`
	UpdatedAt          int64            `json:"updated_at"`
	ProgressPct        float64          `json:"progress_pct,omitempty"`
	BytesDownloaded    int64            `json:"bytes_downloaded,omitempty"`
	BytesTotal         int64            `json:"bytes_total,omitempty"`
	CurrentArtifact    string           `json:"current_artifact,omitempty"`
	Resumable          bool             `json:"resumable,omitempty"`
}

type OneClickInstallRequest struct {
	WorkspaceID         string
	HardwareProfileID   string
	RoleName            string
	UseCase             UseCase
	ContextTokens       int64
	ModelRef            string
	Quantization        string
	PreferGPU           bool
	PlacementPreference PlacementMode
	ComputePreference   string
	RequestedBy         string
}

const FederatedModelManagerPrincipal = "system:node-model-manager"

type FederatedInstallRequest struct {
	ModelRef            string        `json:"model_ref"`
	Quantization        string        `json:"quantization"`
	UseCase             UseCase       `json:"use_case"`
	ContextTokens       int64         `json:"context_tokens"`
	RoleName            string        `json:"role_name"`
	PreferGPU           bool          `json:"prefer_gpu"`
	PlacementPreference PlacementMode `json:"placement_preference,omitempty"`
}

func (s *Service) ensureFederatedModelManager(ctx context.Context) error {
	now := s.clock.UnixMilli()
	_, err := s.db.ExecContext(ctx, `INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES(?, 'system','Node Model Manager','active',1,?,?) ON CONFLICT(id) DO UPDATE SET display_name=excluded.display_name,status='active',updated_at=excluded.updated_at`, FederatedModelManagerPrincipal, now, now)
	return err
}

// FederatedRecommendations always detects the target node locally and scores
// against that node's active trusted catalogue. The caller cannot supply a fake
// hardware profile.
func (s *Service) FederatedRecommendations(ctx context.Context, nodeID string, req RecommendRequest) (HardwareProfile, []Recommendation, error) {
	p, err := s.DetectAndPersist(ctx, nodeID)
	if err != nil {
		return p, nil, err
	}
	recs, err := s.Recommendations(ctx, p.ID, req)
	return p, recs, err
}

// QueueFederatedInstall is intentionally node-scoped (no remote workspace or
// human principal is imported). The target recomputes the fit against its own
// hardware and creates a trusted local plan; a dedicated local system principal
// records the approval provenance.
func (s *Service) QueueFederatedInstall(ctx context.Context, nodeID string, req FederatedInstallRequest) (InstallJob, error) {
	if strings.TrimSpace(nodeID) == "" || strings.TrimSpace(req.ModelRef) == "" || strings.TrimSpace(req.Quantization) == "" || !validateUseCase(req.UseCase) {
		return InstallJob{}, errors.New("invalid federated install request")
	}
	if strings.TrimSpace(req.RoleName) == "" {
		req.RoleName = "remote-managed"
	}
	if req.ContextTokens <= 0 {
		req.ContextTokens = 8192
	}
	profile, recs, err := s.FederatedRecommendations(ctx, nodeID, RecommendRequest{UseCase: req.UseCase, ContextTokens: req.ContextTokens, Limit: 50, MinimumFit: FitMarginal, StorageHeadroomPct: 20, PreferGPU: req.PreferGPU, PlacementPreference: req.PlacementPreference})
	if err != nil {
		return InstallJob{}, err
	}
	var selected *Recommendation
	for i := range recs {
		if strings.EqualFold(recs[i].Model.ModelRef, req.ModelRef) && strings.EqualFold(recs[i].Quantization, req.Quantization) {
			selected = &recs[i]
			break
		}
	}
	if selected == nil {
		return InstallJob{}, errors.New("selected model/quantization is not recommended for target hardware")
	}
	if err := s.ensureFederatedModelManager(ctx); err != nil {
		return InstallJob{}, err
	}
	plan, err := s.CreatePlan(ctx, CreatePlanCommand{NodeID: nodeID, HardwareProfileID: profile.ID, RoleName: req.RoleName, UseCase: req.UseCase, Recommendation: *selected})
	if err != nil {
		return InstallJob{}, err
	}
	plan, err = s.ApprovePlan(ctx, plan.ID, plan.Revision, FederatedModelManagerPrincipal)
	if err != nil {
		return InstallJob{}, err
	}
	actor := FederatedModelManagerPrincipal
	return s.QueueApprovedPlan(ctx, plan.ID, &actor)
}

func (s *Service) QueueOneClickInstall(ctx context.Context, req OneClickInstallRequest) (InstallJob, error) {
	if s == nil || s.catalog == nil {
		return InstallJob{}, errors.New("local AI catalog unavailable")
	}
	if strings.TrimSpace(req.WorkspaceID) == "" || strings.TrimSpace(req.HardwareProfileID) == "" || strings.TrimSpace(req.RoleName) == "" || strings.TrimSpace(req.ModelRef) == "" || strings.TrimSpace(req.Quantization) == "" || strings.TrimSpace(req.RequestedBy) == "" || !validateUseCase(req.UseCase) {
		return InstallJob{}, errors.New("invalid one-click install request")
	}
	if req.ContextTokens <= 0 {
		req.ContextTokens = 8192
	}
	profile, err := s.hardwareProfile(ctx, req.HardwareProfileID)
	if err != nil {
		return InstallJob{}, err
	}
	preference := normalizeComputePreference(req.ComputePreference)
	preferGPU := req.PreferGPU
	placement := req.PlacementPreference
	switch preference {
	case "require_cpu":
		placement = PlacementCPUOnly
	case "require_gpu":
		placement = PlacementSingleDevice
		preferGPU = true
	case "hybrid":
		placement = PlacementCPUOffload
		preferGPU = true
	case "prefer_gpu":
		preferGPU = true
	case "prefer_cpu":
		placement = PlacementCPUOnly
	}
	recommend := func(place PlacementMode, prefer bool) ([]Recommendation, error) {
		return s.Recommendations(ctx, req.HardwareProfileID, RecommendRequest{
			UseCase: req.UseCase, ContextTokens: req.ContextTokens, Limit: 50, MinimumFit: FitMarginal,
			StorageHeadroomPct: 20, PreferGPU: prefer, PlacementPreference: place,
		})
	}
	recs, err := recommend(placement, preferGPU)
	if err != nil && preference == "prefer_cpu" {
		recs, err = recommend("", false)
	}
	if err != nil { return InstallJob{}, err }
	find := func(rows []Recommendation) *Recommendation {
		for i := range rows {
			if strings.EqualFold(rows[i].Model.ModelRef, req.ModelRef) && strings.EqualFold(rows[i].Quantization, req.Quantization) {
				return &rows[i]
			}
		}
		return nil
	}
	selected := find(recs)
	if selected == nil && preference == "prefer_cpu" {
		if fallback, e := recommend("", false); e == nil { selected = find(fallback) }
	}
	if selected == nil {
		return InstallJob{}, errors.New("selected model/quantization does not fit the current hardware, trusted runtime, or storage headroom requirements")
	}
	// The plan itself is generated by trusted harness code, so the human who
	// explicitly requested installation can be its independent approver.
	ws := req.WorkspaceID
	plan, err := s.CreatePlan(ctx, CreatePlanCommand{WorkspaceID: &ws, NodeID: profile.NodeID, HardwareProfileID: profile.ID, RoleName: req.RoleName, UseCase: req.UseCase, Recommendation: *selected, ActorPrincipalID: nil})
	if err != nil {
		return InstallJob{}, err
	}
	plan, err = s.ApprovePlan(ctx, plan.ID, plan.Revision, req.RequestedBy)
	if err != nil {
		return InstallJob{}, err
	}
	return s.QueueApprovedPlan(ctx, plan.ID, &req.RequestedBy)
}

func (s *Service) QueueApprovedPlan(ctx context.Context, planID string, requestedBy *string) (InstallJob, error) {
	var out InstallJob
	p, err := s.Plan(ctx, planID)
	if err != nil {
		return out, err
	}
	if p.Status != PlanApproved {
		return out, fmt.Errorf("plan is %s, not approved", p.Status)
	}
	rec, _, _, err := s.catalog.ResolvePlan(ctx, p)
	if err != nil {
		return out, fmt.Errorf("resolve approved plan from trusted catalog: %w", err)
	}
	now := s.clock.UnixMilli()
	jid, err := s.ids.New("laijob")
	if err != nil {
		return out, err
	}
	out = InstallJob{ID: jid, WorkspaceID: p.WorkspaceID, PlanID: p.ID, CatalogID: &rec.ID, Status: InstallJobQueued, RequestedBy: requestedBy, Revision: 1, CreatedAt: now, UpdatedAt: now}
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO local_ai_install_jobs(id,workspace_id,plan_id,catalog_id,status,attempt_count,requested_by,revision,created_at,updated_at) VALUES(?,?,?,?, 'queued',0,?,1,?,?)`, out.ID, out.WorkspaceID, out.PlanID, out.CatalogID, out.RequestedBy, now, now); err != nil {
			return err
		}
		eid, _ := s.ids.New("evt")
		payload, _ := json.Marshal(map[string]any{"job_id": out.ID, "plan_id": out.PlanID, "catalog_id": rec.ID, "catalog_version": rec.CatalogVersion})
		return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: p.WorkspaceID, Type: "local_ai.install_job_queued", AggregateType: "local_ai_install_job", AggregateID: out.ID, ActorPrincipalID: requestedBy, Payload: payload, OccurredAt: now})
	})
	return out, err
}

func scanInstallJob(row interface{ Scan(...any) error }) (InstallJob, error) {
	var j InstallJob
	var ws, catalog, dep, qual, fail, actor sql.NullString
	var started, completed sql.NullInt64
	err := row.Scan(&j.ID, &ws, &j.PlanID, &catalog, &dep, &qual, &j.Status, &j.AttemptCount, &fail, &actor, &started, &completed, &j.Revision, &j.CreatedAt, &j.UpdatedAt)
	if ws.Valid {
		j.WorkspaceID = &ws.String
	}
	if catalog.Valid {
		j.CatalogID = &catalog.String
	}
	if dep.Valid {
		j.DeploymentID = &dep.String
	}
	if qual.Valid {
		j.QualificationRunID = &qual.String
	}
	if fail.Valid {
		j.FailureReason = &fail.String
	}
	if actor.Valid {
		j.RequestedBy = &actor.String
	}
	if started.Valid {
		v := started.Int64
		j.StartedAt = &v
	}
	if completed.Valid {
		v := completed.Int64
		j.CompletedAt = &v
	}
	return j, err
}

const installJobSelect = `SELECT id,workspace_id,plan_id,catalog_id,deployment_id,qualification_run_id,status,attempt_count,failure_reason,requested_by,started_at,completed_at,revision,created_at,updated_at FROM local_ai_install_jobs WHERE id=?`

func (s *Service) InstallJob(ctx context.Context, idv string) (InstallJob, error) {
	j, err := scanInstallJob(s.db.QueryRowContext(ctx, installJobSelect, idv))
	if err != nil { return j, err }
	s.enrichInstallJobProgress(ctx, &j)
	return j, nil
}

func (s *Service) RecoverInstallJobs(ctx context.Context) error {
	now := s.clock.UnixMilli()
	return s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id,workspace_id FROM local_ai_install_jobs WHERE status IN ('resolving','provisioning','starting','qualifying')`)
		if err != nil {
			return err
		}
		defer rows.Close()
		type item struct {
			id string
			ws sql.NullString
		}
		var items []item
		for rows.Next() {
			var v item
			if err := rows.Scan(&v.id, &v.ws); err != nil {
				return err
			}
			items = append(items, v)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		for _, v := range items {
			if _, err := tx.ExecContext(ctx, `UPDATE local_ai_install_jobs SET status='interrupted',failure_reason='daemon interrupted install job; durable state will be reconciled before resume',revision=revision+1,updated_at=? WHERE id=?`, now, v.id); err != nil {
				return err
			}
			eid, _ := s.ids.New("evt")
			var wsp *string
			if v.ws.Valid {
				x := v.ws.String
				wsp = &x
			}
			payload, _ := json.Marshal(map[string]any{"job_id": v.id})
			if err := s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: wsp, Type: "local_ai.install_job_interrupted", AggregateType: "local_ai_install_job", AggregateID: v.id, Payload: payload, OccurredAt: now}); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Service) TickInstallJobs(ctx context.Context, limit int) ([]InstallJob, error) {
	if limit <= 0 || limit > 20 {
		limit = 2
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM local_ai_install_jobs WHERE status IN ('queued','interrupted') ORDER BY created_at LIMIT ?`, limit)
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
		j, err := s.RunInstallJob(ctx, idv)
		if err != nil {
			if latest, getErr := s.InstallJob(ctx, idv); getErr == nil {
				out = append(out, latest)
			}
			continue
		}
		out = append(out, j)
	}
	return out, nil
}

func (s *Service) RunInstallJob(ctx context.Context, jobID string) (InstallJob, error) {
	j, err := s.InstallJob(ctx, jobID)
	if err != nil {
		return j, err
	}
	if j.Status == InstallJobReady || j.Status == InstallJobCancelled || j.Status == InstallJobFailed {
		return j, nil
	}
	if j.CatalogID == nil {
		return j, s.failInstallJob(ctx, j, "install job is not bound to a trusted catalog")
	}
	p, err := s.Plan(ctx, j.PlanID)
	if err != nil {
		return j, s.failInstallJob(ctx, j, err.Error())
	}

	// Reconcile durable plan/deployment state before repeating any step. This is
	// what makes an interrupted install resumable instead of blindly replayed.
	if p.Status == PlanReady {
		return s.markInstallJobReady(ctx, j)
	}
	if p.Status == PlanFailed || p.Status == PlanCancelled || p.Status == PlanSuperseded {
		return j, s.failInstallJob(ctx, j, "install plan is no longer executable: "+string(p.Status))
	}

	if err := s.setInstallJobStatus(ctx, &j, InstallJobResolving, nil, nil); err != nil {
		return j, err
	}

	var dep inference.ModelDeployment
	if p.Status == PlanApproved {
		_, runtime, model, err := s.catalog.ResolvePlanWithCatalog(ctx, *j.CatalogID, p)
		if err != nil {
			return j, s.failInstallJob(ctx, j, err.Error())
		}
		if err := s.setInstallJobStatus(ctx, &j, InstallJobProvisioning, nil, nil); err != nil {
			return j, err
		}
		dep, err = s.ProvisionApprovedPlan(ctx, p.ID, runtime, model)
		if err != nil {
			if isResumableDownloadError(err) {
				latest, markErr := s.markInstallJobInterrupted(ctx, j, err.Error())
				if markErr != nil {
					return j, markErr
				}
				return latest, err
			}
			return j, s.failInstallJob(ctx, j, err.Error())
		}
	} else {
		dep, err = s.deploymentForPlan(ctx, p.ID)
		if err != nil {
			return j, s.failInstallJob(ctx, j, "reconcile managed deployment: "+err.Error())
		}
	}
	j.DeploymentID = &dep.ID
	if dep.Status == inference.DeploymentReady {
		return s.markInstallJobReady(ctx, j)
	}

	if err := s.setInstallJobStatus(ctx, &j, InstallJobStarting, &dep.ID, nil); err != nil {
		return j, err
	}
	if _, err := s.StartManagedDeployment(ctx, dep.ID); err != nil {
		return j, s.failInstallJob(ctx, j, "start managed runtime: "+err.Error())
	}

	if err := s.setInstallJobStatus(ctx, &j, InstallJobQualifying, &dep.ID, nil); err != nil {
		return j, err
	}
	capability := "inference." + strings.ToLower(strings.TrimSpace(p.UseCase))
	qr, err := s.QualifyManagedDeployment(ctx, QualificationRequest{DeploymentID: dep.ID, HardwareProfileID: p.HardwareProfileID, RoleName: p.RoleName, CapabilityID: capability, RequestedContext: p.ContextTokens, ActorPrincipalID: j.RequestedBy})
	if err != nil {
		return j, s.failInstallJob(ctx, j, "qualify managed deployment: "+err.Error())
	}
	j.QualificationRunID = &qr.ID
	return s.markInstallJobReady(ctx, j)
}

func (s *Service) deploymentForPlan(ctx context.Context, planID string) (inference.ModelDeployment, error) {
	var idv string
	if err := s.db.QueryRowContext(ctx, `SELECT deployment_id FROM managed_local_models WHERE plan_id=? AND deployment_id IS NOT NULL`, planID).Scan(&idv); err != nil {
		return inference.ModelDeployment{}, err
	}
	return s.inference.Deployment(ctx, idv)
}

func (s *Service) setInstallJobStatus(ctx context.Context, j *InstallJob, status InstallJobStatus, deploymentID, qualificationID *string) error {
	now := s.clock.UnixMilli()
	started := any(nil)
	if j.StartedAt == nil {
		started = now
	} else {
		started = *j.StartedAt
	}
	res, err := s.db.ExecContext(ctx, `UPDATE local_ai_install_jobs SET status=?,deployment_id=COALESCE(?,deployment_id),qualification_run_id=COALESCE(?,qualification_run_id),attempt_count=CASE WHEN ? IN ('resolving','queued','interrupted') THEN attempt_count+1 ELSE attempt_count END,failure_reason=NULL,started_at=COALESCE(started_at,?),revision=revision+1,updated_at=? WHERE id=? AND revision=? AND status NOT IN ('ready','failed','cancelled')`, status, deploymentID, qualificationID, status, started, now, j.ID, j.Revision)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return errors.New("install job revision/state conflict")
	}
	*j, err = s.InstallJob(ctx, j.ID)
	return err
}

func (s *Service) markInstallJobReady(ctx context.Context, j InstallJob) (InstallJob, error) {
	now := s.clock.UnixMilli()
	var out InstallJob
	err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE local_ai_install_jobs SET status='ready',deployment_id=COALESCE(?,deployment_id),qualification_run_id=COALESCE(?,qualification_run_id),failure_reason=NULL,completed_at=?,revision=revision+1,updated_at=? WHERE id=? AND status NOT IN ('ready','failed','cancelled')`, j.DeploymentID, j.QualificationRunID, now, now, j.ID)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 && j.Status != InstallJobReady {
			return errors.New("install job completion state conflict")
		}
		eid, _ := s.ids.New("evt")
		payload, _ := json.Marshal(map[string]any{"job_id": j.ID, "plan_id": j.PlanID, "deployment_id": j.DeploymentID, "qualification_run_id": j.QualificationRunID})
		return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: j.WorkspaceID, Type: "local_ai.install_job_ready", AggregateType: "local_ai_install_job", AggregateID: j.ID, ActorPrincipalID: j.RequestedBy, Payload: payload, OccurredAt: now})
	})
	if err != nil {
		return out, err
	}
	return s.InstallJob(ctx, j.ID)
}

func (s *Service) markInstallJobInterrupted(ctx context.Context, j InstallJob, reason string) (InstallJob, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "install download interrupted"
	}
	now := s.clock.UnixMilli()
	err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE local_ai_install_jobs SET status='interrupted',failure_reason=?,completed_at=NULL,revision=revision+1,updated_at=? WHERE id=? AND status NOT IN ('ready','failed','cancelled')`, reason, now, j.ID)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return errors.New("install job interruption state conflict")
		}
		eid, _ := s.ids.New("evt")
		payload, _ := json.Marshal(map[string]any{"job_id": j.ID, "plan_id": j.PlanID, "reason": reason, "resumable": true})
		return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: j.WorkspaceID, Type: "local_ai.install_job_interrupted", AggregateType: "local_ai_install_job", AggregateID: j.ID, ActorPrincipalID: j.RequestedBy, Payload: payload, OccurredAt: now})
	})
	if err != nil {
		return j, err
	}
	return s.InstallJob(ctx, j.ID)
}
func (s *Service) failInstallJob(ctx context.Context, j InstallJob, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "install job failed"
	}
	now := s.clock.UnixMilli()
	_ = s.failPlanByID(ctx, j.PlanID, reason)
	return s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE local_ai_install_jobs SET status='failed',failure_reason=?,completed_at=?,revision=revision+1,updated_at=? WHERE id=? AND status NOT IN ('ready','cancelled')`, reason, now, now, j.ID); err != nil {
			return err
		}
		eid, _ := s.ids.New("evt")
		payload, _ := json.Marshal(map[string]any{"job_id": j.ID, "reason": reason})
		return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: j.WorkspaceID, Type: "local_ai.install_job_failed", AggregateType: "local_ai_install_job", AggregateID: j.ID, ActorPrincipalID: j.RequestedBy, Payload: payload, OccurredAt: now})
	})
}

func (s *Service) failPlanByID(ctx context.Context, planID, reason string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE local_model_install_plans SET status='failed',failure_reason=?,revision=revision+1,updated_at=? WHERE id=? AND status NOT IN ('ready','cancelled','superseded')`, reason, s.clock.UnixMilli(), planID)
	return err
}
