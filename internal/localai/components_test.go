package localai

import (
	"context"
	"testing"
)

func TestManagedComponentLifecycleIsHarnessOwned(t *testing.T) {
	s := &Service{dataDir: t.TempDir()}
	ctx := context.Background()
	all, err := s.ManagedComponents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if all["colibri"].Installed || all["colibri"].Enabled {
		t.Fatal("colibri must be optional by default")
	}
	c, err := s.ManageComponent(ctx, "colibri", "install")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Installed || c.Enabled || c.State != "installed_disabled" {
		t.Fatalf("unexpected install state: %+v", c)
	}
	c, err = s.ManageComponent(ctx, "colibri", "enable")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Installed || !c.Enabled || c.State != "enabled_pending" {
		t.Fatalf("unexpected enable state: %+v", c)
	}
	c, err = s.ManageComponent(ctx, "colibri", "disable")
	if err != nil {
		t.Fatal(err)
	}
	if c.Enabled || c.State != "installed_disabled" {
		t.Fatalf("unexpected disable state: %+v", c)
	}
	c, err = s.ManageComponent(ctx, "colibri", "remove")
	if err != nil {
		t.Fatal(err)
	}
	if c.Installed || c.Enabled || c.State != "not_installed" {
		t.Fatalf("unexpected remove state: %+v", c)
	}
}

func TestEnableRequiresInstall(t *testing.T) {
	s := &Service{dataDir: t.TempDir()}
	if _, err := s.ManageComponent(context.Background(), "omniroute", "enable"); err == nil {
		t.Fatal("expected enable-before-install to fail")
	}
}
