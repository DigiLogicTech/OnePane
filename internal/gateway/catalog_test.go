package gateway

import "testing"

func TestBuiltinGatewayCatalogCoverage(t *testing.T) {
	xs := Builtins()
	if len(xs) < 32 {
		t.Fatalf("expected Hermes-parity gateway catalog, got %d", len(xs))
	}
	seen := map[string]bool{}
	for _, x := range xs {
		if x.ID == "" || x.DisplayName == "" {
			t.Fatalf("invalid preset: %+v", x)
		}
		if seen[x.ID] {
			t.Fatalf("duplicate preset %s", x.ID)
		}
		seen[x.ID] = true
		if !x.Capabilities.Outbound {
			t.Fatalf("notification gateway %s must expose outbound delivery", x.ID)
		}
		if x.DefaultMode != "native_http" && x.DefaultMode != "smtp" && x.DefaultMode != "relay" {
			t.Fatalf("invalid mode for %s: %s", x.ID, x.DefaultMode)
		}
	}
	for _, want := range []string{"discord", "telegram", "slack", "google-chat", "whatsapp", "whatsapp-cloud", "signal", "matrix", "mattermost", "email", "sms", "microsoft-teams", "line", "ntfy", "simplex", "open-webui", "webhook", "raft", "irc", "buzz"} {
		if !seen[want] {
			t.Fatalf("missing %s", want)
		}
	}
	if p, _ := ByID("discord"); !p.Capabilities.Threads {
		t.Fatal("discord must support threads")
	}
	if p, _ := ByID("telegram"); !p.Capabilities.Threads {
		t.Fatal("telegram must support topics/threads")
	}
}

func TestRenderTemplate(t *testing.T) {
	title, text := renderTemplate([]byte(`{"title":"{{event_type}}","text":"{{aggregate_id}} {{payload}} {{task_result}}"}`), "task.completed", "task_1", `{"ok":true}`, map[string]string{"task_result": "done"})
	if title != "task.completed" || text != `task_1 {"ok":true} done` {
		t.Fatalf("unexpected render %q %q", title, text)
	}
}

func TestFixedGatewayCredentialDestinationIsPinned(t *testing.T) {
	p, _ := ByID("discord")
	if _, err := nativeBase(p, "https://evil.example"); err == nil {
		t.Fatal("discord credential must not be sent to custom host")
	}
	if got, err := nativeBase(p, "https://discord.com"); err != nil || got != "https://discord.com" {
		t.Fatalf("canonical host rejected: %q %v", got, err)
	}
	m, _ := ByID("matrix")
	if _, err := nativeBase(m, "https://matrix.example.org"); err != nil {
		t.Fatalf("self-hosted matrix should allow HTTPS homeserver: %v", err)
	}
	if _, err := nativeBase(m, "http://matrix.example.org"); err == nil {
		t.Fatal("plaintext remote homeserver must be rejected")
	}
}
