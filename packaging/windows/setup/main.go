//go:build windows

package main

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/DigiLogicTech/OnePane/internal/buildinfo"
)

const (
	productName = "OnePane"
	serviceName = "OnePane"
	controlURL  = "http://127.0.0.1:18181"
)

var version = buildinfo.Version

//go:embed payload/*
var payloadFS embed.FS

var (
	shell32       = syscall.NewLazyDLL("shell32.dll")
	user32        = syscall.NewLazyDLL("user32.dll")
	pShellExecute = shell32.NewProc("ShellExecuteW")
	pMessageBox   = user32.NewProc("MessageBoxW")
)

func main() {
	uninstall := hasArg("--uninstall")
	repair := hasArg("--repair")
	elevated := hasArg("--elevated")
	silent := hasArg("--silent")
	noLaunch := hasArg("--no-launch")
	skipOptionalRuntime := hasArg("--skip-optional-runtime")
	setModelPool := argValue("--set-model-pool")
	setProjectRoot := argValue("--set-project-root")
	installDirArg := argValue("--install-dir")
	projectRootArg := argValue("--project-root")
	modelPoolArg := argValue("--model-pool")
	if !elevated {
		args := []string{}
		if uninstall {
			args = append(args, "--uninstall")
		}
		if repair {
			args = append(args, "--repair")
		}
		if silent { args = append(args, "--silent") }
		if noLaunch { args = append(args, "--no-launch") }
		if skipOptionalRuntime { args = append(args, "--skip-optional-runtime") }
		for _, item := range [][2]string{{"--set-model-pool", setModelPool}, {"--set-project-root", setProjectRoot}, {"--install-dir", installDirArg}, {"--project-root", projectRootArg}, {"--model-pool", modelPoolArg}} {
			if item[1] != "" {
				args = append(args, item[0], item[1])
			}
		}
		if err := elevateArgs(args...); err != nil {
			message("OnePane Setup", "Administrator approval is required to modify OnePane.\n\n"+err.Error(), 0x10)
		}
		return
	}
	if setModelPool != "" {
		if err := updateConfigPath("model_pool_path", "local_ai", setModelPool); err != nil {
			message("OnePane Settings", "Could not update the model pool:\n\n"+err.Error(), 0x10)
			return
		}
		message("OnePane Settings", "The local model pool is now:\n\n"+setModelPool+"\n\nThe OnePane service has been restarted with the new setting.", 0x40)
		return
	}
	if setProjectRoot != "" {
		if err := updateConfigPath("project_root", "storage", setProjectRoot); err != nil {
			message("OnePane Settings", "Could not update the default Project storage root:\n\n"+err.Error(), 0x10)
			return
		}
		message("OnePane Settings", "The default Project storage root is now:\n\n"+setProjectRoot+"\n\nExisting Projects are not moved automatically.", 0x40)
		return
	}
	if uninstall {
		if err := uninstallProduct(); err != nil {
			logf("uninstall failed: %v", err)
			if !silent { message("OnePane Uninstall", "Uninstall encountered an error:\n\n"+err.Error(), 0x10) }
			os.Exit(1)
		}
		if !silent { message("OnePane Uninstall", "OnePane has been uninstalled.\n\nProjects, models and application data were preserved.", 0x40) }
		return
	}
	if err := installProduct(installDirArg, projectRootArg, modelPoolArg, repair, skipOptionalRuntime); err != nil {
		logf("installation failed: %v", err)
		if !silent { message("OnePane Setup", "Installation failed:\n\n"+err.Error()+"\n\nSee %TEMP%\\OnePaneSetup.log for details.", 0x10) }
		os.Exit(1)
	}
	verb := "installed"
	if repair { verb = "repaired" }
	if !silent { message("OnePane Setup", "OnePane v"+version+" is "+verb+".\n\nThe Windows service is running and the desktop application will open now.", 0x40) }
	if !noLaunch { launchDesktop() }
}
func installProduct(installDirArg, projectRootArg, modelPoolArg string, repair, skipOptionalRuntime bool) (retErr error) {
	logf("starting OnePane %s installation", version)
	programFiles := os.Getenv("ProgramFiles")
	if programFiles == "" {
		programFiles = `C:\Program Files`
	}
	programData := os.Getenv("ProgramData")
	if programData == "" {
		programData = `C:\ProgramData`
	}
	defaultInstallDir := filepath.Join(programFiles, "OnePane")
	dataDir := filepath.Join(programData, "OnePane")
	installDir := strings.TrimSpace(installDirArg)
	if installDir == "" {
		installDir = readInstallPath(dataDir)
	}
	if installDir == "" {
		installDir = defaultInstallDir
	}
	if !repair && strings.TrimSpace(installDirArg) == "" && readInstallPath(dataDir) == "" {
		message("OnePane Setup", "Choose where the OnePane application should be installed.\n\nCancel keeps the default location:\n"+defaultInstallDir, 0x40)
		if chosen, err := chooseFolder("Choose the OnePane application install folder", defaultInstallDir); err == nil && strings.TrimSpace(chosen) != "" {
			installDir = chosen
		}
	}
	if !filepath.IsAbs(installDir) {
		return fmt.Errorf("installation location must be absolute")
	}
	logDir := filepath.Join(dataDir, "logs")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dataDir, "install-path.txt"), []byte(installDir), 0o644); err != nil {
		return err
	}

	// A previous uninstall may still be draining the SCM entry or holding an
	// executable open. Make the fresh-install path deterministic before we
	// replace payloads.
	_ = runHidden("taskkill.exe", "/IM", "OnePane.Desktop.exe", "/F")
	_ = runHidden("sc.exe", "stop", serviceName)
	_ = runHidden("taskkill.exe", "/IM", "OnePane.Backend.exe", "/F")
	_ = runHidden("taskkill.exe", "/IM", "OnePane.Service.exe", "/F")
	_ = runHidden("sc.exe", "delete", serviceName)
	if !waitForServiceDeletion(20 * time.Second) {
		return fmt.Errorf("previous OnePane service is still pending deletion; reboot Windows or wait a moment and run Setup again")
	}

	rollback, err := capturePayloadRollback(installDir, dataDir)
	if err != nil {
		return fmt.Errorf("capture existing OnePane application for rollback: %w", err)
	}
	rollbackArmed := rollback != nil
	defer func() {
		if retErr == nil || !rollbackArmed || rollback == nil {
			return
		}
		logf("installation failed after payload replacement; restoring previous OnePane application")
		if rollbackErr := rollback.Restore(); rollbackErr != nil {
			retErr = fmt.Errorf("%w; application payload rollback failed: %v", retErr, rollbackErr)
			logf("application payload rollback failed: %v", rollbackErr)
			return
		}
		logf("previous OnePane application payload restored and service is healthy")
	}()

	payloads := map[string]string{
		"OnePane.Backend.exe": "b6f6bebfa65677c620e68a5ca15d6c3d1aab4ad58bcdae138217007195196b66",
		"OnePane.Service.exe": "6446a5dd7ba4aeb388fe5eae8bd3514184320353f34121d916b9a2ff7a33fd51",
		"OnePane.Desktop.exe": "b9f2c9514ad7ac91c1918b10bed52b0a393f3a079990bda9f243224b27947a66",
		"OnePane.ico":         "c383c69b14d111e0affa2576950654aea244b533e33e5bffaaf29fac3ac7dd6c",
	}
	for name, want := range payloads {
		if err := extractVerified(name, filepath.Join(installDir, name), want); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
	}
	self, err := os.Executable()
	if err == nil {
		_ = copyFile(self, filepath.Join(installDir, "OnePane.Setup.exe"))
	}

	configPath := filepath.Join(dataDir, "config.yaml")
	defaultProjectRoot := filepath.Join(dataDir, "Projects")
	defaultModelPool := filepath.Join(dataDir, "models")
	projectRoot := strings.TrimSpace(projectRootArg)
	modelPool := strings.TrimSpace(modelPoolArg)
	existingProjectRoot, existingModelPool := readConfiguredStorage(configPath)
	_, statErr := os.Stat(configPath)
	freshConfig := os.IsNotExist(statErr)
	if statErr != nil && !os.IsNotExist(statErr) {
		return statErr
	}
	if projectRoot == "" {
		projectRoot = existingProjectRoot
	}
	if modelPool == "" {
		modelPool = existingModelPool
	}
	if projectRoot == "" {
		projectRoot = defaultProjectRoot
	}
	if modelPool == "" {
		modelPool = defaultModelPool
	}
	if freshConfig && !repair && strings.TrimSpace(projectRootArg) == "" {
		message("OnePane Setup", "Choose the default location for Project and Workspace data.\n\nLibraries and generated artifacts can become large. Cancel keeps:\n"+defaultProjectRoot, 0x40)
		if chosen, err := chooseFolder("Choose the OnePane Project data location", defaultProjectRoot); err == nil && strings.TrimSpace(chosen) != "" {
			projectRoot = chosen
		}
	}
	if freshConfig && !repair && strings.TrimSpace(modelPoolArg) == "" {
		message("OnePane Setup", "Choose where OnePane should store local AI models.\n\nModel files can be large. Cancel keeps:\n"+defaultModelPool, 0x40)
		if chosen, err := chooseFolder("Choose the OnePane local model pool", defaultModelPool); err == nil && strings.TrimSpace(chosen) != "" {
			modelPool = chosen
		}
	}
	if !filepath.IsAbs(projectRoot) || !filepath.IsAbs(modelPool) {
		return fmt.Errorf("Project and model storage paths must be absolute")
	}
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		return fmt.Errorf("create Project root: %w", err)
	}
	if err := os.MkdirAll(modelPool, 0o755); err != nil {
		return fmt.Errorf("create model pool: %w", err)
	}
	logf("project storage root: %s", projectRoot)
	logf("model pool path: %s", modelPool)

	if freshConfig {
		dataPath := filepath.ToSlash(filepath.Join(dataDir, "data"))
		projectPath := filepath.ToSlash(projectRoot)
		modelPath := filepath.ToSlash(modelPool)
		cfg := "server:\n" +
			"  listen: 127.0.0.1:18181\n" +
			"  preview_listen: 127.0.0.1:18182\n" +
			"storage:\n" +
			"  data_dir: \"" + yamlEscape(dataPath) + "\"\n" +
			"  project_root: \"" + yamlEscape(projectPath) + "\"\n" +
			"logging:\n" +
			"  level: info\n" +
			"local_ai:\n" +
			"  model_pool_path: \"" + yamlEscape(modelPath) + "\"\n" +
			"  idle_unload_minutes: 15\n" +
			"  residency_headroom_pct: 10\n" +
			"node_federation:\n" +
			"  enabled: true\n" +
			"  listen: 0.0.0.0:18443\n" +
			"  discovery_enabled: true\n" +
			"  discovery_multicast: 239.255.77.77:47777\n" +
			"  heartbeat_seconds: 10\n" +
			"  stale_seconds: 35\n"
		if err := os.WriteFile(configPath, []byte(cfg), 0o644); err != nil {
			return err
		}
	} else {
		if strings.TrimSpace(projectRootArg) != "" && projectRoot != existingProjectRoot {
			if err := updateConfigFilePath(configPath, "project_root", "storage", projectRoot); err != nil {
				return err
			}
		}
		if strings.TrimSpace(modelPoolArg) != "" && modelPool != existingModelPool {
			if err := updateConfigFilePath(configPath, "model_pool_path", "local_ai", modelPool); err != nil {
				return err
			}
		}
	}

	if err := installWebView2(); err != nil {
		return err
	}
	if freshConfig && !repair && !skipOptionalRuntime {
		if err := offerOllamaInstall(); err != nil {
			logf("optional Ollama installation skipped/failed: %v", err)
		}
	}
	logf("optional runtimes are managed by the OnePane harness after installation and can be installed later from Models")

	_ = runHidden("sc.exe", "delete", serviceName)
	if !waitForServiceDeletion(20 * time.Second) {
		return fmt.Errorf("previous OnePane service is still pending deletion before registration")
	}
	serviceExe := filepath.Join(installDir, "OnePane.Service.exe")
	var createErr error
	for attempt := 0; attempt < 12; attempt++ {
		createErr = runHidden("sc.exe", "create", serviceName, "binPath=", `"`+serviceExe+`"`, "start=", "auto", "DisplayName=", "OnePane")
		if createErr == nil {
			break
		}
		time.Sleep(750 * time.Millisecond)
	}
	if createErr != nil {
		return fmt.Errorf("register Windows service: %w", createErr)
	}
	_ = runHidden("sc.exe", "description", serviceName, "OnePane local-first autonomous AI control plane")
	_ = runHidden("sc.exe", "failure", serviceName, "reset=", "86400", "actions=", "restart/5000/restart/15000/restart/60000")
	_ = runHidden("sc.exe", "failureflag", serviceName, "1")

	if err := createStartMenuShortcut(installDir, programData); err != nil {
		logf("shortcut warning: %v", err)
	}
	desktopExe := filepath.Join(installDir, "OnePane.Desktop.exe")
	_ = runHidden("reg.exe", "add", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, "/v", "OnePane", "/t", "REG_SZ", "/d", `"`+desktopExe+`"`, "/f")
	if err := runHidden("sc.exe", "start", serviceName); err != nil {
		return fmt.Errorf("start OnePane service: %w", err)
	}
	if !waitForHealth(75 * time.Second) {
		logf("service did not become healthy within 75 seconds")
		return fmt.Errorf("the OnePane service installed but did not become healthy; inspect %s", filepath.Join(logDir, "onepane.log"))
	}

	uninstallCmd := `"` + filepath.Join(installDir, "OnePane.Setup.exe") + `" --uninstall`
	repairCmd := `"` + filepath.Join(installDir, "OnePane.Setup.exe") + `" --repair`
	uninstallKey := `HKLM\Software\Microsoft\Windows\CurrentVersion\Uninstall\OnePane`
	_ = runHidden("reg.exe", "add", uninstallKey, "/v", "DisplayName", "/t", "REG_SZ", "/d", "OnePane", "/f")
	_ = runHidden("reg.exe", "add", uninstallKey, "/v", "DisplayVersion", "/t", "REG_SZ", "/d", version, "/f")
	_ = runHidden("reg.exe", "add", uninstallKey, "/v", "Publisher", "/t", "REG_SZ", "/d", "DigiLogic", "/f")
	_ = runHidden("reg.exe", "add", uninstallKey, "/v", "InstallLocation", "/t", "REG_SZ", "/d", installDir, "/f")
	_ = runHidden("reg.exe", "add", uninstallKey, "/v", "DisplayIcon", "/t", "REG_SZ", "/d", filepath.Join(installDir, "OnePane.ico"), "/f")
	_ = runHidden("reg.exe", "add", uninstallKey, "/v", "UninstallString", "/t", "REG_SZ", "/d", uninstallCmd, "/f")
	_ = runHidden("reg.exe", "add", uninstallKey, "/v", "ModifyPath", "/t", "REG_SZ", "/d", repairCmd, "/f")
	_ = runHidden("reg.exe", "add", uninstallKey, "/v", "NoModify", "/t", "REG_DWORD", "/d", "0", "/f")
	_ = runHidden("reg.exe", "add", uninstallKey, "/v", "NoRepair", "/t", "REG_DWORD", "/d", "0", "/f")

	rollbackArmed = false
	if rollback != nil {
		rollback.Cleanup()
	}
	logf("installation complete and health endpoint is responding")
	return nil
}

