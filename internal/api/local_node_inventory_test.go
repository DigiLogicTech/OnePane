package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLocalNodeInventoryWithoutFederation(t *testing.T) {
	for _, tc := range []struct {
		name string
		auth fakeAuth
		want int
	}{
		{"reader", fakeAuth{id: Identity{PrincipalID: "reader", CapabilityScope: json.RawMessage(`["model.read"]`)}}, 200},
		{"denied", fakeAuth{id: Identity{PrincipalID: "reader", CapabilityScope: json.RawMessage(`["task.read"]`)}}, 403},
		{"unauthenticated", fakeAuth{authErr: ErrUnauthenticated}, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewServer(nil, fakeEvents{}, tc.auth)
			s.SetLocalAI(nil, "canonical-local-node")
			r := httptest.NewRecorder()
			s.Handler().ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/v1/nodes", nil))
			if r.Code != tc.want {
				t.Fatalf("status %d: %s", r.Code, r.Body.String())
			}
			if tc.want != 200 {
				return
			}
			var out struct {
				Federation bool `json:"federation_enabled"`
				Nodes      []struct {
					ID    string `json:"id"`
					Local bool   `json:"local"`
				} `json:"nodes"`
			}
			if err := json.Unmarshal(r.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			if out.Federation || len(out.Nodes) != 1 || out.Nodes[0].ID != "canonical-local-node" || !out.Nodes[0].Local {
				t.Fatalf("unexpected inventory: %+v", out)
			}
			// Reading local inventory must not enable any remote operation.
			r = httptest.NewRecorder()
			s.Handler().ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/v1/nodes/peer/pair", nil))
			if r.Code != http.StatusServiceUnavailable {
				t.Fatalf("pairing unexpectedly available: %d", r.Code)
			}
		})
	}
}
