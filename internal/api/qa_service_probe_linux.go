//go:build linux

package api

import "context"

// OnePane's packaged Ubuntu unit is exactly onepane.service. systemctl show
// is a non-mutating query and does not require elevated service privileges.
// Avoid --user, shell pipelines, journal access or any client-supplied unit.
func probeLocalOnePaneService(ctx context.Context)qaOSServiceObservation{
 output,collection:=qaRunServiceCommand(ctx,"/usr/bin/systemctl",
  "show","--no-pager","--property=LoadState,ActiveState,SubState","onepane.service")
 if collection!="observed"{return qaServiceUnavailable("systemd",collection)}
 state,ok:=qaLinuxServiceState(output)
 if !ok{return qaServiceUnavailable("systemd","unavailable")}
 return qaObservedService("systemd",state)
}
