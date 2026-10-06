package accountaccess

import (
	"net/url"
	"testing"
)

func TestPrepareChatGPTPlanAuthorization(t *testing.T) {
	p, err := PrepareChatGPTPlanAuthorization("urn:uuid:123", "Harness", "http://127.0.0.1:1455/auth/callback", "")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(p.AuthorizationURL)
	q := u.Query()
	if q.Get("client_id") != DynamicAgentClientID || q.Get("scope") == "" || q.Get("code_challenge") == "" || p.CodeVerifier == "" {
		t.Fatalf("bad flow %#v", p)
	}
	if _, err := ValidateChatGPTCallback(p, Callback{Code: "c", State: p.State, IssuedClientID: "oaiapp_test"}); err != nil {
		t.Fatal(err)
	}
}
func TestChatGPTCallbackRejectsState(t *testing.T) {
	p, _ := PrepareChatGPTPlanAuthorization("urn:uuid:123", "Harness", "http://127.0.0.1:1455/auth/callback", "")
	if _, err := ValidateChatGPTCallback(p, Callback{Code: "c", State: "wrong", IssuedClientID: "oaiapp_test"}); err == nil {
		t.Fatal("expected state failure")
	}
}
func TestHasPlanScope(t *testing.T) {
	if !HasChatGPTPlanScope("openid chatgpt.tokens.use.direct resource.invoke") {
		t.Fatal("scope missing")
	}
}
