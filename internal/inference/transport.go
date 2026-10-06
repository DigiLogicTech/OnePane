package inference

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

type SecretResolver interface {
	Resolve(context.Context, string) (string, error)
}

type DispatchRequest struct {
	RequestID   string
	Model       Model
	Deployment  ModelDeployment
	Provider    *ProviderConnection
	RequestJSON json.RawMessage
}

type DispatchResult struct {
	ResponseJSON json.RawMessage
	UsageJSON    json.RawMessage
}

type TransportError struct {
	Code         string
	HTTPStatus   int
	RetryAfterMS *int64
	OutcomeKnown bool
	Err          error
}

func (e *TransportError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return fmt.Sprintf("inference transport %s: %v", e.Code, e.Err)
	}
	return "inference transport " + e.Code
}
func (e *TransportError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type Transport interface {
	Dispatch(context.Context, DispatchRequest, SecretResolver) (DispatchResult, error)
}

type TransportRegistry struct {
	mu         sync.RWMutex
	transports map[string]Transport
}

func NewTransportRegistry() *TransportRegistry {
	return &TransportRegistry{transports: map[string]Transport{}}
}
func transportKey(v string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(v), "_", "-"))
}
func (r *TransportRegistry) Register(name string, t Transport) error {
	key := transportKey(name)
	if key == "" || t == nil {
		return fmt.Errorf("transport name and implementation required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.transports[key]; ok {
		return fmt.Errorf("transport %s already registered", key)
	}
	r.transports[key] = t
	return nil
}
func (r *TransportRegistry) Resolve(name string) (Transport, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.transports[transportKey(name)]
	return t, ok
}
func RegisterBuiltinTransports(r *TransportRegistry) error {
	fake := FakeTransport{}
	openai := OpenAICompatibleTransport{}
	chatgptPlan := ChatGPTPlanTransport{}
	anthropic := AnthropicTransport{}
	for _, x := range []struct {
		name string
		t    Transport
	}{{"fake", fake}, {"openai-compatible", openai}, {"openai-chatgpt-plan", chatgptPlan}, {"anthropic", anthropic}} {
		if err := r.Register(x.name, x.t); err != nil {
			return err
		}
	}
	return nil
}

func selectTransportKey(d ModelDeployment, p *ProviderConnection) string {
	if d.RuntimeName != nil && strings.TrimSpace(*d.RuntimeName) != "" {
		return transportKey(*d.RuntimeName)
	}
	if p != nil {
		return transportKey(p.Provider)
	}
	return ""
}
