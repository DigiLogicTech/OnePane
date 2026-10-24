package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/task"
)

type recoveryReadFixture struct {
	inspectedTaskService
	onCases func()
	readErr error
	reads   *int
}

type recoveryReadAuth struct {
	liveRoomAuth
	credentialRevoked bool
	identityPatch     func(Identity) Identity
}

func (a *recoveryReadAuth) Authenticate(r *http.Request) (Identity, error) {
	if a.credentialRevoked {
		return Identity{}, ErrUnauthenticated
	}
	i, err := a.liveRoomAuth.Authenticate(r)
	if a.identityPatch != nil {
		i = a.identityPatch(i)
	}
	return i, err
}

func (f recoveryReadFixture) ListRecoveryCases(context.Context, string, string, string) (task.RecoveryCasePage, error) {
	*f.reads++
	if f.onCases != nil {
		f.onCases()
	}
	return task.RecoveryCasePage{Cases: []task.RecoveryCaseMetadata{{ID: "case", TaskID: "task", AttemptID: "original", Phase: "awaiting_inventory"}}}, f.readErr
}
func (f recoveryReadFixture) GetRecoveryCase(ctx context.Context, ws, id, caseID string) (task.RecoveryCaseMetadata, error) {
	page, e := f.ListRecoveryCases(ctx, ws, id, "")
	return page.Cases[0], e
}

func TestRecoveryCaseReadsRequireLiveScopedTaskPermission(t *testing.T) {
	for _, detail := range []bool{false, true} {
		for _, scenario := range []string{"allowed", "foreign", "denied", "revoked_during_read", "credential_revoked", "credential_revoked_task_read", "principal_changed", "credential_changed", "principal_type_changed", "unauthenticated", "task_missing", "missing", "read_error", "bad_cursor"} {
			t.Run(scenario+fmtBoolAPI(detail), func(t *testing.T) {
				auth := &recoveryReadAuth{liveRoomAuth: liveRoomAuth{denyTask: scenario == "denied"}}
				reads := 0
				fixture := recoveryReadFixture{inspectedTaskService: inspectedTaskService{value: task.Task{ID: "task", WorkspaceID: "tenant"}}, reads: &reads}
				want := 200
				switch scenario {
				case "unauthenticated":
					auth.credentialRevoked = true
					want = 401
				case "task_missing":
					fixture.err = sql.ErrNoRows
					want = 404
				case "foreign":
					fixture.value.WorkspaceID = "other-tenant"
					want = 403
				case "denied":
					want = 403
				case "revoked_during_read":
					fixture.onCases = func() { auth.revoked = true }
					want = 403
				case "credential_revoked":
					fixture.onCases = func() { auth.credentialRevoked = true }
					want = 401
				case "credential_revoked_task_read":
					fixture.onRead = func() { auth.credentialRevoked = true }
					want = 401
				case "principal_changed", "credential_changed", "principal_type_changed":
					fixture.onCases = func() {
						auth.identityPatch = func(i Identity) Identity {
							switch scenario {
							case "principal_changed":
								i.PrincipalID = "another-human"
							case "credential_changed":
								i.CredentialID = "replacement"
							case "principal_type_changed":
								i.PrincipalType = "agent"
							}
							return i
						}
					}
					want = 403
				case "missing":
					fixture.readErr = sql.ErrNoRows
					want = 404
				case "read_error":
					fixture.readErr = errors.New("PRIVATE_CANARY")
					want = 500
				}
				s := NewServer(nil, fakeEvents{}, auth)
				s.tasks = fixture
				path := "/v1/tasks/task/recovery-cases"
				if detail {
					path += "/case"
				}
				if scenario == "bad_cursor" {
					if detail {
						return
					}
					path += "?after=" + strings.Repeat("x", 161)
					want = 400
				}
				w := httptest.NewRecorder()
				s.Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
				if w.Code != want {
					t.Fatalf("status %d want %d body %s", w.Code, want, w.Body.String())
				}
				if strings.Contains(w.Body.String(), "CANARY") {
					t.Fatal("raw read failure leaked")
				}
				if (scenario == "foreign" || scenario == "denied" || scenario == "bad_cursor" || scenario == "unauthenticated" || scenario == "task_missing" || scenario == "credential_revoked_task_read") && reads != 0 {
					t.Fatal("unauthorized/unbounded read executed")
				}
				if want == 200 && (w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), "awaiting_inventory")) {
					t.Fatal("intake metadata/cache contract missing")
				}
			})
		}
	}
}
func fmtBoolAPI(value bool) string {
	if value {
		return "_detail"
	}
	return "_list"
}
