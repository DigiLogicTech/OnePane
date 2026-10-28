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

func TestHealthReportsWatchdogDegradationAndRecovery(t *testing.T) {
	s := NewServer(nil, nil, nil)
	w := &healthWatchdog{}
	s.SetWatchdog(w)
	for _, healthy := range []bool{true, false, true} {
		w.healthy = healthy
		rr := httptest.NewRecorder()
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
	}
}
