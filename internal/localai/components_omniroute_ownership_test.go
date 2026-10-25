package localai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestOmniRouteDisposableChild(t *testing.T) {
	if os.Getenv("ONEPANE_OMNIROUTE_TEST_CHILD") == "" {
		return
	}
	if os.Getenv("ONEPANE_OMNIROUTE_TEST_CHILD") == "exit" {
		fmt.Fprintln(os.Stderr, "PRIVATE_OMNIROUTE_RUNTIME_TOKEN")
		os.Exit(2)
	}
	time.Sleep(30 * time.Second)
	os.Exit(0)
}

func omniRouteLaunchFixture(t *testing.T) (*Service, context.Context, omniRouteObservation, LaunchSpec) {
	t.Helper()
	s, ctx := componentTestService(t)
	if _, err := s.ManagedComponents(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE managed_component_states SET installed_version='3.8.51',observed_state='installed_disabled' WHERE component_id='omniroute'`); err != nil {
		t.Fatal(err)
	}
	o, err := s.readOmniRoute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	spec := LaunchSpec{Executable: exe, Args: []string{"-test.run=^TestOmniRouteDisposableChild$"}, Env: []string{"ONEPANE_OMNIROUTE_TEST_CHILD=1"}, Dir: s.dataDir, LogPath: filepath.Join(s.dataDir, "components", "logs", "omniroute.log")}
	t.Cleanup(func() {
		s.omniRoute.mu.Lock()
		defer s.omniRoute.mu.Unlock()
		if s.omniRoute.cmd != nil {
			_ = s.omniRoute.cmd.Process.Kill()
			if s.omniRoute.done != nil {
				<-s.omniRoute.done
			}
		}
	})
	return s, ctx, o, spec
}

func TestOmniRouteOwnedLaunchStopAndConcurrentStop(t *testing.T) {
	s, ctx, o, spec := omniRouteLaunchFixture(t)
	if err := s.launchOmniRoute(ctx, o, spec, func() bool { return true }); err != nil {
		t.Fatal(err)
	}
	pid := s.omniRoute.cmd.Process.Pid
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- s.stopOmniRoute(ctx) }()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	current, err := s.readOmniRoute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if current.pid != 0 || current.state != "installed_disabled" || s.omniRoute.cmd != nil {
		t.Fatalf("owned stop not settled: pid=%d original=%d state=%s", current.pid, pid, current.state)
	}
}

func TestOmniRouteOwnedExitAllowsPersistenceRetry(t *testing.T) {
	for _, failure := range []string{"initial_pid_write", "final_running_write", "stopped_write", "spontaneous_exit"} {
		t.Run(failure, func(t *testing.T) {
			s, ctx, o, spec := omniRouteLaunchFixture(t)
			if failure == "initial_pid_write" || failure == "final_running_write" {
				state := "enabling"
				if failure == "final_running_write" {
					state = "running"
				}
				statement := fmt.Sprintf(`CREATE TRIGGER reject_omniroute_state BEFORE UPDATE ON managed_component_states WHEN NEW.component_id='omniroute' AND NEW.observed_state='%s' BEGIN SELECT RAISE(ABORT,'reject private lifecycle state'); END`, state)
				if _, err := s.db.ExecContext(ctx, statement); err != nil {
					t.Fatal(err)
				}
			}
			if failure == "spontaneous_exit" {
				spec.Env = []string{"ONEPANE_OMNIROUTE_TEST_CHILD=exit"}
			}
			err := s.launchOmniRoute(ctx, o, spec, func() bool { return failure != "spontaneous_exit" })
			if failure == "initial_pid_write" || failure == "final_running_write" || failure == "spontaneous_exit" {
				if err == nil {
					t.Fatal("rejected/exited launch reported successful")
				}
				if strings.Contains(err.Error(), "PRIVATE_OMNIROUTE_RUNTIME_TOKEN") {
					t.Fatal("private raw runtime log leaked in error")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if failure == "initial_pid_write" || failure == "final_running_write" {
				if _, err = s.db.ExecContext(ctx, `DROP TRIGGER reject_omniroute_state`); err != nil {
					t.Fatal(err)
				}
			}
			if failure == "stopped_write" {
				if _, err = s.db.ExecContext(ctx, `CREATE TRIGGER reject_omniroute_state BEFORE UPDATE ON managed_component_states WHEN NEW.component_id='omniroute' AND NEW.observed_state='installed_disabled' BEGIN SELECT RAISE(ABORT,'reject disabled state'); END`); err != nil {
					t.Fatal(err)
				}
				if err = s.stopOmniRoute(ctx); err == nil {
					t.Fatal("rejected stopped state was reported persisted")
				}
				if _, err = s.db.ExecContext(ctx, `DROP TRIGGER reject_omniroute_state`); err != nil {
					t.Fatal(err)
				}
			}
			if s.omniRoute.done == nil {
				t.Fatal("lost exact owned process generation")
			}
			select {
			case <-s.omniRoute.done:
			case <-time.After(5 * time.Second):
				t.Fatal("owned process was not stopped")
			}
			if err = s.stopOmniRoute(ctx); err != nil {
				t.Fatalf("known exited owner could not settle safely: %v", err)
			}
			current, err := s.readOmniRoute(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if current.pid != 0 || current.state != "installed_disabled" || s.omniRoute.cmd != nil {
				t.Fatalf("known exit retained false running owner: %+v", current)
			}
		})
	}
}

func TestOmniRouteStartAdmissionNeverAdoptsUnownedHealth(t *testing.T) {
	for _, tc := range []struct {
		name           string
		pid            int
		desired, state string
		healthy        bool
		calls          int
	}{
		{"foreign_pid", 12345, "enabled", "running", true, 0},
		{"healthy_listener", 0, "disabled", "installed_disabled", true, 1},
		{"missing_pid_running", 0, "enabled", "running", true, 0},
		{"lost_pid_interrupted", 0, "enabled", "interrupted", false, 0},
		{"fresh_disabled", 0, "disabled", "installed_disabled", false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Service{}
			calls := 0
			err := s.admitOmniRouteStart(omniRouteObservation{pid: tc.pid, desired: tc.desired, state: tc.state}, func() bool { calls++; return tc.healthy })
			if tc.name == "fresh_disabled" {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, errOmniRouteOwnershipDeferred) {
				t.Fatalf("unowned start admitted: %v", err)
			}
			if calls != tc.calls {
				t.Fatalf("health used before authority: calls=%d want=%d", calls, tc.calls)
			}
		})
	}
}

func TestOmniRouteLaunchEnvironmentDoesNotInheritServiceSecrets(t *testing.T) {
	for _, key := range []string{"OPENAI_API_KEY", "HTTP_PROXY", "HTTPS_PROXY", "NODE_OPTIONS", "AWS_SECRET_ACCESS_KEY", "OMNIROUTE_API_KEY"} {
		t.Setenv(key, "PRIVATE_OMNIROUTE_SERVICE_SECRET")
	}
	root := t.TempDir()
	env := omniRouteEnvironment(root)
	for _, entry := range env {
		if strings.Contains(entry, "PRIVATE_OMNIROUTE_SERVICE_SECRET") {
			t.Fatalf("service secret inherited: %s", entry)
		}
	}
	for _, want := range []string{"HOME=" + root, "DATA_DIR=" + root, "TEMP=" + filepath.Join(root, "cache")} {
		found := false
		for _, entry := range env {
			if entry == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing owned environment %s", want)
		}
	}
}

func omniRouteForeignChild(t *testing.T) (*exec.Cmd, <-chan error) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestOmniRouteDisposableChild$")
	cmd.Env = []string{"ONEPANE_OMNIROUTE_TEST_CHILD=1"}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait(); close(done) }()
	t.Cleanup(func() { _ = cmd.Process.Kill(); <-done })
	return cmd, done
}

func TestOmniRouteRecoveredPIDCannotSignalForeignProcess(t *testing.T) {
	s, ctx := componentTestService(t)
	if _, err := s.ManagedComponents(ctx); err != nil {
		t.Fatal(err)
	}
	child, done := omniRouteForeignChild(t)
	metadata, _ := json.Marshal(map[string]any{"pid": child.Process.Pid, "started_at": 1, "private_marker": "preserve"})
	if _, err := s.db.ExecContext(ctx, `UPDATE managed_component_states SET installed_version='3.8.51',desired_state='enabled',observed_state='running',metadata_json=? WHERE component_id='omniroute'`, string(metadata)); err != nil {
		t.Fatal(err)
	}
	err := s.stopOmniRoute(context.Background())
	if err == nil {
		t.Error("recovered foreign PID was reported stopped without owned launch")
	}
	select {
	case <-done:
		t.Error("foreign disposable child was killed from persisted PID")
	case <-time.After(100 * time.Millisecond):
	}
	var actual, state string
	if err := s.db.QueryRowContext(ctx, `SELECT metadata_json,observed_state FROM managed_component_states WHERE component_id='omniroute'`).Scan(&actual, &state); err != nil {
		t.Fatal(err)
	}
	if actual != string(metadata) || state != "running" {
		t.Errorf("unknown owner erased: state=%s metadata=%s", state, actual)
	}
}

func TestOmniRouteUnknownOwnerPreventsInstallAndRemoval(t *testing.T) {
	for _, action := range []string{"install", "remove", "start"} {
		t.Run(action, func(t *testing.T) {
			s, ctx, o, _ := omniRouteLaunchFixture(t)
			child, done := omniRouteForeignChild(t)
			runtimeRoot := s.omniRouteRuntimeRoot()
			if err := os.MkdirAll(runtimeRoot, 0o700); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(runtimeRoot, "preserve-installed-runtime")
			if err := os.WriteFile(marker, []byte("original installed bytes"), 0o600); err != nil {
				t.Fatal(err)
			}
			o.metadata["pid"], _ = json.Marshal(child.Process.Pid)
			if err := s.writeOmniRoute(ctx, o, "enabled", "running", o.metadata, nil); err != nil {
				t.Fatal(err)
			}
			before, err := s.readOmniRoute(ctx)
			if err != nil {
				t.Fatal(err)
			}
			switch action {
			case "install":
				err = s.installOmniRoute(ctx, "no-real-job-no-download")
			case "remove":
				err = s.removeOmniRoute(ctx)
			case "start":
				err = s.startOmniRoute(ctx)
			}
			if !errors.Is(err, errOmniRouteOwnershipDeferred) {
				t.Fatalf("unknown owner admitted %s: %v", action, err)
			}
			after, err := s.readOmniRoute(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if after.raw != before.raw || after.revision != before.revision || after.state != before.state {
				t.Fatalf("%s rewrote unknown owner", action)
			}
			bytes, err := os.ReadFile(marker)
			if err != nil || string(bytes) != "original installed bytes" {
				t.Fatalf("%s changed installed runtime: %v", action, err)
			}
			select {
			case <-done:
				t.Fatal("foreign child was signalled")
			case <-time.After(50 * time.Millisecond):
			}
		})
	}
}

func TestOmniRouteRecoveryDeferredRequiresDurableDegradedWrite(t *testing.T) {
	for _, failure := range []string{"none", "degraded_write", "degraded_cas", "invalid_pid", "cancelled"} {
		t.Run(failure, func(t *testing.T) {
			s, ctx, o, _ := omniRouteLaunchFixture(t)
			o.metadata["pid"] = json.RawMessage(`12345`)
			if failure == "invalid_pid" {
				o.metadata["pid"] = json.RawMessage(`"invalid"`)
			}
			if err := s.writeOmniRoute(ctx, o, "enabled", "running", o.metadata, nil); err != nil {
				t.Fatal(err)
			}
			if failure == "degraded_write" {
				if _, err := s.db.ExecContext(ctx, `CREATE TRIGGER reject_omniroute_degraded BEFORE UPDATE ON managed_component_states WHEN NEW.component_id='omniroute' AND NEW.observed_state='degraded' BEGIN SELECT RAISE(ABORT,'degraded write rejected'); END`); err != nil {
					t.Fatal(err)
				}
			}
			if failure == "degraded_cas" {
				if _, err := s.db.ExecContext(ctx, `CREATE TRIGGER skip_omniroute_degraded BEFORE UPDATE ON managed_component_states WHEN NEW.component_id='omniroute' AND NEW.observed_state='degraded' BEGIN SELECT RAISE(IGNORE); END`); err != nil {
					t.Fatal(err)
				}
			}
			callCtx := ctx
			if failure == "cancelled" {
				var cancel context.CancelFunc
				callCtx, cancel = context.WithCancel(ctx)
				cancel()
			}
			err := s.recoverOmniRoute(callCtx)
			var raw, state, lastError string
			if readErr := s.db.QueryRowContext(ctx, `SELECT metadata_json,observed_state,COALESCE(last_error,'') FROM managed_component_states WHERE component_id='omniroute'`).Scan(&raw, &state, &lastError); readErr != nil {
				t.Fatal(readErr)
			}
			if failure == "none" {
				if !errors.Is(err, errOmniRouteRecoveryDeferred) || state != "degraded" || lastError == "" {
					t.Fatalf("optional recovery did not durably defer: err=%v state=%s", err, state)
				}
			} else if err == nil || errors.Is(err, errOmniRouteRecoveryDeferred) || state != "running" {
				t.Fatalf("failed observation falsely deferred: err=%v state=%s", err, state)
			}
			var metadata map[string]json.RawMessage
			if json.Unmarshal([]byte(raw), &metadata) != nil || string(metadata["pid"]) != string(o.metadata["pid"]) {
				t.Fatal("recovery erased original PID metadata")
			}
		})
	}
}

func TestOmniRouteInstallEnvironmentAndErrorsRemainPrivate(t *testing.T) {
	for _, key := range []string{"NODE_OPTIONS", "NPM_CONFIG_USERCONFIG", "NPM_CONFIG_REGISTRY", "NPM_TOKEN", "AWS_SECRET_ACCESS_KEY", "HTTP_PROXY"} {
		t.Setenv(key, "PRIVATE_INSTALL_CREDENTIAL")
	}
	root := t.TempDir()
	env := omniRouteInstallEnvironment(filepath.Join(root, "node.exe"), filepath.Join(root, "cache"), filepath.Join(root, "data"))
	seen := map[string]bool{}
	for _, entry := range env {
		if strings.Contains(entry, "PRIVATE_INSTALL_CREDENTIAL") {
			t.Fatalf("install inherited service credential: %s", entry)
		}
		name, _, _ := strings.Cut(entry, "=")
		name = strings.ToUpper(name)
		if seen[name] {
			t.Fatalf("duplicate install environment name %s", name)
		}
		seen[name] = true
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	err = runOmniRouteInstallCommand(context.Background(), LaunchSpec{Executable: exe, Args: []string{"-test.run=^TestOmniRouteDisposableChild$"}, Env: []string{"ONEPANE_OMNIROUTE_TEST_CHILD=exit"}, LogPath: filepath.Join(root, "logs", "install.log"), Dir: root})
	if err == nil || strings.Contains(err.Error(), "PRIVATE_OMNIROUTE_RUNTIME_TOKEN") {
		t.Fatalf("install error leaked raw child log: %v", err)
	}
	log, readErr := os.ReadFile(filepath.Join(root, "logs", "install.log"))
	if readErr != nil || !strings.Contains(string(log), "PRIVATE_OMNIROUTE_RUNTIME_TOKEN") {
		t.Fatalf("test child did not actually write private log: %v", readErr)
	}
}
