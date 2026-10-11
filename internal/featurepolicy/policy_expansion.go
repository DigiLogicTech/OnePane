package featurepolicy

import "fmt"

// FeaturePrivilegeExpands checks whether a proposed Project/Workspace policy
// increases any effective capability relative to its currently stored policy.
// It is used at the durable policy-edit boundary: an Agent may restrict a
// grant, but only a human can issue a new permission to autonomous execution.
//
// Only trusted, persisted policy JSON is compared. Model-authored Task
// metadata and unverified policy envelopes can never mint authority.
func FeaturePrivilegeExpands(before,after []byte)(bool,error){
 old,_,err:=parseFeatureDocument(before,AssistedDefaults())
 if err!=nil{return false,fmt.Errorf("previous policy invalid: %w",err)}
 next,_,err:=parseFeatureDocument(after,AssistedDefaults())
 if err!=nil{return false,fmt.Errorf("proposed policy invalid: %w",err)}
 for _,action:=range []string{
  "inference.local","inference.cloud","inference.fallback",
  "files.read","files.write","process.execute","network.connect",
  "secrets.use","projects.share","tasks.autonomous",
 }{
  if !old.ValidateExecution(action)&&next.ValidateExecution(action){
   return true,nil
  }
 }
 for _,pair:=range [][2]State{
  {old.Assistant,next.Assistant},{old.ProjectAI,next.ProjectAI},
  {old.ProjectMemory,next.ProjectMemory},{old.TelemetryAccess,next.TelemetryAccess},
  {old.TelemetryAutonomy,next.TelemetryAutonomy},{old.ModelRouting,next.ModelRouting},
  {old.AutomaticFallback,next.AutomaticFallback},
  {old.CapabilityRouting,next.CapabilityRouting},{old.CloudRouting,next.CloudRouting},
  {old.Images,next.Images},{old.Files,next.Files},
  {old.FileRead,next.FileRead},{old.FileWrite,next.FileWrite},
  {old.ProcessExecute,next.ProcessExecute},{old.NetworkAccess,next.NetworkAccess},
  {old.SecretsAccess,next.SecretsAccess},{old.AudioInput,next.AudioInput},
  {old.AudioOutput,next.AudioOutput},{old.Camera,next.Camera},
  {old.Video,next.Video},{old.Team,next.Team},{old.Council,next.Council},
  {old.CrossProjectShare,next.CrossProjectShare},
  {old.AutonomousRoutines,next.AutonomousRoutines},
 }{
  if pair[1]==Enabled&&pair[0]!=Enabled{
   return true,nil
  }
 }
 rank:=func(a Authority)int{switch a{
 case AuthorityNever:return 0
 case AuthorityRead:return 1
 case AuthorityPropose:return 2
 case AuthorityAuto:return 3
 default:return -1
 }}
 return rank(next.ConfigAuthority)>rank(old.ConfigAuthority),nil
}
