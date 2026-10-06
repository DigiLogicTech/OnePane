//go:build windows

package nodepower

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"syscall"
)

const (
	esSystemRequired = 0x00000001
	esContinuous     = 0x80000000
)

type windowsInhibitor struct{}

func NewPlatformInhibitor() Inhibitor     { return &windowsInhibitor{} }
func (*windowsInhibitor) Supported() bool { return true }
func (*windowsInhibitor) Name() string    { return "windows-SetThreadExecutionState" }

func (*windowsInhibitor) Acquire(ctx context.Context, reason string) (InhibitHandle, error) {
	h := &windowsHandle{release: make(chan struct{}), done: make(chan struct{})}
	started := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(h.done)
		kernel32 := syscall.NewLazyDLL("kernel32.dll")
		proc := kernel32.NewProc("SetThreadExecutionState")
		ret, _, callErr := proc.Call(uintptr(esContinuous | esSystemRequired))
		if ret == 0 {
			if callErr == syscall.Errno(0) {
				callErr = errors.New("SetThreadExecutionState returned zero")
			}
			started <- callErr
			return
		}
		started <- nil
		select {
		case <-ctx.Done():
		case <-h.release:
		}
		_, _, _ = proc.Call(uintptr(esContinuous))
	}()
	if err := <-started; err != nil {
		return nil, err
	}
	_ = reason
	return h, nil
}

type windowsHandle struct {
	once    sync.Once
	release chan struct{}
	done    chan struct{}
}

func (h *windowsHandle) Release() error {
	h.once.Do(func() { close(h.release) })
	<-h.done
	return nil
}
