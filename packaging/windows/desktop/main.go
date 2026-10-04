//go:build windows

package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/DigiLogicTech/OnePane/internal/buildinfo"
)

const (
	appName    = "OnePane"
	controlURL = "http://127.0.0.1:18181"

	coinitApartmentThreaded         = 0x2
	wsOverlappedWindow              = 0x00CF0000
	cwUseDefault                    = 0x80000000
	swHide                          = 0
	swShow                          = 5
	swRestore                       = 9
	wmDestroy                       = 0x0002
	wmSize                          = 0x0005
	wmClose                         = 0x0010
	wmApp                           = 0x8000
	wmLButtonDblClk                 = 0x0203
	wmRButtonUp                     = 0x0205
	trayMessage                     = wmApp + 1
	errorAlreadyExists              = 183
	idiApplication                  = 32512
	idcArrow                        = 32512
	colorWindow                     = 5
	imageIcon                       = 1
	lrLoadFromFile                  = 0x00000010
	dwmwaUseImmersiveDarkMode       = 20
	dwmwaUseImmersiveDarkModeLegacy = 19
	dwmwaBorderColor                = 34
	dwmwaCaptionColor               = 35
	dwmwaTextColor                  = 36

	nimAdd     = 0x00000000
	nimDelete  = 0x00000002
	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004

	mfString       = 0x00000000
	mfSeparator    = 0x00000800
	tpmRightButton = 0x0002
	tpmReturnCmd   = 0x0100

	cmdOpen    = 1001
	cmdRestart = 1002
	cmdQuit    = 1003
)

//go:embed static/*
var inspectionAssets embed.FS

var uiBaseURL string

type point struct{ X, Y int32 }
type rect struct{ Left, Top, Right, Bottom int32 }
type msg struct {
	Hwnd           uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Pt             point
	LPrivate       uint32
}
type wndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}
type notifyIconData struct {
	CbSize            uint32
	HWnd              uintptr
	UID               uint32
	UFlags            uint32
	UCallbackMessage  uint32
	HIcon             uintptr
	SzTip             [128]uint16
	DwState           uint32
	DwStateMask       uint32
	SzInfo            [256]uint16
	UTimeoutOrVersion uint32
	SzInfoTitle       [64]uint16
	DwInfoFlags       uint32
	GuidItem          [16]byte
	HBalloonIcon      uintptr
}

type comVTable4 struct{ QueryInterface, AddRef, Release, Invoke uintptr }
type comHandler struct {
	Vtbl *comVTable4
	Refs int32
	Kind uint32
}
type iUnknown struct{ Vtbl *uintptr }

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	ole32    = syscall.NewLazyDLL("ole32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	advapi32 = syscall.NewLazyDLL("advapi32.dll")
	dwmapi   = syscall.NewLazyDLL("dwmapi.dll")

	pRegisterClassEx     = user32.NewProc("RegisterClassExW")
	pCreateWindowEx      = user32.NewProc("CreateWindowExW")
	pDefWindowProc       = user32.NewProc("DefWindowProcW")
	pShowWindow          = user32.NewProc("ShowWindow")
	pUpdateWindow        = user32.NewProc("UpdateWindow")
	pGetMessage          = user32.NewProc("GetMessageW")
	pTranslateMessage    = user32.NewProc("TranslateMessage")
	pDispatchMessage     = user32.NewProc("DispatchMessageW")
	pPostQuitMessage     = user32.NewProc("PostQuitMessage")
	pGetClientRect       = user32.NewProc("GetClientRect")
	pLoadIcon            = user32.NewProc("LoadIconW")
	pLoadImage           = user32.NewProc("LoadImageW")
	pLoadCursor          = user32.NewProc("LoadCursorW")
	pMessageBox          = user32.NewProc("MessageBoxW")
	pSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	pCreatePopupMenu     = user32.NewProc("CreatePopupMenu")
	pAppendMenu          = user32.NewProc("AppendMenuW")
	pTrackPopupMenu      = user32.NewProc("TrackPopupMenu")
	pDestroyMenu         = user32.NewProc("DestroyMenu")
	pGetCursorPos        = user32.NewProc("GetCursorPos")
	pFindWindow          = user32.NewProc("FindWindowW")

	pGetModuleHandle = kernel32.NewProc("GetModuleHandleW")
	pLoadLibrary     = kernel32.NewProc("LoadLibraryW")
	pGetProcAddress  = kernel32.NewProc("GetProcAddress")
	pCreateMutex     = kernel32.NewProc("CreateMutexW")
	pGetLastError    = kernel32.NewProc("GetLastError")
	pCloseHandle     = kernel32.NewProc("CloseHandle")
	pCoInitializeEx  = ole32.NewProc("CoInitializeEx")
	pCoUninitialize  = ole32.NewProc("CoUninitialize")
	pCoTaskMemFree   = ole32.NewProc("CoTaskMemFree")
	pShellNotifyIcon = shell32.NewProc("Shell_NotifyIconW")

	pRegOpenKeyEx          = advapi32.NewProc("RegOpenKeyExW")
	pRegQueryValueEx       = advapi32.NewProc("RegQueryValueExW")
	pRegCloseKey           = advapi32.NewProc("RegCloseKey")
	pDwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")

	mainHwnd           uintptr
	webviewEnvironment uintptr
	webviewController  uintptr
	webviewObject      uintptr
	quitting           atomic.Bool
	trayData           notifyIconData

	envHandler   *comHandler
	ctrlHandler  *comHandler
	themeHandler *comHandler
	handlerVtbl  comVTable4
)

