//go:build windows

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/DigiLogicTech/OnePane/internal/config"
	"github.com/DigiLogicTech/OnePane/internal/serviceproof"
	"golang.org/x/sys/windows/registry"
	"gopkg.in/yaml.v3"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/DigiLogicTech/OnePane/internal/buildinfo"
	"github.com/DigiLogicTech/OnePane/internal/installroots"
	"github.com/DigiLogicTech/OnePane/internal/startupevidence"
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

	statusHandle         uintptr
	stopOnce             sync.Once
	stopCh               = make(chan struct{})
	childMu              sync.Mutex
	child                *exec.Cmd
	backendShutdownToken string
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

// Even when the backend never binds HTTP, the Windows service records a
// typed failure in its configured persistent data root, not SYSTEM temp.
func serviceEvidence(stage startupevidence.Stage, outcome startupevidence.Outcome) {
	programData := os.Getenv("ProgramData")
	if programData == "" {
		programData = `C:\ProgramData`
	}
	root, err := configuredOnePaneDataRoot(filepath.Join(programData, "OnePane"))
	if err != nil {
		// Do not write private diagnostic material into the wrong data root.
		fmt.Fprintf(os.Stderr, "OnePane service startup evidence unavailable: %v\n", err)
		return
	}
	if installroots.InspectInstallerDataRoot(root).Partial {
		fmt.Fprintf(os.Stderr, "OnePane service startup evidence skipped: partial data root left unchanged\n")
		return
	}
	_ = startupevidence.Record(root, startupevidence.WindowsService, stage, outcome)
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
	serviceEvidence(startupevidence.ServiceLaunch, startupevidence.Begin)

	startupCtx, cancelStartup := context.WithCancel(context.Background())
	defer cancelStartup()
	done := make(chan error, 1)
	go func() { done <- runBackendWithContext(startupCtx) }()

	// Do not tell SCM the service is RUNNING until the actual control-plane
	// health endpoint is responding. This keeps SCM state aligned with backend
	// readiness and surfaces bootstrap failures as service-start failures.
	ready, startupErr := waitForBackendHealth(done, 60*time.Second)
	if !ready {
		// Do not announce STOPPED while a timed-out or stop-requested
		// backend still owns inference workers. The graceful stop is bounded;
		// a failed drain invokes the managed process-tree termination path.
		setStatus(serviceStopPending, 0, 1, 20000, 0)
		if hasLaunchedBackendChild() {
			if !drainBackend(done, stopBackend, func() {
				cancelStartup()
				killBackend()
			}, 5*time.Second, 5*time.Second) {
				fmt.Fprintln(os.Stderr, "OnePane startup cleanup: managed child did not exit within deadline")
			}
		} else {
			// The backend may be creating configuration and have no Process
			// yet. Cancellation also fences its future cmd.Start().
			cancelStartup()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				fmt.Fprintln(os.Stderr, "OnePane startup cleanup: prelaunch backend did not drain in time")
			}
		}
		serviceEvidence(startupevidence.ServiceReady, startupevidence.Failed)
		if startupErr != nil {
			fmt.Fprintln(os.Stderr, "OnePane backend failed during startup:", startupErr)
			recordStartupError(startupErr)
		}
		setStatus(serviceStopped, 0, 0, 0, 1)
		return 0
	}

	serviceEvidence(startupevidence.ServiceReady, startupevidence.OK)
	setStatus(serviceRunning, serviceAcceptStop|serviceAcceptShutdown, 0, 0, 0)

	var exitCode uint32
	select {
	case <-stopCh:
		setStatus(serviceStopPending, 0, 1, 55000, 0)
		stopBackend()
		select {
		case <-done:
			// A forced child termination during an explicit SCM stop is a
			// successful service stop, not a crash/recovery condition.
			exitCode = 0
		case <-time.After(52 * time.Second):
			cancelStartup()
			killBackend()
			// Wait a bounded interval for Wait() to reap the backend and
			// close its KILL_ON_JOB_CLOSE ownership job before SCM stops.
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				fmt.Fprintln(os.Stderr, "OnePane backend still not reaped after forced stop")
			}
		}
	case err := <-done:
		// An unrequested backend exit (even status 0) is a service failure.
		// Otherwise SCM may report an unexpected crash as a clean stop,
		// hiding it from the operator and service-recovery policy.
		if err != nil {
			fmt.Fprintln(os.Stderr, "OnePane backend exited:", err)
		}
		exitCode = classifyBackendExit(stopCh)
	}

	if exitCode != 0 {
		serviceEvidence(startupevidence.ServiceStop, startupevidence.Failed)
	} else {
		serviceEvidence(startupevidence.ServiceStop, startupevidence.Stopped)
	}
	setStatus(serviceStopped, 0, 0, 0, exitCode)
	return 0
}

