package scheduler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/policy"
	"github.com/DigiLogicTech/OnePane/internal/runtimeendpoint"
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
 m.model_ref,m.trust_state,m.static_metadata_json,p.workspace_id,p.provider,p.status,p.auth_type,p.connection_json,
 COALESCE(n.local,0),COALESCE(n.trust_state,''),COALESCE(np.status,''),COALESCE(cm.expires_at,0)
 FROM model_deployments d JOIN models m ON m.id=d.model_id
 LEFT JOIN provider_connections p ON p.id=d.provider_connection_id
 LEFT JOIN harness_nodes n ON n.id=d.node_id
 LEFT JOIN node_pairings np ON np.peer_node_id=n.id
 LEFT JOIN node_capability_manifests cm ON cm.peer_node_id=n.id
 WHERE p.workspace_id IS NULL OR p.workspace_id=?`, ws)
	if err != nil {
		return nil, fmt.Errorf("list model scheduler candidates: %w", err)
	}
	defer rows.Close()
	var out []Candidate
	for rows.Next() {
		var id, status, modelRef, trust, runtimeCfg, metadata string
		var node, providerID, pws, provider, pstatus, pauth, pjson sql.NullString
		var reported, verified sql.NullInt64
		var local int
		var nodeTrust, pairingStatus string
		var manifestExpires int64
		if err := rows.Scan(&id, &status, &node, &providerID, &reported, &verified, &runtimeCfg, &modelRef, &trust, &metadata, &pws, &provider, &pstatus, &pauth, &pjson, &local, &nodeTrust, &pairingStatus, &manifestExpires); err != nil {
			return nil, err
		}
		cand := Candidate{ID: id, Kind: CandidateModel, DisplayName: modelRef, Status: status, TrustState: trust, Local: node.Valid && local == 1 && nodeTrust == "local" && !providerID.Valid, Qualification: QualUntested, ProtocolLevel: "L0", CostClass: CostUnknown, Schedulable: (status == "ready" || status == "degraded") && (trust == "trusted" || trust == "user_trusted"), CapabilityIDs: []string{}, RoleNames: []string{}, ToolCallback: true}
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
		if node.Valid && local == 1 && nodeTrust == "local" && !providerID.Valid {
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
				// The provider's own JSON (including "free", "local", or a
				// hard_zero flag) is not a signed zero-charge contract.
				// Only the explicit monetary-spend/entitlement gates
				// may authorize an opaque provider route. Do not let a
				// missing or misleading declaration consume cloud usage
				// while the operator expects local-only, $0 behaviour.
				switch pc.Scheduling.CostClass {
				case CostPaid, CostProviderManaged:
					cand.CostClass = pc.Scheduling.CostClass
				case CostIncludedSubscription:
					// Only the dedicated first-party plan transport can
					// consume a separately opted-in entitlement. Any
					// generic provider using a fabricated "included" tag
					// must still obtain monetary-spend consent.
					if provider.String == "openai_chatgpt_plan" &&
						pauth.String == "oauth2-pkce" {
						cand.CostClass = CostIncludedSubscription
					} else {
						cand.CostClass = CostUnknown
					}
				default:
					cand.CostClass = CostUnknown
				}
				cand.HardZeroIncrementalCost = false
				cand.AllowedResidency = pc.DataPolicy.AllowedResidency
				// Provider configuration is not cryptographic endpoint proof.
				// A provider-backed deployment must never become an origin/paired
				// Node just because its JSON says "trusted_node"/"origin_node".
				// Preserve an explicit untrusted classification; all other
				// provider-backed destinations stay cloud until verified.
				kind := "cloud"
				if pc.DataPolicy.DestinationKind == "untrusted" {
					kind = "untrusted"
				}
				cand.Destination = destination(kind, ws, "", pc.DataPolicy.MaxConfidentiality)
			}
		 } else if node.Valid && local == 1 && !providerID.Valid {
			// A local flag alone is not an active host trust grant.
			// Revoked/malformed origin-node identity cannot be treated as
			// verified free local compute just because the model is READY.
			cand.Schedulable = false
			cand.Local = false
			cand.TrustedNodeVerified = false
			cand.ComputeMode = "unavailable"
			cand.CostClass = CostUnknown
			cand.HardZeroIncrementalCost = false
			cand.Destination = destination("untrusted", ws, "", "public")
		} else if providerID.Valid {
			// A provider-backed deployment whose provider record disappeared
			// MUST NOT fall back to trusted/zero-cost Node classification.
			// This can occur after legacy imports or a damaged catalogue;
			// a dangling foreign key is never a local-compute grant.
			cand.Schedulable = false
			cand.Local = false
			cand.ComputeMode = "cloud"
			cand.CostClass = CostUnknown
			cand.HardZeroIncrementalCost = false
			cand.Destination = destination("cloud", ws, "", "public")
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
			// A deployment may refine local scheduling metadata, but it cannot
			// downgrade a provider-backed route to "free/local" or claim a
			// zero-cost contract independently of its provider policy.
			if !providerID.Valid && cand.Schedulable && dc.Scheduling.CostClass != "" {
				cand.CostClass = dc.Scheduling.CostClass
				cand.HardZeroIncrementalCost = dc.Scheduling.HardZeroIncrementalCost
			}
			if dc.ToolCallback != nil {
				cand.ToolCallback = *dc.ToolCallback
			}
			// A provider-backed route remains remote/cloud until verified
			// destination attestation exists; model metadata cannot turn it
			// into an apparent CPU or GPU placement on the current host.
			if !providerID.Valid && cand.Schedulable && dc.Compute.Mode != "" {
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
			if !providerID.Valid && cand.Schedulable && dc.Compute.Mode == "" && dc.Placement.Mode != "" {
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
		if node.Valid && local == 0 && !providerID.Valid {
			// A persisted READY deployment cannot authorize a dispatch after
			// its Node's signed/mTLS-paired capability heartbeat has expired.
			// This flag is catalogue-derived, not a model-provided label.
			cand.TrustedNodeVerified = nodeTrust == "paired" &&
				pairingStatus == "paired" && manifestExpires > time.Now().UnixMilli()
			if !cand.TrustedNodeVerified {
				cand.Schedulable = false
				cand.NodeStatus = "unavailable"
				// Until a live pairing and signed capability manifest
				// are verified, the foreign host's advertised "local"
				// cost is not a proven $0 resource. Retain Node ID only
				// for operator diagnostics, never as transfer authority.
				cand.CostClass = CostUnknown
				cand.HardZeroIncrementalCost = false
				cand.Destination = destination("untrusted", ws, "", "public")
			}
			var enabled, idleOnly int
			err := c.db.QueryRowContext(ctx, `SELECT enabled,idle_only FROM remote_node_compute_policies WHERE node_id=?`, node.String).Scan(&enabled, &idleOnly)
			if err == nil {
				if enabled == 0 {
					cand.Schedulable = false
					cand.TrustedNodeVerified = false
					cand.CostClass = CostUnknown
					cand.HardZeroIncrementalCost = false
					cand.Destination = destination("untrusted", ws, "", "public")
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
	rows, err := c.db.QueryContext(ctx, `SELECT r.id,r.workspace_id,r.node_id,r.runtime_kind,r.display_name,r.status,r.trust_state,r.operating_mode,r.protocol_json,r.capabilities_json,r.data_policy_json,r.endpoint_json,r.adapter_name,r.adapter_version,
 COALESCE(n.local,0),COALESCE(n.trust_state,'')
 FROM agent_runtime_connections r LEFT JOIN harness_nodes n ON n.id=r.node_id
 WHERE r.workspace_id IS NULL OR r.workspace_id=?`, ws)
	if err != nil {
		return nil, fmt.Errorf("list runtime scheduler candidates: %w", err)
	}
	defer rows.Close()
	var out []Candidate
	for rows.Next() {
		var id, kind, name, status, trust, mode, protoJSON, capsJSON, dataJSON, endpointJSON,adapterName,adapterVersion,nodeTrust string
		var pws, node sql.NullString
		var nodeLocal int
		if err := rows.Scan(&id, &pws, &node, &kind, &name, &status, &trust, &mode, &protoJSON, &capsJSON, &dataJSON,&endpointJSON,&adapterName,&adapterVersion,&nodeLocal,&nodeTrust); err != nil {
			return nil, err
		}
		var caps runtimeCaps
		_ = json.Unmarshal([]byte(capsJSON), &caps)
		var dp runtimePolicy
		_ = json.Unmarshal([]byte(dataJSON), &dp)
		// The runtime, unlike a verified model deployment, can itself
		// route requests to paid cloud models. Self-reported "local"/"free"
		// costs never establish a zero-cost contract.
		cost := CostUnknown
		switch caps.Scheduling.CostClass {
		case CostPaid,CostProviderManaged:
			// These two are already protected by independent explicit
			// monetary-spend consent. An alleged included subscription is
			// NOT verified: treating it as subscription-only would allow
			// potentially paid use without AllowPotentialMonetarySpend.
			cost=caps.Scheduling.CostClass
		}
		qual := QualUntested
		if len(caps.Capabilities) > 0 || caps.ProtocolLevel != "" {
			qual = QualSupported
		}
		sched := (status == "connected" || status == "degraded") && (trust == "user_trusted" || trust == "trusted_adapter") && mode != "unmanaged"
		// The only supported direct host-origin runtime transport is the
		// built-in HTTP bridge to an exact literal IP loopback address.
		// A node_id or data_policy_json value alone is not endpoint proof.
		localTransport:=node.Valid&&nodeLocal==1&&nodeTrust=="local"&&
			adapterName=="builtin.agent_protocol_http"&&adapterVersion=="1"&&
			runtimeendpoint.LiteralLoopbackHTTP([]byte(endpointJSON))
		kindForFlow:="cloud"
		if dp.DestinationKind=="untrusted"{kindForFlow="untrusted"}
		if dp.DestinationKind=="trusted_node"{
			// Remote Agent Runtime transfer is not implemented as a paired,
			// consent-checked channel; never borrow Node scheduler trust.
			sched=false
		}
		if dp.DestinationKind=="origin_node"{
			if localTransport{kindForFlow="origin_node"}else{sched=false}
		}
		// Local *transport* is not proof the bridge does not use cloud
		// models internally; keep CostUnknown unless independently proven.
		destNode:=""
		if kindForFlow=="origin_node"{destNode=node.String}
		cand := Candidate{ID: id, Kind: CandidateAgentRuntime, DisplayName: name, Provider: kind, Status: status, TrustState: trust, CostClass: cost, HardZeroIncrementalCost: false, Qualification: qual, ProtocolLevel: caps.ProtocolLevel, RoleNames: caps.Roles, CapabilityIDs: caps.Capabilities, ContextMax: caps.ContextMax, Schedulable: sched, ToolCallback: mode == "gateway_mediated", AllowedResidency: dp.AllowedResidency, Destination: destination(kindForFlow, ws, destNode, dp.MaxConfidentiality)}
		if json.Unmarshal([]byte(capsJSON), &caps)!=nil||json.Unmarshal([]byte(dataJSON), &dp)!=nil{cand.Schedulable=false}
		if pws.Valid {
			v := pws.String
			cand.WorkspaceID = &v
		}
		out = append(out, cand)
	}
	return out, rows.Err()
}
