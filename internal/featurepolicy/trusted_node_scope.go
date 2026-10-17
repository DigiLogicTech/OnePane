package featurepolicy

import (
    "bytes"
    "context"
    "database/sql"
    "encoding/json"
    "fmt"
    "io"
    "regexp"
    "sort"
    "strings"

    "github.com/DigiLogicTech/OnePane/internal/observation"
)

// Trusted Node execution requires independently persisted, explicit grants
// in BOTH the Project policy and its canonical Workspace settings. A Task,
// model response or saved Worker route policy cannot create these grants.
//
// Document fragment in both sources:
// {"onepane_routing":{"trusted_node_compute":{
//    "enabled":true,"allowed_node_ids":["paired-node-id"]}}}
type trustedNodeComputeGrant struct {
    Enabled bool `json:"enabled"`
    AllowedNodeIDs []string `json:"allowed_node_ids"`
}

var validTrustedNodeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)

func decodeTrustedNodeGrant(raw string) (trustedNodeComputeGrant, error) {
    // A dual-consent document must have exactly one interpretation.
    // encoding/json silently accepts duplicate (including Unicode-alias)
    // keys. Validate the ENTIRE stored document before extracting grants.
    if err:=observation.ValidateUnambiguousJSON(json.RawMessage(raw));err!=nil{
        return trustedNodeComputeGrant{},fmt.Errorf("ambiguous stored Node transfer policy")
    }
    var doc map[string]json.RawMessage
    if err := json.Unmarshal([]byte(raw), &doc); err != nil || doc == nil {
        return trustedNodeComputeGrant{}, fmt.Errorf("invalid stored policy document")
    }
    routingRaw, ok := doc["onepane_routing"]
    if !ok { return trustedNodeComputeGrant{}, nil }
    var routing map[string]json.RawMessage
    if err := json.Unmarshal(routingRaw, &routing); err != nil || routing == nil {
        return trustedNodeComputeGrant{}, fmt.Errorf("invalid trusted-node policy envelope")
    }
    grantRaw, ok := routing["trusted_node_compute"]
    if !ok { return trustedNodeComputeGrant{}, nil }
    d := json.NewDecoder(bytes.NewReader(grantRaw))
    d.DisallowUnknownFields()
    var grant trustedNodeComputeGrant
    if err := d.Decode(&grant); err != nil {
        return trustedNodeComputeGrant{}, fmt.Errorf("invalid trusted-node grant: %w", err)
    }
    if err := d.Decode(new(any)); err != io.EOF {
        return trustedNodeComputeGrant{}, fmt.Errorf("trailing trusted-node grant data")
    }
    if !grant.Enabled {
        return trustedNodeComputeGrant{}, nil
    }
    if len(grant.AllowedNodeIDs) == 0 || len(grant.AllowedNodeIDs) > 32 {
        return trustedNodeComputeGrant{}, fmt.Errorf("trusted-node grant must have 1..32 exact Node IDs")
    }
    seen := map[string]bool{}
    for _, id := range grant.AllowedNodeIDs {
        if !validTrustedNodeID.MatchString(id) || seen[id] {
            return trustedNodeComputeGrant{}, fmt.Errorf("trusted-node grant has invalid or repeated Node ID")
        }
        seen[id] = true
    }
    return grant, nil
}

// TrustedNodesForTask is a read-only lookup against real persisted ownership.
// If any source grant is missing, there are no transferable destinations.
// Malformed grants fail closed rather than silently widening access.
func (s StoredScopes) TrustedNodesForTask(ctx context.Context, tenantID, taskID string) ([]string, error) {
    if s.DB == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(taskID) == "" {
        return nil, fmt.Errorf("trusted-node resolver unavailable")
    }
    var projectJSON, workspaceJSON string
    err := s.DB.QueryRowContext(ctx, `SELECT p.project_policy_json,w.ai_settings_json
      FROM tasks t
      JOIN workspaces root ON root.id=t.workspace_id AND root.status='active'
      JOIN projects p ON p.id=t.project_id AND p.workspace_id=t.workspace_id AND p.status='active'
      JOIN project_workspaces w ON w.id=t.project_workspace_id AND w.project_id=p.id AND w.status='active'
      WHERE t.id=? AND t.workspace_id=?`, taskID,tenantID).Scan(&projectJSON,&workspaceJSON)
    if err == sql.ErrNoRows { return nil, nil }
    if err != nil { return nil, fmt.Errorf("resolve canonical trusted-node grant: %w",err) }
    projectGrant,err := decodeTrustedNodeGrant(projectJSON)
    if err != nil { return nil, err }
    workspaceGrant,err := decodeTrustedNodeGrant(workspaceJSON)
    if err != nil { return nil, err }
    if !projectGrant.Enabled || !workspaceGrant.Enabled { return nil,nil }
    permitted := map[string]bool{}
    for _,id := range projectGrant.AllowedNodeIDs { permitted[id]=true }
    var out []string
    for _,id := range workspaceGrant.AllowedNodeIDs {
        if permitted[id] { out=append(out,id) }
    }
    sort.Strings(out)
    return out,nil
}

func (s StoredScopes) AuthorizeNodeTransfer(ctx context.Context,tenantID,taskID,nodeID string)(bool,error){
    if !validTrustedNodeID.MatchString(nodeID) { return false,nil }
    nodes,err:=s.TrustedNodesForTask(ctx,tenantID,taskID)
    if err!=nil{return false,err}
    for _,id:=range nodes { if id==nodeID{return true,nil} }
    return false,nil
}

// TrustedNodeGrantExpands determines whether a proposed persisted
// Project/Workspace policy adds any Node transfer authority. Agents can
// narrow grants through normal scoped edits, but cannot self-approve an
// expanded off-host transfer capability.
func TrustedNodeGrantExpands(before, after []byte)(bool,error){
    oldGrant,err:=decodeTrustedNodeGrant(string(before))
    if err!=nil{return false,err}
    newGrant,err:=decodeTrustedNodeGrant(string(after))
    if err!=nil{return false,err}
    if !newGrant.Enabled{return false,nil}
    if !oldGrant.Enabled{return true,nil}
    oldIDs:=map[string]bool{}
    for _,id:=range oldGrant.AllowedNodeIDs{oldIDs[id]=true}
    for _,id:=range newGrant.AllowedNodeIDs{
        if !oldIDs[id]{return true,nil}
    }
    return false,nil
}
