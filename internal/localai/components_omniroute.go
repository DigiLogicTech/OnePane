package localai

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
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

func (s *Service) omniRouteRuntimeRoot() string {
	return filepath.Join(s.dataDir, "runtimes", "omniroute", "3.8.51")
}
func (s *Service) omniRouteDataRoot() string {
	return filepath.Join(s.dataDir, "components", "omniroute-data")
}

func (s *Service) installOmniRoute(ctx context.Context, jobID string) error {
	s.omniRoute.mu.Lock()
	defer s.omniRoute.mu.Unlock()
	if err := s.stopOmniRouteLocked(ctx); err != nil {
		return err
	}
	if goruntime.GOOS == "windows" && goruntime.GOARCH == "amd64" {
		return s.installOmniRouteCLIWindows(ctx, jobID)
	}
	a, err := omniRouteArtifact()
	if err != nil {
		return err
	}

	if err := s.updateComponentProgress(ctx, jobID, "running", "downloading", "downloading", nil, false); err != nil {
		return err
	}
	root := s.omniRouteRuntimeRoot()
	staging := root + ".installing"
	_ = os.RemoveAll(staging)
	if err := os.MkdirAll(staging, 0o700); err != nil {
		return err
	}
	dest := filepath.Join(staging, a.Filename)
	if _, err := s.fetcher.Fetch(ctx, a.SourceURL, dest, a.SHA256); err != nil {
		_ = os.RemoveAll(staging)
		return err
	}
	if goruntime.GOOS != "windows" {
		if err := os.Chmod(dest, 0o700); err != nil {
			_ = os.RemoveAll(staging)
			return err
		}
	}
	if err := s.updateComponentProgress(ctx, jobID, "running", "installing", "installing", nil, false); err != nil {
		return err
	}

	backup := root + ".previous"
	_ = os.RemoveAll(backup)
	if _, err := os.Stat(root); err == nil {
		if err := os.Rename(root, backup); err != nil {
			return err
		}
	}
	if err := os.Rename(staging, root); err != nil {
		if _, e := os.Stat(backup); e == nil {
			_ = os.Rename(backup, root)
		}
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
	if err != nil {
		return "", err
	}
	p := filepath.Join(s.omniRouteRuntimeRoot(), a.Filename)
	st, err := os.Stat(p)
	if err != nil || st.IsDir() {
		return "", errors.New("managed OmniRoute executable is missing")
	}
	return p, nil
}

func (s *Service) omniRouteHealthy() bool {
	client := &http.Client{Timeout: 1500 * time.Millisecond, Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get("http://127.0.0.1:20128/healthz")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return omniRouteHealthResponse(resp)
}

// OmniRoute 3.8.51 /healthz returns a plain-text lifecycle token, not JSON.
// HTTP 200 + "ok" is ready; "starting" and "stopping" are deliberately not ready.
func omniRouteHealthResponse(resp *http.Response) bool {
	if resp == nil || resp.StatusCode != http.StatusOK {
		return false
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256))
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(string(body)), "ok")
}

var errOmniRouteOwnershipDeferred = errors.New("managed OmniRoute ownership requires operator reconciliation")
var errOmniRouteRecoveryDeferred = errors.New("optional managed OmniRoute recovery durably deferred")
var errOmniRouteStateChanged = errors.New("managed OmniRoute state changed during lifecycle operation")
var errOmniRouteUnavailable = errors.New("managed OmniRoute runtime unavailable; inspect private component diagnostics")

// Persisted PIDs and a public health response never grant process authority.
// Only this service's retained launch object may be signalled; reconstruction
// deliberately loses that authority and leaves the original record intact.
type omniRouteProcessState struct {
	mu   sync.Mutex
	cmd  *exec.Cmd
	done chan struct{}
}

type omniRouteObservation struct {
	installed           sql.NullString
	desired, state, raw string
	revision            int64
	metadata            map[string]json.RawMessage
	pid                 int
}

