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

	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type ComputePolicy struct {
	DeploymentID      string        `json:"deployment_id"`
	Preference        string        `json:"preference"`
	PlacementMode     string        `json:"placement_mode"`
	PreferredDeviceIDs []string     `json:"preferred_device_ids"`
	RequiredDeviceIDs  []string     `json:"required_device_ids"`
	Placement         PlacementPlan `json:"placement"`
	Revision          int64         `json:"revision"`
	UpdatedAt         int64         `json:"updated_at"`
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
func uniqueDeviceIDs(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, x := range in {
		x = strings.TrimSpace(x)
		if x != "" && !seen[x] { seen[x] = true; out = append(out, x) }
	}
	return out
}
func (s *Service) ComputePolicy(ctx context.Context, deploymentID string) (ComputePolicy, error) {
	var out ComputePolicy
	var pref, mode, preferred, required string
	err := s.db.QueryRowContext(ctx, `SELECT deployment_id,preference,placement_mode,preferred_device_ids_json,required_device_ids_json,revision,updated_at
		FROM deployment_compute_policies WHERE deployment_id=?`, deploymentID).
		Scan(&out.DeploymentID, &pref, &mode, &preferred, &required, &out.Revision, &out.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		out = ComputePolicy{DeploymentID: deploymentID, Preference: "auto", PlacementMode: "auto"}
	} else if err != nil {
		return out, err
	} else {
		out.Preference, out.PlacementMode = pref, mode
		_ = json.Unmarshal([]byte(preferred), &out.PreferredDeviceIDs)
		_ = json.Unmarshal([]byte(required), &out.RequiredDeviceIDs)
	}
	var raw string
	if err := s.db.QueryRowContext(ctx, `SELECT runtime_config_json FROM model_deployments WHERE id=?`, deploymentID).Scan(&raw); err != nil { return out, err }
	var cfg struct{ Placement PlacementPlan `json:"placement"` }
	_ = json.Unmarshal([]byte(raw), &cfg)
	out.Placement = cfg.Placement
	return out, nil
}
func filterProfileDevices(p HardwareProfile, required, preferred []string) (HardwareProfile, error) {
	if len(required) > 0 {
		want := map[string]bool{}
		for _, x := range required { want[x] = true }
		var gpus []GPU
		for _, g := range p.GPUs {
			id := deviceID(g)
			if want[id] || want[fmt.Sprintf("gpu-%d", g.DeviceIndex)] { gpus = append(gpus, g) }
		}
		if len(gpus) == 0 { return p, errors.New("required GPU device is not present in the current hardware profile") }
		p.GPUs = gpus
		return p, nil
	}
	if len(preferred) > 0 {
		want := map[string]bool{}
		for _, x := range preferred { want[x] = true }
		first, rest := []GPU{}, []GPU{}
		for _, g := range p.GPUs {
			id := deviceID(g)
			if want[id] || want[fmt.Sprintf("gpu-%d", g.DeviceIndex)] { first = append(first, g) } else { rest = append(rest, g) }
		}
		p.GPUs = append(first, rest...)
	}
	return p, nil
}
func (s *Service) runtimeManifestForPlacement(ctx context.Context, runtimeName string, p HardwareProfile, placement PlacementPlan) (RuntimeManifest, error) {
	_, cat, err := s.catalog.Active(ctx)
	if err != nil { return RuntimeManifest{}, err }
	backend := strings.ToLower(strings.TrimSpace(placement.Backend))
	if placement.Mode == PlacementCPUOnly { backend = "cpu" }
	for _, r := range cat.Runtimes {
		if strings.EqualFold(r.Name, runtimeName) && strings.EqualFold(r.OS, p.OSName) && strings.EqualFold(r.Architecture, p.Architecture) && strings.EqualFold(strings.TrimSpace(r.Backend), backend) {
			return RuntimeManifest{Name:r.Name,Version:r.Version,Backend:r.Backend,OS:r.OS,Architecture:r.Architecture,SourceURL:r.SourceURL,SHA256:r.SHA256,ArchiveFormat:r.ArchiveFormat,ExecutableRel:r.ExecutableRel}, nil
		}
	}
	return RuntimeManifest{}, fmt.Errorf("trusted catalog has no %s runtime for backend %s on %s/%s", runtimeName, backend, p.OSName, p.Architecture)
}
func (s *Service) ensureComputeRuntime(ctx context.Context, nodeID string, runtime RuntimeManifest) (string, string, error) {
	backendDir := strings.ToLower(strings.TrimSpace(runtime.Backend))
	if backendDir == "" { backendDir = "generic" }
	root := filepath.Join(s.dataDir, "runtimes", runtime.Name, runtime.Version, backendDir)
	exe := filepath.Join(root, runtime.ExecutableRel)
	if st, err := os.Stat(exe); err != nil || st.IsDir() {
		if _, err := os.Stat(root); err == nil {
			return "", "", errors.New("managed runtime target exists but executable is unavailable")
		} else if !os.IsNotExist(err) {
			return "", "", err
		}
		archive := filepath.Join(s.dataDir, "downloads", "runtime-"+runtime.Name+"-"+runtime.Version+"-"+backendDir)
		if _, err := s.fetcher.Fetch(ctx, runtime.SourceURL, archive, runtime.SHA256); err != nil { return "", "", err }
		staging := root + ".installing"
		_ = os.RemoveAll(staging)
		if runtime.ArchiveFormat == "binary" {
			if err := os.MkdirAll(staging, 0o700); err != nil { return "", "", err }
			raw, err := os.ReadFile(archive); if err != nil { return "", "", err }
			runtime.ExecutableRel = filepath.Base(runtime.ExecutableRel)
			if err := os.WriteFile(filepath.Join(staging, runtime.ExecutableRel), raw, 0o700); err != nil { return "", "", err }
		} else if err := ExtractRuntimeArchive(archive, runtime.ArchiveFormat, staging); err != nil {
			_ = os.RemoveAll(staging); return "", "", err
		}
		staged := filepath.Join(staging, runtime.ExecutableRel)
		if st, err := os.Stat(staged); err != nil || st.IsDir() { _ = os.RemoveAll(staging); return "", "", errors.New("runtime executable missing after install") }
		if err := os.Rename(staging, root); err != nil { _ = os.RemoveAll(staging); return "", "", err }
		exe = filepath.Join(root, runtime.ExecutableRel)
	}
	runtimeID, err := s.ensureManagedRuntime(ctx, nodeID, runtime, root, exe)
	if err != nil { return "", "", err }
	return runtimeID, exe, nil
}

func (s *Service) SetComputePolicy(ctx context.Context, c ComputePolicyCommand) (ComputePolicy, error) {
	c.DeploymentID = strings.TrimSpace(c.DeploymentID)
	c.ActorPrincipalID = strings.TrimSpace(c.ActorPrincipalID)
	if c.DeploymentID == "" || c.ActorPrincipalID == "" { return ComputePolicy{}, errors.New("deployment and actor are required") }

	preference := normalizeComputePreference(c.Preference)
	mode := normalizePlacementMode(c.PlacementMode)
	required := uniqueDeviceIDs(c.RequiredDeviceIDs)
	preferred := uniqueDeviceIDs(c.PreferredDeviceIDs)

	var nodeID, runtimeName string
	var requiredBytes int64
	if err := s.db.QueryRowContext(ctx, `SELECT mm.node_id,p.runtime_name,p.memory_required_bytes
		FROM managed_local_models mm JOIN local_model_install_plans p ON p.id=mm.plan_id
		WHERE mm.deployment_id=? AND mm.status<>'removed'`, c.DeploymentID).
		Scan(&nodeID, &runtimeName, &requiredBytes); err != nil { return ComputePolicy{}, err }
	var profileID string
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM local_hardware_profiles WHERE node_id=? ORDER BY detected_at DESC LIMIT 1`, nodeID).Scan(&profileID); err != nil {
		return ComputePolicy{}, errors.New("detect hardware before changing compute placement")
	}
	profile, err := s.hardwareProfile(ctx, profileID)
	if err != nil { return ComputePolicy{}, err }
	profile, err = filterProfileDevices(profile, required, preferred)
	if err != nil { return ComputePolicy{}, err }

	requested := mode
	switch preference {
	case "require_cpu":
		requested = PlacementCPUOnly
	case "require_gpu":
		if requested == "" || requested == PlacementCPUOnly || requested == PlacementCPUOffload { requested = PlacementSingleDevice }
	case "hybrid":
		requested = PlacementCPUOffload
	case "prefer_cpu":
		_, _, _, fit := bestPlacement(profile, requiredBytes, PlacementCPUOnly)
		if fit != FitTooTight { requested = PlacementCPUOnly }
	}
	placement, runMode, _, fit := bestPlacement(profile, requiredBytes, requested)
	if fit == FitTooTight { return ComputePolicy{}, errors.New("requested compute placement does not fit the detected hardware") }
	if preference == "require_gpu" && runMode != RunGPU && runMode != RunMoE { return ComputePolicy{}, errors.New("required GPU placement could not be satisfied") }
	if preference == "require_cpu" && runMode != RunCPU { return ComputePolicy{}, errors.New("required CPU placement could not be satisfied") }
	if preference == "hybrid" && runMode != RunCPUGPU { return ComputePolicy{}, errors.New("hybrid placement could not be satisfied") }

	manifest, err := s.runtimeManifestForPlacement(ctx, runtimeName, profile, placement)
	if err != nil { return ComputePolicy{}, err }
	runtimeID, executable, err := s.ensureComputeRuntime(ctx, nodeID, manifest)
	if err != nil { return ComputePolicy{}, err }

	active := false
	var instanceStatus string
	if err := s.db.QueryRowContext(ctx, `SELECT status FROM local_runtime_instances WHERE deployment_id=?`, c.DeploymentID).Scan(&instanceStatus); err == nil {
		switch instanceStatus { case "starting", "healthy", "busy", "draining": active = true }
	}
	if active {
		if err := s.supervisor.Stop(ctx, c.DeploymentID); err != nil { return ComputePolicy{}, fmt.Errorf("stop runtime before compute move: %w", err) }
	}

	var raw string
	if err := s.db.QueryRowContext(ctx, `SELECT runtime_config_json FROM model_deployments WHERE id=?`, c.DeploymentID).Scan(&raw); err != nil { return ComputePolicy{}, err }
	cfg := map[string]any{}
	_ = json.Unmarshal([]byte(raw), &cfg)
	cfg["placement"] = placement
	cfg["runtime_backend"] = manifest.Backend
	cfg["executable"] = executable
	cfg["runtime_name"] = manifest.Name
	updatedCfg, _ := json.Marshal(cfg)
	prefJSON, _ := json.Marshal(preferred)
	reqJSON, _ := json.Marshal(required)
	storedMode := "auto"
	if mode != "" { storedMode = string(mode) }
	now := s.clock.UnixMilli()

	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE model_deployments SET runtime_name=?,runtime_version=?,runtime_config_json=?,revision=revision+1,updated_at=? WHERE id=?`,
			manifest.Name, manifest.Version, string(updatedCfg), now, c.DeploymentID); err != nil { return err }
		if _, err := tx.ExecContext(ctx, `UPDATE managed_local_models SET runtime_id=?,revision=revision+1,updated_at=? WHERE deployment_id=?`, runtimeID, now, c.DeploymentID); err != nil { return err }
		// Placement/hardware changes invalidate the current Agent Check and
		// production admission. Preserve completed testbed history as evidence,
		// but never present evidence from the old placement as current.
		placementJSON,_:=json.Marshal(placement)
		if _,err:=tx.ExecContext(ctx,`UPDATE model_spec_sheets SET
		  hardware_profile_id=?,placement_json=?,
		  qualification_json='{"status":"pending_manual_agent_check"}',
		  admission_status='pending',restrictions_json='{}',
		  admitted_by=NULL,admitted_at=NULL,admission_notes=NULL,
		  revision=revision+1,updated_at=?
		 WHERE deployment_id=? AND (placement_json<>? OR hardware_profile_id<>?)`,
		 profileID,string(placementJSON),now,c.DeploymentID,string(placementJSON),profileID);err!=nil{return err}

		hasCPU, hasGPU := false, false
		var ram, vram int64
		deviceIDs := []string{}
		for _, d := range placement.Devices {
			if d.Kind == "cpu" { hasCPU = true; ram += d.AllocatedBytes }
			if d.Kind == "accelerator" {
				hasGPU = true; vram += d.AllocatedBytes
				id := strings.TrimSpace(d.DeviceID)
				if id == "" { id = fmt.Sprintf("gpu-%d", d.DeviceIndex) }
				deviceIDs = append(deviceIDs, id)
			}
		}
		computeMode := "cpu"
		if hasGPU { computeMode = "gpu" }
		if hasCPU && hasGPU { computeMode = "hybrid" }
		deviceRaw, _ := json.Marshal(deviceIDs)
		if _, err := tx.ExecContext(ctx, `INSERT INTO runtime_compute_profiles(deployment_id,compute_mode,backend,ram_bytes,gpu_device_ids_json,vram_bytes,cached,resident,metadata_json,updated_at)
			VALUES(?,?,?,?,?,?,0,0,'{}',?)
			ON CONFLICT(deployment_id) DO UPDATE SET compute_mode=excluded.compute_mode,backend=excluded.backend,ram_bytes=excluded.ram_bytes,
			gpu_device_ids_json=excluded.gpu_device_ids_json,vram_bytes=excluded.vram_bytes,updated_at=excluded.updated_at`,
			c.DeploymentID, computeMode, placement.Backend, ram, string(deviceRaw), vram, now); err != nil { return err }

		_, err := tx.ExecContext(ctx, `INSERT INTO deployment_compute_policies(deployment_id,preference,placement_mode,preferred_device_ids_json,required_device_ids_json,metadata_json,revision,updated_by,created_at,updated_at)
			VALUES(?,?,?,?,?,'{}',1,?,?,?)
			ON CONFLICT(deployment_id) DO UPDATE SET preference=excluded.preference,placement_mode=excluded.placement_mode,
			preferred_device_ids_json=excluded.preferred_device_ids_json,required_device_ids_json=excluded.required_device_ids_json,
			revision=deployment_compute_policies.revision+1,updated_by=excluded.updated_by,updated_at=excluded.updated_at`,
			c.DeploymentID, preference, storedMode, string(prefJSON), string(reqJSON), c.ActorPrincipalID, now, now)
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
