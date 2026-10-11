package main

import (
	"errors"
	"testing"
	"time"
)

func TestFederationReceiptPruneRetriesAfterTransientError(t *testing.T) {
	var s federationReceiptPruneSchedule
	now := time.Date(2026, time.October, 11, 0, 0, 0, 0, time.UTC)
	locked := errors.New("database is locked")
	calls := 0
	prune := func() (int, error) {
		calls++
		if calls == 1 {
			return 0, locked
		}
		return 0, nil
	}
	if err := s.RunIfDue(now, prune); !errors.Is(err, locked) || calls != 1 {
		t.Fatalf("first attempt: calls=%d err=%v", calls, err)
	}
	if err := s.RunIfDue(now.Add(14*time.Minute), prune); err != nil || calls != 1 {
		t.Fatalf("retry backoff bypassed: calls=%d err=%v", calls, err)
	}
	if err := s.RunIfDue(now.Add(15*time.Minute), prune); err != nil || calls != 2 {
		t.Fatalf("failed prune was not retried: calls=%d err=%v", calls, err)
	}
	if err := s.RunIfDue(now.Add(23*time.Hour), prune); err != nil || calls != 2 {
		t.Fatalf("successful sweep retried early: calls=%d err=%v", calls, err)
	}
	if err := s.RunIfDue(now.Add(24*time.Hour+15*time.Minute), prune); err != nil || calls != 3 {
		t.Fatalf("daily retention sweep not scheduled: calls=%d err=%v", calls, err)
	}
}

func TestFederationReceiptPruneHasBoundedWork(t *testing.T) {
	var s federationReceiptPruneSchedule
	now := time.Date(2026, time.October, 11, 0, 0, 0, 0, time.UTC)
	calls := 0
	prune := func() (int, error) {
		calls++
		return federationReceiptPruneBatchSize, nil
	}
	if err := s.RunIfDue(now, prune); err != nil || calls != federationReceiptPruneMaxBatches {
		t.Fatalf("unbounded sweep: calls=%d err=%v", calls, err)
	}
	if err := s.RunIfDue(now.Add(time.Hour), prune); err != nil || calls != federationReceiptPruneMaxBatches {
		t.Fatalf("daily limit violated: calls=%d err=%v", calls, err)
	}
	if err := s.RunIfDue(now.Add(24*time.Hour), prune); err != nil || calls != 2*federationReceiptPruneMaxBatches {
		t.Fatalf("subsequent daily sweep missing: calls=%d err=%v", calls, err)
	}
}

func TestFederationReceiptPruneRejectsInvalidBatchCounts(t *testing.T) {
	for _, rows := range []int{-1, federationReceiptPruneBatchSize + 1} {
		var s federationReceiptPruneSchedule
		now := time.Date(2026, time.October, 11, 0, 0, 0, 0, time.UTC)
		if err := s.RunIfDue(now, func() (int, error) { return rows, nil }); err == nil {
			t.Fatalf("invalid %d row result accepted", rows)
		}
		if !s.lastCompleted.IsZero() {
			t.Fatalf("invalid %d result marked the sweep successful", rows)
		}
	}
}
