package tool

import (
	"fmt"
	"sync"
)

type Binding struct {
	Definition Definition
	Adapter    Adapter
}

type Registry struct {
	mu    sync.RWMutex
	tools map[string]Binding
}

func NewRegistry() *Registry { return &Registry{tools: make(map[string]Binding)} }

func registryKey(id, version string) string { return id + "\x00" + version }

func (r *Registry) Register(def Definition, adapter Adapter) error {
	if r == nil || adapter == nil {
		return fmt.Errorf("%w: registry and adapter are required", ErrInvalidDefinition)
	}
	if err := def.Validate(); err != nil {
		return err
	}
	if adapter.ID() != def.AdapterID || adapter.Version() != def.AdapterVersion {
		return fmt.Errorf("%w: definition=%s@%s adapter=%s@%s", ErrAdapterIdentityMismatch,
			def.AdapterID, def.AdapterVersion, adapter.ID(), adapter.Version())
	}
	key := registryKey(def.ID, def.Version)
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[key]; exists {
		return fmt.Errorf("%w: %s@%s", ErrDuplicateTool, def.ID, def.Version)
	}
	r.tools[key] = Binding{Definition: def, Adapter: adapter}
	return nil
}

func (r *Registry) Resolve(id, version string) (Binding, error) {
	if r == nil {
		return Binding{}, ErrToolNotFound
	}
	r.mu.RLock()
	binding, ok := r.tools[registryKey(id, version)]
	r.mu.RUnlock()
	if !ok {
		return Binding{}, fmt.Errorf("%w: %s@%s", ErrToolNotFound, id, version)
	}
	return binding, nil
}
