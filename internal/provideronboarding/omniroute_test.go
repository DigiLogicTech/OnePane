package provideronboarding

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProbeOmniRouteDetectsStrictZeroCost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"auto"},{"id":"auto/coding"}]}`))
		case "/api/settings/export-json":
			_, _ = w.Write([]byte(`{"routing":{"freeAccessPolicy":"strict"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	u := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1) + "/v1"
	s := New(nil, nil)
	p, err := s.ProbeOmniRoute(context.Background(), u, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !p.StrictZeroCostVerified || len(p.Models) != 2 {
		t.Fatalf("probe %#v", p)
	}
}
func TestNormalizeOmniRouteAllowsRemoteHTTPS(t *testing.T) {
	got, err := normalizeOmniURL("https://gateway.example.com/v1")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://gateway.example.com/v1" {
		t.Fatalf("got %q", got)
	}
}

func TestNormalizeOmniRouteRejectsRemoteHTTP(t *testing.T) {
	if _, err := normalizeOmniURL("http://gateway.example.com/v1"); err == nil {
		t.Fatal("expected insecure remote endpoint rejection")
	}
}

func TestBuiltinsPreferOmniRouteAndProtectChatGPTAllowance(t *testing.T) {
	var omni, plan *Preset
	for i := range Builtins() {
		p := Builtins()[i]
		switch p.ID {
		case PresetOmniRoute:
			omni = &p
		case PresetOpenAIChatGPTPlan:
			plan = &p
		}
	}
	if omni == nil || !omni.RecommendedForFirstRun || !omni.DefaultZeroCostOnly {
		t.Fatalf("OmniRoute should be recommended zero-cost-first preset: %#v", omni)
	}
	if plan == nil || plan.RecommendedForFirstRun || !plan.RequiresExplicitUsageOptIn || plan.UsagePool != "chatgpt_work_codex" {
		t.Fatalf("ChatGPT plan allowance should be protected fallback: %#v", plan)
	}
}

func TestFirstRunPresetsExposeOnlyRecommendedProviders(t *testing.T) {
	ps := FirstRunPresets()
	if len(ps) != 1 || ps[0].ID != PresetOmniRoute || !ps[0].DefaultZeroCostOnly {
		t.Fatalf("unexpected first-run presets: %#v", ps)
	}
}
