//go:build !linux

package serviceinstall

import "fmt"

func InitializeSystemdConfiguration(string, string, string) error {
	return fmt.Errorf("systemd storage configuration requires Linux")
}

func PreviewSystemdStorage(string) (StoragePlan, error) {
	return StoragePlan{}, fmt.Errorf("systemd storage configuration requires Linux")
}
func ConfigureSystemdStorage(string) (StoragePlan, error) {
	return StoragePlan{}, fmt.Errorf("systemd storage configuration requires Linux")
}
