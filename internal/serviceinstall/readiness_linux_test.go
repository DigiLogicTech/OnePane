//go:build linux

package serviceinstall

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/buildinfo"
)

var readinessFixtureConfig = flag.String("config", "", "readiness fixture configuration identity")

// This test-only executable path uses the same normal -config argv shape as
// production. It never starts OnePane or touches an installed service/account.
func TestMain(m *testing.M) {
	if os.Getenv("ONEPANE_READINESS_FIXTURE") == "1" {
		flag.Parse()
		if *readinessFixtureConfig == "" {
			os.Exit(2)
		}
		time.AfterFunc(20*time.Second, func() { os.Exit(0) })
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			os.Exit(3)
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) {
			if next := os.Getenv("ONEPANE_READINESS_REDIRECT"); next != "" {
				http.Redirect(w, r, next, 302)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		})
		mux.HandleFunc("/v1/about", func(w http.ResponseWriter, r *http.Request) {
			version := buildinfo.Version
			if os.Getenv("ONEPANE_READINESS_BAD_BUILD") == "1" {
				version = "foreign-build"
			}
			_ = json.NewEncoder(w).Encode(upgradeBuildIdentity{Version: version, Revision: buildinfo.Revision, BuildTime: buildinfo.BuildTime})
		})
		ready := []byte(strconv.Itoa(listener.Addr().(*net.TCPAddr).Port))
		if os.WriteFile(os.Getenv("ONEPANE_READINESS_READY"), ready, 0600) != nil {
			os.Exit(4)
		}
		if http.Serve(listener, mux) != nil {
			os.Exit(5)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestPackageRecoveryReadinessBindsProcessImageConfigUIDAndOwnedSocket(t *testing.T) {
	root := packageRecoveryFixtureRoot(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	self, err := os.Open(executable)
	if err != nil {
		t.Fatal(err)
	}
	image, err := upgradeImageHash(context.Background(), self)
	self.Close()
	if err != nil {
		t.Fatal(err)
	}
	copyPath := filepath.Join(root, "readiness-fixture")
	source, err := os.Open(executable)
	if err != nil {
		t.Fatal(err)
	}
	target, err := os.OpenFile(copyPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0755)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.Copy(target, source)
	source.Close()
	closeErr := target.Close()
	if err != nil || closeErr != nil {
		t.Fatal("fixture copy failed")
	}
	configPath := filepath.Join(root, "fixture-config.yaml")
	if err = os.WriteFile(configPath, []byte("fixture only; not an application configuration\n"), 0644); err != nil {
		t.Fatal(err)
	}
	start := func(name string, extra ...string) (*exec.Cmd, int) {
		t.Helper()
		data := filepath.Join(root, name)
		if e := os.Mkdir(data, 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.Chown(data, 65534, 65534); e != nil {
			t.Fatal(e)
		}
		ready := filepath.Join(data, "ready")
		cmd := exec.Command(copyPath, "-config", configPath)
		cmd.Env = append(os.Environ(), "ONEPANE_READINESS_FIXTURE=1", "ONEPANE_READINESS_READY="+ready)
		cmd.Env = append(cmd.Env, extra...)
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 65534, Gid: 65534, Groups: []uint32{}}}
		if e := cmd.Start(); e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			raw, e := os.ReadFile(ready)
			if e == nil {
				port, e := strconv.Atoi(string(raw))
				if e != nil {
					t.Fatal(e)
				}
				return cmd, port
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("owned fixture did not become ready")
		return nil, 0
	}
	cmd, port := start("selected-child")
	expected := upgradeProcessExpectation{UID: 65534, ImageSHA256: image, ConfigPath: configPath, HealthURL: fmt.Sprintf("http://127.0.0.1:%d/v1/health", port), Build: upgradeBuildIdentity{Version: buildinfo.Version, Revision: buildinfo.Revision, BuildTime: buildinfo.BuildTime}}
	verify := func(pid int, e upgradeProcessExpectation) error {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		return verifyUpgradeProcess(ctx, pid, e)
	}
	var foreignRequests atomic.Int64
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		foreignRequests.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	t.Cleanup(foreign.Close)
	t.Setenv("HTTP_PROXY", foreign.URL)
	t.Setenv("http_proxy", foreign.URL)
	if err = verify(cmd.Process.Pid, expected); err != nil {
		t.Fatalf("actual owned process/listener proof failed: %v", err)
	}
	for _, scenario := range []string{"uid", "image", "config", "build", "foreign_listener", "foreign_host", "bad_pid"} {
		t.Run(scenario, func(t *testing.T) {
			changed := expected
			pid := cmd.Process.Pid
			switch scenario {
			case "uid":
				changed.UID = 65533
			case "image":
				changed.ImageSHA256 = strings.Repeat("0", 64)
			case "config":
				changed.ConfigPath = configPath + ".foreign"
			case "build":
				changed.Build.Version = "other"
			case "foreign_listener":
				changed.HealthURL = foreign.URL + "/v1/health"
			case "foreign_host":
				changed.HealthURL = "http://example.invalid:80/v1/health"
			case "bad_pid":
				pid = 0
			}
			if err := verify(pid, changed); err == nil {
				t.Fatal("unverified readiness was accepted")
			}
		})
	}
	bad, badPort := start("bad-about", "ONEPANE_READINESS_BAD_BUILD=1")
	badExpected := expected
	badExpected.HealthURL = fmt.Sprintf("http://127.0.0.1:%d/v1/health", badPort)
	if err = verify(bad.Process.Pid, badExpected); err == nil {
		t.Fatal("foreign build metadata accepted")
	}
	redirect, redirectPort := start("redirect", "ONEPANE_READINESS_REDIRECT="+foreign.URL)
	redirectExpected := expected
	redirectExpected.HealthURL = fmt.Sprintf("http://127.0.0.1:%d/v1/health", redirectPort)
	if err = verify(redirect.Process.Pid, redirectExpected); err == nil {
		t.Fatal("redirect readiness accepted")
	}
	if foreignRequests.Load() != 0 {
		t.Fatal("proxy, foreign listener or redirect received readiness traffic")
	}
}

func TestUpgradeProcAddressDecodesNativeKernelCoordinates(t *testing.T) {
	for _, sample := range []struct{ raw, ip string }{{"0100007F", "127.0.0.1"}, {"0200007F", "127.0.0.2"}, {"00000000000000000000000001000000", "::1"}, {"00000000", "0.0.0.0"}} {
		ip, err := upgradeProcIP(sample.raw)
		if err != nil || !ip.Equal(net.ParseIP(sample.ip)) {
			t.Fatalf("kernel coordinate %s => %v / %v", sample.raw, ip, err)
		}
	}
	for _, raw := range []string{"", "xyz", strings.Repeat("z", 8), strings.Repeat("0", 64)} {
		if _, err := upgradeProcIP(raw); err == nil {
			t.Fatal("malformed kernel coordinate accepted")
		}
	}
}
