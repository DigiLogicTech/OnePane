package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/task"
)

type inspectedTaskService struct {
	taskService
	value  task.Task
	err    error
	onRead func()
}

type inspectedTaskAuth struct {
	liveRoomAuth
	identity Identity
	err      error
}

func (a *inspectedTaskAuth) Authenticate(*http.Request) (Identity, error) {
	return a.identity, a.err
}

func TestTaskInspectionRejectsCredentialChangesDuringRead(t *testing.T) {
	for _, scenario := range []string{"revoked", "principal_changed", "credential_changed", "principal_type_changed", "auth_failed"} {
		t.Run(scenario, func(t *testing.T) {
			auth := &inspectedTaskAuth{identity: Identity{PrincipalID: "human", CredentialID: "original", PrincipalType: "human"}}
			service := inspectedTaskService{value: task.Task{ID: "task", WorkspaceID: "tenant", Objective: "PRIVATE_OBJECTIVE_CANARY"}}
			want := 403
			service.onRead = func() {
				switch scenario {
				case "revoked":
					auth.err = ErrUnauthenticated
					want = 401
				case "principal_changed":
					auth.identity.PrincipalID = "another-human"
				case "credential_changed":
					auth.identity.CredentialID = "replacement"
				case "principal_type_changed":
					auth.identity.PrincipalType = "agent"
				case "auth_failed":
					auth.err = errors.New("PRIVATE_AUTH_CANARY")
					want = 500
				}
			}
			s := NewServer(nil, fakeEvents{}, auth)
			s.tasks = service
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/v1/tasks/task", nil))
			if w.Code != want {
				t.Fatalf("status %d want %d body %s", w.Code, want, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "CANARY") {
				t.Fatal("revoked inspection disclosed private Task/auth data")
			}
		})
	}
}

func (f inspectedTaskService) Get(context.Context, string) (task.Task, error) {
	if f.onRead != nil {
		f.onRead()
	}
	return f.value, f.err
}

func TestTaskInspectionUsesPublicMetadataAndLiveReadPermission(t *testing.T) {
	for _, scenario := range []string{"allowed", "foreign", "revoked_during_read", "read_denied", "missing"} {
		t.Run(scenario, func(t *testing.T) {
			auth := &liveRoomAuth{denyTask: scenario == "read_denied"}
			service := inspectedTaskService{value: task.Task{ID: "task", WorkspaceID: "tenant", Objective: "Inspect assignment", State: task.StateCreated, Completion: json.RawMessage(`{"private":"COMPLETION_CANARY"}`), Result: json.RawMessage(`{"private":"RESULT_CANARY"}`)}}
			if scenario == "foreign" {
				service.value.WorkspaceID = "other-tenant"
			}
			if scenario == "revoked_during_read" {
				service.onRead = func() { auth.revoked = true }
			}
			if scenario == "missing" {
				service.err = sql.ErrNoRows
			}
			s := NewServer(nil, fakeEvents{}, auth)
			s.tasks = service
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/v1/tasks/task", nil))
			want := 403
			if scenario == "allowed" {
				want = 200
			}
			if scenario == "missing" {
				want = 404
			}
			if w.Code != want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "CANARY") {
				t.Fatal("private Task fields leaked through inspection")
			}
			if want == 200 && w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("Task inspection can be cached")
			}
		})
	}
}
