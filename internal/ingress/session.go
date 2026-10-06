package ingress

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/clock"
)

var (
	ErrInvalidSession = errors.New("invalid preview session")
	ErrExpiredSession = errors.New("expired preview session")
)

const PreviewCookieName = "onepane_preview"

type endpointGrant struct {
	EndpointID   string
	PrincipalID  string
	CredentialID string
	ExpiresAt    int64
}
type bootstrapGrant struct {
	endpointGrant
	BootstrapExpiresAt int64
}
type browserSession struct {
	Grants    map[string]endpointGrant
	ExpiresAt int64
}

type Sessions struct {
	mu           sync.Mutex
	clock        clock.Clock
	baseOrigin   *url.URL
	bootstrapTTL time.Duration
	sessionTTL   time.Duration
	bootstraps   map[[32]byte]bootstrapGrant
	browsers     map[[32]byte]browserSession
}

func NewSessions(origin string, clk clock.Clock) (*Sessions, error) {
	u, err := url.Parse(strings.TrimSpace(origin))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return nil, ErrInvalidSession
	}
	u.Path = ""
	return &Sessions{clock: clk, baseOrigin: u, bootstrapTTL: time.Minute, sessionTTL: time.Hour, bootstraps: map[[32]byte]bootstrapGrant{}, browsers: map[[32]byte]browserSession{}}, nil
}

func (s *Sessions) MintEndpointGrant(endpointID, principalID, credentialID string) (string, int64, error) {
	if s == nil || strings.TrimSpace(endpointID) == "" || strings.TrimSpace(principalID) == "" || strings.TrimSpace(credentialID) == "" {
		return "", 0, ErrInvalidSession
	}
	raw, err := randomToken()
	if err != nil {
		return "", 0, err
	}
	now := s.clock.UnixMilli()
	expires := now + s.bootstrapTTL.Milliseconds()
	h := sha256.Sum256([]byte(raw))
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(now)
	s.bootstraps[h] = bootstrapGrant{endpointGrant: endpointGrant{EndpointID: endpointID, PrincipalID: principalID, CredentialID: credentialID, ExpiresAt: now + s.sessionTTL.Milliseconds()}, BootstrapExpiresAt: expires}
	origin, err := s.EndpointOrigin(endpointID)
	if err != nil {
		delete(s.bootstraps, h)
		return "", 0, err
	}
	return strings.TrimRight(origin, "/") + "/.onepane/session/" + url.PathEscape(raw), expires, nil
}

func (s *Sessions) EndpointOrigin(endpointID string) (string, error) {
	if s == nil || s.baseOrigin == nil || strings.TrimSpace(endpointID) == "" {
		return "", ErrInvalidSession
	}
	hostname := s.baseOrigin.Hostname()
	if hostname == "" || net.ParseIP(hostname) != nil {
		return "", ErrInvalidSession
	}
	h := sha256.Sum256([]byte(endpointID))
	label := "ep-" + hex.EncodeToString(h[:16])
	host := label + "." + hostname
	if port := s.baseOrigin.Port(); port != "" {
		host = net.JoinHostPort(host, port)
	}
	u := *s.baseOrigin
	u.Host = host
	return strings.TrimRight(u.String(), "/"), nil
}

func (s *Sessions) ConsumeBootstrap(raw, existingCookie, requestHost string) (cookie string, endpointID string, expiresAt int64, err error) {
	if s == nil || strings.TrimSpace(raw) == "" || strings.TrimSpace(requestHost) == "" {
		return "", "", 0, ErrInvalidSession
	}
	now := s.clock.UnixMilli()
	h := sha256.Sum256([]byte(raw))
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(now)
	b, ok := s.bootstraps[h]
	if !ok {
		return "", "", 0, ErrInvalidSession
	}
	delete(s.bootstraps, h) // one-time exchange regardless of later outcome
	if now >= b.BootstrapExpiresAt {
		return "", "", 0, ErrExpiredSession
	}
	expectedOrigin, err := s.EndpointOrigin(b.EndpointID)
	if err != nil {
		return "", "", 0, err
	}
	expectedURL, _ := url.Parse(expectedOrigin)
	if !sameHost(requestHost, expectedURL.Host) {
		return "", "", 0, ErrInvalidSession
	}

	var browserRaw string
	var browserHash [32]byte
	var bs browserSession
	if strings.TrimSpace(existingCookie) != "" {
		browserRaw = existingCookie
		browserHash = sha256.Sum256([]byte(browserRaw))
		bs, ok = s.browsers[browserHash]
		if !ok || now >= bs.ExpiresAt {
			browserRaw = ""
		}
	}
	if browserRaw == "" {
		browserRaw, err = randomToken()
		if err != nil {
			return "", "", 0, err
		}
		browserHash = sha256.Sum256([]byte(browserRaw))
		bs = browserSession{Grants: map[string]endpointGrant{}}
	}
	if bs.Grants == nil {
		bs.Grants = map[string]endpointGrant{}
	}
	bs.ExpiresAt = now + s.sessionTTL.Milliseconds()
	grant := b.endpointGrant
	grant.ExpiresAt = bs.ExpiresAt
	bs.Grants[grant.EndpointID] = grant
	s.browsers[browserHash] = bs
	return browserRaw, grant.EndpointID, bs.ExpiresAt, nil
}

func (s *Sessions) AuthorizeHost(cookie, requestHost string) (endpointID, principalID, credentialID string, expiresAt int64, err error) {
	if s == nil || strings.TrimSpace(cookie) == "" || strings.TrimSpace(requestHost) == "" {
		return "", "", "", 0, ErrInvalidSession
	}
	now := s.clock.UnixMilli()
	h := sha256.Sum256([]byte(cookie))
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(now)
	bs, ok := s.browsers[h]
	if !ok || now >= bs.ExpiresAt {
		return "", "", "", 0, ErrExpiredSession
	}
	for id, g := range bs.Grants {
		if now >= g.ExpiresAt {
			continue
		}
		origin, e := s.EndpointOrigin(id)
		if e != nil {
			continue
		}
		u, _ := url.Parse(origin)
		if sameHost(requestHost, u.Host) {
			return id, g.PrincipalID, g.CredentialID, g.ExpiresAt, nil
		}
	}
	return "", "", "", 0, ErrInvalidSession
}

func (s *Sessions) SecureCookie() bool {
	return s != nil && s.baseOrigin != nil && s.baseOrigin.Scheme == "https"
}

func (s *Sessions) cleanupLocked(now int64) {
	for k, v := range s.bootstraps {
		if now >= v.BootstrapExpiresAt {
			delete(s.bootstraps, k)
		}
	}
	for k, v := range s.browsers {
		if now >= v.ExpiresAt {
			delete(s.browsers, k)
		}
	}
}

func sameHost(a, b string) bool {
	return strings.EqualFold(strings.TrimSuffix(strings.TrimSpace(a), "."), strings.TrimSuffix(strings.TrimSpace(b), "."))
}
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