func init() {
	handlerVtbl = comVTable4{
		QueryInterface: syscall.NewCallback(comQueryInterface),
		AddRef:         syscall.NewCallback(comAddRef),
		Release:        syscall.NewCallback(comRelease),
		Invoke:         syscall.NewCallback(comInvoke),
	}
}

func main() {
	desktopLogf("OnePane Desktop %s starting", buildinfo.Version)
	mutexName, _ := syscall.UTF16PtrFromString(`Local\OnePaneDesktop`)
	mutex, _, _ := pCreateMutex.Call(0, 0, uintptr(unsafe.Pointer(mutexName)))
	if mutex != 0 {
		defer pCloseHandle.Call(mutex)
	}
	lastErr, _, _ := pGetLastError.Call()
	if lastErr == errorAlreadyExists {
		cls, _ := syscall.UTF16PtrFromString("OnePaneDesktopWindow")
		if hwnd, _, _ := pFindWindow.Call(uintptr(unsafe.Pointer(cls)), 0); hwnd != 0 {
			pShowWindow.Call(hwnd, swRestore)
			pSetForegroundWindow.Call(hwnd)
		}
		return
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	r, _, _ := pCoInitializeEx.Call(0, coinitApartmentThreaded)
	if int32(r) < 0 {
		fatalBox(fmt.Sprintf("COM initialization failed (HRESULT 0x%08X).", uint32(r)))
		return
	}
	defer pCoUninitialize.Call()

	if !ensureBackendReady() {
		fatalBox("The OnePane Windows service did not become healthy.\n\nUse the tray/start-menu entry after checking C:\\ProgramData\\OnePane\\logs\\onepane.log, or repair the installation.")
		return
	}

	var uiErr error
	uiBaseURL, uiErr = startDesktopUIServer()
	if uiErr != nil {
		fatalBox("The OnePane desktop UI could not start: " + uiErr.Error())
		return
	}
	desktopLogf("desktop UI gateway listening at %s", uiBaseURL)

	if err := createMainWindow(); err != nil {
		fatalBox(err.Error())
		return
	}
	addTrayIcon()
	defer removeTrayIcon()

	if err := initializeWebView2(); err != nil {
		fatalBox("WebView2 initialization failed: " + err.Error())
		return
	}

	pShowWindow.Call(mainHwnd, swShow)
	pUpdateWindow.Call(mainHwnd)

	var m msg
	for {
		ret, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func createMainWindow() error {
	hinst, _, _ := pGetModuleHandle.Call(0)
	cls, _ := syscall.UTF16PtrFromString("OnePaneDesktopWindow")
	title, _ := syscall.UTF16PtrFromString("OnePane")
	icon := loadOnePaneIcon(32)
	if icon == 0 {
		icon, _, _ = pLoadIcon.Call(0, idiApplication)
	}
	smallIcon := loadOnePaneIcon(16)
	if smallIcon == 0 {
		smallIcon = icon
	}
	cursor, _, _ := pLoadCursor.Call(0, idcArrow)
	wc := wndClassEx{
		CbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		LpfnWndProc:   syscall.NewCallback(wndProc),
		HInstance:     hinst,
		HIcon:         icon,
		HCursor:       cursor,
		HbrBackground: colorWindow + 1,
		LpszClassName: cls,
		HIconSm:       smallIcon,
	}
	if r, _, _ := pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return fmt.Errorf("could not register the OnePane desktop window")
	}
	hwnd, _, _ := pCreateWindowEx.Call(
		0,
		uintptr(unsafe.Pointer(cls)),
		uintptr(unsafe.Pointer(title)),
		wsOverlappedWindow,
		cwUseDefault, cwUseDefault, 1380, 880,
		0, 0, hinst, 0,
	)
	if hwnd == 0 {
		return fmt.Errorf("could not create the OnePane desktop window")
	}
	mainHwnd = hwnd
	applyInitialNativeTheme()
	return nil
}

func wndProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case wmSize:
		resizeWebView()
		return 0
	case wmClose:
		if !quitting.Load() {
			pShowWindow.Call(hwnd, swHide)
			return 0
		}
	case trayMessage:
		switch uint32(lParam) & 0xffff {
		case wmLButtonDblClk:
			showMainWindow()
		case wmRButtonUp:
			showTrayMenu()
		}
		return 0
	case wmDestroy:
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProc.Call(hwnd, uintptr(message), wParam, lParam)
	return r
}

func addTrayIcon() {
	icon := loadOnePaneIcon(16)
	if icon == 0 {
		icon, _, _ = pLoadIcon.Call(0, idiApplication)
	}
	trayData = notifyIconData{
		CbSize:           uint32(unsafe.Sizeof(notifyIconData{})),
		HWnd:             mainHwnd,
		UID:              1,
		UFlags:           nifMessage | nifIcon | nifTip,
		UCallbackMessage: trayMessage,
		HIcon:            icon,
	}
	copyUTF16(trayData.SzTip[:], "OnePane")
	pShellNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(&trayData)))
}
func removeTrayIcon() {
	if trayData.CbSize != 0 {
		pShellNotifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(&trayData)))
	}
}
func showMainWindow() { pShowWindow.Call(mainHwnd, swRestore); pSetForegroundWindow.Call(mainHwnd) }
func showTrayMenu() {
	menu, _, _ := pCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer pDestroyMenu.Call(menu)
	open, _ := syscall.UTF16PtrFromString("Open OnePane")
	restart, _ := syscall.UTF16PtrFromString("Restart service")
	quit, _ := syscall.UTF16PtrFromString("Quit desktop app")
	pAppendMenu.Call(menu, mfString, cmdOpen, uintptr(unsafe.Pointer(open)))
	pAppendMenu.Call(menu, mfString, cmdRestart, uintptr(unsafe.Pointer(restart)))
	pAppendMenu.Call(menu, mfSeparator, 0, 0)
	pAppendMenu.Call(menu, mfString, cmdQuit, uintptr(unsafe.Pointer(quit)))
	var pt point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForegroundWindow.Call(mainHwnd)
	cmd, _, _ := pTrackPopupMenu.Call(menu, tpmRightButton|tpmReturnCmd, uintptr(pt.X), uintptr(pt.Y), 0, mainHwnd, 0)
	switch cmd {
	case cmdOpen:
		showMainWindow()
	case cmdRestart:
		go restartService()
	case cmdQuit:
		quitting.Store(true)
		removeTrayIcon()
		// WM_CLOSE will now destroy rather than hide.
		user32.NewProc("DestroyWindow").Call(mainHwnd)
	}
}

