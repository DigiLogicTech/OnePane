package nodefederation

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/storage"
)

func stableID(prefix string, parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return prefix + "_" + hex.EncodeToString(h[:12])
}

func (s *Service) BuildManifest(ctx context.Context) (CapabilityManifest, error) {
	m := CapabilityManifest{Protocol: ProtocolVersion, NodeID: s.local.ID, Sequence: s.clock.UnixMilli(), Generated: s.clock.UnixMilli()}
	m.Compute = s.localComputeState(ctx)
	rows, err := s.db.QueryContext(ctx, `SELECT d.id,m.model_ref,m.provider_name,m.architecture,m.revision_ref,m.weights_hash,m.quantization,m.modalities_json,d.runtime_name,d.runtime_version,COALESCE(d.context_max_verified,d.context_max_reported,0),COALESCE(ms.admission_status,''),COALESCE(ms.restrictions_json,'{}'),d.runtime_config_json,COALESCE(d.residency_state,'')
FROM model_deployments d JOIN models m ON m.id=d.model_id
LEFT JOIN model_spec_sheets ms ON ms.deployment_id=d.id
WHERE d.node_id=? AND d.provider_connection_id IS NULL AND d.status IN ('ready','degraded') AND m.trust_state IN ('trusted','user_trusted') AND (NOT EXISTS (SELECT 1 FROM managed_local_models mm WHERE mm.deployment_id=d.id) OR ms.admission_status IN ('accepted','restricted'))`, s.local.ID)
	if err != nil {
		return m, err
	}
	defer rows.Close()
	for rows.Next() {
		var c ModelCapability
		var provider, arch, rev, hash, quant, runtime, rv sql.NullString
		var modalities, admission, restrictionsJSON, runtimeConfig, residency string
		if err := rows.Scan(&c.RemoteDeploymentID, &c.ModelRef, &provider, &arch, &rev, &hash, &quant, &modalities, &runtime, &rv, &c.ContextMax, &admission, &restrictionsJSON, &runtimeConfig, &residency); err != nil {
			return m, err
		}
		if provider.Valid {
			v := provider.String
			c.ProviderName = &v
		}
		if arch.Valid {
			v := arch.String
			c.Architecture = &v
		}
		if rev.Valid {
			v := rev.String
			c.RevisionRef = &v
		}
		if hash.Valid {
			v := hash.String
			c.WeightsHash = &v
		}
		if quant.Valid {
			v := quant.String
			c.Quantization = &v
		}
		if runtime.Valid {
			v := runtime.String
			c.RuntimeName = &v
		}
		if rv.Valid {
			v := rv.String
			c.RuntimeVersion = &v
		}
		c.ModalitiesJSON = json.RawMessage(modalities)
		c.ProtocolLevel = "L0"
		c.Qualification = "untested"
		c.ToolUseAllowed = true
		c.Compute = deploymentCompute(runtimeConfig, residency)
		var restrictions struct {
			MaxContextTokens int64    `json:"max_context_tokens,omitempty"`
			DenyCapabilities []string `json:"deny_capabilities,omitempty"`
			DenyRoles        []string `json:"deny_roles,omitempty"`
			AllowToolUse     *bool    `json:"allow_tool_use,omitempty"`
		}
		if admission == "restricted" && json.Unmarshal([]byte(restrictionsJSON), &restrictions) == nil {
			if restrictions.MaxContextTokens > 0 && (c.ContextMax == 0 || restrictions.MaxContextTokens < c.ContextMax) {
				c.ContextMax = restrictions.MaxContextTokens
			}
			if restrictions.AllowToolUse != nil {
				c.ToolUseAllowed = *restrictions.AllowToolUse
			}
		}

		prows, err := s.db.QueryContext(ctx, `SELECT COALESCE(role_name,''),capability_id,protocol_level,qualification,COALESCE(context_max_verified,context_max_supported,0)
FROM compatibility_profiles WHERE deployment_id=? AND stale_at IS NULL AND qualification IN ('supported','verified','mediated','limited')`, c.RemoteDeploymentID)
		if err != nil {
			return m, err
		}
		for prows.Next() {
			var role, cap, proto, qual string
			var cm int64
			if err := prows.Scan(&role, &cap, &proto, &qual, &cm); err != nil {
				prows.Close()
				return m, err
			}
			c.Capabilities = appendUnique(c.Capabilities, cap)
			if role != "" {
				c.Roles = appendUnique(c.Roles, role)
			}
			if protocolRank(proto) > protocolRank(c.ProtocolLevel) {
				c.ProtocolLevel = proto
			}
			if qualificationRank(qual) > qualificationRank(c.Qualification) {
				c.Qualification = qual
			}
			if cm > 0 && (c.ContextMax == 0 || cm < c.ContextMax) {
				c.ContextMax = cm
			}
		}
		prows.Close()
		if admission == "restricted" && len(restrictions.DenyCapabilities) > 0 {
			filtered := c.Capabilities[:0]
			for _, cap := range c.Capabilities {
				denied := false
				for _, x := range restrictions.DenyCapabilities {
					if strings.EqualFold(strings.TrimSpace(x), strings.TrimSpace(cap)) {
						denied = true
						break
					}
				}
				if !denied {
					filtered = append(filtered, cap)
				}
			}
			c.Capabilities = filtered
		}
		if admission == "restricted" && len(restrictions.DenyRoles) > 0 {
			filtered := c.Roles[:0]
			for _, role := range c.Roles {
				denied := false
				for _, x := range restrictions.DenyRoles {
					if strings.EqualFold(strings.TrimSpace(x), strings.TrimSpace(role)) {
						denied = true
						break
					}
				}
				if !denied {
					filtered = append(filtered, role)
				}
			}
			c.Roles = filtered
		}
		if len(c.Capabilities) == 0 {
			// Do not synthesize a general capability if a restricted admission
			// explicitly denied every empirically qualified capability.
			if admission == "restricted" && len(restrictions.DenyCapabilities) > 0 {
				continue
			}
			c.Capabilities = []string{"inference.general"}
		}
		m.Models = append(m.Models, c)
	}
	var agents int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_runtime_connections WHERE node_id=? AND status IN ('connected','degraded') AND operating_mode<>'unmanaged'`, s.local.ID).Scan(&agents)
	m.AgentCount = agents
	return m, rows.Err()
}

func deploymentCompute(runtimeConfig, residency string) DeploymentCompute {
	var cfg struct {
		RuntimeBackend string            `json:"runtime_backend"`
		Compute        DeploymentCompute `json:"compute"`
		Placement      struct {
			Mode    string `json:"mode"`
			Backend string `json:"backend"`
			Devices []struct {
				Kind           string `json:"kind"`
				DeviceID       string `json:"device_id"`
				DeviceIndex    int    `json:"device_index"`
				AllocatedBytes int64  `json:"allocated_bytes"`
			} `json:"devices"`
		} `json:"placement"`
	}
	_ = json.Unmarshal([]byte(runtimeConfig), &cfg)
	if cfg.Compute.Mode != "" {
		cfg.Compute.Resident = cfg.Compute.Resident || residency == "resident" || residency == "busy"
		return cfg.Compute
	}
	out := DeploymentCompute{Mode: "cpu", Backend: cfg.Placement.Backend, Cached: true, Resident: residency == "resident" || residency == "busy"}
	if out.Backend == "" {
		out.Backend = cfg.RuntimeBackend
	}
	hasCPU, hasGPU := false, false
	for _, d := range cfg.Placement.Devices {
		if d.Kind == "accelerator" {
			hasGPU = true
			id := strings.TrimSpace(d.DeviceID)
			if id == "" {
				id = fmt.Sprintf("gpu-%d", d.DeviceIndex)
			}
			out.DeviceIDs = append(out.DeviceIDs, id)
			out.VRAMBytes += d.AllocatedBytes
		}
		if d.Kind == "cpu" {
			hasCPU = true
			out.RAMBytes += d.AllocatedBytes
		}
	}
	if hasGPU {
		out.Mode = "gpu"
	}
	if hasCPU && hasGPU || cfg.Placement.Mode == "cpu_offload" {
		out.Mode = "hybrid"
	}
	return out
}

func (s *Service) localComputeState(ctx context.Context) NodeComputeState {
	out := NodeComputeState{Status: "available"}
	var cpuJSON, memoryJSON, acceleratorsJSON, storageJSON string
	if err := s.db.QueryRowContext(ctx, `SELECT cpu_json,memory_json,accelerators_json,storage_json FROM local_hardware_profiles WHERE node_id=? ORDER BY detected_at DESC LIMIT 1`, s.local.ID).Scan(&cpuJSON, &memoryJSON, &acceleratorsJSON, &storageJSON); err != nil {
		return out
	}
	var cpu struct {
		Name         string `json:"name"`
		LogicalCores int    `json:"logical_cores"`
	}
	var mem struct {
		TotalBytes     int64 `json:"total_bytes"`
		AvailableBytes int64 `json:"available_bytes"`
	}
	var storage struct {
		AvailableBytes int64 `json:"available_bytes"`
	}
	var gpus []struct {
		Name          string   `json:"name"`
		DeviceID      string   `json:"device_id"`
		DeviceIndex   int      `json:"device_index"`
		Backend       string   `json:"backend"`
		Backends      []string `json:"backends"`
		VRAMBytes     int64    `json:"vram_bytes"`
		FreeVRAMBytes int64    `json:"free_vram_bytes"`
	}
	_ = json.Unmarshal([]byte(cpuJSON), &cpu)
	_ = json.Unmarshal([]byte(memoryJSON), &mem)
	_ = json.Unmarshal([]byte(storageJSON), &storage)
	_ = json.Unmarshal([]byte(acceleratorsJSON), &gpus)
	out.MemoryTotalBytes, out.MemoryAvailableBytes, out.StorageAvailableBytes = mem.TotalBytes, mem.AvailableBytes, storage.AvailableBytes
	out.Pools = append(out.Pools, ComputePool{ID: "cpu", Kind: "cpu", Name: cpu.Name, CapacityBytes: mem.TotalBytes, AvailableBytes: mem.AvailableBytes})
	for _, g := range gpus {
		id := strings.TrimSpace(g.DeviceID)
		if id == "" {
			id = fmt.Sprintf("gpu-%d", g.DeviceIndex)
		}
		backend := g.Backend
		if backend == "" && len(g.Backends) > 0 {
			backend = g.Backends[0]
		}
		out.Pools = append(out.Pools, ComputePool{ID: id, Kind: "gpu", Name: g.Name, Backend: backend, CapacityBytes: g.VRAMBytes, AvailableBytes: g.FreeVRAMBytes})
	}
	var busy int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM local_runtime_instances WHERE node_id=? AND status='busy'`, s.local.ID).Scan(&busy)
	if busy > 0 {
		out.Status = "busy"
	}
	return out
}