type payloadRollback struct {
	installDir string
	backupDir  string
	files      []string
}

func capturePayloadRollback(installDir, dataDir string) (*payloadRollback, error) {
	names := []string{"OnePane.Backend.exe", "OnePane.Service.exe", "OnePane.Desktop.exe", "OnePane.ico"}
	stamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	backupDir := filepath.Join(dataDir, "recovery", "package-upgrade", "windows-"+stamp)
	var files []string
	for _, name := range names {
		src := filepath.Join(installDir, name)
		st, err := os.Stat(src)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if st.IsDir() {
			return nil, fmt.Errorf("existing payload %s is a directory", name)
		}
		if len(files) == 0 {
			if err := os.MkdirAll(backupDir, 0o700); err != nil {
				return nil, err
			}
		}
		if err := copyFile(src, filepath.Join(backupDir, name)); err != nil {
			_ = os.RemoveAll(backupDir)
			return nil, err
		}
		files = append(files, name)
	}
	if len(files) == 0 {
		return nil, nil
	}
	logf("captured previous OnePane payload for rollback: %s", backupDir)
	return &payloadRollback{installDir: installDir, backupDir: backupDir, files: files}, nil
}

func (r *payloadRollback) Restore() error {
	if r == nil || len(r.files) == 0 {
		return nil
	}
	_ = runHidden("taskkill.exe", "/IM", "OnePane.Desktop.exe", "/F")
	_ = runHidden("sc.exe", "stop", serviceName)
	_ = runHidden("taskkill.exe", "/IM", "OnePane.Backend.exe", "/F")
	_ = runHidden("taskkill.exe", "/IM", "OnePane.Service.exe", "/F")
	_ = runHidden("sc.exe", "delete", serviceName)
	_ = waitForServiceDeletion(20 * time.Second)

	for _, name := range r.files {
		if err := copyFile(filepath.Join(r.backupDir, name), filepath.Join(r.installDir, name)); err != nil {
			return fmt.Errorf("restore %s: %w", name, err)
		}
	}
	serviceExe := filepath.Join(r.installDir, "OnePane.Service.exe")
	if _, err := os.Stat(serviceExe); err != nil {
		return fmt.Errorf("restored service executable unavailable: %w", err)
	}
	if err := runHidden("sc.exe", "create", serviceName, "binPath=", `"`+serviceExe+`"`, "start=", "auto", "DisplayName=", "OnePane"); err != nil {
		return fmt.Errorf("register restored OnePane service: %w", err)
	}
	_ = runHidden("sc.exe", "description", serviceName, "OnePane local-first autonomous AI control plane")
	_ = runHidden("sc.exe", "failure", serviceName, "reset=", "86400", "actions=", "restart/5000/restart/15000/restart/60000")
	_ = runHidden("sc.exe", "failureflag", serviceName, "1")
	if err := runHidden("sc.exe", "start", serviceName); err != nil {
		return fmt.Errorf("start restored OnePane service: %w", err)
	}
	if !waitForHealth(45 * time.Second) {
		return errors.New("restored OnePane service did not become healthy")
	}
	return nil
}

