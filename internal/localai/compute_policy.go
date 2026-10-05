package localai

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type ComputePolicy struct {
	DeploymentID       string        `json:"deployment_id"`
	Preference         string        `json:"preference"`
	PlacementMode      string        `json:"placement_mode"`
	PreferredDeviceIDs []string      `json:"preferred_device_ids"`
	RequiredDeviceIDs  []string      `json:"required_device_ids"`
	Placement          PlacementPlan `json:"placement"`
	Revision           int64         `json:"revision"`
	UpdatedAt          int64         `json:"updated_at"`
}
type ComputePolicyCommand struct {
	DeploymentID       string
	Preference         string
	PlacementMode      string
	PreferredDeviceIDs []string
	RequiredDeviceIDs  []string
	ActorPrincipalID   string
}

func normalizeComputePreference(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "prefer_gpu", "prefer_cpu", "require_gpu", "require_cpu", "hybrid":
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return "auto"
	}
}
func normalizePlacementMode(v string) PlacementMode {
	switch PlacementMode(strings.ToLower(strings.TrimSpace(v))) {
	case PlacementSingleDevice, PlacementLayerSharded, PlacementRowSharded, PlacementTensorSharded, PlacementCPUOffload, PlacementCPUOnly:
		return PlacementMode(strings.ToLower(strings.TrimSpace(v)))
	default:
		return ""
	}
}
func uniqueIDs(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, x := range in {
		x = strings.TrimSpace(x)
		if x != "" && !seen[x] { seen[x] = true; out = append(out, x) }
	}
	return out
}
func (s *Service) ComputePolicy(ctx context.Context, deploymentID string) (ComputePolicy, error) {
	var x ComputePolicy
	var pref, mode, preferred, required string
	err := s.db.QueryRowContext(ctx, `SELECT deployment_id,preference,placement_mode,preferred_device_ids_json,required_device_ids_json,revision,updated_at
		FROM deployment_compute_policies WHERE deployment_id=?`, deploymentID).
		Scan(&x.DeploymentID, &pref, &mode, &preferred, &required, &x.Revision, &x.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		x = ComputePolicy{DeploymentID: deploymentID, Preference: "auto", PlacementMode: "auto"}
	} else if err != nil {
		return x, err
	} else {
		x.Preference, x.PlacementMode = pref, mode
		_ = json.Unmarshal([]byte(preferred), &x.PreferredDeviceIDs)
		_ = json.Unmarshal([]byte(required), &x.RequiredDeviceIDs)
	}
	var raw string
	if err := s.db.QueryRowContext(ctx, `SELECT runtime_config_json FROM model_deployments WHERE id=?`, deploymentID).Scan(&raw); err != nil { return x, err }
	var cfg struct{ Placement PlacementPlan `json:"placement"` }
	_ = json.Unmarshal([]byte(raw), &cfg)
	x.Placement = cfg.Placement
	return x, nil
}
func filterProfileDevices(p HardwareProfile, required, preferred []string) (HardwareProfile, error) {
	if len(required) > 0 {
		want := map[string]bool{}
		for _, x := range required { want[x] = true }
		var gs []GPU
		for _, g := range p.GPUs {
			id := deviceID(g)
			if want[id] || want[fmt.Sprintf("gpu-%d", g.DeviceIndex)] { gs = append(gs, g) }
		}
		if len(gs) == 0 { return p, errors.New("required GPU device is not present in the current hardware profile") }
		p.GPUs = gs
		return p, nil
	}
	if len(preferred) > 0 {
		want := map[string]bool{}
		for _, x := range preferred { want[x] = true }
		var first, rest []GPU
		for _, g := range p.GPUs {
			id := deviceID(g)
			if want[id] || want[fmt.Sprintf("gpu-%d", g.DeviceIndex)] { first = append(first, g) } else { rest = append(rest, g) }
		}
		p.GPUs = append(first, rest...)
	}
	return p, nil
}
func (s *Service) SetComputePolicy(ctx context.Context, c ComputePolicyCommand) (ComputePolicy, error) {
	c.DeploymentID = strings.TrimSpace(c.DeploymentID)
	c.ActorPrincipalID = strings.TrimSpace(c.ActorPrincipalID)
	if c.DeploymentID == "" || c.ActorPrincipalID == "" { return ComputePolicy{}, errors.New("deployment and actor are required") }

	pref := normalizeComputePreference(c.Preference)
	mode := normalizePlacementMode(c.PlacementMode)
	required := uniqueIDs(c.RequiredDeviceIDs)
	preferred := uniqueIDs(c.PreferredDeviceIDs)

	var nodeID string
	var requiredBytes int64
	if err := s.db.QueryRowContext(ctx, `SELECT mm.node_id,p.memory_required_bytes
		FROM managed_local_models mm JOIN local_model_install_plans p ON p.id=mm.plan_id
		WHERE mm.deployment_id=? AND mm.status<>'removed'`, c.DeploymentID).Scan(&nodeID, &requiredBytes); err != nil {
		return ComputePolicy{}, err
	}
	var profileID string
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM local_hardware_profiles WHERE node_id=? ORDER BY detected_at DESC LIMIT 1`, nodeID).Scan(&profileID); err != nil {
		return ComputePolicy{}, errors.New("detect hardware before changing compute placement")
	}
	profile, err := s.hardwareProfile(ctx, profileID)
	if err != nil { return ComputePolicy{}, err }
	profile, err = filterProfileDevices(profile, required, preferred)
	if err != nil { return ComputePolicy{}, err }

	requested := mode
	switch pref {
	case "require_cpu":
		requested = PlacementCPUOnly
	case "require_gpu":
		if requested == "" || requested == PlacementCPUOnly || requested == PlacementCPUOffload { requested = PlacementSingleDevice }
	case "hybrid":
		requested = PlacementCPUOffload
	case "prefer_cpu":
		if _, _, _, fit := bestPlacement(profile, requiredBytes, PlacementCPUOnly); fit != FitTooTight { requested = PlacementCPUOnly }
	}
	placement, runMode, _, fit := bestPlacement(profile, requiredBytes, requested)
	if fit == FitTooTight { return ComputePolicy{}, errors.New("requested compute placement does not fit the detected hardware") }
	if pref == "require_gpu" && runMode != RunGPU && runMode != RunMoE { return ComputePolicy{}, errors.New("required GPU placement could not be satisfied") }
	if pref == "require_cpu" && runMode != RunCPU { return ComputePolicy{}, errors.New("required CPU placement could not be satisfied") }
	if pref == "hybrid" && runMode != RunCPUGPU { return ComputePolicy{}, errors.New("hybrid placement could not be satisfied") }

	active := false
	var instStatus string
	if err := s.db.QueryRowContext(ctx, `SELECT status FROM local_runtime_instances WHERE deployment_id=? ORDER BY updated_at DESC LIMIT 1`, c.DeploymentID).Scan(&instStatus); err == nil {
		switch instStatus { case "starting", "healthy", "busy": active = true }
	}
	if active {
		if err := s.supervisor.Stop(ctx, c.DeploymentID); err != nil { return ComputePolicy{}, fmt.Errorf("stop runtime before compute move: %w", err) }
	}

	var raw string
	if err := s.db.QueryRowContext(ctx, `SELECT runtime_config_json FROM model_deployments WHERE id=?`, c.DeploymentID).Scan(&raw); err != nil { return ComputePolicy{}, err }
	cfg := map[string]any{}
	_ = json.Unmarshal([]byte(raw), &cfg)
	cfg["placement"] = placement
	updatedCfg, _ := json.Marshal(cfg)

	now := s.clock.UnixMilli()
	pj, _ := json.Marshal(preferred)
	rj, _ := json.Marshal(required)
	storedMode := "auto"
	if mode != "" { storedMode = string(mode) }

	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE model_deployments SET runtime_config_json=?,revision=revision+1,updated_at=? WHERE id=?`, string(updatedCfg), now, c.DeploymentID); err != nil { return err }
		if _, err := tx.ExecContext(ctx, `INSERT INTO deployment_compute_policies(deployment_id,preference,placement_mode,preferred_device_ids_json,required_device_ids_json,metadata_json,revision,updated_by,created_at,updated_at)
			VALUES(?,?,?,?,?,'{}',1,?,?,?)
			ON CONFLICT(deployment_id) DO UPDATE SET preference=excluded.preference,placement_mode=excluded.placement_mode,
			preferred_device_ids_json=excluded.preferred_device_ids_json,required_device_ids_json=excluded.required_device_ids_json,
			revision=deployment_compute_policies.revision+1,updated_by=excluded.updated_by,updated_at=excluded.updated_at`,
			c.DeploymentID, pref, storedMode, string(pj), string(rj), c.ActorPrincipalID, now, now); err != nil { return err }

		hasCPU, hasGPU := false, false
		var ram, vram int64
		ids := []string{}
		for _, d := range placement.Devices {
			if d.Kind == "cpu" { hasCPU = true; ram += d.AllocatedBytes }
			if d.Kind == "accelerator" {
				hasGPU = true; vram += d.AllocatedBytes
				id := d.DeviceID
				if id == "" { id = fmt.Sprintf("gpu-%d", d.DeviceIndex) }
				ids = append(ids, id)
			}
		}
		computeMode := "cpu"
		if hasGPU { computeMode = "gpu" }
		if hasCPU && hasGPU { computeMode = "hybrid" }
		idsRaw, _ := json.Marshal(ids)
		_, err := tx.ExecContext(ctx, `INSERT INTO runtime_compute_profiles(deployment_id,compute_mode,backend,ram_bytes,gpu_device_ids_json,vram_bytes,cached,resident,metadata_json,updated_at)
			VALUES(?,?,?,?,?,?,0,0,'{}',?)
			ON CONFLICT(deployment_id) DO UPDATE SET compute_mode=excluded.compute_mode,backend=excluded.backend,ram_bytes=excluded.ram_bytes,
			gpu_device_ids_json=excluded.gpu_device_ids_json,vram_bytes=excluded.vram_bytes,updated_at=excluded.updated_at`,
			c.DeploymentID, computeMode, placement.Backend, ram, string(idsRaw), vram, now)
		return err
	})
	if err != nil { return ComputePolicy{}, err }

	if active {
		if _, err := s.supervisor.Start(ctx, c.DeploymentID); err != nil {
			return ComputePolicy{}, fmt.Errorf("compute placement saved but runtime restart failed: %w", err)
		}
	}
	return s.ComputePolicy(ctx, c.DeploymentID)
}
