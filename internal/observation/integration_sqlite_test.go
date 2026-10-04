//go:build integration

package observation

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/policy"
	"github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

type observationTestClock struct{ ms int64 }

func (c *observationTestClock) Now() time.Time   { return time.UnixMilli(c.ms).UTC() }
func (c *observationTestClock) UnixMilli() int64 { return c.ms }

func TestSQLiteObservationRecordLatestIntegrityAndImmutability(t *testing.T) {
	ctx := context.Background()
	db := openObservationTestDB(t)
	clk := &observationTestClock{ms: 1_700_000_000_000}
	seedObservationFixture(t, db.SQL(), clk.ms)
	svc := NewService(db.SQL(), db, clk)
	source := "agent1"

	first, err := svc.Record(ctx, RecordCommand{
		WorkspaceID: "ws1", SubjectRef: "dns://host/a", ObservationType: "dns.lookup",
		ProbeToolID: "dns.lookup", ProbeToolVersion: "1", SourcePrincipalID: &source, ActorPrincipalID: &source,
		Value: []byte(`{"ip":"10.0.0.1"}`), ObservedAt: clk.ms - 10,
		Label: policy.DataLabel{WorkspaceID: "ws1", Confidentiality: policy.ConfidentialityInternal, Residency: policy.ResidencyAny, Trust: policy.TrustAuthoritativeData},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.VerifyIntegrity(ctx, first.ID); err != nil {
		t.Fatal(err)
	}

	second, err := svc.Record(ctx, RecordCommand{
		WorkspaceID: "ws1", SubjectRef: "dns://host/a", ObservationType: "dns.lookup",
		ProbeToolID: "dns.lookup", ProbeToolVersion: "1", SourcePrincipalID: &source, ActorPrincipalID: &source,
		Value: []byte(`{"ip":"10.0.0.2"}`), ObservedAt: clk.ms,
		Label: policy.DataLabel{WorkspaceID: "ws1", Confidentiality: policy.ConfidentialityInternal, Residency: policy.ResidencyAny, Trust: policy.TrustAuthoritativeData},
	})
	if err != nil {
		t.Fatal(err)
	}
	latest, err := svc.Latest(ctx, "ws1", "dns://host/a", "dns.lookup")
	if err != nil {
		t.Fatal(err)
	}
	if latest.ID != second.ID {
		t.Fatalf("latest=%s want %s", latest.ID, second.ID)
	}
	if _, err := db.SQL().Exec(`UPDATE observations SET value_json='{}' WHERE id=?`, first.ID); err == nil {
		t.Fatal("immutable observation update unexpectedly succeeded")
	}
	var eventCount int
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM events WHERE aggregate_id=? AND event_type='observation.recorded'`, second.ID).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 {
		t.Fatalf("observation events=%d", eventCount)
	}
}

func TestSQLiteObservationRejectsCrossWorkspaceSourceAndRollsBackOnEventFailure(t *testing.T) {
	ctx := context.Background()
	db := openObservationTestDB(t)
	clk := &observationTestClock{ms: 1_700_000_000_000}
	seedObservationFixture(t, db.SQL(), clk.ms)
	svc := NewService(db.SQL(), db, clk)
	outsider := "outsider"
	_, err := svc.Record(ctx, RecordCommand{
		WorkspaceID: "ws1", SubjectRef: "x", ObservationType: "probe", ProbeToolID: "probe", ProbeToolVersion: "1",
		SourcePrincipalID: &outsider, ActorPrincipalID: &outsider, Value: []byte(`{"ok":true}`),
		Label: policy.DataLabel{WorkspaceID: "ws1", Confidentiality: policy.ConfidentialityInternal, Residency: policy.ResidencyAny, Trust: policy.TrustAuthoritativeData},
	})
	if !errors.Is(err, ErrSourceIneligible) {
		t.Fatalf("cross-workspace source err=%v", err)
	}

	source := "agent1"
	spoofedActor := "outsider"
	_, err = svc.Record(ctx, RecordCommand{
		WorkspaceID: "ws1", SubjectRef: "x", ObservationType: "probe", ProbeToolID: "probe", ProbeToolVersion: "1",
		SourcePrincipalID: &source, ActorPrincipalID: &spoofedActor, Value: []byte(`{"ok":true}`),
		Label: policy.DataLabel{WorkspaceID: "ws1", Confidentiality: policy.ConfidentialityInternal, Residency: policy.ResidencyAny, Trust: policy.TrustAuthoritativeData},
	})
	if !errors.Is(err, ErrSourceActorMismatch) {
		t.Fatalf("spoofed source actor err=%v", err)
	}

	missingActor := "missing"
	_, err = svc.Record(ctx, RecordCommand{
		WorkspaceID: "ws1", SubjectRef: "x", ObservationType: "probe", ProbeToolID: "probe", ProbeToolVersion: "1",
		SourcePrincipalID: &source, ActorPrincipalID: &missingActor, Value: []byte(`{"ok":true}`),
		Label: policy.DataLabel{WorkspaceID: "ws1", Confidentiality: policy.ConfidentialityInternal, Residency: policy.ResidencyAny, Trust: policy.TrustAuthoritativeData},
	})
	if err == nil {
		t.Fatal("expected event foreign-key failure")
	}
	var count int
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM observations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("observation was not rolled back, count=%d", count)
	}
}

func openObservationTestDB(t *testing.T) *sqlite.DB {
	t.Helper()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "observation.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return db
}

func seedObservationFixture(t *testing.T, db *sql.DB, now int64) {
	t.Helper()
	stmts := []string{
		`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('ws1','One','active',1,?,?)`,
		`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('ws2','Two','active',1,?,?)`,
		`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('agent1','agent','Agent','active',1,?,?)`,
		`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('outsider','human','Outsider','active',1,?,?)`,
		`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('ws1','agent1','active',?,?)`,
		`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('ws2','outsider','active',?,?)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt, now, now); err != nil {
			t.Fatal(err)
		}
	}
}
