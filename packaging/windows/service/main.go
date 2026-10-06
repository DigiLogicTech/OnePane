//go:build windows

package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/DigiLogicTech/OnePane/internal/buildinfo"
)

const (
	serviceName = "OnePane"

	serviceStop        = 0x00000001
	serviceShutdown    = 0x00000005
	serviceInterrogate = 0x00000004

	serviceStopped      = 0x00000001
	serviceStartPending = 0x00000002
	serviceStopPending  = 0x00000003
	serviceRunning      = 0x00000004

	serviceAcceptStop      = 0x00000001
	serviceAcceptShutdown  = 0x00000004
	serviceWin32OwnProcess = 0x00000010
)

type serviceStatus struct {
	ServiceType             uint32
	CurrentState            uint32
	ControlsAccepted        uint32
	Win32ExitCode           uint32
	ServiceSpecificExitCode uint32
	CheckPoint              uint32
	WaitHint                uint32
}

type serviceTableEntry struct {
	ServiceName *uint16
	ServiceProc uintptr
}

var (
	advapi32                          = syscall.NewLazyDLL("advapi32.dll")
	procStartServiceCtrlDispatcherW   = advapi32.NewProc("StartServiceCtrlDispatcherW")
	procRegisterServiceCtrlHandlerExW = advapi32.NewProc("RegisterServiceCtrlHandlerExW")
	procSetServiceStatus              = advapi32.NewProc("SetServiceStatus")

	statusHandle uintptr
	stopOnce     sync.Once
	stopCh       = make(chan struct{})
	childMu      sync.Mutex
	child        *exec.Cmd
)

func main() {
	name, _ := syscall.UTF16PtrFromString(serviceName)
	entries := []serviceTableEntry{
		{ServiceName: name, ServiceProc: syscall.NewCallback(serviceMain)},
		{},
	}
	r, _, e := procStartServiceCtrlDispatcherW.Call(uintptr(unsafe.Pointer(&entries[0])))
	if r == 0 {
		// Developer/diagnostic mode: allow direct launch from a console.
		if len(os.Args) > 1 && os.Args[1] == "--console" {
			if err := runBackend(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		}
		fmt.Fprintln(os.Stderr, "OnePane.Service must be launched by the Windows Service Control Manager; use --console for diagnostics:", e)
		os.Exit(1)
	}
}

func serviceMain(argc uint32, argv **uint16) uintptr {
	name, _ := syscall.UTF16PtrFromString(serviceName)
	statusHandle, _, _ = procRegisterServiceCtrlHandlerExW.Call(
		uintptr(unsafe.Pointer(name)),
		syscall.NewCallback(serviceHandler),
		0,
	)
	if statusHandle == 0 {
		return 0
	}
	setStatus(serviceStartPending, 0, 1, 15000, 0)

	done := make(chan error, 1)
	go func() { done <- runBackend() }()

	// Do not tell SCM the service is RUNNING until the actual control-plane
	// health endpoint is responding. This keeps SCM state aligned with backend
	// readiness and surfaces bootstrap failures as service-start failures.
	ready, startupErr := waitForBackendHealth(done, 60*time.Second)
	if !ready {
		if startupErr != nil {
			fmt.Fprintln(os.Stderr, "OnePane backend failed during startup:", startupErr)
		}
		setStatus(serviceStopped, 0, 0, 0, 1)
		return 0
	}

	setStatus(serviceRunning, serviceAcceptStop|serviceAcceptShutdown, 0, 0, 0)

	var exitCode uint32
	select {
	case <-stopCh:
		setStatus(serviceStopPending, 0, 1, 15000, 0)
		stopBackend()
		select {
		case <-done:
			// A forced child termination during an explicit SCM stop is a
			// successful service stop, not a crash/recovery condition.
			exitCode = 0
		case <-time.After(12 * time.Second):
			killBackend()
		}
	case err := <-done:
		if err != nil {
			exitCode = 1
		}
	}

	setStatus(serviceStopped, 0, 0, 0, exitCode)
	return 0
}

func waitForBackendHealth(done <-chan error, timeout time.Duration) (bool, error) {
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(timeout)
	checkpoint := uint32(2)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			if err == nil {
				err = fmt.Errorf("backend exited before becoming healthy")
			}
			return false, err
		case <-stopCh:
			stopBackend()
			return false, fmt.Errorf("service stopped during startup")
		default:
		}

		resp, err := client.Get("http://127.0.0.1:18181/v1/health")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 500 {
				return true, nil
			}
		}

		setStatus(serviceStartPending, 0, checkpoint, 15000, 0)
		checkpoint++
		time.Sleep(time.Second)
	}
	return false, fmt.Errorf("backend health endpoint did not become ready within %s", timeout)
}

func serviceHandler(control, eventType uint32, eventData, context uintptr) uintptr {
	switch control {
	case serviceStop, serviceShutdown:
		stopOnce.Do(func() { close(stopCh) })
	case serviceInterrogate:
		// SCM only needs the current status; SetServiceStatus is harmless here.
	}
	return 0
}

func setStatus(state, accepted, checkpoint, waitHint, exitCode uint32) {
	if statusHandle == 0 {
		return
	}
	s := serviceStatus{
		ServiceType:      serviceWin32OwnProcess,
		CurrentState:     state,
		ControlsAccepted: accepted,
		Win32ExitCode:    exitCode,
		CheckPoint:       checkpoint,
		WaitHint:         waitHint,
	}
	procSetServiceStatus.Call(statusHandle, uintptr(unsafe.Pointer(&s)))
}

func runBackend() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	installDir := filepath.Dir(exe)
	programData := os.Getenv("ProgramData")
	if programData == "" {
		programData = `C:\ProgramData`
	}
	dataDir := filepath.Join(programData, "OnePane")
	logDir := filepath.Join(dataDir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return err
	}
	logPath := filepath.Join(logDir, "onepane.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer logFile.Close()
	fmt.Fprintf(logFile, "OnePane Service %s starting\n", buildinfo.Version)

	backend := filepath.Join(installDir, "OnePane.Backend.exe")
	config := filepath.Join(dataDir, "config.yaml")
	cmd := exec.Command(backend, "-config", config)
	cmd.Dir = installDir
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000} // CREATE_NO_WINDOW

	childMu.Lock()
	child = cmd
	childMu.Unlock()
	defer func() {
		childMu.Lock()
		child = nil
		childMu.Unlock()
	}()

	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Wait()
}

func stopBackend() {
	childMu.Lock()
	cmd := child
	childMu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return
	}
	// The backend handles SIGTERM on Unix, but Windows os.Process.Signal has
	// limited semantics. Give it a short natural-exit window before forcing it.
	time.Sleep(1200 * time.Millisecond)
	_ = cmd.Process.Kill()
}

func killBackend() {
	childMu.Lock()
	defer childMu.Unlock()
	if child != nil && child.Process != nil {
		_ = child.Process.Kill()
	}
}