func restartService() {
	c := exec.Command("sc.exe", "stop", "OnePane")
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = c.Run()
	time.Sleep(1200 * time.Millisecond)
	c = exec.Command("sc.exe", "start", "OnePane")
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = c.Run()
}

func ensureBackendReady() bool {
	client := &http.Client{Timeout: 900 * time.Millisecond}
	healthy := func() bool {
		resp, err := client.Get(controlURL + "/v1/health")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode >= 200 && resp.StatusCode < 500
	}
	if healthy() {
		return true
	}
	c := exec.Command("sc.exe", "start", "OnePane")
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = c.Run()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if healthy() {
			return true
		}
		time.Sleep(600 * time.Millisecond)
	}
	return false
}

func startDesktopUIServer() (string, error) {
	target, err := url.Parse(controlURL)
	if err != nil {
		return "", err
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, e error) {
		desktopLogf("proxy %s failed: %v", r.URL.Path, e)
		http.Error(w, "OnePane control plane unavailable", http.StatusBadGateway)
	}
	mux := http.NewServeMux()
	// Keep the Windows-native settings endpoints on the desktop origin, but
	// proxy every other route to the authoritative control-plane WebGUI. This
	// prevents a second embedded UI copy from drifting out of sync with the
	// backend source.
	mux.HandleFunc("/desktop/settings", desktopSettingsHandler)
	mux.HandleFunc("/desktop/settings/model-pool", desktopModelPoolHandler)
	mux.HandleFunc("/desktop/settings/project-root", desktopProjectRootHandler)
	mux.HandleFunc("/desktop/folder-picker", desktopFolderPickerHandler)
	mux.Handle("/v1/", proxy)
	mux.Handle("/ws/", proxy)
	staticFS, err := fs.Sub(inspectionAssets, "static")
	if err != nil {
		return "", err
	}
	fileServer := http.FileServer(http.FS(staticFS))
	index, err := fs.ReadFile(staticFS, "index.html")
	if err != nil {
		return "", err
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			if r.Method != http.MethodHead {
				_, _ = w.Write(index)
			}
			return
		}
		fileServer.ServeHTTP(w, r)
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	go func() {
		if err := http.Serve(ln, mux); err != nil && !strings.Contains(err.Error(), "closed") {
			desktopLogf("desktop UI server stopped: %v", err)
		}
	}()
	return "http://" + ln.Addr().String(), nil
}

func desktopSettingsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"model_pool_path": readConfigPath("model_pool_path"), "project_root": readConfigPath("project_root")})
}

func desktopModelPoolHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		ModelPoolPath string `json:"model_pool_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.ModelPoolPath) == "" {
		http.Error(w, "model_pool_path is required", http.StatusBadRequest)
		return
	}
	path := strings.TrimSpace(in.ModelPoolPath)
	setup := filepath.Join(filepath.Dir(mustExecutable()), "OnePane.Setup.exe")
	if _, err := os.Stat(setup); err != nil {
		http.Error(w, "OnePane.Setup.exe is unavailable", http.StatusInternalServerError)
		return
	}
	verb, _ := syscall.UTF16PtrFromString("runas")
	file, _ := syscall.UTF16PtrFromString(setup)
	params, _ := syscall.UTF16PtrFromString(`--set-model-pool "` + strings.ReplaceAll(path, `"`, `\\"`) + `"`)
	dir, _ := syscall.UTF16PtrFromString(filepath.Dir(setup))
	proc := shell32.NewProc("ShellExecuteW")
	ret, _, _ := proc.Call(mainHwnd, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(file)), uintptr(unsafe.Pointer(params)), uintptr(unsafe.Pointer(dir)), swShow)
	if ret <= 32 {
		http.Error(w, "administrator approval was cancelled or could not be started", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"accepted": true, "model_pool_path": path})
}

func desktopProjectRootHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		ProjectRoot string `json:"project_root"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.ProjectRoot) == "" {
		http.Error(w, "project_root is required", http.StatusBadRequest)
		return
	}
	path := strings.TrimSpace(in.ProjectRoot)
	setup := filepath.Join(filepath.Dir(mustExecutable()), "OnePane.Setup.exe")
	if _, err := os.Stat(setup); err != nil {
		http.Error(w, "OnePane.Setup.exe is unavailable", http.StatusInternalServerError)
		return
	}
	verb, _ := syscall.UTF16PtrFromString("runas")
	file, _ := syscall.UTF16PtrFromString(setup)
	params, _ := syscall.UTF16PtrFromString(`--set-project-root "` + strings.ReplaceAll(path, `"`, `\\"`) + `"`)
	dir, _ := syscall.UTF16PtrFromString(filepath.Dir(setup))
	proc := shell32.NewProc("ShellExecuteW")
	ret, _, _ := proc.Call(mainHwnd, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(file)), uintptr(unsafe.Pointer(params)), uintptr(unsafe.Pointer(dir)), swShow)
	if ret <= 32 {
		http.Error(w, "administrator approval was cancelled or could not be started", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"accepted": true, "project_root": path})
}

func desktopFolderPickerHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ps := `Add-Type -AssemblyName System.Windows.Forms; $d=New-Object System.Windows.Forms.FolderBrowserDialog; $d.Description='Choose a OnePane model folder'; $d.ShowNewFolderButton=$true; if($d.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK){[Console]::Out.Write($d.SelectedPath)}`
	cmd := exec.Command("powershell.exe", "-NoProfile", "-STA", "-Command", ps)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		http.Error(w, "folder picker failed", http.StatusInternalServerError)
		return
	}
	path := strings.TrimSpace(string(out))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"path": path})
}

func readConfigPath(key string) string {
	pd := os.Getenv("ProgramData")
	if pd == "" {
		pd = `C:\\ProgramData`
	}
	raw, err := os.ReadFile(filepath.Join(pd, "OnePane", "config.yaml"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(raw), "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, key+":") {
			v := strings.TrimSpace(strings.TrimPrefix(trim, key+":"))
			return strings.Trim(v, `"'`)
		}
	}
	return ""
}

func mustExecutable() string {
	x, err := os.Executable()
	if err != nil {
		return "OnePane.Desktop.exe"
	}
	return x
}

func initializeWebView2() error {
	dllPath, err := findWebView2DLL()
	if err != nil {
		return err
	}
	desktopLogf("initializing WebView2 runtime from %s", dllPath)
	dllPtr, _ := syscall.UTF16PtrFromString(dllPath)
	h, _, e := pLoadLibrary.Call(uintptr(unsafe.Pointer(dllPtr)))
	if h == 0 {
		return fmt.Errorf("could not load %s: %v", dllPath, e)
	}
	procName, _ := syscall.BytePtrFromString("CreateWebViewEnvironmentWithOptionsInternal")
	createProc, _, e := pGetProcAddress.Call(h, uintptr(unsafe.Pointer(procName)))
	if createProc == 0 {
		return fmt.Errorf("WebView2 runtime export unavailable: %v", e)
	}

	userData := webViewDataDir()
	_ = os.MkdirAll(userData, 0o755)
	userDataPtr, _ := syscall.UTF16PtrFromString(userData)
	envHandler = &comHandler{Vtbl: &handlerVtbl, Refs: 1, Kind: 1}
	// BOOL checkRunningInstance=true, runtime type=installed(0), userDataFolder,
	// environmentOptions=null, completion handler.
	hr, _, _ := syscall.SyscallN(createProc,
		1,
		0,
		uintptr(unsafe.Pointer(userDataPtr)),
		0,
		uintptr(unsafe.Pointer(envHandler)),
	)
	if int32(hr) < 0 {
		return fmt.Errorf("environment creation returned HRESULT 0x%08X", uint32(hr))
	}
	return nil
}