func (r *payloadRollback) Cleanup() {
	if r == nil || strings.TrimSpace(r.backupDir) == "" {
		return
	}
	_ = os.RemoveAll(r.backupDir)
}

func installWebView2() error {
	if webView2RuntimePresent() {
		logf("Microsoft Edge WebView2 Runtime already present")
		return nil
	}
	tempDir := filepath.Join(os.TempDir(), "OnePaneSetup")
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return err
	}
	// This folder is owned by this Setup run only. Remove abandoned installer
	// downloads when Setup succeeds or fails; never touch shared Windows caches.
	defer os.RemoveAll(tempDir)
	// Microsoft recommends the tiny Evergreen bootstrapper for online clients.
	// It downloads only the matching architecture and keeps the OnePane setup
	// package small. Offline packaging can still embed the standalone runtime.
	installer := filepath.Join(tempDir, "MicrosoftEdgeWebview2Setup.exe")
	if err := downloadFile("https://go.microsoft.com/fwlink/p/?LinkId=2124703", installer); err != nil {
		return fmt.Errorf("download WebView2 bootstrapper: %w", err)
	}
	defer os.Remove(installer)
	if err := verifyAuthenticode(installer); err != nil {
		return fmt.Errorf("verify WebView2 bootstrapper: %w", err)
	}
	logf("installing Microsoft Edge WebView2 Runtime using Evergreen bootstrapper")
	cmd := exec.Command(installer, "/silent", "/install")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	err := cmd.Run()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code := ee.ExitCode()
			if code != 0 && code != 3010 {
				return fmt.Errorf("WebView2 Runtime installer exit code %d", code)
			}
		} else {
			return fmt.Errorf("WebView2 Runtime installer: %w", err)
		}
	}
	if !webView2RuntimePresent() {
		return fmt.Errorf("WebView2 Runtime installer completed but the runtime could not be located")
	}
	return nil
}

