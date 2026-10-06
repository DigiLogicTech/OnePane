package scheduler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/policy"
)

type Catalog struct{ db *sql.DB }

func NewCatalog(db *sql.DB) *Catalog { return &Catalog{db: db} }

func (c *Catalog) Candidates(ctx context.Context, workspaceID, capabilityID, roleName string) ([]Candidate, error) {
	models, err := c.modelCandidates(ctx, workspaceID, capabilityID, roleName)
	if err != nil {
		return nil, err
	}
	runtimes, err := c.runtimeCandidates(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	return append(models, runtimes...), nil
}

type schedulingMeta struct {
	CostClass               CostClass `json:"cost_class"`
	HardZeroIncrementalCost bool      `json:"hard_zero_incremental_cost"`
}
type providerCfg struct {
	DataPolicy struct {
		MaxConfidentiality string   `json:"max_confidentiality"`
		AllowedResidency   []string `json:"allowed_residency"`
		DestinationKind    string   `json:"destination_kind"`
	} `json:"data_policy"`
	Scheduling schedulingMeta `json:"scheduling"`
}
type runtimeCaps struct {
	Roles         []string       `json:"roles"`
	Capabilities  []string       `json:"capabilities"`
	ProtocolLevel string         `json:"protocol_level"`
	ContextMax    int64          `json:"context_max"`
	Scheduling    schedulingMeta `json:"scheduling"`
}
type runtimePolicy struct {
	MaxConfidentiality string   `json:"max_confidentiality"`
	AllowedResidency   []string `json:"allowed_residency"`
	DestinationKind    string   `json:"destination_kind"`
}

func clearance(v string) policy.Confidentiality {
	switch strings.ToLower(v) {
	case "internal":
		return policy.ConfidentialityInternal
	case "confidential":
		return policy.ConfidentialityConfidential
	case "secret":
		return policy.ConfidentialitySecret
	default:
		return policy.ConfidentialityPublic
	}
}
func destination(kind, workspace, node string, max string) policy.FlowDestination {
	d := policy.FlowDestination{WorkspaceID: workspace, Clearance: clearance(max)}
	switch kind {
	case "origin_node":
		d.Kind = policy.DestinationOriginNode
		d.NodeID = node
	case "trusted_node":
		d.Kind = policy.DestinationTrustedNode
		d.NodeID = node
	case "untrusted":
		d.Kind = policy.DestinationUntrusted
	default:
		d.Kind = policy.DestinationCloud
	}
	return d
}

type modelAdmissionRestrictions struct {
	MaxContextTokens int64    `json:"max_context_tokens,omitempty"`
	DenyCapabilities []string `json:"deny_capabilities,omitempty"`
	DenyRoles        []string `json:"deny_roles,omitempty"`
	AllowToolUse     *bool    `json:"allow_tool_use,omitempty"`
}

func (c *Catalog) applyManagedAdmission(ctx context.Context, deploymentID, capability, role string, cand *Candidate) error {
	var managed int
	if err := c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM managed_local_models WHERE deployment_id=?`, deploymentID).Scan(&managed); err != nil {
		return err
	}
	if managed == 0 {
		return nil
	}
	var status, raw string
	err := c.db.QueryRowContext(ctx, `SELECT admission_status,restrictions_json FROM model_spec_sheets WHERE deployment_id=?`, deploymentID).Scan(&status, &raw)
	if err == sql.ErrNoRows {
		cand.Schedulable = false
		return nil
	}
	if err != nil {
		return err
	}
	if status != "accepted" && status != "restricted" {
		cand.Schedulable = false
		return nil
	}
	if status == "restricted" {
		var r modelAdmissionRestrictions
		if json.Unmarshal([]byte(raw), &r) != nil {
			cand.Schedulable = false
			return nil
		}
		for _, denied := range r.DenyCapabilities {
			if strings.EqualFold(strings.TrimSpace(denied), strings.TrimSpace(capability)) {
				cand.Schedulable = false
				return nil
			}
		}
		for _, denied := range r.DenyRoles {
			if strings.EqualFold(strings.TrimSpace(denied), strings.TrimSpace(role)) {
				cand.Schedulable = false
				return nil
			}
		}
		if r.MaxContextTokens > 0 && (cand.ContextMax == 0 || r.MaxContextTokens < cand.ContextMax) {
			cand.ContextMax = r.MaxContextTokens
		}
		if r.AllowToolUse != nil && !*r.AllowToolUse {
			cand.ToolCallback = false
		}
	}
	return nil
}

func (c *Catalog) modelCandidates(ctx context.Context, ws, capability, role string) ([]Candidate, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT d.id,d.status,d.node_id,d.provider_connection_id,d.context_max_reported,d.context_max_verified,d.runtime_config_json,
 m.model_ref,m.trust_state,m.static_metadata_json,p.workspace_id,p.provider,p.status,p.connection_json,
 COALESCE(n.local,0),COALESCE(n.trust_state,'')
 FROM model_deployments d JOIN models m ON m.id=d.model_id
 LEFT JOIN provider_connections p ON p.id=d.provider_connection_id
 LEFT JOIN harness_nodes n ON n.id=d.node_id
 WHERE p.workspace_id IS NULL OR p.workspace_id=?`, ws)
	if err != nil {
		return nil, fmt.Errorf("list model scheduler candidates: %w", err)
	}
	defer rows.Close()
	var out []Candidate
	for rows.Next() {
		var id, status, modelRef, trust, runtimeCfg, metadata string
		var node, providerID, pws, provider, pstatus, pjson sql.NullString
		var reported, verified sql.NullInt64
		var local int
		var nodeTrust string
		if err := rows.Scan(&id, &status, &node, &providerID, &reported, &verified, &runtimeCfg, &modelRef, &trust, &metadata, &pws, &provider, &pstatus, &pjson, &local, &nodeTrust); err != nil {
			return nil, err
		}
		cand := Candidate{ID: id, Kind: CandidateModel, DisplayName: modelRef, Status: status, TrustState: trust, Local: local == 1, Qualification: QualUntested, ProtocolLevel: "L0", CostClass: CostUnknown, Schedulable: (status == "ready" || status == "degraded") && (trust == "trusted" || trust == "user_trusted"), CapabilityIDs: []string{}, RoleNames: []string{}, ToolCallback: true}
		if node.Valid {
			cand.NodeID = node.String
		}
		if verified.Valid {
			cand.ContextMax = verified.Int64
		} else if reported.Valid {
			cand.ContextMax = reported.Int64
		}
		if pws.Valid {
			v := pws.String
			cand.WorkspaceID = &v
		}
		if local == 1 {
			cand.ComputeMode = "cpu"
			cand.CostClass = CostLocal
			cand.HardZeroIncrementalCost = true
			cand.AllowedResidency = []string{"any", "trusted_nodes", "origin_node"}
			cand.Destination = destination("origin_node", ws, node.String, "secret")
		} else if provider.Valid {
			cand.ComputeMode = "cloud"
			cand.Provider = provider.String
			if pstatus.String != "connected" && pstatus.String != "degraded" {
				cand.Schedulable = false
			}
			var pc providerCfg
			if err := json.Unmarshal([]byte(pjson.String), &pc); err != nil {
				cand.Schedulable = false
			} else {
				cand.CostClass = pc.Scheduling.CostClass
				if cand.CostClass == "" {
					cand.CostClass = CostUnknown
				}
				cand.HardZeroIncrementalCost = pc.Scheduling.HardZeroIncrementalCost
				cand.AllowedResidency = pc.DataPolicy.AllowedResidency
				cand.Destination = destination(pc.DataPolicy.DestinationKind, ws, node.String, pc.DataPolicy.MaxConfidentiality)
			}
		} else if node.Valid {
			cand.ComputeMode = "remote"
			cand.CostClass = CostLocal
			cand.HardZeroIncrementalCost = true
			cand.AllowedResidency = []string{"any", "trusted_nodes"}
			kind := "trusted_node"
			if nodeTrust != "local" && nodeTrust != "paired" {
				kind = "untrusted"
			}
			cand.Destination = destination(kind, ws, node.String, "secret")
		} else {
			cand.Schedulable = false
		}
		// Deployment metadata can safely refine scheduling cost classification.
		var dc struct {
			Scheduling   schedulingMeta `json:"scheduling"`
			ToolCallback *bool          `json:"tool_callback,omitempty"`
			Compute      struct {
				Mode     string `json:"mode"`
				Backend  string `json:"backend"`
				Cached   bool   `json:"cached"`
				Resident bool   `json:"resident"`
			} `json:"compute"`
			NodeCompute struct {
				Status      string  `json:"status"`
				CPUUsagePct float64 `json:"cpu_usage_pct"`
			} `json:"node_compute"`
			RuntimeBackend string `json:"runtime_backend"`
			Placement      struct {
				Mode    string `json:"mode"`
				Backend string `json:"backend"`
				Devices []struct {
					Kind string `json:"kind"`
				} `json:"devices"`
			} `json:"placement"`
		}
		if json.Unmarshal([]byte(runtimeCfg), &dc) == nil {
			if dc.Scheduling.CostClass != "" {
				cand.CostClass = dc.Scheduling.CostClass
				cand.HardZeroIncrementalCost = dc.Scheduling.HardZeroIncrementalCost
			}
			if dc.ToolCallback != nil {
				cand.ToolCallback = *dc.ToolCallback
			}
			if dc.Compute.Mode != "" {
				cand.ComputeMode = dc.Compute.Mode
			}
			if dc.Compute.Backend != "" {
				cand.RuntimeBackend = dc.Compute.Backend
			}
			cand.Cached, cand.Resident = dc.Compute.Cached, dc.Compute.Resident
			cand.NodeStatus, cand.NodeLoadPct = dc.NodeCompute.Status, dc.NodeCompute.CPUUsagePct
			if cand.RuntimeBackend == "" {
				cand.RuntimeBackend = firstNonBlank(dc.Placement.Backend, dc.RuntimeBackend)
			}
			if dc.Compute.Mode == "" && dc.Placement.Mode != "" {
				hasCPU, hasGPU := false, false
				for _, dev := range dc.Placement.Devices {
					if dev.Kind == "cpu" {
						hasCPU = true
					}
					if dev.Kind == "accelerator" {
						hasGPU = true
					}
				}
				if hasGPU {
					cand.ComputeMode = "gpu"
				}
				if hasCPU && hasGPU || dc.Placement.Mode == "cpu_offload" {
					cand.ComputeMode = "hybrid"
				}
				if hasCPU && !hasGPU {
					cand.ComputeMode = "cpu"
				}
			}
		}
		prof, ok, err := c.bestProfile(ctx, id, capability, role)
		if err != nil {
			return nil, err
		}
		if ok {
			cand.Qualification = prof.q
			cand.ProtocolLevel = prof.protocol
			cand.CapabilityIDs = []string{capability}
			if prof.role != "" {
				cand.RoleNames = []string{prof.role}
			}
			if prof.contextMax > 0 {
				cand.ContextMax = prof.contextMax
			}
		}
		if err := c.applyManagedAdmission(ctx, id, capability, role, &cand); err != nil {
			return nil, err
		}
		if node.Valid && local == 0 && !provider.Valid {
			var enabled, idleOnly int
			err := c.db.QueryRowContext(ctx, `SELECT enabled,idle_only FROM remote_node_compute_policies WHERE node_id=?`, node.String).Scan(&enabled, &idleOnly)
			if err == nil {
				if enabled == 0 {
					cand.Schedulable = false
				}
				if idleOnly != 0 && strings.EqualFold(cand.NodeStatus, "busy") {
					cand.Schedulable = false
				}
			} else if err != sql.ErrNoRows {
				return nil, err
			}
		}
		out = append(out, cand)
	}
	return out, rows.Err()
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

type profile struct {
	q              Qualification
	protocol, role string
	contextMax     int64
}

func (c *Catalog) bestProfile(ctx context.Context, deployment, capability, role string) (profile, bool, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT qualification,protocol_level,COALESCE(role_name,''),COALESCE(context_max_verified,context_max_supported,0) FROM compatibility_profiles WHERE deployment_id=? AND capability_id=? AND (role_name IS NULL OR role_name=?)`, deployment, capability, role)
	if err != nil {
		return profile{}, false, err
	}
	defer rows.Close()
	var best profile
	found := false
	for rows.Next() {
		var q, proto, r string
		var max int64
		if err := rows.Scan(&q, &proto, &r, &max); err != nil {
			return profile{}, false, err
		}
		p := profile{q: Qualification(q), protocol: proto, role: r, contextMax: max}
		if !found || qualScore(p.q) > qualScore(best.q) {
			best = p
			found = true
		}
	}
	return best, found, rows.Err()
}
func qualScore(q Qualification) int { return qualRank(q) }

func (c *Catalog) runtimeCandidates(ctx context.Context, ws string) ([]Candidate, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT id,workspace_id,node_id,runtime_kind,display_name,status,trust_state,operating_mode,protocol_json,capabilities_json,data_policy_json FROM agent_runtime_connections WHERE workspace_id IS NULL OR workspace_id=?`, ws)
	if err != nil {
		return nil, fmt.Errorf("list runtime scheduler candidates: %w", err)
	}
	defer rows.Close()
	var out []Candidate
	for rows.Next() {
		var id, kind, name, status, trust, mode, protoJSON, capsJSON, dataJSON string
		var pws, node sql.NullString
		if err := rows.Scan(&id, &pws, &node, &kind, &name, &status, &trust, &mode, &protoJSON, &capsJSON, &dataJSON); err != nil {
			return nil, err
		}
		var caps runtimeCaps
		_ = json.Unmarshal([]byte(capsJSON), &caps)
		var dp runtimePolicy
		_ = json.Unmarshal([]byte(dataJSON), &dp)
		cost := caps.Scheduling.CostClass
		if cost == "" {
			cost = CostUnknown
		}
		qual := QualUntested
		if len(caps.Capabilities) > 0 || caps.ProtocolLevel != "" {
			qual = QualSupported
		}
		sched := (status == "connected" || status == "degraded") && (trust == "user_trusted" || trust == "trusted_adapter") && mode != "unmanaged"
		cand := Candidate{ID: id, Kind: CandidateAgentRuntime, DisplayName: name, Provider: kind, Status: status, TrustState: trust, CostClass: cost, HardZeroIncrementalCost: caps.Scheduling.HardZeroIncrementalCost, Qualification: qual, ProtocolLevel: caps.ProtocolLevel, RoleNames: caps.Roles, CapabilityIDs: caps.Capabilities, ContextMax: caps.ContextMax, Schedulable: sched, ToolCallback: mode == "gateway_mediated", AllowedResidency: dp.AllowedResidency, Destination: destination(dp.DestinationKind, ws, node.String, dp.MaxConfidentiality)}
		if pws.Valid {
			v := pws.String
			cand.WorkspaceID = &v
		}
		out = append(out, cand)
	}
	return out, rows.Err()
}
