package botruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type staticSecret string

func (s staticSecret) Resolve(context.Context, string) (string, error) { return string(s), nil }

func TestBotCatalogHasNativeHostedAndRelayModes(t *testing.T) {
	want := map[string]string{"hermes": "harness_api", "chatgpt-gpt": "hosted_surface", "grok-bot": "hosted_surface", "custom-bot-relay": "relay_api"}
	for id, mode := range want {
		p, ok := PresetByID(id)
		if !ok {
			t.Fatalf("missing preset %s", id)
		}
		if p.AccessMode != mode {
			t.Fatalf("%s mode=%s want %s", id, p.AccessMode, mode)
		}
	}
	if p, _ := PresetByID("chatgpt-gpt"); p.InAppChat {
		t.Fatal("Custom GPT must not claim unsupported external in-app API")
	}
	if p, _ := PresetByID("hermes"); !p.InAppChat || !p.NativeTools || !p.NativeMemory {
		t.Fatal("Hermes Bot Mode capabilities incomplete")
	}
}

func TestSafeBaseFailsClosed(t *testing.T) {
	if _, err := safeBase("http://example.com/v1"); err == nil {
		t.Fatal("clear-text remote bot endpoint accepted")
	}
	if _, err := safeBase("http://127.0.0.1:8642"); err != nil {
		t.Fatalf("loopback HTTP rejected: %v", err)
	}
	if _, err := safeBase("https://user:pass@example.com"); err == nil {
		t.Fatal("credential-bearing URL accepted")
	}
}

func TestHostedLaunchHostPinned(t *testing.T) {
	p, _ := PresetByID("chatgpt-gpt")
	if err := validLaunch(p, "https://chatgpt.com/g/g-example"); err != nil {
		t.Fatal(err)
	}
	if err := validLaunch(p, "https://evil.example/g/g-example"); err == nil {
		t.Fatal("untrusted hosted-bot launch host accepted")
	}
}

func TestHermesProfileUsesSessionsAPIAndPreservesProfile(t *testing.T) {
	var sawCreate, sawChat, sawHistory bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			http.Error(w, "bad auth", 401)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/p/coder/api/sessions":
			json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
		case r.Method == http.MethodPost && r.URL.Path == "/p/coder/api/sessions":
			sawCreate = true
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(map[string]any{"session": map[string]any{"id": "remote_session"}})
		case r.Method == http.MethodPost && r.URL.Path == "/p/coder/api/sessions/remote_session/chat":
			sawChat = true
			json.NewEncoder(w).Encode(map[string]any{"run_id": "run_1"})
		case r.Method == http.MethodGet && r.URL.Path == "/p/coder/api/sessions/remote_session/messages":
			sawHistory = true
			json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"id": "m1", "role": "user", "content": "hello"}, map[string]any{"id": "m2", "role": "assistant", "content": "hi from Hermes"}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()
	ref := "vault:key"
	base := ts.URL
	a := HTTPAdapter{Mode: "hermes", Secrets: staticSecret("secret")}
	c := Connection{BaseURL: &base, CredentialRef: &ref}
	b := Bot{RemoteBotID: "coder"}
	rid, err := a.EnsureSession(context.Background(), c, b, "local_1", "Bot Chat", true)
	if err != nil {
		t.Fatal(err)
	}
	if rid != "remote_session" {
		t.Fatalf("remote id=%s", rid)
	}
	reply, err := a.Send(context.Background(), c, b, rid, "hello", "idem-1")
	if err != nil {
		t.Fatal(err)
	}
	if reply.Text != "hi from Hermes" {
		t.Fatalf("reply=%q", reply.Text)
	}
	if !sawCreate || !sawChat || !sawHistory {
		t.Fatalf("missing Hermes session calls create=%v chat=%v history=%v", sawCreate, sawChat, sawHistory)
	}
}

func TestRelayEnvelope(t *testing.T) {
	var got map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/bot/chat" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		json.NewEncoder(w).Encode(map[string]any{"message": map[string]any{"id": "r1", "text": "relay answer"}, "run_id": "run2"})
	}))
	defer ts.Close()
	base := ts.URL
	a := HTTPAdapter{Mode: "relay"}
	c := Connection{BaseURL: &base}
	b := Bot{RemoteBotID: "bot-a"}
	reply, err := a.Send(context.Background(), c, b, "sess-a", "question", "idem-a")
	if err != nil {
		t.Fatal(err)
	}
	if reply.Text != "relay answer" {
		t.Fatal(reply.Text)
	}
	if got["bot_id"] != "bot-a" || got["session_id"] != "sess-a" || got["idempotency_key"] != "idem-a" {
		t.Fatalf("bad relay envelope: %#v", got)
	}
}

func TestHermesProfileRejectsPathInjection(t *testing.T) {
	if _, err := hermesPrefix("../other"); err == nil {
		t.Fatal("unsafe profile accepted")
	}
	if p, err := hermesPrefix("my-bot_1"); err != nil || !strings.Contains(p, "my-bot_1") {
		t.Fatalf("safe profile rejected: %s %v", p, err)
	}
}
