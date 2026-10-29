//go:build !windows

package installroots

import "fmt"

// ProbeVolume is unavailable outside Windows. Tests pass VolumeFact directly.
func ProbeVolume(string) (VolumeFact, error) {
	return VolumeFact{}, fmt.Errorf("volume probe requires Windows")
}
