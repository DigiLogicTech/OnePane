package ingress

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

type fakeRouteResolver struct{ route projectworkspace.IngressRoute }

func (f fakeRouteResolver) ResolveIngressRoute(context.Context, string) (projectworkspace.IngressRoute, error) {
	return f.route, nil
}

type fakeAccess struct{ principal, credential, workspace string }

func (f *fakeAccess) CheckPreviewAccess(_ context.Context, p, c, w string) error {
	f.principal, f.credential, f.workspace = p, c, w
	return nil
}

func TestPreviewProxyIsPerEndpointOriginAndStripsControlPlaneCredential(t *testing.T) {
	var gotAuth, gotCookie, gotPath, gotQuery, gotForwardedHost string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCookie = r.Header.Get("Cookie")
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotForwardedHost = r.Header.Get("X-Forwarded-Host")
		w.Header().Add("Set-Cookie", PreviewCookieName+"=evil; Path=/")
		w.Header().Add("Set-Cookie", "app_session=ok; Path=/")
		_, _ = io.WriteString(w, "upstream-ok")
	}))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)
	_, pstr, _ := strings.Cut(u.Host, ":")
	port, _ := strconv.Atoi(pstr)

	clk := &testClock{t: time.Unix(100, 0).UTC()}
	sessions, _ := NewSessions("http://localhost:8081", clk)
	access := &fakeAccess{}
	appID := "app1"
	prefix := "/base"
	routes := fakeRouteResolver{route: projectworkspace.IngressRoute{WorkspaceID: "ws", ProjectID: "proj", Endpoint: projectworkspace.Endpoint{ID: "ep", ProjectRuntimeID: "rt", ApplicationID: &appID, Protocol: "http", PathPrefix: &prefix, DesiredState: "enabled", Status: "ready"}, Route: projectworkspace.EndpointRoute{EndpointID: "ep", ProjectRuntimeID: "rt", ApplicationID: appID, HostIP: "127.0.0.1", HostPort: port, TransportProtocol: "tcp", Status: "verified"}}}
	preview := NewServer(routes, access, sessions).Handler()

	bootstrapURL, _, err := sessions.MintEndpointGrant("ep", "principal", "credential")
	if err != nil {
		t.Fatal(err)
	}
	bu, _ := url.Parse(bootstrapURL)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, bu.Path, nil)
	req.Host = bu.Host
	preview.ServeHTTP(rr, req)
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/" {
		t.Fatalf("exchange=%d location=%q body=%s", rr.Code, rr.Header().Get("Location"), rr.Body.String())
	}
	var previewCookie *http.Cookie
	for _, c := range rr.Result().Cookies() {
		if c.Name == PreviewCookieName {
			previewCookie = c
		}
	}
	if previewCookie == nil || !previewCookie.HttpOnly {
		t.Fatal("missing HttpOnly preview cookie")
	}

	req = httptest.NewRequest(http.MethodGet, "/hello/world?x=1", nil)
	req.Host = bu.Host
	req.AddCookie(previewCookie)
	req.AddCookie(&http.Cookie{Name: "app_cookie", Value: "yes"})
	req.Header.Set("Authorization", "Bearer must-not-leak")
	rr = httptest.NewRecorder()
	preview.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || rr.Body.String() != "upstream-ok" {
		t.Fatalf("proxy=%d %q", rr.Code, rr.Body.String())
	}
	if gotAuth != "" {
		t.Fatalf("authorization leaked: %q", gotAuth)
	}
	if strings.Contains(gotCookie, PreviewCookieName) || !strings.Contains(gotCookie, "app_cookie=yes") {
		t.Fatalf("cookie forwarding=%q", gotCookie)
	}
	if gotPath != "/base/hello/world" || gotQuery != "x=1" {
		t.Fatalf("upstream target %q?%s", gotPath, gotQuery)
	}
	if gotForwardedHost != bu.Host {
		t.Fatalf("forwarded host=%q want %q", gotForwardedHost, bu.Host)
	}
	if access.principal != "principal" || access.credential != "credential" || access.workspace != "ws" {
		t.Fatalf("access check %#v", access)
	}
	for _, c := range rr.Result().Cookies() {
		if c.Name == PreviewCookieName {
			t.Fatalf("upstream overwrote reserved preview cookie: %#v", c)
		}
	}
	if !strings.Contains(strings.Join(rr.Header().Values("Set-Cookie"), "\n"), "app_session=ok") {
		t.Fatal("app cookie removed")
	}
}
