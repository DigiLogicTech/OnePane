package main

import (
	"fmt"
	"time"
)

const (
	federationReceiptPruneBatchSize = 128
	federationReceiptPruneMaxBatches = 16
	federationReceiptPruneSuccessInterval = 24 * time.Hour
	federationReceiptPruneRetryInterval = 15 * time.Minute
)

// federationReceiptPruneSchedule is owned by the single federation heartbeat
// goroutine. It limits maintenance work and retries transient DB failures,
// without delaying normal peer heartbeats or recreating idempotency receipts.
type federationReceiptPruneSchedule struct {
	lastAttempt time.Time
	lastCompleted time.Time
}

func (s *federationReceiptPruneSchedule) RunIfDue(now time.Time, prune func() (int, error)) error {
	if !s.lastCompleted.IsZero() && now.Sub(s.lastCompleted) < federationReceiptPruneSuccessInterval {
		return nil
	}
	if !s.lastAttempt.IsZero() && now.Sub(s.lastAttempt) < federationReceiptPruneRetryInterval {
		return nil
	}
	s.lastAttempt = now
	for batch := 0; batch < federationReceiptPruneMaxBatches; batch++ {
		redacted, err := prune()
		if err != nil {
			// Do not set lastCompleted: retry after a short backoff.
			// Previously a failed first batch suppressed retries for 24 hours.
			return err
		}
		if redacted < 0 || redacted > federationReceiptPruneBatchSize {
			return fmt.Errorf("invalid federation receipt prune batch size: %d", redacted)
		}
		if redacted < federationReceiptPruneBatchSize {
			break
		}
	}
	s.lastCompleted = now
	return nil
}
