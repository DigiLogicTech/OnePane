package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/tool"
)

type fakeSecrets struct{}

func (fakeSecrets) Resolve(context.Context, string) (string, error) { return "token", nil }
func (fakeSecrets) ValidatePluginCredentialRef(_ context.Context, _ string, _ string) error {
	return nil
}

func TestConnectorCatalogIncludesCommonWorkTools(t *testing.T) {
	if len(Builtins()) < 20 {
		t.Fatalf("catalog too small: %d", len(Builtins()))
	}
	for _, id := range []string{"gmail", "google-calendar", "google-drive", "outlook-mail", "outlook-calendar", "teams", "github", "slack", "notion", "linear", "jira", "confluence"} {
		if _, ok := ByID(id); !ok {
			t.Fatalf("missing %s", id)
		}
	}
}

func TestEmailSendCannotUseMutateTool(t *testing.T) {
	p, _ := ByID("gmail")
	a := NewHTTPAdapter(p, "mutate", fakeSecrets{})
	_, err := a.Invoke(context.Background(), tool.AdapterRequest{Input: json.RawMessage(`{"secret_ref":"vault:x","method":"POST","path":"/gmail/v1/users/me/messages/send","body":{}}`)})
	if err == nil {
		t.Fatal("expected external-send guard")
	}
}

func TestLinearGraphQLMutationCannotUseReadTool(t *testing.T) {
	p, _ := ByID("linear")
	a := NewHTTPAdapter(p, "read", fakeSecrets{})
	_, err := a.Invoke(context.Background(), tool.AdapterRequest{Input: json.RawMessage(`{"secret_ref":"vault:x","method":"POST","path":"/graphql","body":{"query":"mutation { issueCreate { success } }"}}`)})
	if err == nil {
		t.Fatal("expected GraphQL mutation guard")
	}
}

func TestSlackLogicalFailureIsNotSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"error":"invalid_auth"}`))
	}))
	defer srv.Close()
	p := Preset{ID: "slack", DisplayName: "Slack", BaseURL: srv.URL, AuthHeader: "Authorization", AuthPrefix: "Bearer", AllowedPrefixes: []string{"/api/"}, ReadMethods: []string{"GET"}, MutationMethods: []string{"POST"}, LogicalSuccess: "slack_ok"}
	a := NewHTTPAdapter(p, "read", fakeSecrets{})
	_, err := a.Invoke(context.Background(), tool.AdapterRequest{Input: json.RawMessage(`{"secret_ref":"vault:x","method":"GET","path":"/api/auth.test"}`)})
	if err == nil {
		t.Fatal("expected Slack logical failure")
	}
	var known tool.KnownAdapterFailure
	if !errors.As(err, &known) {
		t.Fatalf("expected known failure, got %T %v", err, err)
	}
}
