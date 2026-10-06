//go:build darwin

package nodepower

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
)

type darwinInhibitor struct{ path string }

func NewPlatformInhibitor() Inhibitor {
	path, err := exec.LookPath("caffeinate")
	if err != nil {
		return unsupportedInhibitor{name: "macos-caffeinate"}
	}
	return &darwinInhibitor{path: path}
}

func (i *darwinInhibitor) Supported() bool { return i.path != "" }
func (i *darwinInhibitor) Name() string    { return "macos-caffeinate" }
func (i *darwinInhibitor) Acquire(ctx context.Context, reason string) (InhibitHandle, error) {
	if !i.Supported() {
		return nil, ErrInhibitorUnsupported
	}
	cmd := exec.Command(i.path, "-i")
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("nodepower: start caffeinate: %w", err)
	}
	h := &darwinProcessHandle{cmd: cmd}
	go func() { <-ctx.Done(); _ = h.Release() }()
	_ = reason
	return h, nil
}

type darwinProcessHandle struct {
	once sync.Once
	cmd  *exec.Cmd
	err  error
}

func (h *darwinProcessHandle) Release() error {
	h.once.Do(func() {
		if h.cmd == nil || h.cmd.Process == nil {
			return
		}
		_ = h.cmd.Process.Kill()
		h.err = h.cmd.Wait()
		if _, ok := h.err.(*exec.ExitError); ok {
			h.err = nil
		}
	})
	return h.err
}
