//go:build !windows

package runtimeaccount

import "os/exec"

// Prepare is a no-op outside Windows. Model processes there are not LocalSystem.
func Prepare(cmd *exec.Cmd, roots Roots) (func(), error) {
	if cmd == nil {
		return nil, errMissingCommand
	}
	return func() {}, nil
}
