// Package runtimeaccount keeps model runtime children off a full LocalSystem
// token. The Windows service itself stays LocalSystem so existing key ACLs
// and the installed service identity are unchanged. Children receive a
// restricted token whose restricting SIDs are NT SERVICE\OnePane and
// BUILTIN\Users, after an allow ACE is added on the runtime roots.
package runtimeaccount

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	homeDirName = "runtime-account"
	runtimeSID  = `NT SERVICE\OnePane`
)

// Roots are the directories a managed model process may use. Key and vault
// directories under DataDir are never granted.
type Roots struct {
	DataDir    string
	ModelRoot  string
	LogDir     string
	WorkDir    string
	Executable string
}

// Grant is one directory the restricted child must be able to use.
type Grant struct {
	Path            string
	Write           bool
	Create          bool
	OptionalMissing bool
}

// Plan is the ACL migration for one launch. It does not touch the filesystem.
type Plan struct {
	Home      string
	Grants    []Grant
	Protected []string
}

func PlanRoots(roots Roots) (Plan, error) {
	data := clean(roots.DataDir)
	if data == "" {
		return Plan{}, errors.New("model runtime storage root is not configured")
	}
	plan := Plan{
		Home: filepath.Join(data, homeDirName),
		Protected: []string{
			filepath.Join(data, "keys"),
			filepath.Join(data, "vault"),
		},
	}
	plan.Grants = addGrant(plan.Grants, plan.Home, true, true, false)
	plan.Grants = addGrant(plan.Grants, roots.ModelRoot, true, false, false)
	plan.Grants = addGrant(plan.Grants, roots.LogDir, true, true, false)
	plan.Grants = addGrant(plan.Grants, roots.WorkDir, true, false, false)
	if exe := strings.TrimSpace(roots.Executable); exe != "" {
		plan.Grants = addGrant(plan.Grants, filepath.Dir(exe), false, false, false)
	}
	plan.Grants = addGrant(plan.Grants, filepath.Join(data, "runtimes"), false, false, true)
	for _, grant := range plan.Grants {
		if !grant.Write {
			continue
		}
		if samePath(grant.Path, data) {
			return Plan{}, fmt.Errorf("refusing to grant the model runtime write access to the data directory %s", data)
		}
		for _, protected := range plan.Protected {
			if samePath(grant.Path, protected) || isAncestor(grant.Path, protected) {
				return Plan{}, fmt.Errorf("refusing to grant the model runtime access to %s; it covers protected identity material in %s", grant.Path, protected)
			}
		}
	}
	return plan, nil
}

// Host is the operating-system boundary. Tests substitute a fake.
type Host interface {
	LocalSystem() bool
	RuntimeSID() (string, error)
	UsersSID() (string, error)
	EnsureDir(path string) error
	Grant(path, sid string, write bool) error
	Restrict(runtimeSID, usersSID string) (uintptr, error)
}

// Apply prepares cmd when the parent is LocalSystem. A non-system parent is
// left unchanged. Failure does not attach a token; the caller must not start
// the process as LocalSystem.
func Apply(cmd *exec.Cmd, roots Roots, host Host) (uintptr, error) {
	if cmd == nil {
		return 0, errors.New("missing model runtime command")
	}
	if host == nil || !host.LocalSystem() {
		return 0, nil
	}
	plan, err := PlanRoots(roots)
	if err != nil {
		return 0, err
	}
	sid, err := host.RuntimeSID()
	if err != nil {
		return 0, fmt.Errorf("model runtime account unavailable: %w", err)
	}
	users, err := host.UsersSID()
	if err != nil {
		return 0, fmt.Errorf("model runtime users SID unavailable: %w", err)
	}
	for _, grant := range plan.Grants {
		if grant.Create {
			if err := host.EnsureDir(grant.Path); err != nil {
				return 0, fmt.Errorf("prepare model runtime directory %s: %w", grant.Path, err)
			}
		}
		if err := host.Grant(grant.Path, sid, grant.Write); err != nil {
			if grant.OptionalMissing && errors.Is(err, os.ErrNotExist) {
				continue
			}
			return 0, fmt.Errorf("grant model runtime access to %s: %w", grant.Path, err)
		}
	}
	token, err := host.Restrict(sid, users)
	if err != nil || token == 0 {
		if err == nil {
			err = errors.New("restricted token was not created")
		}
		return 0, fmt.Errorf("model runtime was not started because it would inherit LocalSystem: %w", err)
	}
	cmd.Env = withRuntimeHome(cmd.Env, plan.Home)
	return token, nil
}

func withRuntimeHome(env []string, home string) []string {
	replacements := [][2]string{
		{"TEMP", home},
		{"TMP", home},
		{"TMPDIR", home},
		{"USERPROFILE", home},
		{"LOCALAPPDATA", filepath.Join(home, "AppData", "Local")},
		{"APPDATA", filepath.Join(home, "AppData", "Roaming")},
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(env)+len(replacements))
	for _, entry := range env {
		name, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		upper := strings.ToUpper(name)
		replaced := false
		for _, item := range replacements {
			if upper == item[0] {
				if !seen[upper] {
					out = append(out, item[0]+"="+item[1])
					seen[upper] = true
				}
				replaced = true
				break
			}
		}
		if !replaced {
			out = append(out, entry)
		}
	}
	for _, item := range replacements {
		if !seen[item[0]] {
			out = append(out, item[0]+"="+item[1])
		}
	}
	return out
}

func addGrant(list []Grant, path string, write, create, optional bool) []Grant {
	path = clean(path)
	if path == "" || path == "." {
		return list
	}
	for i, grant := range list {
		if samePath(grant.Path, path) {
			if write {
				list[i].Write = true
			}
			if create {
				list[i].Create = true
			}
			if !optional {
				list[i].OptionalMissing = false
			}
			return list
		}
	}
	return append(list, Grant{Path: path, Write: write, Create: create, OptionalMissing: optional})
}

func clean(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

func isAncestor(parent, child string) bool {
	parent, child = filepath.Clean(parent), filepath.Clean(child)
	if samePath(parent, child) {
		return true
	}
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}
