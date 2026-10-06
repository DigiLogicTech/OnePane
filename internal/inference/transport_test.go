package inference

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type staticSecrets map[string]string

func (s staticSecrets) Resolve(_ context.Context, ref string) (string, error) {
	v, ok := s[ref]
	if !ok {
		return "", errors.New("missing")
	}
	return v, nil
}

func TestOpenAICompatibleTransportPinsRegisteredModelAndBrokersSecret(t *testing.T) {
	var gotModel, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		gotModel, _ = body["model"].(string)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","choices":[],"usage":{"total_tokens":7}}`))
	}))
	defer srv.Close()
	providerID := "provider"
	secretRef := "secret/test"
	runtime := "openai-compatible"
	p := ProviderConnection{ID: providerID, Provider: "openai_compatible", AuthType: "bearer", SecretRef: &secretRef, Status: ProviderConnected, ConnectionJSON: json.RawMessage(`{"base_url":"` + srv.URL + `","data_policy":{"max_confidentiality":"public","allowed_residency":["any"],"destination_kind":"cloud","allow_raw_secrets":false}}`)}
	d := ModelDeployment{ID: "dep", ModelID: "model", ProviderConnectionID: &providerID, RuntimeName: &runtime, RuntimeConfigJSON: json.RawMessage(`{}`), Status: DeploymentReady}
	result, err := (OpenAICompatibleTransport{}).Dispatch(context.Background(), DispatchRequest{RequestID: "req", Model: Model{ID: "model", ModelRef: "catalog/model"}, Deployment: d, Provider: &p, RequestJSON: json.RawMessage(`{"model":"attacker/override","messages":[{"role":"user","content":"hi"}]}`)}, staticSecrets{"secret/test": "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	if gotModel != "catalog/model" {
		t.Fatalf("model=%q", gotModel)
	}
	if gotAuth != "Bearer abc123" {
		t.Fatalf("auth=%q", gotAuth)
	}
	if string(result.UsageJSON) != `{"total_tokens":7}` {
		t.Fatalf("usage=%s", result.UsageJSON)
	}
}

func TestOpenAICompatible429IsKnownRateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "2")
		http.Error(w, "slow", http.StatusTooManyRequests)
	}))
	defer srv.Close()
	p := ProviderConnection{AuthType: "none", Status: ProviderConnected, ConnectionJSON: json.RawMessage(`{"base_url":"` + srv.URL + `"}`)}
	d := ModelDeployment{RuntimeConfigJSON: json.RawMessage(`{}`)}
	_, err := (OpenAICompatibleTransport{}).Dispatch(context.Background(), DispatchRequest{Model: Model{ModelRef: "m"}, Deployment: d, Provider: &p, RequestJSON: json.RawMessage(`{"messages":[]}`)}, nil)
	var te *TransportError
	if !errors.As(err, &te) || te.HTTPStatus != 429 || !te.OutcomeKnown || te.RetryAfterMS == nil {
		t.Fatalf("err=%#v", err)
	}
}

func TestFakeTransportReturnsDeterministicEnvelope(t *testing.T) {
	res, err := (FakeTransport{}).Dispatch(context.Background(), DispatchRequest{RequestID: "abc", Model: Model{ModelRef: "builtin/fake"}, RequestJSON: json.RawMessage(`{"messages":[]}`)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(res.ResponseJSON, &body); err != nil {
		t.Fatal(err)
	}
	if body["id"] != "fake-abc" || body["model"] != "builtin/fake" {
		t.Fatalf("response=%v", body)
	}
}

func TestRequestStateMachine(t *testing.T) {
	path := []RequestStatus{RequestCreated, RequestRouted, RequestDispatched, RequestExecuting, RequestSucceeded}
	for i := 0; i < len(path)-1; i++ {
		if !CanRequestTransition(path[i], path[i+1]) {
			t.Fatalf("transition %s -> %s denied", path[i], path[i+1])
		}
	}
	if CanRequestTransition(RequestSucceeded, RequestExecuting) {
		t.Fatal("terminal request reopened")
	}
}

func TestProviderDataPolicyDefaultsToPublicCloud(t *testing.T) {
	raw, err := normalizeProviderConnectionJSON(json.RawMessage(`{"base_url":"http://localhost"}`))
	if err != nil {
		t.Fatal(err)
	}
	p, err := providerDataPolicy(raw)
	if err != nil {
		t.Fatal(err)
	}
	if p.MaxConfidentiality != "public" || p.DestinationKind != "cloud" || p.AllowRawSecrets {
		t.Fatalf("policy=%+v", p)
	}
}

func TestBuiltinTransportRegistryNormalizesOpenAICompatibleAlias(t *testing.T) {
	r := NewTransportRegistry()
	if err := RegisterBuiltinTransports(r); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Resolve("openai_compatible"); !ok {
		t.Fatal("underscore alias did not normalize")
	}
	if _, ok := r.Resolve("openai-compatible"); !ok {
		t.Fatal("hyphen transport missing")
	}
}

func TestOpenAICompatibleRawAPIKeyAndHostPin(t *testing.T) {
	var got string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("api-key")
		_, _ = w.Write([]byte(`{"choices":[],"usage":{}}`))
	}))
	defer srv.Close()
	client := srv.Client()
	baseRT := client.Transport
	client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		clone := r.Clone(r.Context())
		clone.URL.Scheme = "https"
		clone.URL.Host = srv.Listener.Addr().String()
		return baseRT.RoundTrip(clone)
	})
	secret := "vault:key"
	provider := ProviderConnection{AuthType: "api_key", SecretRef: &secret, ConnectionJSON: json.RawMessage(`{"base_url":"https://demo.openai.azure.com/openai/v1","auth_header":"api-key","auth_raw":true,"allowed_host_suffixes":[".openai.azure.com"]}`)}
	_, err := (OpenAICompatibleTransport{Client: client}).Dispatch(context.Background(), DispatchRequest{Model: Model{ModelRef: "deployment"}, Deployment: ModelDeployment{RuntimeConfigJSON: json.RawMessage(`{}`)}, Provider: &provider, RequestJSON: json.RawMessage(`{"messages":[]}`)}, staticSecrets{"vault:key": "abc"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "abc" {
		t.Fatalf("api-key header=%q", got)
	}
	provider.ConnectionJSON = json.RawMessage(`{"base_url":"https://evil.example/v1","allowed_host_suffixes":["api.openai.com"]}`)
	if _, err := (OpenAICompatibleTransport{}).Dispatch(context.Background(), DispatchRequest{Model: Model{ModelRef: "x"}, Deployment: ModelDeployment{RuntimeConfigJSON: json.RawMessage(`{}`)}, Provider: &provider, RequestJSON: json.RawMessage(`{"messages":[]}`)}, staticSecrets{"vault:key": "abc"}); err == nil {
		t.Fatal("expected credential destination pinning")
	}
}

func TestRetryAfterMillisAcceptsHTTPDate(t *testing.T) {
	h := http.Header{}
	target := time.Now().UTC().Add(3 * time.Hour).Truncate(time.Second)
	h.Set("Retry-After", target.Format(http.TimeFormat))
	got := retryAfterMillis(h)
	if got == nil {
		t.Fatal("missing retry-after")
	}
	delta := time.UnixMilli(*got).Sub(target)
	if delta < -time.Second || delta > time.Second {
		t.Fatalf("retry-after=%v target=%v", time.UnixMilli(*got), target)
	}
}
