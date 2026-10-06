package inference

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type testSecret string

func (s testSecret) Resolve(context.Context, string) (string, error) { return string(s), nil }
func TestChatGPTPlanTransportPinsModelAndRequiresStreaming(t *testing.T) {
	var got map[string]any
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r\",\"usage\":{\"input_tokens\":1}}}\n\n"))
	}))
	defer srv.Close()
	provider := ProviderConnection{AuthType: "oauth2-pkce", SecretRef: strp("env:TOKEN"), ConnectionJSON: json.RawMessage(`{"base_url":"https://api.openai.com/v1"}`)}
	// Rewrite transport host via a custom RoundTripper while preserving the required public URL check.
	client := srv.Client()
	base := client.Transport
	client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		clone := r.Clone(r.Context())
		clone.URL.Scheme = "https"
		clone.URL.Host = srv.Listener.Addr().String()
		return base.RoundTrip(clone)
	})
	tr := ChatGPTPlanTransport{Client: client}
	res, err := tr.Dispatch(context.Background(), DispatchRequest{Model: Model{ModelRef: "allowed-model"}, Deployment: ModelDeployment{}, Provider: &provider, RequestJSON: json.RawMessage(`{"model":"attacker-model","input":[{"role":"user","content":"hi"}]}`)}, testSecret("tok"))
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(res.ResponseJSON) {
		t.Fatal("invalid result")
	}
	if got["model"] != "allowed-model" || got["stream"] != true || got["store"] != false {
		t.Fatalf("body %#v", got)
	}
}
func TestChatGPTPlanRejectsUnsupportedField(t *testing.T) {
	p := ProviderConnection{AuthType: "oauth2-pkce", SecretRef: strp("env:T"), ConnectionJSON: json.RawMessage(`{"base_url":"https://api.openai.com/v1"}`)}
	_, err := (ChatGPTPlanTransport{}).Dispatch(context.Background(), DispatchRequest{Model: Model{ModelRef: "m"}, Deployment: ModelDeployment{}, Provider: &p, RequestJSON: json.RawMessage(`{"input":[],"temperature":1}`)}, testSecret("t"))
	if err == nil {
		t.Fatal("expected rejection")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func strp(v string) *string                                               { return &v }
