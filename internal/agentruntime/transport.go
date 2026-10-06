package agentruntime

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/DigiLogicTech/OnePane/internal/agentprotocol"
)

type SecretResolver interface {
	Resolve(context.Context, string) (string, error)
}
type RuntimeTransport interface {
	Invoke(context.Context, Connection, agentprotocol.Request, SecretResolver) (agentprotocol.Response, error)
}
type RuntimeTransportError struct {
	Code         string
	HTTPStatus   int
	OutcomeKnown bool
	Err          error
}

func (e *RuntimeTransportError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return fmt.Sprintf("agent runtime transport %s: %v", e.Code, e.Err)
	}
	return "agent runtime transport " + e.Code
}
func (e *RuntimeTransportError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type RuntimeTransportRegistry struct {
	mu    sync.RWMutex
	items map[string]RuntimeTransport
}

func NewRuntimeTransportRegistry() *RuntimeTransportRegistry {
	return &RuntimeTransportRegistry{items: map[string]RuntimeTransport{}}
}
func runtimeTransportKey(name, version string) string {
	return strings.TrimSpace(name) + "@" + strings.TrimSpace(version)
}
func (r *RuntimeTransportRegistry) Register(name, version string, t RuntimeTransport) error {
	key := runtimeTransportKey(name, version)
	if strings.TrimSpace(name) == "" || strings.TrimSpace(version) == "" || t == nil {
		return fmt.Errorf("runtime transport identity required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.items[key]; ok {
		return fmt.Errorf("runtime transport %s already registered", key)
	}
	r.items[key] = t
	return nil
}
func (r *RuntimeTransportRegistry) Resolve(name, version string) (RuntimeTransport, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.items[runtimeTransportKey(name, version)]
	return t, ok
}
func RegisterBuiltinRuntimeTransports(r *RuntimeTransportRegistry) error {
	return r.Register("builtin.agent_protocol_http", "1", AgentProtocolHTTPTransport{})
}
