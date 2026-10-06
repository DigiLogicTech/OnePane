package routine

import (
	"testing"
	"time"
)

func TestNextIntervalTrigger(t *testing.T) {
	created := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	after := created.Add(90 * time.Minute)
	next, _, ok, err := nextTrigger(Trigger{Kind: "interval", EverySeconds: 3600}, "UTC", created, after)
	if err != nil || !ok {
		t.Fatalf("err=%v ok=%v", err, ok)
	}
	want := created.Add(2 * time.Hour)
	if !next.Equal(want) {
		t.Fatalf("next=%s want=%s", next, want)
	}
}

func TestNextDailyTriggerRespectsTimezoneAndWeekday(t *testing.T) {
	created := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	// Brisbane is UTC+10; 08:00 local is 22:00 UTC on the prior date.
	after := time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC)
	next, local, ok, err := nextTrigger(Trigger{Kind: "daily", LocalTime: "08:00"}, "Australia/Brisbane", created, after)
	if err != nil || !ok {
		t.Fatalf("err=%v ok=%v", err, ok)
	}
	if got := next.In(time.FixedZone("AEST", 10*3600)).Format("15:04"); got != "08:00" {
		t.Fatalf("local time=%s (%s)", got, local)
	}
}

func TestTriggerValidationRejectsSubMinuteInterval(t *testing.T) {
	if err := validateTrigger(Trigger{Kind: "interval", EverySeconds: 30}); err == nil {
		t.Fatal("expected rejection")
	}
}

func TestPolicyDefaultsToLatest(t *testing.T) {
	p, err := normalizePolicy(Policy{})
	if err != nil {
		t.Fatal(err)
	}
	if p.CatchUp != "latest" || p.MaxCatchUp != 10 {
		t.Fatalf("policy=%+v", p)
	}
}
