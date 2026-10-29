package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/config"
	"github.com/DigiLogicTech/OnePane/internal/localai"
	"github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
	modernsqlite "modernc.org/sqlite"
)

func TestOptionalRecoveryFailureClassification(t *testing.T) {
	if optionalRecoveryFailure(nil) != nil {
		t.Fatal("nil error aborted")
	}
	if optionalRecoveryFailure(errors.New("OmniRoute binary missing")) != nil {
		t.Fatal("broken optional component aborted startup")
	}
	if optionalRecoveryFailure(context.Canceled) == nil {
		t.Fatal("cancellation was treated as degradation")
	}
	store := &modernsqlite.Error{}
	if optionalRecoveryFailure(fmt.Errorf("recover managed runtimes: %w", store)) == nil {
		t.Fatal("store failure was treated as an optional component")
	}
	if !sqlite.IsStoreFailure(store) {
		t.Fatal("driver error was not a store failure")
	}
	if sqlite.IsStoreFailure(errors.New("Colibri did not start")) {
		t.Fatal("component text was classified as a store failure")
	}
}

func TestBrokenOptionalComponentDoesNotAbortBootstrap(t *testing.T) {
	previousRuntime, previousComponent := recoverManagedRuntimesHook, recoverManagedComponentsHook
	t.Cleanup(func() {
		recoverManagedRuntimesHook = previousRuntime
		recoverManagedComponentsHook = previousComponent
	})
	recoverManagedRuntimesHook = func(context.Context, *localai.Service) error {
		return errors.New("managed runtime process identity could not be re-established")
	}
	recoverManagedComponentsHook = func(context.Context, *localai.Service) error {
		return errors.New("Colibri runtime unavailable")
	}
	cfg := config.Default()
	cfg.Storage.DataDir = t.TempDir()
	cfg.Storage.ProjectRoot = filepath.Join(cfg.Storage.DataDir, "projects")
	cfg.Federation.Enabled = false
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	rt, err := Open(ctx, cfg)
	if err != nil {
		t.Fatalf("optional component recovery aborted startup: %v", err)
	}
	t.Cleanup(func() { _ = rt.DB.Close() })
	status, reason := rt.LocalAI.ObservedRuntimeHealth(ctx)
	if status != "degraded" || !strings.Contains(reason, "Colibri") || !strings.Contains(reason, "managed runtime") {
		t.Fatalf("degraded optional components were not visible: %s %s", status, reason)
	}
}
