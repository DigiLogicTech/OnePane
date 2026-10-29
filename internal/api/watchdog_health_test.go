package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type healthWatchdog struct{ healthy bool }

func (w *healthWatchdog) Healthy(context.Context) bool { return w.healthy }

func TestHealthReportsObservedStateNotAPILiveness(t *testing.T) {
	s := NewServer(nil, nil, nil)
	w := &healthWatchdog{healthy: true}
	s.SetWatchdog(w)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/health", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("liveness HTTP status = %d", rr.Code)
	}
	var up map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &up); err != nil {
		t.Fatal(err)
	}
	if up["status"] == "ok" || up["worker"] != "unobserved" {
		t.Fatalf("API liveness was reported healthy: %v", up)
	}

	okPart := func(context.Context) (string, string) { return "ok", "" }
	s.SetObservedHealth(okPart, okPart, okPart)
	for _, healthy := range []bool{true, false, true} {
		w.healthy = healthy
		rr = httptest.NewRecorder()
		s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/health", nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("liveness HTTP status = %d", rr.Code)
		}
		var body map[string]string
		if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		want := "degraded"
		if healthy {
			want = "ok"
		}
		if body["status"] != want {
			t.Fatalf("health = %v, want %s", body, want)
		}
		if !healthy && body["watchdog"] != "stale_or_unavailable" {
			t.Fatalf("missing stale watchdog signal: %v", body)
		}
		if healthy && (body["worker"] != "ok" || body["assurance"] != "ok" || body["runtime"] != "ok") {
			t.Fatalf("healthy snapshot hid a part: %v", body)
		}
	}

	s.SetObservedHealth(func(context.Context) (string, string) {
		return "halted", "autonomous worker is parking an uncommitted write"
	}, okPart, func(context.Context) (string, string) {
		return "degraded", "omniroute degraded"
	})
	w.healthy = true
	rr = httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/health", nil))
	var halted map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &halted); err != nil {
		t.Fatal(err)
	}
	if halted["status"] != "halted" || halted["worker"] != "halted" || halted["runtime"] != "degraded" {
		t.Fatalf("halted worker was hidden: %v", halted)
	}
	if halted["reason"] == "" {
		t.Fatal("halted health omitted the reason")
	}
}
