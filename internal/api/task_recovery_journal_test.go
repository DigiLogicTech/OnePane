package api

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/task"
)

type recoveryJournalReadFixture struct {
	recoveryReadFixture
	journal      task.RecoveryJournalView
	onJournal    func()
	journalErr   error
	journalReads int
}

func (f *recoveryJournalReadFixture) ObserveRecoveryJournal(context.Context, string, string, string) (task.RecoveryJournalView, error) {
	f.journalReads++
	if f.onJournal != nil {
		f.onJournal()
	}
	return f.journal, f.journalErr
}

func TestRecoveryCaseReadsJournalRechecksAuthorityAndScope(t *testing.T) {
	for _, scenario := range []string{"allowed", "denied", "foreign", "credential_revoked", "permission_revoked", "credential_changed", "task_moved", "task_revision_changed", "project_changed", "case_changed", "raw_error"} {
		t.Run(scenario, func(t *testing.T) {
			auth := &recoveryReadAuth{}
			reads := 0
			fixture := &recoveryJournalReadFixture{recoveryReadFixture: recoveryReadFixture{inspectedTaskService: inspectedTaskService{value: task.Task{ID: "task", WorkspaceID: "tenant", Revision: 7}}, reads: &reads}, journal: task.RecoveryJournalView{CaseID: "case", TaskID: "task", AttemptID: "original", Phase: "awaiting_inventory", TaskRevision: 7, Availability: "recorded", RunID: "worker", SnapshotSHA256: "public-metadata-fingerprint", Steps: []task.RecoveryJournalStep{{ID: "step", Number: 1, Kind: "model", Status: "unknown"}}}}
			want := 200
			switch scenario {
			case "denied":
				auth.denyTask = true
				want = 403
			case "foreign":
				fixture.value.WorkspaceID = "foreign"
				want = 403
			case "credential_revoked":
				fixture.onJournal = func() { auth.credentialRevoked = true }
				want = 401
			case "permission_revoked":
				fixture.onJournal = func() { auth.revoked = true }
				want = 403
			case "credential_changed":
				fixture.onJournal = func() { auth.identityPatch = func(i Identity) Identity { i.CredentialID = "replacement"; return i } }
				want = 403
			case "task_moved":
				fixture.onJournal = func() { fixture.value.WorkspaceID = "foreign" }
				want = 403
			case "task_revision_changed":
				fixture.onJournal = func() { fixture.value.Revision++ }
				want = 403
			case "project_changed":
				fixture.onJournal = func() { value := "another"; fixture.value.ProjectID = &value }
				want = 403
			case "case_changed":
				fixture.journal.AttemptID = "foreign-attempt"
				want = 403
			case "raw_error":
				fixture.journalErr = errors.New("PRIVATE_DATABASE_CANARY")
				want = 503
			}
			s := NewServer(nil, fakeEvents{}, auth)
			s.tasks = fixture
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/v1/tasks/task/recovery-cases/case/journal", nil))
			if w.Code != want {
				t.Fatalf("status=%d want=%d body=%s", w.Code, want, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "PRIVATE_") {
				t.Fatal("raw storage error leaked")
			}
			if (scenario == "denied" || scenario == "foreign") && fixture.journalReads != 0 {
				t.Fatal("unauthorized expensive observation ran")
			}
			if want != 200 && strings.Contains(w.Body.String(), "public-metadata-fingerprint") {
				t.Fatal("revoked/changed read disclosed journal")
			}
			if want == 200 && (w.Header().Get("Cache-Control") != "no-store, private" || w.Header().Get("X-Content-Type-Options") != "nosniff") {
				t.Fatal("private journal cache/header contract missing")
			}
		})
	}
}
