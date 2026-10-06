package browserworkspace

import (
	"github.com/DigiLogicTech/OnePane/internal/policy"
	"testing"
)

func TestBrowserWorkspaceIsNeverSchedulable(t *testing.T) {
	if DefaultPolicy().Schedulable() {
		t.Fatal("interactive browser cannot be an autonomous provider")
	}
}
func TestModelOutputImportsUnverified(t *testing.T) {
	l, err := ImportLabel("ws", ModelOutput)
	if err != nil || l.Trust != policy.TrustUnverifiedDerived {
		t.Fatalf("%v %#v", err, l)
	}
}

func TestBrowserWorkspaceProtectsCredentialsAndSubscriptionUse(t *testing.T) {
	p := DefaultPolicy()
	if p.CredentialBoundary != "browser_profile_only" || p.CredentialExtraction != "forbidden" {
		t.Fatalf("credential boundary weakened: %#v", p)
	}
	if p.SubscriptionUse != "user_initiated_only" || p.ImportMode != "explicit_user_import" {
		t.Fatalf("unexpected browser subscription policy: %#v", p)
	}
}