// Record SCM readiness failures in the persistent service log: the service's
// stderr is not normally visible to an end user running Setup.
func recordStartupError(startupErr error) {
	if startupErr == nil {
		return
	}
	programData := os.Getenv("ProgramData")
	if programData == "" {
		programData = `C:\\ProgramData`
	}
	root, resolveErr := configuredOnePaneDataRoot(filepath.Join(programData, "OnePane"))
	if resolveErr != nil {
		// An invalid installer path cannot make us log failures to C: instead.
		fmt.Fprintf(os.Stderr, "OnePane service startup failure (persistent log unavailable): %v; data root: %v\n", startupErr, resolveErr)
		return
	}
	if installroots.InspectInstallerDataRoot(root).Partial {
		fmt.Fprintf(os.Stderr, "OnePane service startup failure (partial data root left unchanged): %v\n", startupErr)
		return
	}
	logPath := filepath.Join(root, "logs", "onepane.log")
	// Same protected append/rotation path as the backend log. An early
	// startup failure must not leave a second, broadly readable log.
	f, err := openBoundedLogWriter(logPath)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s OnePane service startup failure: %v\n", time.Now().Format(time.RFC3339Nano), startupErr)
}

func probeLaunchedBackend(client *http.Client, address, token string) bool {
	if client == nil || len(token) < 32 {
		return false
	}
	challenge := make([]byte, serviceproof.ChallengeBytes)
	if _, err := rand.Read(challenge); err != nil {
		return false
	}
	req, err := http.NewRequest(http.MethodGet, address, nil)
	if err != nil {
		return false
	}
	req.Header.Set(serviceproof.ChallengeHeader, hex.EncodeToString(challenge))
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	// An unrelated 204 responder or a child from an older service launch
	// cannot produce the correct HMAC for this fresh random challenge.
	return resp.StatusCode == http.StatusNoContent &&
		serviceproof.Verify(token, challenge, resp.Header.Get(serviceproof.ProofHeader))
}

func waitForBackendHealth(done <-chan error, timeout time.Duration) (bool, error) {
	client := &http.Client{
		Timeout: time.Second,
		// Do not accept a redirect to a different host as backend readiness.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
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

		// Every service launch has a new random secret, passed only to the
		// child. A pre-existing unrelated loopback listener must NEVER make
		// SCM declare this backend running.
		childMu.Lock()
		token := backendShutdownToken
		childMu.Unlock()
		if probeLaunchedBackend(client, "http://127.0.0.1:18181/__onepane/service/ready", token) {
			// A child that has already exited is never healthy.
			select {
			case err := <-done:
				if err == nil {
					err = fmt.Errorf("backend exited after readiness response")
				}
				return false, err
			default:
				return true, nil
			}
		}

		setStatus(serviceStartPending, 0, checkpoint, 15000, 0)
		checkpoint++
		time.Sleep(time.Second)
	}
	return false, fmt.Errorf("backend health endpoint did not become ready within %s", timeout)
}

// Only a stop explicitly requested by SCM can make a child exit count as
// successful. A spontaneous clean exit still terminates the service's job
// tree and is reported as a failure for diagnosis and recovery policy.
func classifyBackendExit(stopping <-chan struct{}) uint32 {
	select {
	case <-stopping:
		return 0
	default:
		return 1
	}
}

// A failure that already consumed the exited backend's done signal does
// not need to wait through another shutdown grace period.
func hasLaunchedBackendChild() bool {
	childMu.Lock()
	defer childMu.Unlock()
	return child != nil && child.Process != nil
}

