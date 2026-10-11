package sandboxrunner

// GameEngineCapability is a static declaration of the local tools OnePane
// actually supports. Discovery is not an enablement token. Tool Gateway,
// Workspace/Task authority, signed-pinned image and rootless OCI admission
// remain mandatory for any action.
type GameEngineCapability struct{
 EngineID string `json:"engine_id"`
 DisplayName string `json:"display_name"`
 Adapter string `json:"adapter"`
 Status string `json:"status"`
 SupportedActions []string `json:"supported_actions"`
 Requires string `json:"requires"`
}

func GameEngineCapabilities()[]GameEngineCapability{
 return []GameEngineCapability{
  {EngineID:"godot",DisplayName:"Godot 4",Adapter:"governed_oci_cli",
   Status:"implemented_physical_validation_pending",
   SupportedActions:[]string{"import","run"},
   Requires:"Approved immutable Godot 4 OCI image and verified rootless Workspace; publication is separate"},
  {EngineID:"unreal_engine",DisplayName:"Unreal Engine 5.8+",Adapter:"mcp_local_editor",
   Status:"experimental_read_only_probe",
   SupportedActions:[]string{"probe_local_editor"},
   Requires:"Unreal MCP and All Toolsets enabled; editor and probe co-located in same OCI network namespace; no remote MCP exposure"},
  {EngineID:"uefn",DisplayName:"Unreal Editor for Fortnite",Adapter:"mcp_local_editor",
   Status:"planned_not_connected",
   SupportedActions:[]string{},
   Requires:"UEFN-specific Toolset Registry/Verse permissions and separately approved local connector"},
 }
}
