//go:build windows

package sqlite

import (
	"fmt"

	"golang.org/x/sys/windows"
)

func availableBytes(path string) (uint64, error) {
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, fmt.Errorf("migration backup volume: %w", err)
	}
	var free, total, avail uint64
	if err := windows.GetDiskFreeSpaceEx(ptr, &avail, &total, &free); err != nil {
		return 0, err
	}
	return avail, nil
}
