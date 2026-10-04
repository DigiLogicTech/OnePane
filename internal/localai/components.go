package localai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ManagedComponent is an optional runtime owned by the OnePane harness. Its
// lifecycle is deliberately independent from harness health: a component may
// be disabled, unhealthy, or quarantined while OnePane remains available.
type ManagedComponent struct {
	ID           string `json:"id"`
	DisplayName  string `json:"display_name"`
	Version      string `json:"version"`
	Installed    bool   `json:"installed"`
	Enabled      bool   `json:"enabled"`
	State        string `json:"state"`
	Sandbox      string `json:"sandbox"`
	GPU          bool   `json:"gpu"`
	ModelPool    bool   `json:"model_pool"`
	Internet     bool   `json:"internet"`
	TrustedNodes bool   `json:"trusted_nodes"`
	Inbound      bool   `json:"inbound"`
	LastError    string `json:"last_error,omitempty"`
	UpdatedAt    int64  `json:"updated_at"`
}

type componentStateFile struct {
	Components map[string]ManagedComponent `json:"components"`
}

var componentMu sync.Mutex

func componentDefinitions() map[string]ManagedComponent {
	return map[string]ManagedComponent{
		"colibri":   {ID: "colibri", DisplayName: "Colibri Large Model", Version: "1.12.1", State: "not_installed", Sandbox: "managed_component", GPU: true, ModelPool: true, Internet: true, TrustedNodes: true, Inbound: false},
		"omniroute": {ID: "omniroute", DisplayName: "OmniRoute", Version: "managed", State: "not_installed", Sandbox: "managed_component", GPU: false, ModelPool: false, Internet: true, TrustedNodes: false, Inbound: false},
	}
}

func (s *Service) componentStatePath() string {
	return filepath.Join(s.dataDir, "components", "state.json")
}
func (s *Service) componentRoot(id string) string { return filepath.Join(s.dataDir, "components", id) }

func (s *Service) ManagedComponents(context.Context) (map[string]ManagedComponent, error) {
	componentMu.Lock()
	defer componentMu.Unlock()
	return s.loadComponentStateLocked()
}

func (s *Service) loadComponentStateLocked() (map[string]ManagedComponent, error) {
	defs := componentDefinitions()
	b, err := os.ReadFile(s.componentStatePath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		var f componentStateFile
		if json.Unmarshal(b, &f) == nil {
			for id, st := range f.Components {
				if d, ok := defs[id]; ok {
					d.Installed, d.Enabled, d.State, d.LastError, d.UpdatedAt = st.Installed, st.Enabled, st.State, st.LastError, st.UpdatedAt
					defs[id] = d
				}
			}
		}
	}
	return defs, nil
}

func (s *Service) saveComponentStateLocked(v map[string]ManagedComponent) error {
	dir := filepath.Dir(s.componentStatePath())
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	b, err := json.MarshalIndent(componentStateFile{Components: v}, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.componentStatePath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, s.componentStatePath())
}

// ManageComponent records the harness-owned lifecycle. Installation creates a
// private component root only; the component sandbox provisioner populates and
// starts it. Enable/disable never changes harness health.
func (s *Service) ManageComponent(_ context.Context, id, action string) (ManagedComponent, error) {
	id, action = strings.ToLower(strings.TrimSpace(id)), strings.ToLower(strings.TrimSpace(action))
	componentMu.Lock()
	defer componentMu.Unlock()
	all, err := s.loadComponentStateLocked()
	if err != nil {
		return ManagedComponent{}, err
	}
	c, ok := all[id]
	if !ok {
		return ManagedComponent{}, errors.New("unknown managed component")
	}
	now := time.Now().UnixMilli()
	switch action {
	case "install":
		if err := os.MkdirAll(s.componentRoot(id), 0o750); err != nil {
			return ManagedComponent{}, fmt.Errorf("create component sandbox root: %w", err)
		}
		c.Installed, c.Enabled, c.State, c.LastError = true, false, "installed_disabled", ""
	case "enable":
		if !c.Installed {
			return ManagedComponent{}, errors.New("component must be installed before it can be enabled")
		}
		c.Enabled, c.State, c.LastError = true, "enabled_pending", ""
	case "disable":
		if !c.Installed {
			return ManagedComponent{}, errors.New("component is not installed")
		}
		c.Enabled, c.State = false, "installed_disabled"
	case "remove":
		c.Enabled = false
		if err := os.RemoveAll(s.componentRoot(id)); err != nil {
			return ManagedComponent{}, fmt.Errorf("remove component sandbox root: %w", err)
		}
		c.Installed, c.State, c.LastError = false, "not_installed", ""
	default:
		return ManagedComponent{}, errors.New("unsupported component action")
	}
	c.UpdatedAt = now
	all[id] = c
	if err := s.saveComponentStateLocked(all); err != nil {
		return ManagedComponent{}, err
	}
	return c, nil
}