func (s *Service) readOmniRoute(ctx context.Context) (omniRouteObservation, error) {
	var o omniRouteObservation
	err := s.db.QueryRowContext(ctx, `SELECT installed_version,desired_state,observed_state,metadata_json,revision FROM managed_component_states WHERE component_id='omniroute'`).Scan(&o.installed, &o.desired, &o.state, &o.raw, &o.revision)
	if err != nil {
		return o, err
	}
	if json.Unmarshal([]byte(o.raw), &o.metadata) != nil || o.metadata == nil {
		return o, errors.New("managed OmniRoute metadata is invalid; operator reconciliation required")
	}
	if raw, ok := o.metadata["pid"]; ok {
		if json.Unmarshal(raw, &o.pid) != nil || o.pid <= 1 {
			return o, errors.New("managed OmniRoute PID metadata is invalid; operator reconciliation required")
		}
	}
	return o, nil
}

func (s *Service) ownsOmniRoute(pid int) bool {
	owner := &s.omniRoute
	if owner.cmd == nil || owner.cmd.Process == nil || owner.cmd.Process.Pid != pid || owner.done == nil {
		return false
	}
	select {
	case <-owner.done:
		return false
	default:
		return true
	}
}

func (s *Service) writeOmniRoute(ctx context.Context, o omniRouteObservation, desired, state string, metadata map[string]json.RawMessage, lastError any) error {
	raw, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE managed_component_states SET desired_state=?,observed_state=?,metadata_json=?,last_error=?,revision=revision+1,updated_at=? WHERE component_id='omniroute' AND revision=? AND metadata_json=?`, desired, state, string(raw), lastError, s.clock.UnixMilli(), o.revision, o.raw)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errOmniRouteStateChanged
	}
	return nil
}

func omniRouteEnvironment(dataRoot string) []string {
	cacheDir := filepath.Join(dataRoot, "cache")
	env := managedRuntimeHostEnvironment(os.Environ())
	// Host directories and arbitrary credential/proxy/Node options must not
	// escape into the local routing service. All writable caches are scoped.
	filtered := env[:0]
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(name) {
		case "TEMP", "TMP", "TMPDIR":
		default:
			filtered = append(filtered, entry)
		}
	}
	return append(filtered,
		"HOME="+dataRoot, "USERPROFILE="+dataRoot, "APPDATA="+dataRoot,
		"XDG_CONFIG_HOME="+dataRoot, "XDG_CACHE_HOME="+cacheDir,
		"NODE_COMPILE_CACHE="+filepath.Join(cacheDir, "node-compile"),
		"TEMP="+cacheDir, "TMP="+cacheDir, "TMPDIR="+cacheDir,
		"OMNIROUTE_CLI_SKIP_REPO_ENV=1", "OMNIROUTE_NO_UPDATE_NOTIFIER=1",
		"DATA_DIR="+dataRoot, "OMNIROUTE_DATA_DIR="+dataRoot,
		"OMNIROUTE_SERVER_HOST=127.0.0.1", "APP_BIND_HOST=127.0.0.1", "HOST=127.0.0.1",
		"OMNIROUTE_PORT=20128", "PORT=20128", "REQUIRE_API_KEY=false",
		"NODE_ENV=production", "APP_LOG_TO_FILE=false", "APPIMAGE_EXTRACT_AND_RUN=1")
}

func (s *Service) startOmniRoute(ctx context.Context) error {
	s.omniRoute.mu.Lock()
	defer s.omniRoute.mu.Unlock()
	return s.startOmniRouteLocked(ctx)
}

func (s *Service) startOmniRouteLocked(ctx context.Context) error {
	o, err := s.readOmniRoute(ctx)
	if err != nil {
		return err
	}
	if !o.installed.Valid {
		return errors.New("managed OmniRoute is not installed")
	}
	if err = s.admitOmniRouteStart(o, s.omniRouteHealthy); err != nil {
		return err
	}
	if o.pid != 0 {
		if o.desired != "enabled" || o.state != "running" {
			return s.writeOmniRoute(ctx, o, "enabled", "running", o.metadata, nil)
		}
		return nil
	}
	var exe string
	var args []string
	if goruntime.GOOS == "windows" {
		exe, args, err = s.omniRouteCLICommand()
	} else {
		exe, err = s.omniRouteExecutable()
		args = []string{"--no-open", "--port", "20128"}
	}
	if err != nil {
		return errOmniRouteUnavailable
	}
	if err = os.MkdirAll(filepath.Join(s.omniRouteDataRoot(), "cache"), 0o700); err != nil {
		return err
	}
	return s.launchOmniRoute(ctx, o, LaunchSpec{Executable: exe, Args: args, Env: omniRouteEnvironment(s.omniRouteDataRoot()), Dir: s.omniRouteDataRoot(), LogPath: filepath.Join(s.dataDir, "components", "logs", "omniroute.log")}, s.omniRouteHealthy)
}

func (s *Service) admitOmniRouteStart(o omniRouteObservation, healthy func() bool) error {
	if o.pid != 0 {
		if !s.ownsOmniRoute(o.pid) {
			return errOmniRouteOwnershipDeferred
		}
		if !healthy() || !s.ownsOmniRoute(o.pid) {
			return errOmniRouteOwnershipDeferred
		}
		return nil
	}
	if s.omniRoute.cmd != nil || o.desired == "enabled" || o.state == "running" || o.state == "enabling" || o.state == "disabling" {
		return errOmniRouteOwnershipDeferred
	}
	// A healthy listener that predates this launch is never adopted.
	if healthy() {
		return errOmniRouteOwnershipDeferred
	}
	return nil
}

// Caller holds the component mutex. The probe is fixed in production; focused
// subprocess tests replace readiness only, never the process ownership check.
func (s *Service) launchOmniRoute(ctx context.Context, o omniRouteObservation, spec LaunchSpec, healthy func() bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	logf, err := openManagedRuntimeLog(spec.LogPath)
	if err != nil {
		return err
	}
	writer, err := newBoundedRuntimeLogWriter(logf)
	if err != nil {
		_ = logf.Close()
		return err
	}
	cmd := exec.Command(spec.Executable, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = append([]string{}, spec.Env...)
	cmd.Stdout, cmd.Stderr = writer, writer
	cmd.WaitDelay = 5 * time.Second
	closeRuntime, err := s.prepareManagedCommand(cmd, spec.LogPath, spec.Dir, spec.Executable)
	if err != nil {
		_ = logf.Close()
		return err
	}
	defer closeRuntime()
	if err = cmd.Start(); err != nil {
		_ = logf.Close()
		return errOmniRouteUnavailable
	}
	s.omniRoute.cmd = cmd
	s.omniRoute.done = make(chan struct{})
	done := s.omniRoute.done
	go func() { _ = cmd.Wait(); _ = logf.Close(); close(done) }()
	pid := cmd.Process.Pid
	o.metadata["pid"], _ = json.Marshal(pid)
	o.metadata["started_at"], _ = json.Marshal(s.clock.UnixMilli())
	o.metadata["endpoint"], _ = json.Marshal("http://127.0.0.1:20128/v1")
	// Record ownership before waiting on HTTP. A rejected PID write never
	// advertises running and stops only this exact retained launch object.
	if err = s.writeOmniRoute(ctx, o, "enabled", "enabling", o.metadata, nil); err != nil {
		return errors.Join(err, s.killOwnedOmniRoute(context.Background()))
	}
	o, err = s.readOmniRoute(ctx)
	if err != nil {
		return errors.Join(err, s.killOwnedOmniRoute(context.Background()))
	}
	timer := time.NewTimer(120 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if !s.ownsOmniRoute(pid) {
			return errOmniRouteOwnershipDeferred
		}
		if healthy() && s.ownsOmniRoute(pid) {
			if err = s.writeOmniRoute(ctx, o, "enabled", "running", o.metadata, nil); err != nil {
				return errors.Join(err, s.killOwnedOmniRoute(context.Background()))
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.Join(ctx.Err(), s.killOwnedOmniRoute(context.Background()))
		case <-done:
			return errOmniRouteUnavailable
		case <-timer.C:
			return errors.Join(errOmniRouteUnavailable, s.killOwnedOmniRoute(context.Background()))
		case <-ticker.C:
		}
	}
}

func (s *Service) killOwnedOmniRoute(ctx context.Context) error {
	owner := &s.omniRoute
	if owner.cmd == nil || owner.cmd.Process == nil || owner.done == nil {
		return errOmniRouteOwnershipDeferred
	}
	select {
	case <-owner.done:
		return errOmniRouteOwnershipDeferred
	default:
	}
	// Retain exec.Cmd's actual process object rather than reopening a PID.
	if err := owner.cmd.Process.Kill(); err != nil {
		return errOmniRouteOwnershipDeferred
	}
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return errOmniRouteOwnershipDeferred
	case <-owner.done:
		return nil
	}
}

func (s *Service) stopOmniRoute(ctx context.Context) error {
	s.omniRoute.mu.Lock()
	defer s.omniRoute.mu.Unlock()
	return s.stopOmniRouteLocked(ctx)
}

func (s *Service) stopOmniRouteLocked(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	o, err := s.readOmniRoute(ctx)
	if err != nil {
		return err
	}
	owner := &s.omniRoute
	if owner.cmd != nil {
		if owner.cmd.Process == nil || owner.done == nil || (o.pid != 0 && o.pid != owner.cmd.Process.Pid) {
			return errOmniRouteOwnershipDeferred
		}
		// The retained generation's completed Wait is independent evidence that
		// this exact parent exited. It permits cleanup/retry after a rejected
		// state write, even when initial PID persistence was rejected. Never
		// reopen or signal its historical PID. This does not prove child-tree
		// resource release; that remains separate physical lifecycle acceptance.
		select {
		case <-owner.done:
		default:
			if err = s.killOwnedOmniRoute(ctx); err != nil {
				return err
			}
		}
	} else if o.pid != 0 || o.desired == "enabled" || o.state == "running" || o.state == "enabling" || o.state == "disabling" {
		return errOmniRouteOwnershipDeferred
	}
	if o.installed.Valid {
		delete(o.metadata, "pid")
		if err = s.writeOmniRoute(ctx, o, "disabled", "installed_disabled", o.metadata, nil); err != nil {
			return err
		}
	}
	s.omniRoute.cmd = nil
	s.omniRoute.done = nil
	return nil
}

func (s *Service) removeOmniRoute(ctx context.Context) error {
	s.omniRoute.mu.Lock()
	defer s.omniRoute.mu.Unlock()
	if err := s.stopOmniRouteLocked(ctx); err != nil {
		return err
	}
	if err := os.RemoveAll(s.omniRouteRuntimeRoot()); err != nil {
		return err
	}
	now := s.clock.UnixMilli()
	_, err := s.db.ExecContext(ctx, `UPDATE managed_component_states SET installed_version=NULL,desired_state='removed',observed_state='removed',last_error=NULL,metadata_json=json_set(COALESCE(metadata_json,'{}'),'$.preserved_data_root',?),revision=revision+1,updated_at=? WHERE component_id='omniroute'`, s.omniRouteDataRoot(), now)
	return err
}

func (s *Service) recoverOmniRoute(ctx context.Context) error {
	s.omniRoute.mu.Lock()
	defer s.omniRoute.mu.Unlock()
	o, err := s.readOmniRoute(ctx)
	if err != nil {
		return err
	}
	if !o.installed.Valid || o.desired != "enabled" {
		return nil
	}
	if err = s.startOmniRouteLocked(ctx); err != nil {
		if !errors.Is(err, errOmniRouteOwnershipDeferred) && !errors.Is(err, errOmniRouteUnavailable) {
			return err
		}
		// Safe fixed diagnostics only. Preserve PID, provenance and roots, and
		// propagate a failed degraded write instead of pretending recovery ran.
		current, readErr := s.readOmniRoute(ctx)
		if readErr != nil {
			return readErr
		}
		if writeErr := s.writeOmniRoute(ctx, current, current.desired, "degraded", current.metadata, "Managed OmniRoute requires operator reconciliation; no recovered process was signalled"); writeErr != nil {
			return writeErr
		}
		return errors.Join(errOmniRouteRecoveryDeferred, err)
	}
	return nil
}
