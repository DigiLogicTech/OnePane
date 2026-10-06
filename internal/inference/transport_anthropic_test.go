package inference

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAnthropicTransportPinsModelAndNormalizesResponse(t *testing.T) {
	var got map[string]any
	var gotKey string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-api-key")
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"{\"protocol_version\":\"v1\"}"}],"usage":{"input_tokens":3,"output_tokens":4}}`))
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
	secret := "vault:anthropic"
	p := ProviderConnection{AuthType: "api_key", SecretRef: &secret, ConnectionJSON: json.RawMessage(`{"base_url":"https://api.anthropic.com","allowed_host_suffixes":["api.anthropic.com"]}`)}
	res, err := (AnthropicTransport{Client: client}).Dispatch(context.Background(), DispatchRequest{Model: Model{ModelRef: "claude-test"}, Deployment: ModelDeployment{RuntimeConfigJSON: json.RawMessage(`{}`)}, Provider: &p, RequestJSON: json.RawMessage(`{"model":"attacker","messages":[{"role":"system","content":"system rule"},{"role":"user","content":"hello"}],"max_tokens":100,"response_format":{"type":"json_object"}}`)}, staticSecrets{"vault:anthropic": "key"})
	if err != nil {
		t.Fatal(err)
	}
	if gotKey != "key" || got["model"] != "claude-test" || got["system"] != "system rule" {
		t.Fatalf("key/model/system mismatch key=%q body=%v", gotKey, got)
	}
	if _, ok := got["response_format"]; ok {
		t.Fatal("OpenAI response_format leaked into Anthropic request")
	}
	var normalized map[string]any
	if err := json.Unmarshal(res.ResponseJSON, &normalized); err != nil {
		t.Fatal(err)
	}
	if _, ok := normalized["choices"]; !ok {
		t.Fatalf("response was not normalized: %s", res.ResponseJSON)
	}
}
