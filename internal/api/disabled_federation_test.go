package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/nodefederation"
)

func TestDisabledFederationReturnsUnavailableWithoutPanic(t *testing.T) {
	var disabled *nodefederation.Service
	for _, service := range []nodeFederationService{nil, disabled} {
		server := NewServer(nil, fakeEvents{}, fakeAuth{id: Identity{PrincipalID: "qa-admin"}})
		server.SetFederation(service)
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/nodes", nil))
		if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "node federation unavailable") {
			t.Fatalf("disabled nodes response: %d %s", response.Code, response.Body.String())
		}
	}
}