func comQueryInterface(this, riid, ppv uintptr) uintptr {
	if ppv == 0 {
		return 0x80004003
	}
	*(*uintptr)(unsafe.Pointer(ppv)) = this
	comAddRef(this)
	return 0
}
func comAddRef(this uintptr) uintptr {
	h := (*comHandler)(unsafe.Pointer(this))
	return uintptr(atomic.AddInt32(&h.Refs, 1))
}
func comRelease(this uintptr) uintptr {
	h := (*comHandler)(unsafe.Pointer(this))
	n := atomic.AddInt32(&h.Refs, -1)
	if n < 1 {
		atomic.StoreInt32(&h.Refs, 1)
		return 1
	}
	return uintptr(n)
}
func comInvoke(this, result, object uintptr) uintptr {
	h := (*comHandler)(unsafe.Pointer(this))
	if h.Kind == 3 {
		// WebMessageReceived callback signature is (sender, args), not
		// (HRESULT, object). Read the string payload from event args slot 5.
		if object == 0 {
			return 0
		}
		var value *uint16
		hr := callCOM(object, 5, uintptr(unsafe.Pointer(&value)))
		if int32(hr) >= 0 && value != nil {
			message := utf16PtrToString(value)
			pCoTaskMemFree.Call(uintptr(unsafe.Pointer(value)))
			handleWebMessage(message)
		}
		return 0
	}
	if int32(result) < 0 || object == 0 {
		desktopLogf("WebView2 callback kind=%d failed: HRESULT 0x%08X object=%#x", h.Kind, uint32(result), object)
		return result
	}
	switch h.Kind {
	case 1:
		// The completion-handler result is a COM interface. Retain it beyond
		// this callback; otherwise the runtime may release it as soon as Invoke
		// returns, leaving the asynchronously-created controller without a live
		// environment owner.
		_ = callCOM(object, 1) // IUnknown::AddRef
		webviewEnvironment = object
		desktopLogf("WebView2 environment created")
		// ICoreWebView2Environment::CreateCoreWebView2Controller is vtable slot 3.
		ctrlHandler = &comHandler{Vtbl: &handlerVtbl, Refs: 1, Kind: 2}
		hr := callCOM(object, 3, mainHwnd, uintptr(unsafe.Pointer(ctrlHandler)))
		if int32(hr) < 0 {
			desktopLogf("CreateCoreWebView2Controller failed: HRESULT 0x%08X", uint32(hr))
		}
		return hr
	case 2:
		// Retain controller and WebView after the completion callback. WebView2
		// owns the callback result only for the duration of Invoke.
		_ = callCOM(object, 1) // IUnknown::AddRef
		webviewController = object
		desktopLogf("WebView2 controller created")
		// ICoreWebView2Controller::GetCoreWebView2 is vtable slot 25.
		var web uintptr
		hr := callCOM(object, 25, uintptr(unsafe.Pointer(&web)))
		if int32(hr) < 0 || web == 0 {
			desktopLogf("GetCoreWebView2 failed: HRESULT 0x%08X web=%#x", uint32(hr), web)
			return hr
		}
		_ = callCOM(web, 1) // IUnknown::AddRef
		webviewObject = web

		// Listen for WebUI theme changes so the native Windows caption remains
		// visually continuous with OnePane instead of reverting to a bright
		// system title bar. ICoreWebView2::add_WebMessageReceived is slot 34.
		themeHandler = &comHandler{Vtbl: &handlerVtbl, Refs: 1, Kind: 3}
		var themeToken int64
		if r := callCOM(web, 34, uintptr(unsafe.Pointer(themeHandler)), uintptr(unsafe.Pointer(&themeToken))); int32(r) < 0 {
			desktopLogf("AddWebMessageReceived failed: HRESULT 0x%08X", uint32(r))
		}

		// Make controller visible, size it, then navigate to the authenticated
		// OnePane control-plane UI (18181), never the preview ingress (18182).
		if r := callCOM(object, 4, 1); int32(r) < 0 {
			desktopLogf("PutIsVisible failed: HRESULT 0x%08X", uint32(r))
		}
		resizeWebView()
		uri, _ := syscall.UTF16PtrFromString(uiBaseURL + "/")
		hr = callCOM(web, 5, uintptr(unsafe.Pointer(uri))) // Navigate
		desktopLogf("WebView2 Navigate(%s/) returned HRESULT 0x%08X", uiBaseURL, uint32(hr))
		return hr
	}
	return 0
}

func callCOM(obj uintptr, slot int, args ...uintptr) uintptr {
	if obj == 0 {
		return 0x80004003
	}
	vtbl := *(*uintptr)(unsafe.Pointer(obj))
	proc := *(*uintptr)(unsafe.Pointer(vtbl + uintptr(slot)*unsafe.Sizeof(uintptr(0))))
	a := make([]uintptr, 0, 1+len(args))
	a = append(a, obj)
	a = append(a, args...)
	r, _, _ := syscall.SyscallN(proc, a...)
	return r
}

func resizeWebView() {
	if mainHwnd == 0 || webviewController == 0 {
		return
	}
	var r rect
	if ok, _, _ := pGetClientRect.Call(mainHwnd, uintptr(unsafe.Pointer(&r))); ok == 0 {
		return
	}
	_ = callCOM(webviewController, 6, uintptr(unsafe.Pointer(&r))) // PutBounds(RECT by value/indirect on Win64 ABI)
	_ = callCOM(webviewController, 23)                             // NotifyParentWindowPositionChanged
}