func downloadFile(rawURL, dst string) error {
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Get(rawURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, resp.Body)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	_ = os.Remove(dst)
	return os.Rename(tmp, dst)
}

func verifyAuthenticode(path string) error {
	// Runtime bootstrap URLs are intentionally evergreen. Rather than pin a
	// hash that changes underneath us, require Windows to validate the signed
	// executable before OnePane ever launches it.
	script := `$s=Get-AuthenticodeSignature -LiteralPath '` + psQuote(path) + `'; if($s.Status -ne 'Valid'){Write-Error ('Invalid Authenticode status: '+$s.Status); exit 23}`
	if err := runHidden("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script); err != nil {
		return err
	}
	return nil
}

func ollamaInstalled() bool {
	if _, err := exec.LookPath("ollama.exe"); err == nil {
		return true
	}
	for _, root := range []string{os.Getenv("LOCALAPPDATA"), os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
		if strings.TrimSpace(root) == "" {
			continue
		}
		for _, rel := range []string{filepath.Join("Programs", "Ollama", "ollama.exe"), filepath.Join("Ollama", "ollama.exe")} {
			if st, err := os.Stat(filepath.Join(root, rel)); err == nil && !st.IsDir() {
				return true
			}
		}
	}
	return false
}

func hardwareSummary() string {
	script := `$gpu=(Get-CimInstance Win32_VideoController -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Name); $ram=[math]::Round((Get-CimInstance Win32_ComputerSystem).TotalPhysicalMemory/1GB); [Console]::Out.Write((($gpu -join ', ')+'|'+$ram))`
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return "Hardware scan unavailable; OnePane can still install local runtimes later from Models."
	}
	parts := strings.SplitN(strings.TrimSpace(string(out)), "|", 2)
	gpu := strings.TrimSpace(parts[0])
	ram := ""
	if len(parts) > 1 {
		ram = strings.TrimSpace(parts[1])
	}
	if gpu == "" {
		gpu = "No dedicated GPU reported"
	}
	if ram != "" {
		return fmt.Sprintf("Detected graphics: %s\nSystem memory: %s GB", gpu, ram)
	}
	return "Detected graphics: " + gpu
}

