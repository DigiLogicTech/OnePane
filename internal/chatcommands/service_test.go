package chatcommands

import (
	"context"
	"errors"
	"testing"
)

type authStub struct{ denyYolo bool }

func (a authStub) CanControlSession(string, string) error { return nil }
func (a authStub) CanEnableAutoApproval(string) error {
	if a.denyYolo {
		return errors.New("denied")
	}
	return nil
}

func TestParseAliasesAndQuotes(t *testing.T) {
	c, err := Parse(`/bg "check auth package"`)
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "background" || len(c.Args) != 1 || c.Args[0] != "check auth package" {
		t.Fatalf("unexpected %#v", c)
	}
}
func TestQueueFIFOAndMove(t *testing.T) {
	ctx := context.Background()
	s := NewService(NewMemoryStore())
	for _, p := range []string{"first", "second", "third"} {
		if _, err := s.Handle(ctx, "s1", "/queue "+p); err != nil {
			t.Fatal(err)
		}
	}
	r, err := s.Handle(ctx, "s1", "/queue move 3 1")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"third", "first", "second"}
	for i, q := range r.Queue {
		if q.Prompt != want[i] {
			t.Fatalf("pos %d got %q", i, q.Prompt)
		}
	}
}
func TestQueuePausePersists(t *testing.T) {
	ctx := context.Background()
	s := NewService(NewMemoryStore())
	r, err := s.Handle(ctx, "s1", "/queue pause")
	if err != nil {
		t.Fatal(err)
	}
	if r.Controls == nil || !r.Controls.QueuePaused {
		t.Fatal("queue should be paused")
	}
}
func TestBusyMode(t *testing.T) {
	ctx := context.Background()
	s := NewService(NewMemoryStore())
	r, err := s.Handle(ctx, "s1", "/busy steer")
	if err != nil {
		t.Fatal(err)
	}
	if r.Controls.BusyMode != BusySteer {
		t.Fatalf("got %s", r.Controls.BusyMode)
	}
}
func TestYoloSessionOnly(t *testing.T) {
	ctx := context.Background()
	s := NewService(NewMemoryStore())
	s.Authority = authStub{}
	r, err := s.Handle(ctx, "s1", "/yolo on")
	if err != nil {
		t.Fatal(err)
	}
	if r.Controls.ApprovalMode != ApprovalAutoAuthorized {
		t.Fatalf("got %s", r.Controls.ApprovalMode)
	}
	r2, err := s.Handle(ctx, "s2", "/yolo status")
	if err != nil {
		t.Fatal(err)
	}
	if r2.Controls.ApprovalMode != ApprovalManual {
		t.Fatal("yolo leaked between sessions")
	}
}
func TestYoloRequiresUserAuthority(t *testing.T) {
	ctx := context.Background()
	s := NewService(NewMemoryStore())
	s.Authority = authStub{denyYolo: true}
	if _, err := s.Handle(ctx, "s1", "/yolo on"); err == nil {
		t.Fatal("expected denial")
	}
}
func TestUnknownCommand(t *testing.T) {
	if _, err := Parse("/doesnotexist"); !errors.Is(err, ErrUnknownCommand) {
		t.Fatalf("got %v", err)
	}
}

func TestApprovalProfilesDefaultMediumAndSessionOnly(t *testing.T) {
	ctx := context.Background()
	s := NewService(NewMemoryStore())
	r, err := s.Handle(ctx, "s1", "/approvals status")
	if err != nil {
		t.Fatal(err)
	}
	if r.Controls == nil || r.Controls.ApprovalLevel != ApprovalMedium {
		t.Fatalf("default got %v", r.Controls)
	}
	r, err = s.Handle(ctx, "s1", "/approvals low")
	if err != nil {
		t.Fatal(err)
	}
	if r.Controls.ApprovalLevel != ApprovalLow || r.Controls.ApprovalMode != ApprovalManual {
		t.Fatalf("unexpected controls %#v", r.Controls)
	}
	r2, err := s.Handle(ctx, "s2", "/approvals status")
	if err != nil {
		t.Fatal(err)
	}
	if r2.Controls.ApprovalLevel != ApprovalMedium {
		t.Fatal("approval level leaked between sessions")
	}
}

func TestApprovalDecisionMatrix(t *testing.T) {
	cases := []struct {
		name     string
		controls SessionControls
		risk     ApprovalRisk
		want     ApprovalDecision
	}{
		{"high-low", SessionControls{ApprovalLevel: ApprovalHigh}, RiskLow, DecisionPrompt},
		{"medium-low", SessionControls{ApprovalLevel: ApprovalMedium}, RiskLow, DecisionAutoApprove},
		{"medium-medium", SessionControls{ApprovalLevel: ApprovalMedium}, RiskMedium, DecisionPrompt},
		{"low-medium", SessionControls{ApprovalLevel: ApprovalLow}, RiskMedium, DecisionAutoApprove},
		{"low-high", SessionControls{ApprovalLevel: ApprovalLow}, RiskHigh, DecisionPrompt},
		{"low-critical", SessionControls{ApprovalLevel: ApprovalLow}, RiskCritical, DecisionPrompt},
		{"yolo-critical", SessionControls{ApprovalLevel: ApprovalHigh, ApprovalMode: ApprovalAutoAuthorized}, RiskCritical, DecisionAutoApprove},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DecideApproval(tc.controls, tc.risk, true, false); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
	if got := DecideApproval(SessionControls{ApprovalLevel: ApprovalLow, ApprovalMode: ApprovalAutoAuthorized}, RiskLow, true, true); got != DecisionDeny {
		t.Fatalf("hard denial got %s", got)
	}
	if got := DecideApproval(SessionControls{ApprovalLevel: ApprovalLow}, RiskLow, false, false); got != DecisionDeny {
		t.Fatalf("unauthorized got %s", got)
	}
}

func TestApprovalProfileExitsYolo(t *testing.T) {
	ctx := context.Background()
	s := NewService(NewMemoryStore())
	s.Authority = authStub{}
	if _, err := s.Handle(ctx, "s1", "/yolo on"); err != nil {
		t.Fatal(err)
	}
	r, err := s.Handle(ctx, "s1", "/approvals medium")
	if err != nil {
		t.Fatal(err)
	}
	if r.Controls.ApprovalMode != ApprovalManual || r.Controls.ApprovalLevel != ApprovalMedium {
		t.Fatalf("unexpected controls %#v", r.Controls)
	}
}