func findWebView2DLL() (string, error) {
	const subkey = `SOFTWARE\Microsoft\EdgeUpdate\ClientState\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`
	for _, root := range []uintptr{0xffffffff80000002, 0xffffffff80000001} { // HKLM, HKCU (sign-extended predefined handles)
		if base, err := regReadString(root, subkey, "EBWebView"); err == nil && base != "" {
			p := filepath.Join(base, "EBWebView", "x64", "EmbeddedBrowserWebView.dll")
			if _, err := os.Stat(p); err == nil {
				return p, nil
			}
		}
	}
	// Last-resort filesystem discovery for installations whose registry view is unusual.
	roots := []string{os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramFiles")}
	var candidates []string
	for _, root := range roots {
		if root == "" {
			continue
		}
		pattern := filepath.Join(root, "Microsoft", "EdgeWebView", "Application", "*", "EBWebView", "x64", "EmbeddedBrowserWebView.dll")
		m, _ := filepath.Glob(pattern)
		candidates = append(candidates, m...)
	}
	if len(candidates) > 0 {
		sort.Strings(candidates)
		return candidates[len(candidates)-1], nil
	}
	return "", fmt.Errorf("Microsoft Edge WebView2 Runtime was not found; repair the OnePane installation")
}

func regReadString(root uintptr, subkey, value string) (string, error) {
	sub, _ := syscall.UTF16PtrFromString(subkey)
	val, _ := syscall.UTF16PtrFromString(value)
	var key uintptr
	const keyRead = 0x20019
	const keyWow6432 = 0x0200
	r, _, _ := pRegOpenKeyEx.Call(root, uintptr(unsafe.Pointer(sub)), 0, keyRead|keyWow6432, uintptr(unsafe.Pointer(&key)))
	if r != 0 {
		return "", syscall.Errno(r)
	}
	defer pRegCloseKey.Call(key)
	var typ uint32
	var size uint32
	r, _, _ = pRegQueryValueEx.Call(key, uintptr(unsafe.Pointer(val)), 0, uintptr(unsafe.Pointer(&typ)), 0, uintptr(unsafe.Pointer(&size)))
	if r != 0 || size < 2 {
		return "", syscall.Errno(r)
	}
	buf := make([]uint16, (size+1)/2)
	r, _, _ = pRegQueryValueEx.Call(key, uintptr(unsafe.Pointer(val)), 0, uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r != 0 {
		return "", syscall.Errno(r)
	}
	n := 0
	for n < len(buf) && buf[n] != 0 {
		n++
	}
	return string(utf16.Decode(buf[:n])), nil
}

func loadOnePaneIcon(size int) uintptr {
	exe, err := os.Executable()
	if err != nil {
		return 0
	}
	path := filepath.Join(filepath.Dir(exe), "OnePane.ico")
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0
	}
	h, _, _ := pLoadImage.Call(0, uintptr(unsafe.Pointer(p)), imageIcon, uintptr(size), uintptr(size), lrLoadFromFile)
	return h
}

func applyInitialNativeTheme() {
	if windowsAppsUseLightTheme() {
		applyNativeTheme("light", "#ffffff", "#17202a", "#dbe3eb")
		return
	}
	applyNativeTheme("dark", "#0d1721", "#e7edf4", "#203142")
}

func handleWebMessage(message string) {
	if !strings.HasPrefix(message, "onepane-theme|") {
		return
	}
	parts := strings.Split(message, "|")
	if len(parts) < 5 {
		return
	}
	applyNativeTheme(parts[1], parts[2], parts[3], parts[4])
}

func applyNativeTheme(mode, captionHex, textHex, borderHex string) {
	if mainHwnd == 0 {
		return
	}
	dark := int32(0)
	if strings.EqualFold(mode, "dark") {
		dark = 1
	}
	// Windows 11 and current Windows 10 builds use attribute 20; some older
	// Windows 10 builds used 19. Applying both is harmless and gives us a
	// graceful compatibility path.
	r, _, _ := pDwmSetWindowAttribute.Call(mainHwnd, dwmwaUseImmersiveDarkMode, uintptr(unsafe.Pointer(&dark)), unsafe.Sizeof(dark))
	if int32(r) < 0 {
		pDwmSetWindowAttribute.Call(mainHwnd, dwmwaUseImmersiveDarkModeLegacy, uintptr(unsafe.Pointer(&dark)), unsafe.Sizeof(dark))
	}
	if c, ok := cssHexToColorRef(captionHex); ok {
		pDwmSetWindowAttribute.Call(mainHwnd, dwmwaCaptionColor, uintptr(unsafe.Pointer(&c)), unsafe.Sizeof(c))
	}
	if c, ok := cssHexToColorRef(textHex); ok {
		pDwmSetWindowAttribute.Call(mainHwnd, dwmwaTextColor, uintptr(unsafe.Pointer(&c)), unsafe.Sizeof(c))
	}
	if c, ok := cssHexToColorRef(borderHex); ok {
		pDwmSetWindowAttribute.Call(mainHwnd, dwmwaBorderColor, uintptr(unsafe.Pointer(&c)), unsafe.Sizeof(c))
	}
}

func cssHexToColorRef(s string) (uint32, bool) {
	s = strings.TrimSpace(strings.TrimPrefix(s, "#"))
	if len(s) != 6 {
		return 0, false
	}
	var rgb uint32
	if _, err := fmt.Sscanf(s, "%06x", &rgb); err != nil {
		return 0, false
	}
	r := rgb >> 16
	g := (rgb >> 8) & 0xff
	b := rgb & 0xff
	return (b << 16) | (g << 8) | r, true
}

func windowsAppsUseLightTheme() bool {
	const rootHKCU = uintptr(0xffffffff80000001)
	const subkey = `SOFTWARE\Microsoft\Windows\CurrentVersion\Themes\Personalize`
	sub, _ := syscall.UTF16PtrFromString(subkey)
	val, _ := syscall.UTF16PtrFromString("AppsUseLightTheme")
	var key uintptr
	const keyRead = 0x20019
	if r, _, _ := pRegOpenKeyEx.Call(rootHKCU, uintptr(unsafe.Pointer(sub)), 0, keyRead, uintptr(unsafe.Pointer(&key))); r != 0 {
		return false
	}
	defer pRegCloseKey.Call(key)
	var typ uint32
	var data uint32
	size := uint32(unsafe.Sizeof(data))
	if r, _, _ := pRegQueryValueEx.Call(key, uintptr(unsafe.Pointer(val)), 0, uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&data)), uintptr(unsafe.Pointer(&size))); r != 0 {
		return false
	}
	return data != 0
}

