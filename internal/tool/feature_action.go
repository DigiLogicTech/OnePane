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
  // The built-in Project tool IDs are trusted only with their exact
  // registered authority mode. A plugin cannot use a familiar read-only
  // ID but execute with sandbox/mutate/send semantics instead.
  switch id{
  case "project.runtime.inspect","project.app.inspect","project.image.inspect":
    if mode!=authority.ActionObserve{return nil,false}
  case "project.app.files.inspect","project.app.files.edit",
    "project.app.files.publish","project.app.git.inspect",
    "project.app.git.mutate","project.app.exec","project.app.godot.build",
    "project.app.tools.discover","project.app.toolchain.preflight",
    "project.app.unreal.mcp.probe":
    if mode!=authority.ActionExecuteSandboxed{return nil,false}
  case "project.runtime.ensure","project.runtime.stop","project.app.ensure",
    "project.app.stop","project.app.pull":
    if mode!=authority.ActionMutate{return nil,false}
  case "project.goal.tcp_observer.pause","project.goal.tcp_observer.resume":
    if mode!=authority.ActionMutate{return nil,false}
  }
  if (mode==authority.ActionMutate||mode==authority.ActionExternalSend)&&
    (cap=="filesystem.read"||cap=="file.read"){
    return nil,false
  }
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
  case "project.goal.tcp_observer.pause","project.goal.tcp_observer.resume":
    // Resume may re-enable separately approved network observations;
    // require the explicit Workspace network-connect policy in addition to
    // the human-owned Operation permit, not a generic custom Tool default.
    return []string{"network.connect"},true
  case "project.app.pull":
    // PullImage mutates the local OCI image store. Network + Execute
    // alone must not bypass an explicitly read-only Workspace.
    return []string{"files.write","process.execute","network.connect"},true
  }
  // Never let a new Project-scoped tool borrow the generic capability
  // vocabulary without an explicit reviewed effect manifest. These names
  // carry privileged canonical Project/Workspace meaning and are reserved.
  if strings.HasPrefix(id,"project.app.")||
    strings.HasPrefix(id,"project.runtime.")||
    strings.HasPrefix(id,"project.image."){
    return nil,false
  }
  // Unknown adapter identities must be classified by their *actual
  // execution mode first*. A custom sandbox command declaring
  // filesystem.read may still write/execute; a network-sending adapter
  // declaring vault.read may still exfiltrate its secret.
  // Only the explicit, audited tool IDs above have narrowed effects.
  switch mode {
  case authority.ActionExecuteSandboxed:
    actions:=[]string{"files.read","files.write","process.execute"}
    switch cap{
    case "secrets.use","vault.read","vault.use":
      actions=append(actions,"secrets.use")
    case "network.send","network.external","http.send":
      actions=append(actions,"network.connect")
    }
    return actions,true
  case authority.ActionExternalSend:
    actions:=[]string{"network.connect"}
    if cap=="secrets.use"||cap=="vault.read"||cap=="vault.use"{
      actions=append(actions,"secrets.use")
    }
    return actions,true
  }
  switch cap {
  case "filesystem.read","file.read":return []string{"files.read"},true
  case "filesystem.write","file.write":return []string{"files.read","files.write"},true
  case "network.send","network.external","http.send":return []string{"network.connect"},true
  case "secrets.use","vault.read","vault.use":return []string{"secrets.use"},true
  }
  return nil,false
}
