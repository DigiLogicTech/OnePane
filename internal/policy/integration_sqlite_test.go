//go:build integration

package policy

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/authority"
	"github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
	"github.com/DigiLogicTech/OnePane/internal/system"
)

type testClock struct{ ms int64 }

func (c *testClock) Now() time.Time   { return time.UnixMilli(c.ms).UTC() }
func (c *testClock) UnixMilli() int64 { return c.ms }

func TestSQLiteAuthorityAndPolicyDenials(t *testing.T) {
	ctx := context.Background()
	db := openM5TestDB(t)
	clk := &testClock{ms: 1_700_000_000_000}
	seedM5IdentityFixture(t, db.SQL(), clk.ms)

	systemService := system.NewService(db.SQL(), db, clk)
	if _, err := systemService.EnsureBootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	st, err := systemService.SetMode(ctx, 1, system.ModeCommissioning, "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := systemService.SetMode(ctx, st.Revision, system.ModeNormal, "test", nil); err != nil {
		t.Fatal(err)
	}

	authorityService := authority.NewService(db.SQL(), db, clk)
	engine := NewEngine(authorityService, systemService, clk)
	usageLimit := int64(3)
	lease, err := authorityService.Issue(ctx, authority.IssueCommand{
		WorkspaceID: "ws1", PrincipalID: "agent1", CapabilityID: "repo.file.write", IssuedBy: "admin",
		ExpiresAt: clk.ms + 60_000, UsageLimit: &usageLimit,
		Scope: authority.Scope{
			ResourceRefs: []string{"repo://alpha/README.md"},
			Actions:      []authority.ActionMode{authority.ActionRead, authority.ActionMutate},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	base := AuthorityInput{
		LeaseID: lease.ID, WorkspaceID: "ws1", PrincipalID: "agent1", TaskID: "task1",
		CapabilityID: "repo.file.write", Action: authority.ActionMutate,
		ResourceRef: "repo://alpha/README.md", Risk: RiskMedium,
	}
	if decision := engine.EvaluateAuthority(ctx, base); !decision.Allowed {
		t.Fatalf("valid authority denied: %+v", decision)
	}

	outOfScope := base
	outOfScope.ResourceRef = "repo://alpha/other.txt"
	decision := engine.EvaluateAuthority(ctx, outOfScope)
	if decision.Allowed || !hasReason(decision.Reasons, ReasonResourceOutOfScope) {
		t.Fatalf("out-of-scope resource not denied: %+v", decision)
	}

	crossWorkspace := base
	crossWorkspace.WorkspaceID = "ws2"
	crossWorkspace.TaskID = "task2"
	decision = engine.EvaluateAuthority(ctx, crossWorkspace)
	if decision.Allowed || !hasReason(decision.Reasons, ReasonLeaseWorkspaceMismatch) {
		t.Fatalf("cross-workspace lease use not denied: %+v", decision)
	}

	revoked, err := authorityService.Issue(ctx, authority.IssueCommand{
		WorkspaceID: "ws1", PrincipalID: "agent1", CapabilityID: "repo.file.write", IssuedBy: "admin",
		ExpiresAt: clk.ms + 60_000,
		Scope:     authority.Scope{ResourceRefs: []string{"repo://alpha/README.md"}, Actions: []authority.ActionMode{authority.ActionMutate}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authorityService.Revoke(ctx, authority.TransitionCommand{LeaseID: revoked.ID, ExpectedRevision: revoked.Revision, Reason: "test"}); err != nil {
		t.Fatal(err)
	}
	revokedInput := base
	revokedInput.LeaseID = revoked.ID
	decision = engine.EvaluateAuthority(ctx, revokedInput)
	if decision.Allowed || !hasReason(decision.Reasons, ReasonLeaseRevoked) {
		t.Fatalf("revoked lease not denied: %+v", decision)
	}

	expiring, err := authorityService.Issue(ctx, authority.IssueCommand{
		WorkspaceID: "ws1", PrincipalID: "agent1", CapabilityID: "repo.file.write", IssuedBy: "admin",
		ExpiresAt: clk.ms + 10,
		Scope:     authority.Scope{ResourceRefs: []string{"repo://alpha/README.md"}, Actions: []authority.ActionMode{authority.ActionMutate}},
	})
	if err != nil {
		t.Fatal(err)
	}
	clk.ms += 11
	expiredInput := base
	expiredInput.LeaseID = expiring.ID
	decision = engine.EvaluateAuthority(ctx, expiredInput)
	if decision.Allowed || !hasReason(decision.Reasons, ReasonLeaseExpired) {
		t.Fatalf("expired lease not denied: %+v", decision)
	}
}

func TestSQLiteUsageExhaustionAndTransactionalEvents(t *testing.T) {
	ctx := context.Background()
	db := openM5TestDB(t)
	clk := &testClock{ms: 1_700_000_000_000}
	seedM5IdentityFixture(t, db.SQL(), clk.ms)
	svc := authority.NewService(db.SQL(), db, clk)

	limit := int64(1)
	lease, err := svc.Issue(ctx, authority.IssueCommand{
		WorkspaceID: "ws1", PrincipalID: "agent1", CapabilityID: "repo.file.read", IssuedBy: "admin",
		ExpiresAt: clk.ms + 60_000, UsageLimit: &limit,
		Scope: authority.Scope{ResourceRefs: []string{"repo://alpha/README.md"}, Actions: []authority.ActionMode{authority.ActionRead}},
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err = svc.Consume(ctx, authority.ConsumeCommand{LeaseID: lease.ID, ExpectedRevision: lease.Revision, Uses: 1})
	if err != nil {
		t.Fatal(err)
	}
	if lease.Status != authority.StatusExhausted || lease.UsageCount != 1 {
		t.Fatalf("lease after consume=%+v", lease)
	}
	if _, err := svc.Consume(ctx, authority.ConsumeCommand{LeaseID: lease.ID, ExpectedRevision: lease.Revision, Uses: 1}); !errors.Is(err, authority.ErrExhausted) {
		t.Fatalf("second use err=%v want exhausted", err)
	}

	var issued, used, exhausted int
	for typ, dst := range map[string]*int{
		"capability_lease.issued": &issued, "capability_lease.used": &used, "capability_lease.exhausted": &exhausted,
	} {
		if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM events WHERE aggregate_id = ? AND event_type = ?`, lease.ID, typ).Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	if issued != 1 || used != 1 || exhausted != 1 {
		t.Fatalf("events issued=%d used=%d exhausted=%d", issued, used, exhausted)
	}

	// A failed causal Event write must roll the lease insert back.
	missing := "principal_missing"
	_, err = svc.Issue(ctx, authority.IssueCommand{
		WorkspaceID: "ws1", PrincipalID: "agent1", CapabilityID: "repo.file.read", IssuedBy: "admin",
		ActorPrincipalID: &missing, ExpiresAt: clk.ms + 60_000,
		Scope: authority.Scope{ResourceRefs: []string{"repo://alpha/README.md"}, Actions: []authority.ActionMode{authority.ActionRead}},
	})
	if err == nil {
		t.Fatal("expected event foreign-key failure")
	}
	var count int
	if err := db.SQL().QueryRow(`SELECT COUNT(*) FROM capability_leases WHERE capability_id = 'repo.file.read'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("failed issue was not rolled back, count=%d", count)
	}

	// The same atomicity guarantee applies to revocation.
	active, err := svc.Issue(ctx, authority.IssueCommand{
		WorkspaceID: "ws1", PrincipalID: "agent1", CapabilityID: "repo.file.mutate", IssuedBy: "admin",
		ExpiresAt: clk.ms + 60_000,
		Scope:     authority.Scope{ResourceRefs: []string{"repo://alpha/README.md"}, Actions: []authority.ActionMode{authority.ActionMutate}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Revoke(ctx, authority.TransitionCommand{LeaseID: active.ID, ExpectedRevision: active.Revision, ActorPrincipalID: &missing})
	if err == nil {
		t.Fatal("expected revoke event foreign-key failure")
	}
	active, err = svc.Get(ctx, active.ID)
	if err != nil {
		t.Fatal(err)
	}
	if active.Status != authority.StatusActive || active.Revision != 1 {
		t.Fatalf("failed revoke was not rolled back: %+v", active)
	}
}

func openM5TestDB(t *testing.T) *sqlite.DB {
	t.Helper()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "m5.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return db
}

func seedM5IdentityFixture(t *testing.T, db *sql.DB, now int64) {
	t.Helper()
	stmts := []string{
		`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('ws1','One','active',1,?,?)`,
		`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('ws2','Two','active',1,?,?)`,
		`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('admin','human','Admin','active',1,?,?)`,
		`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('agent1','agent','Agent','active',1,?,?)`,
		`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('ws1','admin','active',?,?)`,
		`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('ws2','admin','active',?,?)`,
		`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('ws1','agent1','active',?,?)`,
		`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('ws2','agent1','active',?,?)`,
		`INSERT INTO tasks(id,workspace_id,objective,state,scheduling_class,priority,completion_json,revision,created_at,updated_at) VALUES('task1','ws1','one','created','normal_task',0,'{}',1,?,?)`,
		`INSERT INTO tasks(id,workspace_id,objective,state,scheduling_class,priority,completion_json,revision,created_at,updated_at) VALUES('task2','ws2','two','created','normal_task',0,'{}',1,?,?)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt, now, now); err != nil {
			t.Fatal(err)
		}
	}
}
