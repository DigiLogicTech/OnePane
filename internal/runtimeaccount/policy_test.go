package runtimeaccount

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type fakeHost struct {
	system   bool
	sid      string
	users    string
	ensured  []string
	grants   []Grant
	token    uintptr
	grantErr error
	restrict error
}

func (f *fakeHost) LocalSystem() bool { return f.system }
func (f *fakeHost) RuntimeSID() (string, error) {
	if f.sid == "" {
		return "", errors.New("no sid")
	}
	return f.sid, nil
}
func (f *fakeHost) UsersSID() (string, error) { return f.users, nil }
func (f *fakeHost) EnsureDir(path string) error {
	f.ensured = append(f.ensured, path)
	return nil
}
func (f *fakeHost) Grant(path, sid string, write bool) error {
	if f.grantErr != nil {
		return f.grantErr
	}
	f.grants = append(f.grants, Grant{Path: path, Write: write})
	if sid == "" {
		return errors.New("missing sid")
	}
	return nil
}
func (f *fakeHost) Restrict(runtimeSID, usersSID string) (uintptr, error) {
	if f.restrict != nil {
		return 0, f.restrict
	}
	if runtimeSID == "" || usersSID == "" {
		return 0, errors.New("missing restricting sid")
	}
	return f.token, nil
}

func TestPlanDoesNotGrantKeysOrTheDataDirectory(t *testing.T) {
	data := filepath.Join(string(os.PathSeparator), "onepane", "data")
	plan, err := PlanRoots(Roots{
		DataDir: data, ModelRoot: filepath.Join(string(os.PathSeparator), "onepane", "models"),
		LogDir: filepath.Join(data, "logs"), Executable: filepath.Join(data, "runtimes", "llama", "llama-server"),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, grant := range plan.Grants {
		if strings.EqualFold(grant.Path, data) || strings.Contains(strings.ToLower(grant.Path), "keys") || strings.Contains(strings.ToLower(grant.Path), "vault") {
			t.Fatalf("protected path granted: %+v", grant)
		}
	}
	if _, err := PlanRoots(Roots{DataDir: data, ModelRoot: data}); err == nil {
		t.Fatal("write grant on the data directory was accepted")
	}
}

func TestNonSystemParentIsUnchanged(t *testing.T) {
	cmd := exec.Command("onepane-runtime")
	cmd.Env = []string{"PATH=/usr/bin"}
	host := &fakeHost{}
	token, err := Apply(cmd, Roots{DataDir: t.TempDir()}, host)
	if err != nil || token != 0 {
		t.Fatalf("token=%d err=%v", token, err)
	}
	if len(host.grants) != 0 || len(cmd.Env) != 1 {
		t.Fatalf("non-system launch was rewritten: grants=%v env=%v", host.grants, cmd.Env)
	}
}

func TestSystemParentRestrictsAndFailsClosed(t *testing.T) {
	data := t.TempDir()
	model := filepath.Join(data, "models")
	cmd := exec.Command("llama-server")
	cmd.Env = []string{"PATH=/usr/bin", "TEMP=/system"}
	host := &fakeHost{system: true, sid: "S-1-5-80-test", users: "S-1-5-32-545", token: 42}
	token, err := Apply(cmd, Roots{DataDir: data, ModelRoot: model, Executable: filepath.Join(model, "llama-server")}, host)
	if err != nil || token != 42 {
		t.Fatalf("token=%d err=%v", token, err)
	}
	var sawModel bool
	for _, grant := range host.grants {
		if strings.EqualFold(grant.Path, model) && grant.Write {
			sawModel = true
		}
		if strings.Contains(strings.ToLower(grant.Path), "keys") {
			t.Fatalf("keys granted: %+v", grant)
		}
	}
	if !sawModel {
		t.Fatalf("model root was not granted: %+v", host.grants)
	}
	joined := strings.Join(cmd.Env, "\n")
	if !strings.Contains(joined, "TEMP="+filepath.Join(data, homeDirName)) || strings.Contains(joined, "TEMP=/system") {
		t.Fatalf("runtime home env = %s", joined)
	}

	host.restrict = errors.New("token refused")
	cmd = exec.Command("llama-server")
	if _, err := Apply(cmd, Roots{DataDir: data, ModelRoot: model}, host); err == nil || !strings.Contains(err.Error(), "LocalSystem") {
		t.Fatalf("system fallback was allowed: %v", err)
	}
}
