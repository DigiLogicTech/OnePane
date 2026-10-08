package localai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
 "io"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"time"
)

type OmniRouteArtifact struct {
	Version   string
	SourceURL string
	SHA256    string
	Filename  string
}

func omniRouteArtifact() (OmniRouteArtifact, error) {
	const version = "3.8.51"
	switch {
	case goruntime.GOOS == "windows" && goruntime.GOARCH == "amd64":
		return OmniRouteArtifact{Version: version, SourceURL: "https://github.com/diegosouzapw/OmniRoute/releases/download/v3.8.51/OmniRoute.exe", SHA256: "5d49b514a485266af23266e4092fade2ae0a47b8de11cc91901dac1caf47476e", Filename: "omniroute.exe"}, nil
	case goruntime.GOOS == "linux" && goruntime.GOARCH == "amd64":
		return OmniRouteArtifact{Version: version, SourceURL: "https://github.com/diegosouzapw/OmniRoute/releases/download/v3.8.51/OmniRoute-3.8.51.AppImage", SHA256: "1d4c6c9562c65fbe8833624e570b5ae7255fbfaa032b8fe44524e9adf63ce024", Filename: "omniroute.AppImage"}, nil
	default:
		return OmniRouteArtifact{}, fmt.Errorf("OmniRoute %s/%s is not supported by the Alpha 3.1 managed installer", goruntime.GOOS, goruntime.GOARCH)
	}
}

func (s *Service) omniRouteRuntimeRoot() string { return filepath.Join(s.dataDir, "runtimes", "omniroute", "3.8.51") }
func (s *Service) omniRouteDataRoot() string { return filepath.Join(s.dataDir, "components", "omniroute-data") }

