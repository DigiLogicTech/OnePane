//go:build !linux && !windows

package localai

import "errors"

func probeHostMemory() (Memory, error) {
	return Memory{}, errors.New("host memory probe is unavailable")
}
