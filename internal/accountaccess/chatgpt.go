package accountaccess

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

const (
	ChatGPTAuthorizeURL  = "https://auth.openai.com/api/accounts/authorize"
	ChatGPTTokenURL      = "https://auth.openai.com/api/accounts/oauth/token"
	ChatGPTResource      = "https://api.openai.com/v1"
	DynamicAgentClientID = "dynamic_agent_client"
	ChatGPTPlanScope     = "chatgpt.tokens.use.direct"
	ChatGPTScopes        = "openid profile email offline_access resource.invoke chatgpt.tokens.use.direct"
)

type PendingAuthorization struct {
	ClientID            string `json:"client_id"`
	DynamicRegistration bool   `json:"dynamic_registration"`
	ExtAgentHostID      string `json:"ext_agent_host_id"`
	RedirectURI         string `json:"redirect_uri"`
	State               string `json:"state"`
	Nonce               string `json:"nonce"`
	CodeVerifier        string `json:"-"`
	AuthorizationURL    string `json:"authorization_url"`
}

type Callback struct{ Code, State, IssuedClientID, Error string }

var ErrInvalidOAuthFlow = errors.New("invalid ChatGPT plan OAuth flow")

func randomURLSafe(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func PrepareChatGPTPlanAuthorization(extHostID, agentName, redirectURI, existingClientID string) (PendingAuthorization, error) {
	if !strings.HasPrefix(extHostID, "urn:uuid:") || strings.TrimSpace(agentName) == "" {
		return PendingAuthorization{}, fmt.Errorf("%w: host id and agent name required", ErrInvalidOAuthFlow)
	}
	ru, err := url.Parse(redirectURI)
	if err != nil || ru.Scheme != "http" || ru.Hostname() != "127.0.0.1" || ru.Path != "/auth/callback" {
		return PendingAuthorization{}, fmt.Errorf("%w: redirect must be http://127.0.0.1:<port>/auth/callback", ErrInvalidOAuthFlow)
	}
	state, err := randomURLSafe(32)
	if err != nil {
		return PendingAuthorization{}, err
	}
	nonce, err := randomURLSafe(32)
	if err != nil {
		return PendingAuthorization{}, err
	}
	verifier, err := randomURLSafe(48)
	if err != nil {
		return PendingAuthorization{}, err
	}
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	clientID := strings.TrimSpace(existingClientID)
	dynamic := clientID == ""
	if dynamic {
		clientID = DynamicAgentClientID
	}
	q := url.Values{"client_id": {clientID}, "ext_agent_host_id": {extHostID}, "response_type": {"code"}, "redirect_uri": {redirectURI}, "scope": {ChatGPTScopes}, "resource": {ChatGPTResource}, "state": {state}, "nonce": {nonce}, "code_challenge_method": {"S256"}, "code_challenge": {challenge}}
	if dynamic {
		q.Set("agent_name_hint", agentName)
	}
	return PendingAuthorization{ClientID: clientID, DynamicRegistration: dynamic, ExtAgentHostID: extHostID, RedirectURI: redirectURI, State: state, Nonce: nonce, CodeVerifier: verifier, AuthorizationURL: ChatGPTAuthorizeURL + "?" + q.Encode()}, nil
}
func ValidateChatGPTCallback(p PendingAuthorization, cb Callback) (string, error) {
	if cb.State == "" || cb.State != p.State {
		return "", fmt.Errorf("%w: state mismatch", ErrInvalidOAuthFlow)
	}
	if cb.Error != "" {
		return "", fmt.Errorf("%w: authorization returned %s", ErrInvalidOAuthFlow, cb.Error)
	}
	if strings.TrimSpace(cb.Code) == "" {
		return "", fmt.Errorf("%w: authorization code missing", ErrInvalidOAuthFlow)
	}
	if p.DynamicRegistration {
		if strings.TrimSpace(cb.IssuedClientID) == "" || cb.IssuedClientID == DynamicAgentClientID {
			return "", fmt.Errorf("%w: issued client id missing", ErrInvalidOAuthFlow)
		}
		return cb.IssuedClientID, nil
	}
	if cb.IssuedClientID != "" && cb.IssuedClientID != p.ClientID {
		return "", fmt.Errorf("%w: issued client id changed", ErrInvalidOAuthFlow)
	}
	return p.ClientID, nil
}
func HasChatGPTPlanScope(scope string) bool {
	for _, s := range strings.Fields(scope) {
		if s == ChatGPTPlanScope {
			return true
		}
	}
	return false
}
