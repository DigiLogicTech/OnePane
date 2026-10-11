package sandboxrunner

// WorkspaceCapabilityFamily describes classes of work OnePane can host when
// the user supplies a compatible immutable OCI toolchain image. This catalog
// does NOT assert software is installed or grant any new permission. The
// authoritative inventory is the observed, scoped project.app.tools.discover
// result; execution uses the existing capability-gated project.app.exec.
type WorkspaceCapabilityFamily struct{
 ID string `json:"id"`
 Name string `json:"name"`
 Examples []string `json:"examples"`
 Execution string `json:"execution"`
 Availability string `json:"availability"`
}

func GeneralWorkspaceCapabilities()[]WorkspaceCapabilityFamily{
 const conditional="requires_approved_installed_toolchain"
 const exec="project.app.exec"
 return []WorkspaceCapabilityFamily{
  {ID:"software_development",Name:"Software development and build systems",
   Examples:[]string{"Python","Node.js","Go","Rust","C/C++","CMake","Java","Kotlin",".NET","Swift","Dart"},
   Execution:exec,Availability:conditional},
  {ID:"web_applications",Name:"Web, APIs, services and testing",
   Examples:[]string{"React","Vite","Next.js","Django","FastAPI","Spring","Playwright"},
   Execution:exec,Availability:conditional},
  {ID:"data_research",Name:"Data science, analytics and research",
   Examples:[]string{"R","Julia","Jupyter","SQLite","DuckDB","NumPy","PyTorch"},
   Execution:exec,Availability:conditional},
  {ID:"creative_media",Name:"Creative, media and game development",
   Examples:[]string{"Godot","Blender","FFmpeg","ImageMagick","Unreal Editor via separately approved MCP"},
   Execution:exec,Availability:conditional},
  {ID:"automation",Name:"Automation, CLI and version control",
   Examples:[]string{"Git","Shell scripts","Make","Task runners","CI-compatible test commands"},
   Execution:exec,Availability:conditional},
  {ID:"infrastructure",Name:"Infrastructure authoring and validation",
   Examples:[]string{"OpenTofu","Terraform","Ansible","Helm","Kubernetes manifests"},
   Execution:exec,Availability:conditional},
  {ID:"documents",Name:"Documents, publishing and content conversion",
   Examples:[]string{"Pandoc","LibreOffice headless","LaTeX","Markdown","EPUB tooling"},
   Execution:exec,Availability:conditional},
  {ID:"mcp_integrations",Name:"Agent-facing MCP integrations",
   Examples:[]string{"Local MCP servers with explicit per-capability grants and Workspace isolation"},
   Execution:"dedicated_authorized_connector_required",Availability:"planned_not_unrestricted"},
 }
}
