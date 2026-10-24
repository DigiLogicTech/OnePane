package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/projectworkspace"
	"github.com/DigiLogicTech/OnePane/internal/team"
)

type inspectedTeamRead struct {
	teamService
	stage     string
	afterRead func()
}

func (s inspectedTeamRead) session() team.Session {
	return team.Session{ID: "room", TaskID: "task", WorkspaceID: "tenant", Config: json.RawMessage(`{"text":"PRIVATE_SESSION_CANARY"}`)}
}
func (s inspectedTeamRead) Session(context.Context, string) (team.Session, error) {
	if s.stage == "session" {
		s.afterRead()
	}
	return s.session(), nil
}
func (s inspectedTeamRead) SessionByTask(context.Context, string) (team.Session, error) {
	s.afterRead()
	return s.session(), nil
}
func (s inspectedTeamRead) messages() []team.Message {
	return []team.Message{{ID: "message", SessionID: "room", WorkspaceID: "tenant", Content: json.RawMessage(`{"text":"PRIVATE_TRANSCRIPT_CANARY"}`)}}
}
func (s inspectedTeamRead) ListMessages(context.Context, string, int) ([]team.Message, error) {
	s.afterRead()
	return s.messages(), nil
}
func (s inspectedTeamRead) PageMessages(context.Context, string, team.MessagePageOptions) (team.MessagePage, error) {
	s.afterRead()
	return team.MessagePage{Messages: s.messages()}, nil
}
func (s inspectedTeamRead) LiveRoom(context.Context, string) (team.LiveRoom, error) {
	s.afterRead()
	return team.LiveRoom{ID: "room", TaskID: "task", WorkspaceID: "tenant", Members: []team.LiveMember{{Name: "PRIVATE_ROSTER_CANARY"}}}, nil
}

func (s inspectedTeamRead) Team(context.Context, string) (team.Team, error) {
	return team.Team{ID: "team", WorkspaceID: "tenant"}, nil
}
func (s inspectedTeamRead) ListTeams(context.Context, string) ([]team.Team, error) {
	s.afterRead()
	return []team.Team{{ID: "team", WorkspaceID: "tenant", Name: "PRIVATE_TEAM_CANARY"}}, nil
}
func (s inspectedTeamRead) ListMembers(context.Context, string) ([]team.Member, error) {
	s.afterRead()
	return []team.Member{{ID: "member", WorkspaceID: "tenant", DisplayName: "PRIVATE_MEMBER_CANARY"}}, nil
}
func (s inspectedTeamRead) SessionManifest(context.Context, string) (team.SessionManifest, error) {
	if s.stage == "manifest" {
		s.afterRead()
	}
	return team.SessionManifest{SessionID: "room", WorkspaceID: "tenant", Snapshot: json.RawMessage(`{"text":"PRIVATE_MANIFEST_CANARY"}`)}, nil
}
func (s inspectedTeamRead) ListSeatBindings(context.Context, string) ([]team.SeatBinding, error) {
	if s.stage == "bindings" {
		s.afterRead()
	}
	return []team.SeatBinding{{SessionID: "room", MemberID: "PRIVATE_BINDING_CANARY"}}, nil
}
func (s inspectedTeamRead) ListTurns(context.Context, string) ([]team.TurnRequest, error) {
	s.afterRead()
	return []team.TurnRequest{{ID: "PRIVATE_TURN_CANARY", SessionID: "room", WorkspaceID: "tenant"}}, nil
}
func (s inspectedTeamRead) ListPlans(context.Context, string) ([]team.Plan, error) {
	s.afterRead()
	return []team.Plan{{ID: "plan", SessionID: "room", WorkspaceID: "tenant", Body: json.RawMessage(`{"text":"PRIVATE_PLAN_CANARY"}`)}}, nil
}
func (s inspectedTeamRead) ListObjections(context.Context, string) ([]team.Objection, error) {
	s.afterRead()
	return []team.Objection{{ID: "objection", SessionID: "room", WorkspaceID: "tenant", Summary: "PRIVATE_OBJECTION_CANARY"}}, nil
}
func (s inspectedTeamRead) ListDecisions(context.Context, string) ([]team.Decision, error) {
	s.afterRead()
	return []team.Decision{{ID: "decision", SessionID: "room", WorkspaceID: "tenant", Body: json.RawMessage(`{"text":"PRIVATE_DECISION_CANARY"}`)}}, nil
}

type inspectedResearchRead struct {
	projectService
	afterRead func()
}

