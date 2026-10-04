package watchdog

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type fakeClock struct{ now time.Time }

func (f *fakeClock) Now() time.Time   { return f.now }
func (f *fakeClock) UnixMilli() int64 { return f.now.UnixMilli() }

// Compile-time assertions keep the service on the same deterministic clock and
// transactor abstractions as the rest of the control plane.
var _ clock.Clock = (*fakeClock)(nil)
var _ storage.Transactor = (*noopTx)(nil)

type noopTx struct{}

func (*noopTx) Within(context.Context, func(context.Context, storage.Tx) error) error { return nil }

func TestConstructorDefaults(t *testing.T) {
	c := &fakeClock{now: time.Now().UTC()}
	s := NewService((*sql.DB)(nil), nil, c, "", 0)
	if s.name != "control-plane" {
		t.Fatalf("name=%s", s.name)
	}
	if s.maxAge != 15*time.Second {
		t.Fatalf("maxAge=%s", s.maxAge)
	}
}
