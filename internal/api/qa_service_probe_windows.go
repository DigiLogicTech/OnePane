//go:build windows

package api

import (
 "context"
 "os"
 "path/filepath"
)

// Windows packaging installs the OnePane service; never inspect a service
// supplied by the user. Use the System32 executable directly without shell
// expansion or a PATH lookup. If SystemRoot is absent, fail closed.
func probeLocalOnePaneService(ctx context.Context)qaOSServiceObservation{
 root:=os.Getenv("SystemRoot")
 if !filepath.IsAbs(root){return qaServiceUnavailable("windows_scm","unavailable")}
 output,collection:=qaRunServiceCommand(ctx,filepath.Join(root,"System32","sc.exe"),"query","OnePane")
 if collection!="observed"{return qaServiceUnavailable("windows_scm",collection)}
 state,ok:=qaWindowsServiceState(output)
 if !ok{return qaServiceUnavailable("windows_scm","unavailable")}
 return qaObservedService("windows_scm",state)
}
