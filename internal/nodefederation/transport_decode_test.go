package nodefederation

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFederationDecodeRequiresSingleJSONDocument(t *testing.T) {
	tests := []struct {
		name string
		body string
		wantValid bool
	}{
		{name: "single document", body: `{"request_id":"id"}`, wantValid: true},
		{name: "trailing whitespace", body: "{\"request_id\":\"id\"}\n \t", wantValid: true},
		{name: "second object", body: `{"request_id":"id"} {"request_id":"other"}`},
		{name: "second null", body: `{"request_id":"id"} null`},
		{name: "second array", body: `{"request_id":"id"} []`},
		{name: "garbage suffix", body: `{"request_id":"id"} trailing`},
		{name: "unknown field", body: `{"request_id":"id","admin":true}`},
		{name: "malformed first document", body: `{"request_id":`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/federation/v1/inference", strings.NewReader(tt.body))
			w := httptest.NewRecorder()
			var request struct {
				RequestID string `json:"request_id"`
			}
			ok := decode(w, r, &request)
			if ok != tt.wantValid {
				t.Fatalf("decode accepted=%t, want %t (response %d: %s)", ok, tt.wantValid, w.Code, w.Body.String())
			}
			if ok && request.RequestID != "id" {
				t.Fatalf("valid request ID changed: %q", request.RequestID)
			}
			if !ok && w.Code != http.StatusBadRequest {
				t.Fatalf("invalid JSON must return 400, got %d", w.Code)
			}
		})
	}
}
