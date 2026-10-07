package localai

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func componentTestService(t *testing.T) (*Service, context.Context) {
	t.Helper()
	ctx := context.Background()
	db, err := sqliteStore.Open(filepath.Join(t.TempDir(), "harness.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	svc := NewService(db.SQL(), db, clock.Real{}, nil, t.TempDir(), nil)
	return svc, ctx
}

func TestManagedComponentLifecycleIsDurableAndHarnessOwned(t *testing.T) {
	s, ctx := componentTestService(t)

	all, err := s.ManagedComponents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if all["colibri"].Installed || all["colibri"].Enabled || all["colibri"].State != "not_installed" {
		t.Fatalf("colibri must be optional by default: %+v", all["colibri"])
	}
	omni, ok := all["omniroute"]
	if !ok {
		t.Fatal("OmniRoute managed-local lifecycle must be present")
	}
	if omni.Installed || omni.Enabled || omni.State != "not_installed" {
		t.Fatalf("OmniRoute must remain optional by default: %+v", omni)
	}
	llama, ok := all["llamacpp"]
	if !ok {
		t.Fatal("llama.cpp managed runtime lifecycle must be present")
	}
	if llama.Installed || llama.Enabled || llama.State != "not_installed" {
		t.Fatalf("llama.cpp must remain optional by default: %+v", llama)
	}

	// Installation itself is covered by archive/fetcher tests and release-manifest
	// contract checks. Seed the durable installed state here so this unit test can
	// exercise enable/disable/remove without making a network request.
	if err := os.MkdirAll(s.componentRuntimeRoot(), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE managed_component_states
		SET installed_version='1.12.1',desired_state='disabled',observed_state='installed_disabled',
		    metadata_json='{}',revision=revision+1,updated_at=1
		WHERE component_id='colibri'`); err != nil {
		t.Fatal(err)
	}

	c, err := s.ManageComponent(ctx, "colibri", "enable")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Installed || !c.Enabled || c.State != "running" {
		t.Fatalf("unexpected enable state: %+v", c)
	}

	c, err = s.ManageComponent(ctx, "colibri", "disable")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Installed || c.Enabled || c.State != "installed_disabled" {
		t.Fatalf("unexpected disable state: %+v", c)
	}

	c, err = s.ManageComponent(ctx, "colibri", "remove")
	if err != nil {
		t.Fatal(err)
	}
	if c.Installed || c.Enabled || c.State != "removed" {
		t.Fatalf("unexpected remove state: %+v", c)
	}
}

func TestEnableRequiresInstallForManagedComponents(t *testing.T) {
	s, ctx := componentTestService(t)
	if _, err := s.ManageComponent(ctx, "colibri", "enable"); err == nil {
		t.Fatal("expected enable-before-install to fail")
	}
	if _, err := s.ManageComponent(ctx, "omniroute", "enable"); err == nil {
		t.Fatal("expected OmniRoute enable-before-install to fail")
	}
}
