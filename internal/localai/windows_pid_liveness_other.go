//go:build !windows

package localai

// Untracked Unix processes are checked with signal(0) in OSProcessBackend.
// This stub allows the host OS guard to remain explicit at its call site.
func windowsUntrackedPIDAlive(pid int) bool { return false }
