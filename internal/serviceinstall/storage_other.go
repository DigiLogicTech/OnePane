//go:build !linux

package serviceinstall

import "fmt"

func PreviewSystemdStorage(string) (StoragePlan, error) {
	return StoragePlan{}, fmt.Errorf("systemd storage configuration requires Linux")
}
func ConfigureSystemdStorage(string) (StoragePlan, error) {
	return StoragePlan{}, fmt.Errorf("systemd storage configuration requires Linux")
}