func offerOllamaInstall() error {
	if ollamaInstalled() {
		logf("existing Ollama installation detected; OnePane will reuse it")
		return nil
	}
	summary := hardwareSummary()
	if !confirm("OnePane Setup - Local AI", summary+"\n\nInstall Ollama now?\n\nThis is optional. If you skip it, OnePane can install a compatible Ollama/runtime later when you download a model.") {
		logf("user deferred Ollama installation")
		return nil
	}
	tempDir := filepath.Join(os.TempDir(), "OnePaneSetup")
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return err
	}
	// This folder is owned by this Setup run only. Remove abandoned installer
	// downloads when Setup succeeds or fails; never touch shared Windows caches.
	defer os.RemoveAll(tempDir)
	installer := filepath.Join(tempDir, "OllamaSetup.exe")
	if err := downloadFile("https://ollama.com/download/OllamaSetup.exe", installer); err != nil {
		return fmt.Errorf("download Ollama: %w", err)
	}
	defer os.Remove(installer)
	if err := verifyAuthenticode(installer); err != nil {
		return fmt.Errorf("verify Ollama installer: %w", err)
	}
	logf("installing optional Ollama runtime")
	cmd := exec.Command(installer, "/VERYSILENT", "/NORESTART", "/SUPPRESSMSGBOXES")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Ollama installer: %w", err)
	}
	return nil
}

