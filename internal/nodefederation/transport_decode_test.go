package nodefederation

import (
	"encoding/json"
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
		{name: "duplicate top-level key", body: `{"request_id":"id","request_id":"different"}`},
		{name: "duplicate escaped key alias", body: `{"request_id":"id","request\u005fid":"different"}`},
		{name: "oversized request", body: `{"request_id":"id","unused":"`+strings.Repeat("x",4<<20)+`"}`},
		{name: "deep nested duplicate", body: `{"request_id":"id","nested":{"a":1,"a":2}}`},
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

func TestFederationDecoderRejectsNestedDuplicateInAllowedEnvelope(t *testing.T){
 for _,raw:=range []string{
  `{"request_json":{"model":"alpha","model":"beta"}}`,
  `{"request_json":{"name":"alpha","na\u006de":"beta"}}`,
 }{
  req:=httptest.NewRequest(http.MethodPost,"/federation/v1/inference",strings.NewReader(raw))
  recorder:=httptest.NewRecorder()
  var envelope struct{Request json.RawMessage `json:"request_json"`}
  if decode(recorder,req,&envelope)||recorder.Code!=http.StatusBadRequest{
   t.Fatalf("ambiguous nested fields were accepted: status=%d",recorder.Code)
  }
 }
}
