package authority

import "testing"

func TestLeaseStateTransitionsExhaustive(t *testing.T) {
	states := []Status{StatusActive, StatusExpired, StatusRevoked, StatusExhausted, StatusInvalidated}
	allowed := map[[2]Status]bool{
		{StatusActive, StatusExpired}:     true,
		{StatusActive, StatusRevoked}:     true,
		{StatusActive, StatusExhausted}:   true,
		{StatusActive, StatusInvalidated}: true,
	}
	for _, from := range states {
		for _, to := range states {
			if got, want := CanTransition(from, to), allowed[[2]Status{from, to}]; got != want {
				t.Fatalf("CanTransition(%s,%s)=%v want %v", from, to, got, want)
			}
		}
	}
}

func TestScopeIsFailClosedAndExact(t *testing.T) {
	if err := (Scope{}).Validate(); err == nil {
		t.Fatal("empty scope must be rejected")
	}
	s := Scope{
		ResourceRefs:     []string{"repo://alpha/file.txt"},
		ResourcePrefixes: []string{"repo://alpha/src/"},
		Actions:          []ActionMode{ActionRead, ActionMutate},
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		ref  string
		want bool
	}{
		{"repo://alpha/file.txt", true},
		{"repo://alpha/src/main.go", true},
		{"repo://alpha/src2/main.go", false},
		{"repo://beta/file.txt", false},
	}
	for _, tc := range cases {
		if got := s.AllowsResource(tc.ref); got != tc.want {
			t.Fatalf("AllowsResource(%q)=%v want %v", tc.ref, got, tc.want)
		}
	}
	if !s.AllowsAction(ActionRead) || s.AllowsAction(ActionExternalSend) {
		t.Fatal("action scope mismatch")
	}
}
