package sandboxrunner

// VerifyNodeLocalServiceSource verifies that an observed rootless OCI container
// belongs to the exact managed Workspace. Unlike a generic isolation flag,
// this checks the only writable host mount against OnePane's canonical path.
//
// This is intended for a trusted local broker after a fresh InspectContainer,
// never as a substitute for that inspection or an authorization decision.
func VerifyNodeLocalServiceSource(dataRoot, runtimeID, applicationID string, observed ContainerState) bool {
 if dataRoot=="" || runtimeID=="" || applicationID=="" ||
  observed.Status!="running"||!observed.IsolationVerified||
  !observed.ReadOnlyRootFS||!observed.NetworkInternal||
  observed.ID==""||observed.SpecHash==""||
  observed.RuntimeID!=runtimeID||observed.ApplicationID!=applicationID{
  return false
 }
 workspace,exists,err:=managedWorkspacePath(dataRoot,runtimeID,false)
 return err==nil&&exists&&exactWorkspaceMount(observed,workspace)
}
