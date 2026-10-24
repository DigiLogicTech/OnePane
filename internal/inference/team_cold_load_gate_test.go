package inference

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/teamgate"
)

type coldLoadTeamResolver struct {
	endpoint                  string
	loaded, released, checked bool
}

func (r *coldLoadTeamResolver) ResolveLocalEndpoint(context.Context, string) (string, error) {
	return r.endpoint, nil
}
func (r *coldLoadTeamResolver) AcquireLocalEndpoint(context.Context, string) (string, func(), error) {
	r.loaded = true
	return r.endpoint, func() { r.released = true }, nil
}
func (r *coldLoadTeamResolver) AuthorizeLocalTaskExecution(_ context.Context, ws string, taskID *string) error {
	r.checked = r.loaded && ws == "tenant" && taskID != nil && *taskID == "contribution"
	return teamgate.ErrExecution
}

func TestTeamAuthorityRecheckedAfterManagedColdLoadBeforePrompt(t *testing.T) {
	var called atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called.Store(true) }))
	defer server.Close()
	resolver := &coldLoadTeamResolver{endpoint: server.URL}
	id := "contribution"
	_, err := (LocalOpenAITransport{Resolver: resolver}).Dispatch(context.Background(), DispatchRequest{WorkspaceID: "tenant", TaskID: &id, RequestJSON: json.RawMessage(`{"messages":[{"role":"user","content":"PRIVATE_PROMPT"}]}`)}, nil)
	if !errors.Is(err, teamgate.ErrExecution) || !resolver.checked || !resolver.released || called.Load() {
		t.Fatalf("revoked Team prompt escaped managed acquisition: checked=%v released=%v sent=%v err=%v", resolver.checked, resolver.released, called.Load(), err)
	}
}
