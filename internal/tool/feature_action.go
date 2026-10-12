package tool

import (
    "strings"
    "github.com/DigiLogicTech/OnePane/internal/authority"
)

// scopedFileAction maps only the file operations represented by the
// feature-policy vocabulary. Other tools retain all existing authority,
// workspace sandbox and operation-coordinator checks.
func scopedFileAction(capabilityID, toolID string) string {
	capabilityID = strings.ToLower(strings.TrimSpace(capabilityID))
	toolID = strings.ToLower(strings.TrimSpace(toolID))
	switch {
	case capabilityID == "filesystem.read", capabilityID == "file.read",
		toolID == "project.app.files.inspect", toolID == "file.read":
		return "files.read"
	case capabilityID == "filesystem.write", capabilityID == "file.write",
		toolID == "project.app.files.edit",toolID == "project.app.files.publish",toolID == "file.write":
		return "files.write"
	default:
		return ""
	}
}

// scopedToolActions declares all Workspace capability classes a registered
// operation can exercise. A false classification denies execution when an
// explicit Project/Workspace feature policy is active. This policy is
// independent of the legacy authority lease and actual sandbox confinement.
//
// A generic command interpreter must be treated as capable of both file
// reads and writes: the command line is not a safe security boundary.
func scopedToolActions(capabilityID, toolID string, mode authority.ActionMode)([]string,bool){
  cap:=strings.ToLower(strings.TrimSpace(capabilityID))
  id:=strings.ToLower(strings.TrimSpace(toolID))
  switch id{
  case "project.app.files.inspect":return []string{"files.read"},true
  case "project.app.files.edit":return []string{"files.read","files.write"},true
  case "project.app.files.publish":return []string{"files.read","files.write"},true
  case "project.app.git.inspect":return []string{"files.read","process.execute"},true
  case "project.app.git.mutate":return []string{"files.read","files.write","process.execute"},true
  case "project.app.exec","project.app.godot.build":
    return []string{"files.read","files.write","process.execute"},true
  case "project.app.tools.discover","project.app.toolchain.preflight":
    return []string{"files.read","process.execute"},true
  case "project.app.unreal.mcp.probe":
    return []string{"process.execute","network.connect"},true
  case "project.runtime.inspect","project.app.inspect","project.image.inspect":
    return []string{},true
  case "project.runtime.ensure","project.runtime.stop","project.app.ensure","project.app.stop":
    return []string{"process.execute"},true
  case "project.app.pull":
    // PullImage mutates the local OCI image store. Network + Execute
    // alone must not bypass an explicitly read-only Workspace.
    return []string{"files.write","process.execute","network.connect"},true
  }
  switch cap {
  case "filesystem.read","file.read":return []string{"files.read"},true
  case "filesystem.write","file.write":return []string{"files.read","files.write"},true
  case "network.send","network.external","http.send":return []string{"network.connect"},true
  case "secrets.use","vault.read","vault.use":return []string{"secrets.use"},true
  }
  switch mode {
  case authority.ActionExecuteSandboxed:
    // Only the authorised sandbox adapter can bound arbitrary side effects.
    return []string{"files.read","files.write","process.execute"},true
  case authority.ActionExternalSend:
    return []string{"network.connect"},true
  }
  return nil,false
}
