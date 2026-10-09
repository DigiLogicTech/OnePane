//go:build !windows && !linux

package api

import "context"

// No supported system service manager available on this platform.
func probeLocalOnePaneService(context.Context)qaOSServiceObservation{
 return qaServiceUnavailable("not_collected","not_collected")
}
