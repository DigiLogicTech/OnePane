package inference

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type endpointResolverFunc func(context.Context, string) (string, error)

func (f endpointResolverFunc) ResolveLocalEndpoint(ctx context.Context, id string) (string, error) {
	return f(ctx, id)
}

func TestLocalOpenAITransportPinsModelAndDisablesStreaming(t *testing.T) {
	var received map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"OK"}}],"usage":{"completion_tokens":1}}`))
	}))
	defer srv.Close()
	if !strings.HasPrefix(srv.URL, "http://127.0.0.1:") {
		t.Skip("httptest did not bind IPv4 loopback")
	}
	tr := LocalOpenAITransport{Resolver: endpointResolverFunc(func(context.Context, string) (string, error) { return srv.URL, nil })}
	result, err := tr.Dispatch(context.Background(), DispatchRequest{Model: Model{ModelRef: "trusted-model"}, Deployment: ModelDeployment{ID: "dep-1"}, RequestJSON: json.RawMessage(`{"model":"attacker-model","stream":true,"messages":[]}`)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if received["model"] != "trusted-model" {
		t.Fatalf("model=%v", received["model"])
	}
	if stream, ok := received["stream"].(bool); !ok || stream {
		t.Fatalf("stream=%v", received["stream"])
	}
	if !json.Valid(result.ResponseJSON) {
		t.Fatal("invalid response")
	}
}

func TestLocalOpenAITransportRejectsNonLoopbackEndpoint(t *testing.T) {
	tr := LocalOpenAITransport{Resolver: endpointResolverFunc(func(context.Context, string) (string, error) { return "http://10.0.0.10:8080", nil })}
	_, err := tr.Dispatch(context.Background(), DispatchRequest{Model: Model{ModelRef: "m"}, Deployment: ModelDeployment{ID: "d"}, RequestJSON: json.RawMessage(`{}`)}, nil)
	if err == nil {
		t.Fatal("expected unsafe endpoint rejection")
	}
}

type activityResolver struct {
	url   string
	calls []bool
}

func (a *activityResolver) ResolveLocalEndpoint(context.Context, string) (string, error) {
	return a.url, nil
}
func (a *activityResolver) SetLocalRuntimeBusy(_ context.Context, _ string, busy bool) error {
	a.calls = append(a.calls, busy)
	return nil
}

func TestLocalOpenAITransportMarksRuntimeBusyForDispatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"OK"}}]}`))
	}))
	defer srv.Close()
	if !strings.HasPrefix(srv.URL, "http://127.0.0.1:") {
		t.Skip("httptest did not bind IPv4 loopback")
	}
	r := &activityResolver{url: srv.URL}
	tr := LocalOpenAITransport{Resolver: r}
	if _, err := tr.Dispatch(context.Background(), DispatchRequest{Model: Model{ModelRef: "m"}, Deployment: ModelDeployment{ID: "dep"}, RequestJSON: json.RawMessage(`{"messages":[]}`)}, nil); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 2 || !r.calls[0] || r.calls[1] {
		t.Fatalf("busy calls=%v, want [true false]", r.calls)
	}
}

type leaseResolver struct {
	url      string
	acquired int
	released int
}

func (l *leaseResolver) ResolveLocalEndpoint(context.Context, string) (string, error) {
	return l.url, nil
}
func (l *leaseResolver) AcquireLocalEndpoint(context.Context, string) (string, func(), error) {
	l.acquired++
	return l.url, func() { l.released++ }, nil
}

func TestLocalOpenAITransportPrefersAtomicRuntimeLease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"OK"}}]}`))
	}))
	defer srv.Close()
	if !strings.HasPrefix(srv.URL, "http://127.0.0.1:") {
		t.Skip("httptest did not bind IPv4 loopback")
	}
	r := &leaseResolver{url: srv.URL}
	tr := LocalOpenAITransport{Resolver: r}
	if _, err := tr.Dispatch(context.Background(), DispatchRequest{Model: Model{ModelRef: "m"}, Deployment: ModelDeployment{ID: "dep"}, RequestJSON: json.RawMessage(`{"messages":[]}`)}, nil); err != nil {
		t.Fatal(err)
	}
	if r.acquired != 1 || r.released != 1 {
		t.Fatalf("lease acquired=%d released=%d", r.acquired, r.released)
	}
}
