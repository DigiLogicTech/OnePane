// Package serviceinstall configures the installed Linux service's storage
// boundary. It never starts the daemon or migrates an existing data root.
package serviceinstall

import (
	"fmt"
	"net"
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/DigiLogicTech/OnePane/internal/config"
)

const storageMarker = "# Managed by OnePane systemd storage v1\n"

type StoragePlan struct {
	Home, Data, Models, Projects string
	ConfigSHA256                 string
	HealthPort                   int
	HealthURL                    string
}

// UpgradeSettings is a deliberately small versioned maintenance projection.
// Data paths reject control characters, so five records remain unambiguous;
// callers must treat every field as data and never evaluate it as shell code.
func (p StoragePlan) UpgradeSettings() string {
	return fmt.Sprintf("onepane.service-upgrade/v1\n%s\n%d\n%s\n%s\n", p.Data, p.HealthPort, p.ConfigSHA256, p.HealthURL)
}

func storagePath(value string) (string, error) {
	if value == "" || value != strings.TrimSpace(value) || !strings.HasPrefix(value, "/") {
		return "", fmt.Errorf("storage requires an explicit absolute Linux path")
	}
	for _, r := range value {
		if unicode.IsControl(r) || r == '\\' {
			return "", fmt.Errorf("storage path contains unsupported characters")
		}
	}
	for _, part := range strings.Split(value, "/") {
		if part == ".." {
			return "", fmt.Errorf("storage path must not traverse ancestors")
		}
	}
	value = path.Clean(value)
	parts := strings.Split(strings.TrimPrefix(value, "/"), "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("select a dedicated storage directory below a mount/root, not the root itself")
	}
	for _, blocked := range []string{"/bin", "/boot", "/dev", "/etc", "/home", "/lib", "/lib64", "/proc", "/root", "/run", "/sbin", "/sys", "/tmp", "/usr", "/var"} {
		if value == blocked || strings.HasPrefix(value, blocked+"/") {
			if blocked == "/var" && strings.HasPrefix(value, "/var/lib/") {
				break
			}
			return "", fmt.Errorf("storage path is a protected/ephemeral system location; select a dedicated directory such as /srv/onepane/data")
		}
	}
	return value, nil
}

// PlanStorage mirrors runtime defaults without changing configuration. It is
// pure and can be reviewed before the explicit privileged configuration step.
func PlanStorage(cfg config.Config, home string) (StoragePlan, error) {
	var p StoragePlan
	var err error
	host, port, err := net.SplitHostPort(cfg.Server.Listen)
	if err != nil {
		return p, fmt.Errorf("service health requires a valid configured API address")
	}
	p.HealthPort, err = strconv.Atoi(port)
	if err != nil || p.HealthPort < 1 || p.HealthPort > 65535 {
		return p, fmt.Errorf("service health requires a fixed configured API port")
	}
	localHost := host
	switch host {
	case "", "0.0.0.0":
		localHost = "127.0.0.1"
	case "::":
		localHost = "::1"
	}
	if ip := net.ParseIP(localHost); ip != nil && ip.IsLoopback() {
		p.HealthURL = "http://" + net.JoinHostPort(ip.String(), strconv.Itoa(p.HealthPort)) + "/v1/health"
	} else if localHost == "localhost" {
		p.HealthURL = "http://localhost:" + strconv.Itoa(p.HealthPort) + "/v1/health"
	}
	if p.Home, err = storagePath(home); err != nil {
		return p, err
	}
	if p.Data, err = storagePath(cfg.Storage.DataDir); err != nil {
		return p, err
	}
	models := cfg.LocalAI.ModelPoolPath
	if models == "" {
		models = path.Join(p.Data, "models", "managed")
	}
	projects := cfg.Storage.ProjectRoot
	if projects == "" {
		projects = path.Join(p.Data, "projects")
	}
	if p.Models, err = storagePath(models); err != nil {
		return p, err
	}
	if p.Projects, err = storagePath(projects); err != nil {
		return p, err
	}
	return p, nil
}

func systemdQuoted(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	value = strings.ReplaceAll(value, "%", "%%")
	return "\"" + value + "\""
}

func (p StoragePlan) storageRoots() []string {
	roots := []string{p.Home, p.Data, p.Models, p.Projects}
	sort.Strings(roots)
	var out []string
	for _, root := range roots {
		covered := false
		for _, parent := range out {
			if root == parent || strings.HasPrefix(root, parent+"/") {
				covered = true
				break
			}
		}
		if !covered {
			out = append(out, root)
		}
	}
	return out
}

func (p StoragePlan) DropIn() string {
	roots := p.storageRoots()
	quoted := make([]string, 0, len(roots))
	for _, root := range roots {
		quoted = append(quoted, systemdQuoted(root))
	}
	return storageMarker + "# Configuration SHA-256: " + p.ConfigSHA256 + "\n[Unit]\nRequiresMountsFor=" + strings.Join(quoted, " ") + "\n[Service]\n" +
		"ReadWritePaths=\nReadWritePaths=" + strings.Join(quoted, " ") + " /run/onepane\n" +
		"Environment=" + systemdQuoted("HOME="+p.Home) + "\n" +
		"Environment=" + systemdQuoted("XDG_CONFIG_HOME="+path.Join(p.Home, ".config")) + "\n" +
		"Environment=" + systemdQuoted("XDG_DATA_HOME="+path.Join(p.Home, ".local", "share")) + "\n" +
		"Environment=" + systemdQuoted("ONEPANE_STARTUP_EVIDENCE_ROOT="+p.Data) + "\n"
}
