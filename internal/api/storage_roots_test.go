package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStorageRootsAreReadOnlyAndExplicit(t *testing.T) {
	server := NewServer(nil, fakeEvents{}, fakeAuth{id: Identity{PrincipalID: "owner"}})
	server.SetRuntimeConfig("", `D:\OnePaneData\models`)
	server.SetStorageRoots(`D:\OnePaneData\data`, `D:\OnePaneData\Projects`)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/settings/storage-roots", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var got struct {
		DataDir     string `json:"data_dir"`
		DataRoot    string `json:"data_root"`
		ModelRoot   string `json:"model_root"`
		ProjectRoot string `json:"project_root"`
		ReadOnly    bool   `json:"read_only"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.DataDir != `D:\OnePaneData\data` || got.DataRoot != `D:\OnePaneData` || got.ModelRoot != `D:\OnePaneData\models` || got.ProjectRoot != `D:\OnePaneData\Projects` || !got.ReadOnly {
		t.Fatalf("roots = %+v", got)
	}
	denied := NewServer(nil, fakeEvents{}, fakeAuth{authErr: ErrUnauthenticated})
	denied.SetStorageRoots(`D:\OnePaneData\data`, `D:\OnePaneData\Projects`)
	response = httptest.NewRecorder()
	denied.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/settings/storage-roots", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status=%d", response.Code)
	}
}
