//go:build windows

package installroots

import (
	"fmt"
	"strings"

	"golang.org/x/sys/windows"
)

// ProbeVolume reads filesystem and free space for a drive-letter path.
// It does not create the path.
func ProbeVolume(path string) (VolumeFact, error) {
	drive, ok := volumeLetter(path)
	if !ok {
		return VolumeFact{}, fmt.Errorf("not a local drive-letter path")
	}
	root := drive + `\`
	p, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return VolumeFact{}, err
	}
	var free, total, avail uint64
	if err := windows.GetDiskFreeSpaceEx(p, &avail, &total, &free); err != nil {
		return VolumeFact{Drive: drive, Present: false}, nil
	}
	fsBuf := make([]uint16, 32)
	if err := windows.GetVolumeInformation(p, nil, 0, nil, nil, nil, &fsBuf[0], uint32(len(fsBuf))); err != nil {
		return VolumeFact{Drive: drive, Present: true, FreeBytes: avail}, nil
	}
	return VolumeFact{
		Drive:      drive,
		FileSystem: strings.TrimRight(windows.UTF16ToString(fsBuf), "\x00"),
		FreeBytes:  avail,
		Present:    true,
	}, nil
}
