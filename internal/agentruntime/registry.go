package agentruntime

import (
	"fmt"
	"strings"
	"sync"
)

type AdapterDescriptor struct {
	Name           string
	Version        string
	SupportedModes []OperatingMode
	Description    string
}

type Registry struct {
	mu       sync.RWMutex
	adapters map[string]AdapterDescriptor
}

func NewRegistry() *Registry { return &Registry{adapters: map[string]AdapterDescriptor{}} }

func adapterKey(name, version string) string {
	return strings.TrimSpace(name) + "@" + strings.TrimSpace(version)
}

func (r *Registry) Register(d AdapterDescriptor) error {
	if strings.TrimSpace(d.Name) == "" || strings.TrimSpace(d.Version) == "" || len(d.SupportedModes) == 0 {
		return fmt.Errorf("invalid adapter descriptor")
	}
	seen := map[OperatingMode]bool{}
	for _, m := range d.SupportedModes {
		if !ValidOperatingMode(m) || seen[m] {
			return fmt.Errorf("invalid adapter mode %q", m)
		}
		seen[m] = true
	}
	key := adapterKey(d.Name, d.Version)
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.adapters[key]; ok {
		return fmt.Errorf("adapter %s already registered", key)
	}
	r.adapters[key] = d
	return nil
}

func (r *Registry) Resolve(name, version string) (AdapterDescriptor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.adapters[adapterKey(name, version)]
	return d, ok
}

func (d AdapterDescriptor) Supports(mode OperatingMode) bool {
	for _, m := range d.SupportedModes {
		if m == mode {
			return true
		}
	}
	return false
}

func RegisterBuiltinAdapters(r *Registry) error {
	// Canonical external-agent transport. A third-party harness can expose this
	// protocol directly or through a thin bridge. The bridge must preserve the
	// proposal boundary: external runtimes never receive origin ToolGateway leases.
	if err := r.Register(AdapterDescriptor{Name: "builtin.agent_protocol_http", Version: "1", SupportedModes: []OperatingMode{ProposalOnly, GatewayMediated}, Description: "Canonical external-agent HTTP bridge; suitable for Hermes and custom harness shims."}); err != nil {
		return err
	}
	if err := r.Register(AdapterDescriptor{Name: "builtin.unmanaged_external", Version: "1", SupportedModes: []OperatingMode{Unmanaged}, Description: "Explicit human-only registration for opaque external harnesses."}); err != nil {
		return err
	}
	return nil
}