// drainBackend bounds both phases. Reaping through done permits runBackend to
// release its Windows Job Object (which owns model-server descendants).
// Returning false is visible to the service log, never an assertion that all
// processes or physical hardware were proven stopped.
func drainBackend(done <-chan error, graceful, force func(),
	grace, afterForce time.Duration) bool {
	if graceful != nil {
		graceful()
	}
	graceTimer := time.NewTimer(grace)
	defer graceTimer.Stop()
	select {
	case <-done:
		return true
	case <-graceTimer.C:
	}
	if force != nil {
		force()
	}
	forcedTimer := time.NewTimer(afterForce)
	defer forcedTimer.Stop()
	select {
	case <-done:
		return true
	case <-forcedTimer.C:
		return false
	}
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

// Registry configuration is written by the MSI, not by the desktop user.
// A service launched under LocalSystem must use the same root selected by
// the installation wizard and never fall back after a configured drive fails.
// A missing legacy value is different from a present but malformed or
// unreadable value. Typed lookup avoids parsing reg.exe's localized stdout.
func onePaneRegistryPathValue(name string) (string, bool, error) {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SOFTWARE\DigiLogic\OnePane`, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if errors.Is(err, registry.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("open installer storage registry key: %w", err)
	}
	defer key.Close()
	value, _, err := key.GetStringValue(name)
	if errors.Is(err, registry.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read installer %s: %w", name, err)
	}
	return value, true, nil
}

// A DataRoot explicitly written by the installer is authoritative. Never
// launch a second, empty OnePane on C: when that registry value is corrupt,
// unreadable, or relative; the legacy default is used only if the setting is
// genuinely absent. This function does not create directories or change data.
func configuredOnePaneDataRoot(fallback string) (string, error) {
	value, present, err := onePaneRegistryPathValue("DataRoot")
	if err != nil {
		return "", err
	}
	root, err := resolveOnePaneDataRoot(fallback, value, present)
	if err != nil {
		return "", err
	}
	// MSI-selected persistent state must not be redirected through an
	// existing NTFS junction/symlink to the previous C: database.
	if present {
		if err := verifySelectedRootComponents(root); err != nil {
			return "", err
		}
	}
	return root, nil
}

// Separated for Windows-native deterministic regression without touching
// HKLM, service registrations or the operator's selected storage drive.
func resolveOnePaneDataRoot(fallback, configured string, present bool) (string, error) {
	if !present {
		return filepath.Clean(fallback), nil
	}
	value := strings.TrimSpace(configured)
	if value == "" || !filepath.IsAbs(value) {
		return "", fmt.Errorf("invalid installer storage path: expected absolute persistent path")
	}
	cleaned := filepath.Clean(value)
	volume := filepath.VolumeName(cleaned)
	// filepath.IsAbs also accepts UNC shares and Windows device namespaces.
	// The MSI promises local NTFS volumes; remote/device paths would silently
	// redirect secrets, model weights or databases outside the selected drive.
	if len(volume) != 2 || volume[1] != ':' ||
		!(volume[0] >= 'A' && volume[0] <= 'Z' || volume[0] >= 'a' && volume[0] <= 'z') {
		return "", fmt.Errorf("invalid installer storage path: select a local drive-letter directory")
	}
	return cleaned, nil
}

// resolveInstallerStoragePath validates explicit storage choices without
// touching user data. Default legacy paths retain their existing behaviour.
func resolveInstallerStoragePath(fallback, configured string, present bool) (string, error) {
	resolved, err := resolveOnePaneDataRoot(fallback, configured, present)
	if err != nil {
		return "", err
	}
	if present {
		if err := verifySelectedRootComponents(resolved); err != nil {
			return "", err
		}
	}
	return resolved, nil
}

// A first-run local Windows installation must not join LAN multicast or
// expose a federation listener without operator opt-in. Existing YAML is
// never modified, so previously approved Node settings are preserved.
func firstRunWindowsServiceConfig(root, projectRoot, modelRoot string) config.Config {
	cfg := config.Default()
	cfg.Server.Listen = "127.0.0.1:18181"
	cfg.Server.PreviewListen = "127.0.0.1:18182"
	cfg.Storage.DataDir = filepath.Join(root, "data")
	cfg.Storage.ProjectRoot = projectRoot
	cfg.LocalAI.ModelPoolPath = modelRoot
	cfg.Federation.Enabled = false
	cfg.Federation.DiscoveryEnabled = false
	cfg.Federation.Listen = "127.0.0.1:18443"
	return cfg
}

func ensureOnePaneConfig(root string) error {
	path := filepath.Join(root, "config.yaml")
	// Validate both registry-selected directories even on existing installs;
	// the presence of a config file must not bypass invalid MSI path choices.
	projectValue, projectPresent, err := onePaneRegistryPathValue("ProjectRoot")
	if err != nil {
		return err
	}
	projectRoot, err := resolveInstallerStoragePath(filepath.Join(root, "Projects"), projectValue, projectPresent)
	if err != nil {
		return fmt.Errorf("invalid installer ProjectRoot: %w", err)
	}
	modelValue, modelPresent, err := onePaneRegistryPathValue("ModelRoot")
	if err != nil {
		return err
	}
	modelRoot, err := resolveInstallerStoragePath(filepath.Join(root, "models"), modelValue, modelPresent)
	if err != nil {
		return fmt.Errorf("invalid installer ModelRoot: %w", err)
	}

	// Existing configuration is never rewritten. An MSI-selected DataRoot
	// cannot silently use a copied config pointing back to an old database.
	if err := inspectExistingServiceConfig(path); err == nil {
		_, explicit, regErr := onePaneRegistryPathValue("DataRoot")
		if regErr != nil {
			return regErr
		}
		return verifyExistingConfigStorageSelections(path, root, explicit,
			projectRoot, projectPresent, modelRoot, modelPresent)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	cfg := firstRunWindowsServiceConfig(root, projectRoot, modelRoot)
	buf, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return persistInitialServiceConfig(path, buf)
}
func runBackend() error {
	return runBackendWithContext(context.Background())
}

func runBackendWithContext(ctx context.Context) (retErr error) {
	if ctx == nil {
		return fmt.Errorf("nil service startup context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	installDir := filepath.Dir(exe)
	programData := os.Getenv("ProgramData")
	if programData == "" {
		programData = `C:\ProgramData`
	}
	dataDir, err := configuredOnePaneDataRoot(filepath.Join(programData, "OnePane"))
	if err != nil {
		return fmt.Errorf("resolve persistent installer data root: %w", err)
	}
	if err := rejectPartialDataRoot(dataDir); err != nil {
		return err
	}
	logDir := filepath.Join(dataDir, "logs")
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ensureOnePaneConfig(dataDir); err != nil {
		return fmt.Errorf("prepare OnePane persistent configuration: %w", err)
	}
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return err
	}
	logPath := filepath.Join(logDir, "onepane.log")
	logFile, err := openBoundedLogWriter(logPath)
	if err != nil {
		return err
	}
	defer logFile.Close()
	defer func() {
		if retErr != nil {
			fmt.Fprintf(logFile, "%s OnePane backend launch/exit failure: %v\n", time.Now().Format(time.RFC3339Nano), retErr)
		}
	}()
	fmt.Fprintf(logFile, "OnePane Service %s starting\n", buildinfo.Version)
	noteUnselectedPartialRoot(logFile, dataDir, programData)

	backend := filepath.Join(installDir, "OnePane.Backend.exe")
	config := filepath.Join(dataDir, "config.yaml")
	// CommandContext prevents a still-preparing service from spawning a
	// backend after SCM stop, cancellation or readiness timeout.
	cmd := exec.CommandContext(ctx, backend, "-config", config)
	cmd.Dir = installDir
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	// One token per Windows service launch, never persisted on disk.
	seed := make([]byte, 32)
	if _, err := rand.Read(seed); err != nil {
		return err
	}
	token := hex.EncodeToString(seed)
	cmd.Env = append(os.Environ(), "ONEPANE_SERVICE_SHUTDOWN_TOKEN="+token,
		"ONEPANE_STARTUP_EVIDENCE_ROOT="+dataDir)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000} // CREATE_NO_WINDOW

	childMu.Lock()
	child = cmd
	backendShutdownToken = token
	childMu.Unlock()
	defer func() {
		childMu.Lock()
		child = nil
		backendShutdownToken = ""
		childMu.Unlock()
	}()

	if err := ctx.Err(); err != nil {
		return err
	}
	job, err := newOwnedBackendJob()
	if err != nil {
		return fmt.Errorf("create managed process job: %w", err)
	}
	defer job.Close()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launch OnePane backend %s: %w", backend, err)
	}
	if err := job.Assign(cmd.Process.Pid); err != nil {
		// Never leave an unowned daemon running. On Windows 8+ nested jobs
		// are supported; an inability to enroll fails closed.
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("enroll harness in Windows child-process job: %w", err)
	}
	return cmd.Wait()
}

// Give harnessd an authenticated, loopback-only opportunity to unload
// managed inference processes and remove its own child process records.
func stopBackend() {
	childMu.Lock()
	cmd := child
	token := backendShutdownToken
	childMu.Unlock()
	if cmd == nil || cmd.Process == nil || len(token) < 32 {
		return
	}
	nonce := make([]byte, serviceproof.ChallengeBytes)
	if _, err := rand.Read(nonce); err != nil {
		fmt.Fprintln(os.Stderr, "OnePane shutdown nonce unavailable:", err)
		return
	}
	req, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:18181/__onepane/service/shutdown", nil)
	if err != nil {
		return
	}
	req.Header.Set(serviceproof.ChallengeHeader, hex.EncodeToString(nonce))
	req.Header.Set(serviceproof.ProofHeader, serviceproof.SignShutdown(token, nonce))
	client := &http.Client{Timeout: 3 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "OnePane graceful stop request failed:", err)
		return
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		fmt.Fprintln(os.Stderr, "OnePane graceful stop rejected:", resp.StatusCode)
	}
}

// A forced service stop must terminate the entire harnessd child tree; simply
// killing harnessd leaks llama-server and llmfit processes and locks CUDA DLLs.
func killBackend() {
	childMu.Lock()
	cmd := child
	childMu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := strconv.Itoa(cmd.Process.Pid)
	killer := exec.Command("taskkill.exe", "/PID", pid, "/T", "/F")
	if out, err := killer.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "OnePane managed process tree cleanup failed: %v (%s)\\n", err, string(out))
		_ = cmd.Process.Kill()
	}
}
