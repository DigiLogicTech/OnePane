//go:build linux

package nodepower

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
)

type linuxInhibitor struct {
	path string
}

func NewPlatformInhibitor() Inhibitor {
	path, err := exec.LookPath("systemd-inhibit")
	if err != nil {
		return unsupportedInhibitor{name: "linux-systemd-inhibit"}
	}
	return &linuxInhibitor{path: path}
}

func (i *linuxInhibitor) Supported() bool { return i.path != "" }
func (i *linuxInhibitor) Name() string    { return "linux-systemd-inhibit" }

func (i *linuxInhibitor) Acquire(ctx context.Context, reason string) (InhibitHandle, error) {
	if !i.Supported() {
		return nil, ErrInhibitorUnsupported
	}
	cmd := exec.Command(i.path,
		"--what=sleep",
		"--mode=block",
		"--who=OnePane",
		"--why="+reason,
		"/bin/sh", "-c", "while :; do sleep 3600; done",
	)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("nodepower: start systemd sleep inhibitor: %w", err)
	}
	h := &processHandle{cmd: cmd}
	go func() { <-ctx.Done(); _ = h.Release() }()
	return h, nil
}

type processHandle struct {
	once sync.Once
	cmd  *exec.Cmd
	err  error
}

func (h *processHandle) Release() error {
	h.once.Do(func() {
		if h.cmd == nil || h.cmd.Process == nil {
			return
		}
		_ = h.cmd.Process.Kill()
		h.err = h.cmd.Wait()
		if h.err != nil {
			if _, ok := h.err.(*exec.ExitError); ok {
				h.err = nil
			}
		}
	})
	return h.err
}