func appendUnique(xs []string, v string) []string {
	for _, x := range xs {
		if x == v {
			return xs
		}
	}
	return append(xs, v)
}
func protocolRank(v string) int {
	switch strings.ToUpper(v) {
	case "L3":
		return 3
	case "L2":
		return 2
	case "L1":
		return 1
	default:
		return 0
	}
}
func qualificationRank(v string) int {
	switch v {
	case "verified":
		return 5
	case "supported":
		return 4
	case "mediated":
		return 3
	case "limited":
		return 2
	default:
		return 1
	}
}

func (s *Service) ReceiveHeartbeat(ctx context.Context, peerID string, h Heartbeat) error {
	if h.Protocol != ProtocolVersion || h.NodeID != peerID || h.Manifest.NodeID != peerID || h.Manifest.Protocol != ProtocolVersion {
		return fmt.Errorf("invalid heartbeat")
	}
	now := s.clock.UnixMilli()
	expires := now + s.staleAfter.Milliseconds()
	return s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		var trust string
		if err := tx.QueryRowContext(ctx, `SELECT trust_state FROM harness_nodes WHERE id=?`, peerID).Scan(&trust); err != nil {
			return err
		}
		if trust != "paired" && trust != "unavailable" {
			return ErrNodeNotPaired
		}
		manifestJSON, _ := json.Marshal(h.Manifest)
		_, err := tx.ExecContext(ctx, `INSERT INTO node_capability_manifests(peer_node_id,sequence,manifest_json,received_at,expires_at,revision,updated_at) VALUES(?,?,?,?,?,1,?)
ON CONFLICT(peer_node_id) DO UPDATE SET sequence=excluded.sequence,manifest_json=excluded.manifest_json,received_at=excluded.received_at,expires_at=excluded.expires_at,revision=node_capability_manifests.revision+1,updated_at=excluded.updated_at
WHERE excluded.sequence>=node_capability_manifests.sequence`, peerID, h.Manifest.Sequence, string(manifestJSON), now, expires, now)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE harness_nodes SET trust_state='paired',last_seen_at=?,capabilities_json=?,revision=revision+1,updated_at=? WHERE id=?`, now, string(manifestJSON), now, peerID); err != nil {
			return err
		}
		return s.importManifestTx(ctx, tx, peerID, h.Manifest, now)
	})
}

func (s *Service) importManifestTx(ctx context.Context, tx storage.Tx, peerID string, m CapabilityManifest, now int64) error {
	seen := map[string]bool{}
	for _, c := range m.Models {
		if c.RemoteDeploymentID == "" || c.ModelRef == "" {
			continue
		}
		modelID := stableID("fedmodel", c.ModelRef, deref(c.RevisionRef), deref(c.WeightsHash), deref(c.Quantization))
		var existing string
		err := tx.QueryRowContext(ctx, `SELECT id FROM models WHERE model_ref=? AND COALESCE(revision_ref,'')=? AND COALESCE(weights_hash,'')=? AND COALESCE(quantization,'')=? LIMIT 1`, c.ModelRef, deref(c.RevisionRef), deref(c.WeightsHash), deref(c.Quantization)).Scan(&existing)
		if err == nil {
			modelID = existing
		} else if errorsIsNoRows(err) {
			mods := string(c.ModalitiesJSON)
			if mods == "" || !json.Valid([]byte(mods)) {
				mods = "[]"
			}
			meta, _ := json.Marshal(map[string]any{"federated_from": peerID})
			_, err = tx.ExecContext(ctx, `INSERT INTO models(id,provider_name,model_ref,architecture,revision_ref,weights_hash,quantization,modalities_json,static_metadata_json,trust_state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,'user_trusted',?,?)`, modelID, c.ProviderName, c.ModelRef, c.Architecture, c.RevisionRef, c.WeightsHash, c.Quantization, mods, string(meta), now, now)
			if err != nil {
				return err
			}
		} else {
			return err
		}

		deploymentID := stableID("feddeploy", peerID, c.RemoteDeploymentID)
		seen[deploymentID] = true
		cfg, _ := json.Marshal(map[string]any{"federation": map[string]any{"remote_deployment_id": c.RemoteDeploymentID, "peer_node_id": peerID}, "scheduling": map[string]any{"cost_class": "local", "hard_zero_incremental_cost": true}, "tool_callback": c.ToolUseAllowed, "compute": c.Compute, "node_compute": m.Compute})
		runtime := "remote-node"
		fp := stableID("fp", peerID, c.RemoteDeploymentID, c.ModelRef)
		ctxmax := c.ContextMax
		_, err = tx.ExecContext(ctx, `INSERT INTO model_deployments(id,model_id,node_id,provider_connection_id,runtime_name,runtime_version,runtime_config_json,status,residency_state,context_max_reported,context_max_verified,deployment_fingerprint,revision,discovered_at,updated_at)
VALUES(?,?,?,NULL,?,?,?,'ready','resident',?,?,?,1,?,?)
ON CONFLICT(id) DO UPDATE SET model_id=excluded.model_id,runtime_name=excluded.runtime_name,runtime_version=excluded.runtime_version,runtime_config_json=excluded.runtime_config_json,status='ready',residency_state='resident',context_max_reported=excluded.context_max_reported,context_max_verified=excluded.context_max_verified,deployment_fingerprint=excluded.deployment_fingerprint,revision=model_deployments.revision+1,updated_at=excluded.updated_at`, deploymentID, modelID, peerID, runtime, c.RuntimeVersion, string(cfg), nullableInt(ctxmax), nullableInt(ctxmax), fp, now, now)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM compatibility_profiles WHERE deployment_id=?`, deploymentID); err != nil {
			return err
		}
		caps := c.Capabilities
		if len(caps) == 0 {
			caps = []string{"inference.general"}
		}
		roles := c.Roles
		if len(roles) == 0 {
			roles = []string{""}
		}
		for _, cap := range caps {
			for _, role := range roles {
				pid := stableID("fedprof", deploymentID, cap, role)
				var roleVal any = role
				if role == "" {
					roleVal = nil
				}
				qual := c.Qualification
				if qual == "" {
					qual = "supported"
				}
				proto := c.ProtocolLevel
				if proto == "" {
					proto = "L0"
				}
				profileFP := stableID("pfp", peerID, c.RemoteDeploymentID, cap, role, proto, qual)
				_, err := tx.ExecContext(ctx, `INSERT INTO compatibility_profiles(id,deployment_id,role_name,capability_id,protocol_level,context_min,context_max_verified,context_max_supported,risk_class,qualification,mediation_json,evidence_json,profile_fingerprint,verified_at,revision,created_at,updated_at) VALUES(?,?,?,?,?,0,?,?,'low',?,'{}',?,?,?,1,?,?)`, pid, deploymentID, roleVal, cap, proto, nullableInt(ctxmax), nullableInt(ctxmax), qual, `{"source":"paired_node_manifest"}`, profileFP, now, now, now)
				if err != nil {
					return err
				}
			}
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM model_deployments WHERE node_id=? AND json_extract(runtime_config_json,'$.federation.remote_deployment_id') IS NOT NULL`, peerID)
	if err != nil {
		return err
	}
	var stale []string
	for rows.Next() {
		var idv string
		if err := rows.Scan(&idv); err != nil {
			rows.Close()
			return err
		}
		if !seen[idv] {
			stale = append(stale, idv)
		}
	}
	rows.Close()
	for _, idv := range stale {
		if _, err := tx.ExecContext(ctx, `UPDATE model_deployments SET status='unavailable',residency_state='stopped',revision=revision+1,updated_at=? WHERE id=?`, now, idv); err != nil {
			return err
		}
	}
	return nil
}

func errorsIsNoRows(err error) bool { return err == sql.ErrNoRows }
func nullableInt(v int64) any {
	if v <= 0 {
		return nil
	}
	return v
}
func deref(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func (s *Service) ExpireStale(ctx context.Context) (int, error) {
	now := s.clock.UnixMilli()
	rows, err := s.db.QueryContext(ctx, `SELECT m.peer_node_id FROM node_capability_manifests m JOIN harness_nodes n ON n.id=m.peer_node_id WHERE m.expires_at<=? AND n.trust_state='paired'`, now)
	if err != nil {
		return 0, err
	}
	var peers []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			rows.Close()
			return 0, err
		}
		peers = append(peers, p)
	}
	rows.Close()
	count := 0
	for _, p := range peers {
		err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
			if _, err := tx.ExecContext(ctx, `UPDATE harness_nodes SET trust_state='unavailable',revision=revision+1,updated_at=? WHERE id=? AND trust_state='paired'`, now, p); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, `UPDATE model_deployments SET status='unavailable',residency_state='stopped',revision=revision+1,updated_at=? WHERE node_id=? AND json_extract(runtime_config_json,'$.federation.remote_deployment_id') IS NOT NULL`, now, p)
			return err
		})
		if err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}