func webView2RuntimePresent() bool {
	for _, root := range []string{os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramFiles")} {
		if strings.TrimSpace(root) == "" {
			continue
		}
		pattern := filepath.Join(root, "Microsoft", "EdgeWebView", "Application", "*", "EBWebView", "x64", "EmbeddedBrowserWebView.dll")
		if matches, _ := filepath.Glob(pattern); len(matches) > 0 {
			return true
		}
	}
	return false
}

func chooseFolder(description, defaultPath string) (string, error) {
	script := `Add-Type -AssemblyName System.Windows.Forms; ` +
		`$d=New-Object System.Windows.Forms.FolderBrowserDialog; ` +
		`$d.Description='` + psQuote(description) + `'; ` +
		`$d.ShowNewFolderButton=$true; ` +
		`$d.SelectedPath='` + psQuote(defaultPath) + `'; ` +
		`if($d.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK){[Console]::Out.Write($d.SelectedPath)}`
	cmd := exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-STA", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func uninstallProduct() error {
	programFiles := os.Getenv("ProgramFiles")
	if programFiles == "" {
		programFiles = `C:\Program Files`
	}
	programData := os.Getenv("ProgramData")
	if programData == "" {
		programData = `C:\ProgramData`
	}
	dataDir := filepath.Join(programData, "OnePane")
	installDir := readInstallPath(dataDir)
	if installDir == "" {
		installDir = filepath.Join(programFiles, "OnePane")
	}
	logf("starting resilient uninstall; durable ProgramData content will be preserved")
	_ = runHidden("taskkill.exe", "/IM", "OnePane.Desktop.exe", "/F")
	_ = runHidden("sc.exe", "stop", serviceName)
	_ = runHidden("taskkill.exe", "/IM", "OnePane.Backend.exe", "/F")
	_ = runHidden("taskkill.exe", "/IM", "OnePane.Service.exe", "/F")
	_ = runHidden("sc.exe", "delete", serviceName)
	_ = waitForServiceDeletion(20 * time.Second)
	_ = runHidden("reg.exe", "delete", `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, "/v", "OnePane", "/f")
	_ = runHidden("reg.exe", "delete", `HKLM\Software\Microsoft\Windows\CurrentVersion\Uninstall\OnePane`, "/f")
	_ = os.Remove(filepath.Join(programData, "Microsoft", "Windows", "Start Menu", "Programs", "OnePane.lnk"))

	// Remove the application payload deterministically. Durable ProgramData is
	// intentionally retained. When Setup is launched from outside installDir
	// (CI, repair media, or an administrator copy), remove the directory
	// synchronously and report a real failure if a lock remains. When the
	// installed Setup is uninstalling itself, remove every sibling first and
	// schedule only the final self/delete step after this process exits.
	// Only delete the known signed application payload. An existing installation
	// may contain a user-selected Models or Projects folder; recursive deletion
	// of the installation root would destroy user data.
	self, _ := os.Executable()
	if err := removeInstallSiblings(installDir, self); err != nil {
		return fmt.Errorf("remove OnePane application payload: %w", err)
	}
	if !pathWithin(self, installDir) {
		// Setup was launched from outside the installation. Remove only the
		// installed Setup file, then remove the root iff it is empty.
		if err := removeInstalledSetup(installDir); err != nil {
			return err
		}
		removeEmptyInstallRoot(installDir)
		return nil
	}
	// Setup cannot delete its running executable. The deferred command may
	// remove that single file and then try to remove an EMPTY directory only.
	// It must never use rmdir /s: model weights and Project files may be here.
	cmd := exec.Command("cmd.exe", "/d", "/c",
		"ping 127.0.0.1 -n 3 >nul & del /f /q \""+self+"\" >nul 2>&1 & rmdir /q \""+installDir+"\" >nul 2>&1")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("schedule OnePane uninstaller cleanup: %w", err)
	}
	return nil
}

func pathWithin(path, root string) bool {
	pathAbs, err1 := filepath.Abs(path)
	rootAbs, err2 := filepath.Abs(root)
	if err1 != nil || err2 != nil {
		return false
	}
	pathAbs = filepath.Clean(pathAbs)
	rootAbs = filepath.Clean(rootAbs)
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func removeTreeWithRetry(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		lastErr = os.RemoveAll(path)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return nil
		}
		if time.Now().After(deadline) {
			if lastErr == nil {
				lastErr = fmt.Errorf("directory still exists")
			}
			return lastErr
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// uninstallPayloadNames is the exact allowlist of application-owned files.
// Unknown files and directories (including Models and Projects) are user data.
var uninstallPayloadNames = []string{
	"OnePane.Backend.exe", "OnePane.Service.exe", "OnePane.Desktop.exe",
	"OnePane.ico",
}

func removeInstallSiblings(installDir, self string) error {
	selfAbs, _ := filepath.Abs(self)
	for _, name := range uninstallPayloadNames {
		path := filepath.Join(installDir, name)
		if strings.EqualFold(filepath.Clean(path), filepath.Clean(selfAbs)) {
			continue
		}
		if err := removePayloadFile(path); err != nil {
			return err
		}
	}
	return nil
}

func removePayloadFile(path string) error {
	st, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) { return nil }
	if err != nil { return err }
	if st.IsDir() { return fmt.Errorf("refusing to remove directory at application payload path: %s", path) }
	deadline := time.Now().Add(10 * time.Second)
	for {
		if err = os.Remove(path); err == nil || errors.Is(err, os.ErrNotExist) { return nil }
		if time.Now().After(deadline) { return fmt.Errorf("remove application file %s: %w", path, err) }
		time.Sleep(500 * time.Millisecond)
	}
}

func removeInstalledSetup(installDir string) error {
	return removePayloadFile(filepath.Join(installDir, "OnePane.Setup.exe"))
}

func removeEmptyInstallRoot(installDir string) {
	if err := os.Remove(installDir); err != nil {
		// ENOTEMPTY is intentional when user-owned content is present.
		logf("preserved non-application files in install directory %s: %v", installDir, err)
	}
}

func waitForServiceDeletion(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		// sc.exe query returns non-zero once the service no longer exists.
		// ERROR_SERVICE_DOES_NOT_EXIST (1060) is the desired state.
		if err := runHidden("sc.exe", "query", serviceName); err != nil {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

func createStartMenuShortcut(installDir, programData string) error {
	link := filepath.Join(programData, "Microsoft", "Windows", "Start Menu", "Programs", "OnePane.lnk")
	target := filepath.Join(installDir, "OnePane.Desktop.exe")
	icon := filepath.Join(installDir, "OnePane.ico")
	script := `$w=New-Object -ComObject WScript.Shell; $s=$w.CreateShortcut('` + psQuote(link) + `'); $s.TargetPath='` + psQuote(target) + `'; $s.WorkingDirectory='` + psQuote(installDir) + `'; $s.Description='OnePane'; $s.IconLocation='` + psQuote(icon) + `,0'; $s.Save()`
	return runHidden("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
}

func waitForHealth(timeout time.Duration) bool {
	client := &http.Client{Timeout: 1 * time.Second}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(controlURL + "/v1/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return true
			}
		}
		time.Sleep(650 * time.Millisecond)
	}
	return false
}

func launchDesktop() {
	programFiles := os.Getenv("ProgramFiles")
	if programFiles == "" {
		programFiles = `C:\Program Files`
	}
	programData := os.Getenv("ProgramData")
	if programData == "" {
		programData = `C:\ProgramData`
	}
	installDir := readInstallPath(filepath.Join(programData, "OnePane"))
	if installDir == "" {
		installDir = filepath.Join(programFiles, "OnePane")
	}
	exe := filepath.Join(installDir, "OnePane.Desktop.exe")
	cmd := exec.Command(exe)
	cmd.Dir = filepath.Dir(exe)
	_ = cmd.Start()
}

func extract(name, dst string) error {
	b, err := payloadFS.ReadFile("payload/" + name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".new"
	if err := os.WriteFile(tmp, b, 0o755); err != nil {
		return err
	}
	_ = os.Remove(dst)
	return os.Rename(tmp, dst)
}
func extractVerified(name, dst, wantHex string) error {
	b, err := payloadFS.ReadFile("payload/" + name)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(b)
	got := hex.EncodeToString(sum[:])
	if !strings.EqualFold(got, wantHex) {
		return fmt.Errorf("embedded payload integrity failure for %s: got %s want %s", name, got, wantHex)
	}
	return extract(name, dst)
}
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, cpErr := io.Copy(out, in)
	closeErr := out.Close()
	if cpErr != nil {
		return cpErr
	}
	return closeErr
}
func runHidden(name string, args ...string) error {
	logf("run: %s %s", name, strings.Join(args, " "))
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if len(out) > 0 {
		logf("%s", strings.TrimSpace(string(out)))
	}
	return err
}
func elevateArgs(args ...string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	verb, _ := syscall.UTF16PtrFromString("runas")
	file, _ := syscall.UTF16PtrFromString(exe)
	parts := []string{"--elevated"}
	for _, a := range args {
		if strings.ContainsAny(a, " \t\"") {
			parts = append(parts, `"`+strings.ReplaceAll(a, `"`, `\"`)+`"`)
		} else {
			parts = append(parts, a)
		}
	}
	p, _ := syscall.UTF16PtrFromString(strings.Join(parts, " "))
	cwd, _ := os.Getwd()
	d, _ := syscall.UTF16PtrFromString(cwd)
	r, _, e := pShellExecute.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(file)), uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(d)), 1)
	if r <= 32 {
		return fmt.Errorf("ShellExecute elevation failed: %v", e)
	}
	return nil
}

func updateConfigFilePath(configPath, key, section, value string) error {
	linesBytes, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	lines := strings.Split(string(linesBytes), "\n")
	quoted := `"` + yamlEscape(filepath.ToSlash(value)) + `"`
	replaced := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), key+":") {
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			lines[i] = indent + key + ": " + quoted
			replaced = true
			break
		}
	}
	if !replaced {
		inserted := false
		for i, line := range lines {
			if strings.TrimSpace(line) == section+":" {
				lines = append(lines[:i+1], append([]string{"  " + key + ": " + quoted}, lines[i+1:]...)...)
				inserted = true
				break
			}
		}
		if !inserted {
			lines = append(lines, "", section+":", "  "+key+": "+quoted)
		}
	}
	tmp := configPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, configPath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func updateConfigPath(key, section, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("path is empty")
	}
	if !filepath.IsAbs(value) {
		return fmt.Errorf("path must be absolute")
	}
	if err := os.MkdirAll(value, 0o755); err != nil {
		return fmt.Errorf("create path: %w", err)
	}
	programData := os.Getenv("ProgramData")
	if programData == "" {
		programData = `C:\ProgramData`
	}
	configPath := filepath.Join(programData, "OnePane", "config.yaml")
	if err := updateConfigFilePath(configPath, key, section, value); err != nil {
		return err
	}
	logf("%s updated from GUI: %s", key, value)
	_ = runHidden("sc.exe", "stop", serviceName)
	time.Sleep(900 * time.Millisecond)
	if err := runHidden("sc.exe", "start", serviceName); err != nil {
		return fmt.Errorf("restart OnePane service: %w", err)
	}
	if !waitForHealth(45 * time.Second) {
		return fmt.Errorf("OnePane did not become healthy after applying %s", key)
	}
	return nil
}

func readConfiguredStorage(configPath string) (projectRoot, modelPool string) {
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return "", ""
	}
	for _, line := range strings.Split(string(raw), "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "project_root:") {
			projectRoot = strings.Trim(strings.TrimSpace(strings.TrimPrefix(trim, "project_root:")), `"'`)
		}
		if strings.HasPrefix(trim, "model_pool_path:") {
			modelPool = strings.Trim(strings.TrimSpace(strings.TrimPrefix(trim, "model_pool_path:")), `"'`)
		}
	}
	return
}

