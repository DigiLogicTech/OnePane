package sandboxrunner

import (
 "fmt"
)

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
