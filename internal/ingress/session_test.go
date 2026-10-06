package ingress

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

type testClock struct{ t time.Time }

func (c *testClock) Now() time.Time   { return c.t }
func (c *testClock) UnixMilli() int64 { return c.t.UnixMilli() }

func TestBootstrapIsOneTimeHostBoundAndShortLived(t *testing.T) {
	clk := &testClock{t: time.Unix(100, 0).UTC()}
	s, err := NewSessions("http://localhost:8081", clk)
	if err != nil {
		t.Fatal(err)
	}
	u, expires, err := s.MintEndpointGrant("ep", "p", "cred")
	if err != nil || u == "" {
		t.Fatalf("mint: %v %q", err, u)
	}
	if expires != clk.UnixMilli()+time.Minute.Milliseconds() {
		t.Fatalf("bootstrap expiry=%d", expires)
	}
	parsed, _ := url.Parse(u)
	if !strings.HasPrefix(parsed.Hostname(), "ep-") || !strings.HasSuffix(parsed.Hostname(), ".localhost") {
		t.Fatalf("endpoint origin=%s", parsed.Host)
	}
	token := parsed.Path[strings.LastIndex(parsed.Path, "/")+1:]
	if _, _, _, err := s.ConsumeBootstrap(token, "", "wrong.localhost:8081"); err == nil {
		t.Fatal("bootstrap accepted on wrong endpoint origin")
	}

	// Wrong-origin use is deliberately one-time/burned. Mint another grant.
	u, _, _ = s.MintEndpointGrant("ep", "p", "cred")
	parsed, _ = url.Parse(u)
	token = parsed.Path[strings.LastIndex(parsed.Path, "/")+1:]
	cookie, endpoint, _, err := s.ConsumeBootstrap(token, "", parsed.Host)
	if err != nil || cookie == "" || endpoint != "ep" {
		t.Fatalf("consume: %v", err)
	}
	if _, _, _, err := s.ConsumeBootstrap(token, "", parsed.Host); err == nil {
		t.Fatal("bootstrap reused")
	}
	if endpoint, p, c, _, err := s.AuthorizeHost(cookie, parsed.Host); err != nil || endpoint != "ep" || p != "p" || c != "cred" {
		t.Fatalf("authorize=%s %s/%s %v", endpoint, p, c, err)
	}
	if _, _, _, _, err := s.AuthorizeHost(cookie, "other.localhost:8081"); err == nil {
		t.Fatal("preview cookie accepted on wrong origin")
	}

	u, _, _ = s.MintEndpointGrant("late", "p", "cred")
	parsed, _ = url.Parse(u)
	token = parsed.Path[strings.LastIndex(parsed.Path, "/")+1:]
	clk.t = clk.t.Add(2 * time.Minute)
	if _, _, _, err := s.ConsumeBootstrap(token, "", parsed.Host); err == nil {
		t.Fatal("expired bootstrap accepted")
	}
}
