//go:build integration

package webauth

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	dbsqlite "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestBootstrapAdminAndSessionLifecycleSQLite(t *testing.T) {
	ctx := context.Background()
	db, err := dbsqlite.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	svc := NewService(db.SQL(), db, clock.Real{})
	st, err := svc.SetupStatus(ctx)
	if err != nil || !st.Required {
		t.Fatalf("status=%#v err=%v", st, err)
	}
	out, err := svc.BootstrapAdmin(ctx, BootstrapAdminCommand{Username: "admin", DisplayName: "Admin", Password: "correct horse battery staple", WorkspaceName: "Default"})
	if err != nil {
		t.Fatal(err)
	}
	st, err = svc.SetupStatus(ctx)
	if err != nil || st.Required {
		t.Fatalf("setup should be closed: %#v %v", st, err)
	}
	if _, err := svc.BootstrapAdmin(ctx, BootstrapAdminCommand{Username: "other", Password: "another strong password"}); err != ErrSetupComplete {
		t.Fatalf("second bootstrap err=%v", err)
	}
	if _, err := svc.AuthenticateSession(ctx, out.SessionToken, "wrong", true); err != ErrCSRF {
		t.Fatalf("csrf err=%v", err)
	}
	if _, err := svc.AuthenticateSession(ctx, out.SessionToken, out.CSRFToken, true); err != nil {
		t.Fatal(err)
	}
	if err := svc.Logout(ctx, out.SessionToken); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthenticateSession(ctx, out.SessionToken, out.CSRFToken, false); err != ErrInvalidSession {
		t.Fatalf("revoked session err=%v", err)
	}
}