func (s *Service) installOmniRoute(ctx context.Context, jobID string) error {
	if goruntime.GOOS=="windows" && goruntime.GOARCH=="amd64" { return s.installOmniRouteCLIWindows(ctx,jobID) }
	a, err := omniRouteArtifact()
	if err != nil { return err }
	_ = s.stopOmniRoute(ctx)

	if err := s.updateComponentProgress(ctx, jobID, "running", "downloading", "downloading", nil, false); err != nil { return err }
	root := s.omniRouteRuntimeRoot()
	staging := root + ".installing"
	_ = os.RemoveAll(staging)
	if err := os.MkdirAll(staging, 0o700); err != nil { return err }
	dest := filepath.Join(staging, a.Filename)
	if _, err := s.fetcher.Fetch(ctx, a.SourceURL, dest, a.SHA256); err != nil { _ = os.RemoveAll(staging); return err }
	if goruntime.GOOS != "windows" {
		if err := os.Chmod(dest, 0o700); err != nil { _ = os.RemoveAll(staging); return err }
	}
	if err := s.updateComponentProgress(ctx, jobID, "running", "installing", "installing", nil, false); err != nil { return err }

	backup := root + ".previous"
	_ = os.RemoveAll(backup)
	if _, err := os.Stat(root); err == nil {
		if err := os.Rename(root, backup); err != nil { return err }
	}
	if err := os.Rename(staging, root); err != nil {
		if _, e := os.Stat(backup); e == nil { _ = os.Rename(backup, root) }
		return err
	}
	_ = os.RemoveAll(backup)

	now := s.clock.UnixMilli()
	meta, _ := json.Marshal(map[string]any{
		"source_url": a.SourceURL, "sha256": a.SHA256, "executable": a.Filename,
		"endpoint": "http://127.0.0.1:20128/v1", "data_root": s.omniRouteDataRoot(),
	})
	_, err = s.db.ExecContext(ctx, `UPDATE managed_component_states
		SET installed_version=?,available_version=?,desired_state='disabled',observed_state='installed_disabled',last_error=NULL,metadata_json=?,revision=revision+1,updated_at=?
		WHERE component_id='omniroute'`, a.Version, a.Version, string(meta), now)
	return err
}
func (s *Service) omniRouteExecutable() (string, error) {
	a, err := omniRouteArtifact()
	if err != nil { return "", err }
	p := filepath.Join(s.omniRouteRuntimeRoot(), a.Filename)
	st, err := os.Stat(p)
	if err != nil || st.IsDir() { return "", errors.New("managed OmniRoute executable is missing") }
	return p, nil
}
func (s *Service) omniRoutePID(ctx context.Context) (int, error) {
	var raw string
	if err := s.db.QueryRowContext(ctx, `SELECT metadata_json FROM managed_component_states WHERE component_id='omniroute'`).Scan(&raw); err != nil { return 0, err }
	m := map[string]any{}
	_ = json.Unmarshal([]byte(raw), &m)
	switch x := m["pid"].(type) {
	case float64:
		if int(x) > 0 { return int(x), nil }
	case string:
		if n, _ := strconv.Atoi(x); n > 0 { return n, nil }
	}
	return 0, errors.New("OmniRoute is not running")
}
func (s *Service) omniRouteHealthy() bool {
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	resp, err := client.Get("http://127.0.0.1:20128/healthz")
	if err != nil { return false }
	defer resp.Body.Close()
	return omniRouteHealthResponse(resp)
}
// OmniRoute 3.8.51 /healthz returns a plain-text lifecycle token, not JSON.
// HTTP 200 + "ok" is ready; "starting" and "stopping" are deliberately not ready.
func omniRouteHealthResponse(resp *http.Response) bool {
 if resp==nil || resp.StatusCode!=http.StatusOK {return false}
 body,err:=io.ReadAll(io.LimitReader(resp.Body,256));if err!=nil{return false}
 return strings.EqualFold(strings.TrimSpace(string(body)),"ok")
}
func (s *Service) omniRouteLogTail(limit int64) string {
	if limit <= 0 { limit = 4096 }
	path := filepath.Join(s.dataDir, "components", "logs", "omniroute.log")
	f, err := os.Open(path)
	if err != nil { return "" }
	defer f.Close()
	st, err := f.Stat()
	if err != nil { return "" }
	start := st.Size() - limit
	if start < 0 { start = 0 }
	if _, err := f.Seek(start, 0); err != nil { return "" }
	buf := make([]byte, st.Size()-start)
	n, _ := f.Read(buf)
	text := strings.TrimSpace(string(buf[:n]))
	if text == "" { return "" }
	if len(text) > 1200 { text = text[len(text)-1200:] }
	return ": " + text
}
func (s *Service) startOmniRoute(ctx context.Context) error {
	if s.omniRouteHealthy() { return nil }
	if pid, err := s.omniRoutePID(ctx); err == nil && pid > 0 {
		if p, err := os.FindProcess(pid); err == nil { _ = p.Kill() }
	}
	var exe string
	var args []string
	var err error
	if goruntime.GOOS=="windows" {
		exe,args,err=s.omniRouteCLICommand()
	} else {
		exe,err=s.omniRouteExecutable()
		args=[]string{"--no-open","--port","20128"}
	}
	if err != nil { return err }
	if err := os.MkdirAll(s.omniRouteDataRoot(), 0o700); err != nil { return err }
	cacheDir:=filepath.Join(s.omniRouteDataRoot(),"cache")
	if err:=os.MkdirAll(cacheDir,0o700);err!=nil{return err}
	logDir := filepath.Join(s.dataDir, "components", "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil { return err }
	logf, err := os.OpenFile(filepath.Join(logDir, "omniroute.log"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil { return err }

	cmd := exec.Command(exe, args...)
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.Env = append(os.Environ(),
		"HOME="+s.omniRouteDataRoot(),
		"USERPROFILE="+s.omniRouteDataRoot(),
		"APPDATA="+s.omniRouteDataRoot(),
		"XDG_CONFIG_HOME="+s.omniRouteDataRoot(),
		"XDG_CACHE_HOME="+cacheDir,
		"NODE_COMPILE_CACHE="+filepath.Join(cacheDir,"node-compile"),
		"OMNIROUTE_CLI_SKIP_REPO_ENV=1",
		"OMNIROUTE_NO_UPDATE_NOTIFIER=1",
		"DATA_DIR="+s.omniRouteDataRoot(),
		"OMNIROUTE_DATA_DIR="+s.omniRouteDataRoot(),
		"OMNIROUTE_SERVER_HOST=127.0.0.1",
  "APP_BIND_HOST=127.0.0.1",
  "HOST=127.0.0.1",
		"OMNIROUTE_PORT=20128",
		"PORT=20128",
		"REQUIRE_API_KEY=false",
		"NODE_ENV=production",
		"APP_LOG_TO_FILE=false",
		"APPIMAGE_EXTRACT_AND_RUN=1",
	)
	if err := cmd.Start(); err != nil { _ = logf.Close(); return err }
	_ = logf.Close()
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()

	healthy := false
	for i := 0; i < 120; i++ {
		select {
		case <-ctx.Done():
			_ = cmd.Process.Kill()
			return ctx.Err()
		case err := <-waitCh:
			tail := s.omniRouteLogTail(4096)
			if err == nil { err = errors.New("process exited before becoming healthy") }
			return fmt.Errorf("managed OmniRoute exited during startup: %v%s", err, tail)
		case <-time.After(time.Second):
		}
		if s.omniRouteHealthy() { healthy = true; break }
	}
	if !healthy {
		_ = cmd.Process.Kill()
		tail := s.omniRouteLogTail(4096)
		return fmt.Errorf("managed OmniRoute readiness /healthz did not report status ok at 127.0.0.1:20128 within 120s; inspect component logs%s", tail)
	}

	var raw string
	_ = s.db.QueryRowContext(ctx, `SELECT metadata_json FROM managed_component_states WHERE component_id='omniroute'`).Scan(&raw)
	m := map[string]any{}
	_ = json.Unmarshal([]byte(raw), &m)
	m["pid"] = cmd.Process.Pid
	m["endpoint"] = "http://127.0.0.1:20128/v1"
	m["started_at"] = s.clock.UnixMilli()
	meta, _ := json.Marshal(m)
	now := s.clock.UnixMilli()
	_, err = s.db.ExecContext(ctx, `UPDATE managed_component_states SET desired_state='enabled',observed_state='running',last_error=NULL,metadata_json=?,revision=revision+1,updated_at=? WHERE component_id='omniroute'`, string(meta), now)
	return err
}
func (s *Service) stopOmniRoute(ctx context.Context) error {
	pid, err := s.omniRoutePID(ctx)
	if err == nil && pid > 0 {
		if p, e := os.FindProcess(pid); e == nil { _ = p.Kill() }
	}
	var raw string
	_ = s.db.QueryRowContext(ctx, `SELECT metadata_json FROM managed_component_states WHERE component_id='omniroute'`).Scan(&raw)
	m := map[string]any{}
	_ = json.Unmarshal([]byte(raw), &m)
	delete(m, "pid")
	meta, _ := json.Marshal(m)
	now := s.clock.UnixMilli()
	_, err = s.db.ExecContext(ctx, `UPDATE managed_component_states SET desired_state='disabled',observed_state='installed_disabled',metadata_json=?,revision=revision+1,updated_at=? WHERE component_id='omniroute' AND installed_version IS NOT NULL`, string(meta), now)
	return err
}
func (s *Service) removeOmniRoute(ctx context.Context) error {
	_ = s.stopOmniRoute(ctx)
	if err := os.RemoveAll(s.omniRouteRuntimeRoot()); err != nil { return err }
	now := s.clock.UnixMilli()
	_, err := s.db.ExecContext(ctx, `UPDATE managed_component_states SET installed_version=NULL,desired_state='removed',observed_state='removed',last_error=NULL,active_job_id=NULL,
		metadata_json=json_set(COALESCE(metadata_json,'{}'),'$.preserved_data_root',?),revision=revision+1,updated_at=? WHERE component_id='omniroute'`,
		s.omniRouteDataRoot(), now)
	return err
}
func (s *Service) recoverOmniRoute(ctx context.Context) error {
	all, err := s.ManagedComponents(ctx)
	if err != nil { return err }
	o, ok := all["omniroute"]
	if !ok || !o.Installed || o.DesiredState != "enabled" { return nil }
	if err := s.startOmniRoute(ctx); err != nil {
		msg := strings.TrimSpace(err.Error())
		now := s.clock.UnixMilli()
		_, _ = s.db.ExecContext(ctx, `UPDATE managed_component_states SET observed_state='degraded',last_error=?,revision=revision+1,updated_at=? WHERE component_id='omniroute'`, msg, now)
		return err
	}
	return nil
}