func utf16PtrToString(p *uint16) string {
	if p == nil {
		return ""
	}
	var u []uint16
	for i := uintptr(0); ; i++ {
		v := *(*uint16)(unsafe.Pointer(uintptr(unsafe.Pointer(p)) + i*2))
		if v == 0 {
			break
		}
		u = append(u, v)
	}
	return string(utf16.Decode(u))
}

func desktopLogf(format string, args ...any) {
	dir := filepath.Join(os.Getenv("LOCALAPPDATA"), "OnePane")
	if os.Getenv("LOCALAPPDATA") == "" {
		dir = filepath.Join(os.TempDir(), "OnePane")
	}
	_ = os.MkdirAll(dir, 0o755)
	f, err := os.OpenFile(filepath.Join(dir, "desktop.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s "+format+"\r\n", append([]any{time.Now().Format(time.RFC3339)}, args...)...)
}

func webViewDataDir() string {
	d := os.Getenv("LOCALAPPDATA")
	if d == "" {
		d = os.TempDir()
	}
	return filepath.Join(d, "OnePane", "WebView2")
}
func copyUTF16(dst []uint16, s string) {
	v := utf16.Encode([]rune(s))
	if len(v) >= len(dst) {
		v = v[:len(dst)-1]
	}
	copy(dst, v)
}
func fatalBox(s string) {
	text, _ := syscall.UTF16PtrFromString(s)
	title, _ := syscall.UTF16PtrFromString("OnePane")
	pMessageBox.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), 0x10)
}
