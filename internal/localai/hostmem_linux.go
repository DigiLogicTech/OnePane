package localai

import "errors"

func probeHostMemory() (Memory, error) {
	m := readMemory()
	if m.TotalBytes <= 0 || m.AvailableBytes < 0 {
		return Memory{}, errors.New("host memory is unavailable")
	}
	return m, nil
}
