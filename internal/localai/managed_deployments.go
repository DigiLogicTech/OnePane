package localai

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/inference"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

const ColibriRuntimeVersion = "1.12.1"

type ManagedDeploymentSummary struct {
	DeploymentID       string   `json:"deployment_id"`
	ModelID            string   `json:"model_id"`
	ModelRef           string   `json:"model_ref"`
	DisplayName        string   `json:"display_name,omitempty"`
	RuntimeName        string   `json:"runtime_name"`
	RuntimeVersion     string   `json:"runtime_version,omitempty"`
	RuntimeBackend     string   `json:"runtime_backend,omitempty"`
	LocalPath          string   `json:"local_path"`
	Quantization       string   `json:"quantization,omitempty"`
	Status             string   `json:"status"`
	AdmissionStatus    string   `json:"admission_status,omitempty"`
	ContextMaxReported *int64   `json:"context_max_reported,omitempty"`
	ContextMaxVerified *int64   `json:"context_max_verified,omitempty"`
	UpdatedAt          int64    `json:"updated_at"`
	ComputeMode        string   `json:"compute_mode,omitempty"`
	ComputeBackend     string   `json:"compute_backend,omitempty"`
	ComputeDeviceIDs   []string `json:"compute_device_ids,omitempty"`
	RAMBytes           int64    `json:"ram_bytes,omitempty"`
	VRAMBytes          int64    `json:"vram_bytes,omitempty"`
	Qualified          bool     `json:"qualified"`
	InvestigationState string   `json:"investigation_state,omitempty"`
}

