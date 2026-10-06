package ingress

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

const maxPreviewResponseBytes int64 = 64 << 20

type RouteResolver interface {
	ResolveIngressRoute(context.Context, string) (projectworkspace.IngressRoute, error)
}

type Server struct {
	mux      *http.ServeMux
	routes   RouteResolver
	access   AccessChecker
	sessions *Sessions
}

func NewServer(routes RouteResolver, access AccessChecker, sessions *Sessions) *Server {
	s := &Server{mux: http.NewServeMux(), routes: routes, access: access, sessions: sessions}
	s.mux.HandleFunc("GET /.onepane/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"ok"}\n`)
	})
	s.mux.HandleFunc("GET /.onepane/session/{token}", s.exchangeSession)
	s.mux.HandleFunc("/", s.proxyEndpoint)
	return s
}
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
		s.mux.ServeHTTP(w, r)
	})
}
func (s *Server) exchangeSession(w http.ResponseWriter, r *http.Request) {
	if s.sessions == nil {
		http.Error(w, "preview unavailable", http.StatusServiceUnavailable)
		return
	}
	var existing string
	if c, err := r.Cookie(PreviewCookieName); err == nil {
		existing = c.Value
	}
	cookieValue, _, expiresAt, err := s.sessions.ConsumeBootstrap(r.PathValue("token"), existing, r.Host)
	if err != nil {
		http.Error(w, "invalid or expired preview session", http.StatusUnauthorized)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: PreviewCookieName, Value: cookieValue, Path: "/", HttpOnly: true, Secure: s.sessions.SecureCookie(), SameSite: http.SameSiteStrictMode, Expires: time.UnixMilli(expiresAt)})
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
func (s *Server) proxyEndpoint(w http.ResponseWriter, r *http.Request) {
	if s.sessions == nil || s.routes == nil || s.access == nil {
		http.Error(w, "preview unavailable", http.StatusServiceUnavailable)
		return
	}
	c, err := r.Cookie(PreviewCookieName)
	if err != nil {
		http.Error(w, "preview session required", http.StatusUnauthorized)
		return
	}
	endpointID, principalID, credentialID, _, err := s.sessions.AuthorizeHost(c.Value, r.Host)
	if err != nil {
		http.Error(w, "preview session required", http.StatusUnauthorized)
		return
	}
	route, err := s.routes.ResolveIngressRoute(r.Context(), endpointID)
	if err != nil {
		http.Error(w, "preview route unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := s.access.CheckPreviewAccess(r.Context(), principalID, credentialID, route.WorkspaceID); err != nil {
		http.Error(w, "preview access revoked", http.StatusForbidden)
		return
	}
	if route.Endpoint.Protocol != "http" {
		http.Error(w, "this endpoint protocol is not yet previewable", http.StatusNotImplemented)
		return
	}
	if route.Route.HostIP != "127.0.0.1" || route.Route.HostPort < 1 || route.Route.HostPort > 65535 || route.Route.TransportProtocol != "tcp" {
		http.Error(w, "invalid preview route", http.StatusServiceUnavailable)
		return
	}
	prefix := "/"
	if route.Endpoint.PathPrefix != nil {
		prefix = *route.Endpoint.PathPrefix
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<20)
	proto := "http"
	if s.sessions.SecureCookie() {
		proto = "https"
	}
	newLoopbackProxy(route.Route.HostPort, prefix, r.URL.Path, r.Host, proto).ServeHTTP(w, r)
}

func upstreamPath(prefix, requestPath string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "/"
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	requestPath = "/" + strings.TrimPrefix(requestPath, "/")
	clean := path.Join(prefix, requestPath)
	if strings.HasSuffix(requestPath, "/") && !strings.HasSuffix(clean, "/") {
		clean += "/"
	}
	if clean == "." {
		clean = "/"
	}
	return clean
}
func stripReservedCookieHeader(h http.Header) {
	cookies := h.Values("Cookie")
	if len(cookies) == 0 {
		return
	}
	var keep []string
	for _, line := range cookies {
		for _, part := range strings.Split(line, ";") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			name, _, _ := strings.Cut(part, "=")
			if strings.TrimSpace(name) == PreviewCookieName {
				continue
			}
			keep = append(keep, part)
		}
	}
	h.Del("Cookie")
	if len(keep) > 0 {
		h.Set("Cookie", strings.Join(keep, "; "))
	}
}
func stripReservedSetCookies(h http.Header) {
	values := h.Values("Set-Cookie")
	if len(values) == 0 {
		return
	}
	h.Del("Set-Cookie")
	for _, v := range values {
		name, _, _ := strings.Cut(v, "=")
		if strings.TrimSpace(name) == PreviewCookieName {
			continue
		}
		h.Add("Set-Cookie", v)
	}
}

func newLoopbackProxy(hostPort int, prefix, requestPath, forwardedHost, forwardedProto string) *httputil.ReverseProxy {
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(hostPort))
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if network != "tcp" && network != "tcp4" && network != "tcp6" {
			return nil, fmt.Errorf("unsupported preview network")
		}
		if address != addr {
			return nil, fmt.Errorf("preview target changed")
		}
		return (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}).DialContext(ctx, "tcp", addr)
	}, ResponseHeaderTimeout: 20 * time.Second, IdleConnTimeout: 60 * time.Second}
	return &httputil.ReverseProxy{
		Transport: transport,
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme = "http"
			pr.Out.URL.Host = addr
			pr.Out.URL.Path = upstreamPath(prefix, requestPath)
			pr.Out.URL.RawPath = ""
			pr.Out.Host = addr
			pr.Out.Header.Del("Authorization")
			pr.Out.Header.Del("Proxy-Authorization")
			stripReservedCookieHeader(pr.Out.Header)
			pr.Out.Header.Del("X-Forwarded-For")
			pr.Out.Header.Del("X-Forwarded-Host")
			pr.Out.Header.Del("X-Forwarded-Proto")
			pr.Out.Header.Set("X-Forwarded-Host", forwardedHost)
			pr.Out.Header.Set("X-Forwarded-Proto", forwardedProto)
		},
		ModifyResponse: func(resp *http.Response) error {
			stripReservedSetCookies(resp.Header)
			if resp.ContentLength > maxPreviewResponseBytes {
				return errors.New("preview response exceeds limit")
			}
			resp.Header.Set("Referrer-Policy", "no-referrer")
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(w, "preview upstream unavailable", http.StatusBadGateway)
		},
	}
}
