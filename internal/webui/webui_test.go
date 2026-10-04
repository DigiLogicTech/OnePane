package webui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRootAndIndexServeDirectlyWithoutRedirect(t *testing.T) {
	h := Handler()

	for _, target := range []string{"/", "/index.html"} {
		t.Run(target, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, target, nil)
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)

			res := rr.Result()
			defer res.Body.Close()

			if res.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusOK)
			}
			if got := res.Header.Get("Location"); got != "" {
				t.Fatalf("unexpected redirect Location %q", got)
			}
			body, err := io.ReadAll(res.Body)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(strings.ToLower(string(body)), "<html") {
				t.Fatalf("response does not look like the embedded UI shell")
			}
		})
	}
}

func TestSPAFallbackServesShellWithoutRedirect(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/models/deep/link", nil)
	rr := httptest.NewRecorder()
	Handler().ServeHTTP(rr, req)

	res := rr.Result()
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusOK)
	}
	if got := res.Header.Get("Location"); got != "" {
		t.Fatalf("unexpected redirect Location %q", got)
	}
}

func TestStaticAssetStillServed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	rr := httptest.NewRecorder()
	Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if rr.Body.Len() == 0 {
		t.Fatal("app.js response is empty")
	}
}