func (s *Service) ManagedDeployments(ctx context.Context, workspaceID string) ([]ManagedDeploymentSummary, error) {
	q := `SELECT d.id,d.model_id,mm.model_ref,COALESCE(json_extract(m.static_metadata_json,'$.display_name'),mm.model_ref),COALESCE(d.runtime_name,''),COALESCE(d.runtime_version,''),COALESCE(json_extract(d.runtime_config_json,'$.runtime_backend'),''),mm.local_path,COALESCE(m.quantization,''),d.status,COALESCE(ms.admission_status,''),COALESCE(mis.investigation_state,'uninvestigated'),d.context_max_reported,d.context_max_verified,d.updated_at,p.plan_json,d.runtime_config_json
          FROM managed_local_models mm JOIN model_deployments d ON d.id=mm.deployment_id JOIN models m ON m.id=d.model_id JOIN local_model_install_plans p ON p.id=mm.plan_id LEFT JOIN model_spec_sheets ms ON ms.deployment_id=d.id LEFT JOIN model_identity_specs mis ON mis.model_uid=d.model_id
          WHERE mm.status<>'removed'`
	args := []any{}
	if strings.TrimSpace(workspaceID) != "" {
		q += ` AND p.workspace_id=?`
		args = append(args, strings.TrimSpace(workspaceID))
	}
	q += ` ORDER BY d.updated_at DESC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ManagedDeploymentSummary{}
	for rows.Next() {
		var x ManagedDeploymentSummary
		var rep, ver sql.NullInt64
		var planJSON, runtimeConfigJSON string
		if err := rows.Scan(&x.DeploymentID, &x.ModelID, &x.ModelRef, &x.DisplayName, &x.RuntimeName, &x.RuntimeVersion, &x.RuntimeBackend, &x.LocalPath, &x.Quantization, &x.Status, &x.AdmissionStatus, &x.InvestigationState, &rep, &ver, &x.UpdatedAt, &planJSON, &runtimeConfigJSON); err != nil {
			return nil, err
		}
		if rep.Valid {
			x.ContextMaxReported = &rep.Int64
		}
		if ver.Valid {
			x.ContextMaxVerified = &ver.Int64
		}
		x.Qualified = x.InvestigationState == "qualified" || x.AdmissionStatus == "accepted" || x.AdmissionStatus == "restricted"
		var rec Recommendation
		_ = json.Unmarshal([]byte(planJSON), &rec)
		var runtimeCfg struct {
			Placement PlacementPlan `json:"placement"`
		}
		if json.Unmarshal([]byte(runtimeConfigJSON), &runtimeCfg) == nil && runtimeCfg.Placement.Mode != "" {
			rec.Placement = runtimeCfg.Placement
		}
		if rec.Placement.Mode != "" {
			x.ComputeBackend = rec.Placement.Backend
			hasCPU, hasGPU := false, false
			for _, dev := range rec.Placement.Devices {
				if dev.Kind == "accelerator" {
					hasGPU = true
					x.VRAMBytes += dev.AllocatedBytes
					id := strings.TrimSpace(dev.DeviceID)
					if id == "" {
						id = fmt.Sprintf("gpu-%d", dev.DeviceIndex)
					}
					x.ComputeDeviceIDs = append(x.ComputeDeviceIDs, id)
				}
				if dev.Kind == "cpu" {
					hasCPU = true
					x.RAMBytes += dev.AllocatedBytes
				}
			}
			x.ComputeMode = "cpu"
			if hasGPU {
				x.ComputeMode = "gpu"
			}
			if hasCPU && hasGPU || rec.Placement.Mode == PlacementCPUOffload {
				x.ComputeMode = "hybrid"
			}
		}
		if x.ComputeBackend == "" {
			x.ComputeBackend = x.RuntimeBackend
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

type RegisterColibriCommand struct {
	WorkspaceID      string `json:"workspace_id"`
	ModelPath        string `json:"model_path"`
	ModelRef         string `json:"model_ref"`
	DisplayName      string `json:"display_name"`
	ContextTokens    int64  `json:"context_tokens"`
	ActorPrincipalID string `json:"-"`
}

func (s *Service) RegisterColibriFolder(ctx context.Context, cmd RegisterColibriCommand) (inference.ModelDeployment, error) {
	var out inference.ModelDeployment
	cmd.WorkspaceID = strings.TrimSpace(cmd.WorkspaceID)
	cmd.ModelPath = strings.TrimSpace(cmd.ModelPath)
	cmd.ModelRef = strings.TrimSpace(cmd.ModelRef)
	cmd.DisplayName = strings.TrimSpace(cmd.DisplayName)
	if cmd.WorkspaceID == "" || cmd.ModelPath == "" || cmd.ActorPrincipalID == "" {
		return out, errors.New("workspace, model path and actor are required")
	}
	if cmd.ModelRef == "" {
		cmd.ModelRef = filepath.Base(filepath.Clean(cmd.ModelPath))
	}
	if cmd.DisplayName == "" {
		cmd.DisplayName = cmd.ModelRef
	}
	if cmd.ContextTokens <= 0 {
		cmd.ContextTokens = 8192
	}
	if !filepath.IsAbs(cmd.ModelPath) {
		return out, errors.New("Colibri model path must be absolute")
	}
	cmd.ModelPath = filepath.Clean(cmd.ModelPath)
	st, err := os.Stat(cmd.ModelPath)
	if err != nil || !st.IsDir() {
		return out, errors.New("Colibri model path must be an existing model directory")
	}
	if _, err := os.Stat(filepath.Join(cmd.ModelPath, "config.json")); err != nil {
		return out, errors.New("Colibri model folder must contain config.json")
	}
	rel, err := filepath.Rel(s.modelRoot, cmd.ModelPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return out, fmt.Errorf("Colibri model folder must be inside the configured OnePane model pool %s", s.modelRoot)
	}
	artifact, err := colibriArtifact()
	if err != nil {
		return out, err
	}
	runtimeRoot := filepath.Join(s.dataDir, "runtimes", "colibri", ColibriRuntimeVersion)
	serverPath := filepath.Join(runtimeRoot, artifact.Manifest.ExecutableRel)
	enginePath := filepath.Join(runtimeRoot, artifact.EngineRel)
	if _, err := os.Stat(serverPath); err != nil {
		return out, errors.New("Colibri runtime is not installed; install it from Models first")
	}
	if _, err := os.Stat(enginePath); err != nil {
		return out, errors.New("Colibri engine is not installed")
	}
	var nodeID string
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM harness_nodes WHERE local=1 LIMIT 1`).Scan(&nodeID); err != nil {
		return out, err
	}
	var hardwareID string
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM local_hardware_profiles WHERE node_id=? ORDER BY detected_at DESC LIMIT 1`, nodeID).Scan(&hardwareID); err != nil {
		return out, errors.New("detect local hardware before registering a Colibri model")
	}
	var existing string
	if err := s.db.QueryRowContext(ctx, `SELECT d.id FROM model_deployments d JOIN models m ON m.id=d.model_id WHERE d.node_id=? AND COALESCE(d.runtime_name,'')='colibri' AND m.model_ref=? AND json_extract(d.runtime_config_json,'$.model_path')=? LIMIT 1`, nodeID, cmd.ModelRef, cmd.ModelPath).Scan(&existing); err == nil {
		return s.inference.Deployment(ctx, existing)
	} else if err != sql.ErrNoRows {
		return out, err
	}
	provider := "local"
	arch := "moe"
	quant := "colibri"
	metadata, _ := json.Marshal(map[string]any{"managed": true, "display_name": cmd.DisplayName, "colibri": true, "source": "local-folder"})
	var model inference.Model
	var modelID string
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM models WHERE model_ref=? AND COALESCE(weights_hash,'')='' AND COALESCE(quantization,'')=? LIMIT 1`, cmd.ModelRef, quant).Scan(&modelID); err == nil {
		model, err = s.inference.Model(ctx, modelID)
		if err != nil {
			return out, err
		}
	} else if err == sql.ErrNoRows {
		model, err = s.inference.RegisterModel(ctx, inference.RegisterModelCommand{ProviderName: &provider, ModelRef: cmd.ModelRef, Architecture: &arch, Quantization: &quant, StaticMetadataJSON: metadata, TrustState: inference.ModelQuarantined, ActorPrincipalID: &cmd.ActorPrincipalID})
		if err != nil {
			return out, err
		}
	} else {
		return out, err
	}
	runtimeName := "colibri"
	runtimeVersion := ColibriRuntimeVersion
	placement := PlacementPlan{Mode: PlacementCPUOffload, Backend: "colibri", Notes: []string{"Colibri manages VRAM/RAM/NVMe expert placement; OnePane remains scheduler and admission authority."}}
	cfg, _ := json.Marshal(map[string]any{"managed": true, "executable": serverPath, "engine_path": enginePath, "model_path": cmd.ModelPath, "model_ref": cmd.ModelRef, "context_tokens": cmd.ContextTokens, "runtime_backend": "colibri", "placement": placement, "colibri_tier": defaultColibriTier()})
	dep, err := s.inference.RegisterDeployment(ctx, inference.RegisterDeploymentCommand{ModelID: model.ID, NodeID: &nodeID, RuntimeName: &runtimeName, RuntimeVersion: &runtimeVersion, RuntimeConfigJSON: cfg, ContextMaxReported: &cmd.ContextTokens, ActorPrincipalID: &cmd.ActorPrincipalID})
	if err != nil {
		return out, err
	}
	dep, err = s.inference.SetDeploymentStatus(ctx, inference.SetDeploymentStatusCommand{DeploymentID: dep.ID, ExpectedRevision: dep.Revision, Status: inference.DeploymentQualifying, ActorPrincipalID: &cmd.ActorPrincipalID, Reason: "Colibri local folder registered; empirical Agent Check required"})
	if err != nil {
		return out, err
	}
	now := s.clock.UnixMilli()
	planID, _ := s.ids.New("lmp")
	rec := Recommendation{Model: ModelSpec{ModelRef: cmd.ModelRef, DisplayName: cmd.DisplayName, Provider: "Local", Architecture: "moe", ContextLength: cmd.ContextTokens, UseCases: []UseCase{UseGeneral, UseChat, UseReasoning, UseCoding}, SourceRef: "local://" + filepath.ToSlash(cmd.ModelPath), Runtime: "colibri"}, Quantization: quant, ContextTokens: cmd.ContextTokens, FitLevel: FitMarginal, RunMode: RunMoE, MemoryRequired: 1, MemoryAvailable: 1, DiskRequired: 1, Placement: placement, Notes: []string{"Imported local Colibri container; qualification is authoritative."}}
	planJSON, _ := json.Marshal(rec)
	runtimeID, err := s.ensureManagedRuntime(ctx, nodeID, artifact.Manifest, runtimeRoot, serverPath)
	if err != nil {
		return out, err
	}
	managedID, _ := s.ids.New("managedmodel")
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO local_model_install_plans(id,workspace_id,node_id,hardware_profile_id,role_name,use_case,model_ref,source_ref,runtime_name,quantization,context_tokens,fit_level,run_mode,memory_required_bytes,disk_required_bytes,download_scratch_bytes,plan_json,status,created_by,approved_by,revision,created_at,updated_at) VALUES(?,?,?,?,?,'general',?,?,?,?,?,'marginal','moe',1,1,0,?,'qualifying',?,?,1,?,?)`, planID, cmd.WorkspaceID, nodeID, hardwareID, "user-selected", cmd.ModelRef, "local://"+filepath.ToSlash(cmd.ModelPath), runtimeName, quant, cmd.ContextTokens, string(planJSON), cmd.ActorPrincipalID, cmd.ActorPrincipalID, now, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO managed_local_models(id,node_id,plan_id,model_id,deployment_id,runtime_id,model_ref,source_ref,local_path,status,revision,installed_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,'qualifying',1,?,?)`, managedID, nodeID, planID, model.ID, dep.ID, runtimeID, cmd.ModelRef, "local://"+filepath.ToSlash(cmd.ModelPath), cmd.ModelPath, now, now); err != nil {
			return err
		}
		claims, _ := json.Marshal(map[string]any{"runtime": "colibri", "version": ColibriRuntimeVersion, "imported_folder": cmd.ModelPath})
		if err := s.ensurePendingSpecSheetTx(ctx, tx, dep.ID, model.ID, hardwareID, placement, claims, json.RawMessage(`{}`), json.RawMessage(`{"status":"pending_agent_check"}`), now); err != nil {
			return err
		}
		eid, _ := s.ids.New("evt")
		payload, _ := json.Marshal(map[string]any{"deployment_id": dep.ID, "model_ref": cmd.ModelRef, "runtime": "colibri", "model_path": cmd.ModelPath})
		return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: &cmd.WorkspaceID, Type: "local_ai.colibri_model_registered", AggregateType: "model_deployment", AggregateID: dep.ID, ActorPrincipalID: &cmd.ActorPrincipalID, Payload: payload, OccurredAt: now})
	})
	if err != nil {
		return out, err
	}
	return s.inference.Deployment(ctx, dep.ID)
}
