package localai

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type rejectComponentRecoveryCommit struct {
	inner storage.Transactor
	err   error
}

// A crash can occur after the job claim and before first progress, or after
// the component action and before terminal job persistence. Recovery must use
// exact job ownership rather than only a list of intermediate UI states.
func TestManagedComponentRecoveryClaimedOwnerAcrossProgressStages(t *testing.T) {
	for _, observed := range []string{"queued", "running", "installed_disabled", "removed", "downloading"} {
		t.Run(observed, func(t *testing.T) {
			s, ctx := componentTestService(t)
			if _, err := s.db.ExecContext(ctx, `INSERT INTO managed_component_jobs(id,component_id,action,status,stage,revision,created_at,updated_at) VALUES('claimed-job','colibri','disable','running','queued',1,1,1)`); err != nil {
				t.Fatal(err)
			}
			if _, err := s.db.ExecContext(ctx, `UPDATE managed_component_states SET observed_state=?,active_job_id='claimed-job' WHERE component_id='colibri'`, observed); err != nil {
				t.Fatal(err)
			}
			if err := s.RecoverManagedComponents(ctx); err != nil {
				t.Fatal(err)
			}
			job, err := s.ComponentJob(ctx, "claimed-job")
			if err != nil {
				t.Fatal(err)
			}
			states, err := s.ManagedComponents(ctx)
			if err != nil {
				t.Fatal(err)
			}
			state := states["colibri"]
			if job.Status != "interrupted" || state.State != "interrupted" || state.ActiveJobID != nil {
				t.Fatalf("crash at %s left claimed owner stranded: job=%+v state=%+v", observed, job, state)
			}
			if err = s.RecoverManagedComponents(ctx); err != nil {
				t.Fatal(err)
			}
			repeated, err := s.ManagedComponents(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(states, repeated) {
				t.Fatal("repeated interruption changed preserved component state")
			}
		})
	}
}

func TestManagedComponentRecoveryPreservesUnmatchedJobOwner(t *testing.T) {
	for _, owner := range []string{"missing", "terminal", "foreign_component"} {
		t.Run(owner, func(t *testing.T) {
			s, ctx := componentTestService(t)
			if owner != "missing" {
				component, status := "colibri", "succeeded"
				if owner == "foreign_component" {
					component, status = "omniroute", "interrupted"
				}
				if _, err := s.db.ExecContext(ctx, `INSERT INTO managed_component_jobs(id,component_id,action,status,stage,revision,created_at,updated_at) VALUES('unmatched-job',?,'disable',?,'complete',1,1,1)`, component, status); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.db.ExecContext(ctx, `UPDATE managed_component_states SET observed_state='downloading',active_job_id='unmatched-job' WHERE component_id='colibri'`); err != nil {
				t.Fatal(err)
			}
			before, err := s.ManagedComponents(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.RecoverManagedComponents(ctx); err != nil {
				t.Fatal(err)
			}
			after, err := s.ManagedComponents(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("unmatched %s owner was silently cleared", owner)
			}
		})
	}
}

func (r rejectComponentRecoveryCommit) Within(ctx context.Context, f func(context.Context, storage.Tx) error) error {
	return r.inner.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if err := f(ctx, tx); err != nil {
			return err
		}
		return r.err
	})
}

// These tests exercise actual migrated SQLite and disposable legacy files only.
// No component is installed, launched, downloaded or signalled.
func TestManagedComponentRecoveryInterruptionIsAtomic(t *testing.T) {
	for _, rejected := range []string{"jobs", "states", "none"} {
		t.Run(rejected, func(t *testing.T) {
			s, ctx := componentTestService(t)
			if _, err := s.db.ExecContext(ctx, `INSERT INTO managed_component_jobs(id,component_id,action,status,stage,revision,created_at,updated_at) VALUES('recover-job','colibri','install','running','downloading',1,1,1); UPDATE managed_component_states SET observed_state='downloading',active_job_id='recover-job' WHERE component_id='colibri'`); err != nil {
				t.Fatal(err)
			}
			beforeJob, err := s.ComponentJob(ctx, "recover-job")
			if err != nil {
				t.Fatal(err)
			}
			beforeStates, err := s.ManagedComponents(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if rejected != "none" {
				table := "managed_component_" + rejected
				if _, err = s.db.ExecContext(ctx, `CREATE TRIGGER reject_recovery BEFORE UPDATE ON `+table+` BEGIN SELECT RAISE(ABORT,'recovery write rejected'); END`); err != nil {
					t.Fatal(err)
				}
			}
			err = s.RecoverManagedComponents(ctx)
			afterJob, readErr := s.ComponentJob(ctx, "recover-job")
			if readErr != nil {
				t.Fatal(readErr)
			}
			afterStates, readErr := s.ManagedComponents(ctx)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if rejected != "none" {
				if err == nil || !strings.Contains(err.Error(), "recovery write rejected") || !reflect.DeepEqual(beforeJob, afterJob) || !reflect.DeepEqual(beforeStates, afterStates) {
					t.Fatalf("rejected recovery was not atomic: err=%v job=%+v states=%+v", err, afterJob, afterStates)
				}
				return
			}
			if err != nil || afterJob.Status != "interrupted" || afterStates["colibri"].State != "interrupted" || afterStates["colibri"].ActiveJobID != nil {
				t.Fatalf("healthy recovery not recorded: %v %+v %+v", err, afterJob, afterStates["colibri"])
			}
			if err = s.RecoverManagedComponents(ctx); err != nil {
				t.Fatal(err)
			}
			repeatedJob, _ := s.ComponentJob(ctx, "recover-job")
			repeatedStates, _ := s.ManagedComponents(ctx)
			if !reflect.DeepEqual(afterJob, repeatedJob) || !reflect.DeepEqual(afterStates, repeatedStates) {
				t.Fatal("second recovery changed already-interrupted history")
			}
		})
	}
}

func TestManagedComponentLegacyMigrationPreservesRejectedSource(t *testing.T) {
	for _, rejected := range []string{"write", "missing_row", "malformed", "none"} {
		t.Run(rejected, func(t *testing.T) {
			s, ctx := componentTestService(t)
			legacy := []byte(`{"components":{"colibri":{"installed":true,"enabled":false,"state":"installed_disabled","updated_at":17}}}`)
			if rejected == "malformed" {
				legacy = []byte(`{"components":`)
			}
			path := s.legacyComponentStatePath()
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, legacy, 0600); err != nil {
				t.Fatal(err)
			}
			if rejected == "write" {
				if _, err := s.db.ExecContext(ctx, `CREATE TRIGGER reject_legacy BEFORE UPDATE ON managed_component_states WHEN NEW.component_id='colibri' BEGIN SELECT RAISE(ABORT,'legacy write rejected'); END`); err != nil {
					t.Fatal(err)
				}
			}
			if rejected == "missing_row" {
				if _, err := s.db.ExecContext(ctx, `DELETE FROM managed_component_states WHERE component_id='colibri'`); err != nil {
					t.Fatal(err)
				}
			}
			err := s.migrateLegacyComponentState(ctx)
			if rejected != "none" {
				got, readErr := os.ReadFile(path)
				if err == nil || readErr != nil || string(got) != string(legacy) {
					t.Fatalf("legacy source lost/reported success after rejected migration: err=%v read=%v", err, readErr)
				}
				if _, statErr := os.Stat(path + ".migrated"); !errors.Is(statErr, os.ErrNotExist) {
					t.Fatalf("rejected legacy source archived: %v", statErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("successful import source not archived: %v", err)
			}
			archived, err := os.ReadFile(path + ".migrated")
			if err != nil || string(archived) != string(legacy) {
				t.Fatalf("archived bytes changed: %v", err)
			}
			all, err := s.ManagedComponents(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !all["colibri"].Installed || all["colibri"].State != "installed_disabled" || all["colibri"].UpdatedAt != 17 {
				t.Fatalf("legacy state not imported: %+v", all["colibri"])
			}
		})
	}
}

func TestManagedComponentRecoveryCancelledContextPropagates(t *testing.T) {
	s, ctx := componentTestService(t)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := s.RecoverManagedComponents(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled recovery reported success: %v", err)
	}
}

func TestManagedComponentRecoveryQueuedReadClosesBeforeDispatch(t *testing.T) {
	s, ctx := componentTestService(t)
	// A single connection makes an unclosed queue reader block either the
	// following recovery observation or the queued worker's durable writes.
	s.db.SetMaxOpenConns(1)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO managed_component_jobs(id,component_id,action,status,stage,revision,created_at,updated_at) VALUES('queued-safe-disable','colibri','disable','queued','queued',1,1,1); UPDATE managed_component_states SET active_job_id='queued-safe-disable',observed_state='queued' WHERE component_id='colibri'`); err != nil {
		t.Fatal(err)
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.RecoverManagedComponents(bounded); err != nil {
		t.Fatal(err)
	}
	timer := time.NewTicker(5 * time.Millisecond)
	defer timer.Stop()
	for {
		job, err := s.ComponentJob(bounded, "queued-safe-disable")
		if err != nil {
			t.Fatal(err)
		}
		if job.Status == "succeeded" {
			return
		}
		select {
		case <-bounded.Done():
			t.Fatalf("queued safe metadata-only job did not finish: %s", job.Status)
		case <-timer.C:
		}
	}
}

func TestManagedComponentRecoveryOptionalOwnershipAndPersistenceErrors(t *testing.T) {
	for _, rejected := range []bool{false, true} {
		name := "deferred"
		if rejected {
			name = "rejected_degraded_write"
		}
		t.Run(name, func(t *testing.T) {
			s, ctx := componentTestService(t)
			// Synthetic unowned metadata: the new ownership boundary must not use this
			// number as a signal target or launch a replacement component.
			if _, err := s.db.ExecContext(ctx, `UPDATE managed_component_states SET installed_version='3.8.51',desired_state='enabled',observed_state='running',metadata_json='{"pid":2147483647}' WHERE component_id='omniroute'`); err != nil {
				t.Fatal(err)
			}
			if rejected {
				if _, err := s.db.ExecContext(ctx, `CREATE TRIGGER reject_degraded BEFORE UPDATE ON managed_component_states WHEN NEW.component_id='omniroute' AND NEW.observed_state='degraded' BEGIN SELECT RAISE(ABORT,'degraded write rejected'); END`); err != nil {
					t.Fatal(err)
				}
			}
			err := s.RecoverManagedComponents(ctx)
			var observed, metadata string
			if readErr := s.db.QueryRowContext(ctx, `SELECT observed_state,metadata_json FROM managed_component_states WHERE component_id='omniroute'`).Scan(&observed, &metadata); readErr != nil {
				t.Fatal(readErr)
			}
			if metadata != `{"pid":2147483647}` {
				t.Fatalf("recovery erased unowned metadata: %s", metadata)
			}
			if rejected {
				if err == nil || !strings.Contains(err.Error(), "degraded write rejected") || observed != "running" {
					t.Fatalf("failed optional observation hidden: %v %s", err, observed)
				}
			} else if err != nil || observed != "degraded" {
				t.Fatalf("durable optional deferral stopped startup: %v %s", err, observed)
			}
		})
	}
}

func TestManagedComponentRecoveryTransactionFailureNeverDispatches(t *testing.T) {
	s, ctx := componentTestService(t)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO managed_component_jobs(id,component_id,action,status,stage,revision,created_at,updated_at) VALUES('running','colibri','install','running','downloading',1,1,1),('queued','colibri','disable','queued','queued',1,1,1); UPDATE managed_component_states SET observed_state='downloading',active_job_id='running' WHERE component_id='colibri'`); err != nil {
		t.Fatal(err)
	}
	before, _ := s.ManagedComponents(ctx)
	rejected := errors.New("recovery transaction rejected before commit")
	s.tx = rejectComponentRecoveryCommit{s.tx, rejected}
	if err := s.RecoverManagedComponents(ctx); !errors.Is(err, rejected) {
		t.Fatalf("transaction failure was hidden: %v", err)
	}
	after, err := s.ManagedComponents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	running, err := s.ComponentJob(ctx, "running")
	if err != nil {
		t.Fatal(err)
	}
	queued, err := s.ComponentJob(ctx, "queued")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) || running.Status != "running" || running.Revision != 1 || queued.Status != "queued" || queued.Revision != 1 {
		t.Fatalf("failed transaction applied/dispatched work: running=%+v queued=%+v", running, queued)
	}
}

func TestManagedComponentRecoveryMissingOptionalRuntimeDoesNotBlockUI(t *testing.T) {
	s, ctx := componentTestService(t)
	if _, err := s.db.ExecContext(ctx, `UPDATE managed_component_states SET installed_version='3.8.51',desired_state='enabled',observed_state='installed_disabled',metadata_json='{}' WHERE component_id='omniroute'`); err != nil {
		t.Fatal(err)
	}
	// The disposable root has no executable; no component process can launch.
	if err := s.RecoverManagedComponents(ctx); err != nil {
		t.Fatalf("missing optional runtime prevented operator recovery UI: %v", err)
	}
	var observed, desired string
	if err := s.db.QueryRowContext(ctx, `SELECT observed_state,desired_state FROM managed_component_states WHERE component_id='omniroute'`).Scan(&observed, &desired); err != nil {
		t.Fatal(err)
	}
	if observed != "degraded" || desired != "enabled" {
		t.Fatalf("missing runtime was not truthfully recorded: %s %s", observed, desired)
	}
}
