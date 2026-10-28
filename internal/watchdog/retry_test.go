package watchdog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type codedError int

func (e codedError) Error() string { return "SQLite result" }
func (e codedError) Code() int     { return int(e) }

type heartbeatScript struct {
	errors []error
	calls  int
	cancel context.CancelFunc
}

func (s *heartbeatScript) Within(context.Context, func(context.Context, storage.Tx) error) error {
	s.calls++
	if s.calls <= len(s.errors) {
		return s.errors[s.calls-1]
	}
	s.cancel()
	return nil
}

func TestRunRetriesContentionIncludingExtendedCodes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	tx := &heartbeatScript{errors: []error{fmt.Errorf("wrapped: %w", codedError(517)), codedError(262), nil}, cancel: cancel}
	s := NewService(&sql.DB{}, tx, clock.Real{}, "", time.Second)
	if err := s.Run(ctx, time.Millisecond, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v, want cancellation after recovery", err)
	}
	if tx.calls != 4 {
		t.Fatalf("heartbeat calls = %d, want contention, contention, success, cancellation", tx.calls)
	}
}

func TestRunDoesNotRetryPermanentErrors(t *testing.T) {
	for _, failure := range []error{errors.New("storage failed"), codedError(11), errors.New("text SQLITE_BUSY is not a driver code")} {
		t.Run(failure.Error(), func(t *testing.T) {
			tx := &heartbeatScript{errors: []error{failure}}
			s := NewService(&sql.DB{}, tx, clock.Real{}, "", time.Second)
			if err := s.Run(context.Background(), time.Millisecond, nil); !errors.Is(err, failure) {
				t.Fatalf("Run = %v, want %v", err, failure)
			}
			if tx.calls != 1 {
				t.Fatalf("unexpected retry: %d", tx.calls)
			}
		})
	}
}
