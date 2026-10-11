package sandboxrunner

import (
 "fmt"
 "encoding/json"
)

// Strictly restrict this Tool's input object, rather than relying only
// on Go JSON struct decoding (which otherwise silently ignores unknown keys).
// This prevents operators or agents from assuming a supplied host path,
// environment, image, mount or shell command was actually honoured.
func validGodotEnvelope(raw json.RawMessage)bool{
 var m map[string]json.RawMessage
 if err:=json.Unmarshal(raw,&m);err!=nil||len(m)<3||len(m)>5{return false}
 for k:=range m{
  switch k {
  case "runtime_id","application_id","action","timeout_seconds","required_executables":
  default:return false
  }
 }
 for _,required:=range []string{"runtime_id","application_id","action"}{
  if _,ok:=m[required];!ok{return false}
 }
 return true
}

// godotBuildCommand is a fixed, rootless OCI toolchain command. The caller
// chooses only a documented action, never an executable, project path,
// arguments, environment, shell, destination or plugin. The sandbox adapter
// independently verifies the registered live OCI Workspace before invoking.
func godotBuildCommand(action string)([]string,error){
 switch action{
 case "import":
  return []string{"godot","--headless","--path","/workspace","--editor","--import"},nil
 case "run":
  // Run the project's configured main scene, bounded inside a rootless OCI
  // process. Exit code reflects actual scene/import errors, not a guarantee
  // about correctness of the produced content.
  return []string{"godot","--headless","--path","/workspace","--quit-after","60"},nil
 default:
  return nil,fmt.Errorf("%w: unsupported Godot build action",ErrInvalidInput)
 }
}