func (s inspectedResearchRead) FreezeResearchEvidence(context.Context, string, string, []projectworkspace.WorkspaceEvidenceSelection) (projectworkspace.ResearchEvidenceSelection, error) {
	panic("read must not freeze evidence")
}
func (s inspectedResearchRead) ResearchEvidence(context.Context, string, string) (projectworkspace.ResearchEvidenceSelection, error) {
	s.afterRead()
	return projectworkspace.ResearchEvidenceSelection{SessionID: "room", ProjectID: "PRIVATE_EVIDENCE_CANARY"}, nil
}

func TestTeamReadsRejectCredentialChangesDuringRead(t *testing.T) {
	for _, target := range []struct{ stage, path string }{
		{"lookup", "/v1/tasks/task/team-session"},
		{"session", "/v1/team-sessions/room"},
		{"live", "/v1/team-sessions/room/live"},
		{"messages", "/v1/team-sessions/room/messages"},
		{"page", "/v1/team-sessions/room/messages/page"},
		{"teams", "/v1/teams?workspace_id=tenant"},
		{"members", "/v1/teams/team/members"},
		{"manifest", "/v1/team-sessions/room/manifest"},
		{"bindings", "/v1/team-sessions/room/manifest"},
		{"turns", "/v1/team-sessions/room/turns"},
		{"plans", "/v1/team-sessions/room/plans"},
		{"objections", "/v1/team-sessions/room/objections"},
		{"decisions", "/v1/team-sessions/room/decisions"},
		{"evidence", "/v1/team-sessions/room/research/evidence-selection"},
	} {
		for _, scenario := range []string{"allowed", "revoked", "principal_changed", "credential_changed", "principal_type_changed", "auth_failed", "permission_revoked"} {
			t.Run(target.stage+"/"+scenario, func(t *testing.T) {
				auth := &inspectedTaskAuth{identity: Identity{PrincipalID: "human", CredentialID: "original", PrincipalType: "human"}}
				want := 200
				service := inspectedTeamRead{stage: target.stage, afterRead: func() {
					want = 403
					switch scenario {
					case "allowed":
						want = 200
					case "revoked":
						auth.err = ErrUnauthenticated
						want = 401
					case "principal_changed":
						auth.identity.PrincipalID = "replacement"
					case "credential_changed":
						auth.identity.CredentialID = "replacement"
					case "principal_type_changed":
						auth.identity.PrincipalType = "agent"
					case "auth_failed":
						auth.err = errors.New("PRIVATE_AUTH_CANARY")
						want = 500
					case "permission_revoked":
						auth.revoked = true
					}
				}}
				s := NewServer(nil, fakeEvents{}, auth)
				s.SetTeam(service)
				s.projects = inspectedResearchRead{afterRead: service.afterRead}
				w := httptest.NewRecorder()
				s.Handler().ServeHTTP(w, httptest.NewRequest("GET", target.path, nil))
				if w.Code != want {
					t.Fatalf("status %d want %d body %s", w.Code, want, w.Body.String())
				}
				if want != 200 && strings.Contains(w.Body.String(), "CANARY") {
					t.Fatal("revoked credential disclosed Team data")
				}
				if want == 200 && (w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), "CANARY")) {
					t.Fatal("successful scoped read lost its payload/cache contract")
				}
			})
		}
	}
}

type revokedTeamLookup struct {
	inspectedTeamRead
	collected *bool
}

func (s revokedTeamLookup) Session(context.Context, string) (team.Session, error) {
	s.afterRead()
	return s.session(), nil
}
func (s revokedTeamLookup) Team(context.Context, string) (team.Team, error) {
	s.afterRead()
	return team.Team{ID: "team", WorkspaceID: "tenant"}, nil
}
func (s revokedTeamLookup) ListMessages(context.Context, string, int) ([]team.Message, error) {
	*s.collected = true
	return s.messages(), nil
}
func (s revokedTeamLookup) ListMembers(context.Context, string) ([]team.Member, error) {
	*s.collected = true
	return []team.Member{{DisplayName: "PRIVATE_MEMBER_CANARY"}}, nil
}

func TestTeamReadsRejectLookupRevocationBeforePrivateCollection(t *testing.T) {
	for _, path := range []string{"/v1/teams/team/members", "/v1/team-sessions/room/messages"} {
		t.Run(path, func(t *testing.T) {
			auth := &inspectedTaskAuth{identity: Identity{PrincipalID: "human", CredentialID: "original", PrincipalType: "human"}}
			collected := false
			service := revokedTeamLookup{
				inspectedTeamRead: inspectedTeamRead{afterRead: func() { auth.err = ErrUnauthenticated }},
				collected:         &collected,
			}
			s := NewServer(nil, fakeEvents{}, auth)
			s.SetTeam(service)
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
			if w.Code != 401 || collected || strings.Contains(w.Body.String(), "CANARY") {
				t.Fatalf("lookup revocation not fenced: status %d collected %v body %s", w.Code, collected, w.Body.String())
			}
		})
	}
}