func readInstallPath(dataDir string) string {
	b, err := os.ReadFile(filepath.Join(dataDir, "install-path.txt"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func yamlEscape(s string) string { return strings.ReplaceAll(s, `"`, `\\"`) }

func argValue(name string) string {
	for i, a := range os.Args[1:] {
		if strings.EqualFold(a, name) && i+2 <= len(os.Args[1:]) {
			return os.Args[1:][i+1]
		}
		prefix := name + "="
		if strings.HasPrefix(strings.ToLower(a), strings.ToLower(prefix)) {
			return strings.TrimPrefix(a, prefix)
		}
	}
	return ""
}

func hasArg(want string) bool {
	for _, a := range os.Args[1:] {
		if strings.EqualFold(a, want) {
			return true
		}
	}
	return false
}
func confirm(title, text string) bool {
	t, _ := syscall.UTF16PtrFromString(text)
	h, _ := syscall.UTF16PtrFromString(title)
	r, _, _ := pMessageBox.Call(0, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(h)), 0x00000004|0x00000020|0x00000100)
	return r == 6
}
func message(title, text string, flags uintptr) {
	t, _ := syscall.UTF16PtrFromString(text)
	h, _ := syscall.UTF16PtrFromString(title)
	pMessageBox.Call(0, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(h)), flags)
}
func logf(format string, args ...any) {
	f, err := os.OpenFile(filepath.Join(os.TempDir(), "OnePaneSetup.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s "+format+"\r\n", append([]any{time.Now().Format(time.RFC3339)}, args...)...)
}
func psQuote(s string) string { return strings.ReplaceAll(s, "'", "''") }
