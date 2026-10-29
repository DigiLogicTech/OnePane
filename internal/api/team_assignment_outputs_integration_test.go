//go:build integration

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	"github.com/DigiLogicTech/OnePane/internal/projectworkspace"
	"github.com/DigiLogicTech/OnePane/internal/sandboxrunner"
	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/team"
	"github.com/DigiLogicTech/OnePane/internal/verification"
	"github.com/DigiLogicTech/OnePane/internal/workspacepublisher"
)

type teamAssignmentOutputsFixture struct {
	*publishedLibraryFixture
	team                            *team.Service
	sessionID, taskID, checkpointID string
	auth                            *inspectedTaskAuth
}

// Exercise the real accepted graph, Task/Attempt lifecycle, publisher,
// verification and completion ledger. Runtime/application rows are declarations:
// this does not execute OCI, a model or an installed application.
func newTeamAssignmentOutputsFixture(t *testing.T) *teamAssignmentOutputsFixture {
	t.Helper()
	f := publishedLibraryHTTPFixture(t, nil)
	ctx := context.Background()
	tasks := task.NewService(f.db.SQL(), f.db, clock.Real{})
	root, err := tasks.Create(ctx, task.CreateCommand{WorkspaceID: "tenant", ProjectID: &f.projectID, ProjectWorkspaceID: &f.workspaceID, Objective: "Produce a checked Team result", Completion: json.RawMessage(`{"required_verification":"V1"}`)})
	if err != nil {
		t.Fatal(err)
	}
	teams := team.NewService(f.db.SQL(), f.db, clock.Real{}, tasks)
	group, err := teams.CreateTeam(ctx, team.CreateTeamCommand{WorkspaceID: "tenant", Name: "Publishers", CreatedBy: "human"})
	if err != nil {
		t.Fatal(err)
	}
	member, err := teams.AddMember(ctx, team.AddMemberCommand{TeamID: group.ID, MemberKind: "agent", DisplayName: "Publisher", RoleName: "Build", CreatedBy: "human"})
	if err != nil {
		t.Fatal(err)
	}
	ss, err := teams.StartSession(ctx, team.StartSessionCommand{TaskID: root.ID, TeamID: group.ID, CreatedBy: "human", ExecutionMode: "team"})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"assignments": []team.AssignmentSpec{{Key: "publish", MemberID: member.ID, Objective: "Publish checked bytes"}}})
	plan, err := teams.ProposePlan(ctx, team.ProposePlanCommand{SessionID: ss.ID, ProposedByPrincipalID: "human", Plan: body})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = teams.AcceptPlan(ctx, team.AcceptPlanCommand{SessionID: ss.ID, PlanID: plan.ID, AcceptedByPrincipalID: "human"}); err != nil {
		t.Fatal(err)
	}
	tasks.SetAdmissionGuard(teams)
	var childID string
	if err = f.db.SQL().QueryRow(`SELECT task_id FROM team_execution_assignments WHERE session_id=? AND assignment_key='publish'`, ss.ID).Scan(&childID); err != nil {
		t.Fatal(err)
	}
	child, err := tasks.Get(ctx, childID)
	if err != nil {
		t.Fatal(err)
	}
	child, err = tasks.MarkReady(ctx, task.TransitionCommand{TaskID: child.ID, ExpectedRevision: child.Revision})
	if err != nil {
		t.Fatal(err)
	}
	human := "human"
	child, attempt, err := tasks.Start(ctx, task.StartCommand{TaskID: child.ID, ExpectedRevision: child.Revision, WorkerPrincipalID: &human})
	if err != nil {
		t.Fatal(err)
	}
	var runtimeID, applicationID string
	if err = f.db.SQL().QueryRow(`SELECT runtime_id,application_id FROM workspace_file_publications WHERE library_asset_id=? LIMIT 1`, f.assetID).Scan(&runtimeID, &applicationID); err != nil {
		t.Fatal(err)
	}
	receipt, err := workspacepublisher.New(f.db.SQL(), f.artifacts, f.projects, "local-node").PublishWorkspaceFile(ctx, sandboxrunner.WorkspacePublicationRequest{WorkspaceID: "tenant", TaskID: child.ID, AttemptID: attempt.ID, RuntimeID: runtimeID, ApplicationID: applicationID, Path: "outputs/result.txt", Name: "Checked result", MediaType: "text/plain", Content: f.body, ContentHash: f.contentHash})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.LibraryAssetID != f.assetID || receipt.ArtifactID != f.artifactID || receipt.Version != f.version {
		t.Fatal("genuine unchanged publication did not reuse immutable earlier version")
	}
	child, err = tasks.RequestCompletion(ctx, task.TransitionCommand{TaskID: child.ID, ExpectedRevision: child.Revision})
	if err != nil {
		t.Fatal(err)
	}
	child, err = tasks.BeginVerification(ctx, task.TransitionCommand{TaskID: child.ID, ExpectedRevision: child.Revision})
	if err != nil {
		t.Fatal(err)
	}
	verifier := verification.NewService(f.db.SQL(), f.db, clock.Real{})
	level := policy.VerificationV1
	v, err := verifier.Create(ctx, verification.CreateCommand{WorkspaceID: "tenant", TaskID: &child.ID, SubjectRef: "test-contribution:" + child.ID, RequiredLevel: level, Spec: json.RawMessage(`{"fixture":true}`)})
	if err != nil {
		t.Fatal(err)
	}
	v, err = verifier.Resolve(ctx, verification.ResolveCommand{VerificationID: v.ID, ExpectedRevision: v.Revision, Status: verification.StatusPass, AchievedLevel: &level, VerifiedBy: human})
	if err != nil {
		t.Fatal(err)
	}
	cp, err := verifier.CreateCheckpoint(ctx, verification.CheckpointCommand{WorkspaceID: "tenant", TaskID: child.ID, VerificationID: v.ID, State: json.RawMessage(`{"result":"recorded synthetic checked contribution"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tasks.CompleteVerified(ctx, task.CompleteCommand{TaskID: child.ID, ExpectedRevision: child.Revision, CheckpointID: cp.ID}); err != nil {
		t.Fatal(err)
	}
	auth := &inspectedTaskAuth{identity: Identity{PrincipalID: "human", PrincipalType: "human", CredentialID: "original"}}
	f.server = NewServer(f.projects, fakeEvents{}, auth)
	f.server.SetTeam(teams)
	f.server.SetLibraryArtifacts(f.artifacts)
	return &teamAssignmentOutputsFixture{publishedLibraryFixture: f, team: teams, sessionID: ss.ID, taskID: child.ID, checkpointID: cp.ID, auth: auth}
}

type observedAssignmentOutputsLibrary struct {
	*projectworkspace.Service
	afterRead func(int)
	reads     int
}

func (s *observedAssignmentOutputsLibrary) TaskLibraryPublications(ctx context.Context, p, w, taskID string) ([]projectworkspace.WorkspacePublishedOutput, error) {
	out, err := s.Service.TaskLibraryPublications(ctx, p, w, taskID)
	s.reads++
	if err == nil && s.afterRead != nil {
		s.afterRead(s.reads)
	}
	return out, err
}

func TestTeamAssignmentOutputsRealPublicationAndCurrentReadFences(t *testing.T) {
	for _, scenario := range []string{"allowed", "checkpoint_invalidated", "root_criteria_changed", "grant_revoked", "project_archived_at_final_collection", "principal_changed", "credential_changed", "principal_type_changed", "credential_revoked", "permission_revoked"} {
		t.Run(scenario, func(t *testing.T) {
			f := newTeamAssignmentOutputsFixture(t)
			var originalProvenance string
			if err := f.db.SQL().QueryRow(`SELECT provenance_json FROM project_library_asset_versions WHERE asset_id=? AND version=?`, f.assetID, f.version).Scan(&originalProvenance); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(originalProvenance, f.taskID) {
				t.Fatal("fixture failed to preserve older originating Task provenance")
			}
			before := assignmentOutputReadState(t, f)
			reader := &observedAssignmentOutputsLibrary{Service: f.projects}
			reader.afterRead = func(n int) {
				if scenario == "project_archived_at_final_collection" {
					if n == 2 {
						if _, err := f.db.SQL().Exec(`UPDATE projects SET status='archived' WHERE id=?`, f.projectID); err != nil {
							t.Fatal(err)
						}
					}
					return
				}
				if n != 1 {
					return
				}
				switch scenario {
				case "root_criteria_changed":
					if _, err := f.db.SQL().Exec(`UPDATE tasks SET completion_json='{"required_verification":"V1","changed":true}' WHERE id=(SELECT parent_task_id FROM tasks WHERE id=?)`, f.taskID); err != nil {
						t.Fatal(err)
					}
				case "checkpoint_invalidated":
					if _, err := f.db.SQL().Exec(`UPDATE checkpoints SET invalidated_at=1 WHERE id=?`, f.checkpointID); err != nil {
						t.Fatal(err)
					}
				case "grant_revoked":
					if _, err := f.db.SQL().Exec(`UPDATE workspace_library_grants SET enabled=0 WHERE asset_id=?`, f.assetID); err != nil {
						t.Fatal(err)
					}
				case "principal_changed":
					f.auth.identity.PrincipalID = "replacement"
				case "credential_changed":
					f.auth.identity.CredentialID = "replacement"
				case "principal_type_changed":
					f.auth.identity.PrincipalType = "agent"
				case "credential_revoked":
					f.auth.err = ErrUnauthenticated
				case "permission_revoked":
					f.auth.revoked = true
				}
			}
			f.server.projects = reader
			w := httptest.NewRecorder()
			f.server.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/v1/team-sessions/"+f.sessionID+"/assignments/"+f.taskID+"/outputs", nil))
			want := 403
			if scenario == "allowed" {
				want = 200
			}
			if scenario == "checkpoint_invalidated" || scenario == "root_criteria_changed" {
				want = 409
			}
			if scenario == "credential_revoked" {
				want = 401
			}
			if w.Code != want {
				t.Fatalf("status=%d want=%d body=%s", w.Code, want, w.Body.String())
			}
			if scenario == "allowed" {
				var out assignmentPublicationsResponse
				if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
					t.Fatal(err)
				}
				if out.Completion.TaskID != f.taskID || out.Completion.CheckpointID != f.checkpointID || out.Completion.VerificationLevel != "V1" || len(out.Outputs) != 1 || out.Outputs[0].AssetID != f.assetID || out.Outputs[0].RelativePath != "outputs/result.txt" || out.Outputs[0].ContentHash != f.contentHash {
					t.Fatalf("wrong accepted Task associations: %+v", out)
				}
				if w.Header().Get("Cache-Control") != "no-store, private" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
					t.Fatalf("unsafe metadata cache headers: %v", w.Header())
				}
				if strings.Contains(w.Body.String(), f.artifactID) || strings.Contains(w.Body.String(), "artifact:") || strings.Contains(w.Body.String(), "provenance") || strings.Contains(w.Body.String(), string(f.body)) {
					t.Fatal("private storage or unrequested content disclosed")
				}
				// The separately scoped content endpoint independently authorizes
				// a click after metadata collection.
				if _, err := f.db.SQL().Exec(`UPDATE workspace_library_grants SET enabled=0 WHERE asset_id=?`, f.assetID); err != nil {
					t.Fatal(err)
				}
				clicked := httptest.NewRecorder()
				f.server.Handler().ServeHTTP(clicked, httptest.NewRequest("GET", "/v1/projects/"+f.projectID+"/workspaces/"+f.workspaceID+"/library/"+f.assetID+"/versions/1/preview", nil))
				if clicked.Code != 403 || strings.Contains(clicked.Body.String(), string(f.body)) {
					t.Fatalf("metadata became transferable content grant: %d %s", clicked.Code, clicked.Body.String())
				}
			} else {
				for _, private := range []string{f.contentHash, f.assetID, f.checkpointID, "outputs/result.txt"} {
					if strings.Contains(w.Body.String(), private) {
						t.Fatalf("denied read disclosed %s", private)
					}
				}
			}
			if got := assignmentOutputReadState(t, f); got != before {
				t.Fatalf("read dispatched work or changed lifecycle: before=%s after=%s", before, got)
			}
			var afterProvenance string
			if err := f.db.SQL().QueryRow(`SELECT provenance_json FROM project_library_asset_versions WHERE asset_id=? AND version=?`, f.assetID, f.version).Scan(&afterProvenance); err != nil {
				t.Fatal(err)
			}
			if afterProvenance != originalProvenance {
				t.Fatal("read rewrote historical immutable version provenance")
			}
		})
	}
}

func assignmentOutputReadState(t *testing.T, f *teamAssignmentOutputsFixture) string {
	t.Helper()
	var state string
	err := f.db.SQL().QueryRow(`SELECT json_array(
 (SELECT COUNT(*) FROM events),(SELECT COUNT(*) FROM task_attempts),
 (SELECT COUNT(*) FROM inference_requests),(SELECT COUNT(*) FROM tool_invocations),
 (SELECT COUNT(*) FROM agent_runtime_invocations),
 (SELECT revision FROM tasks WHERE id=?),(SELECT state FROM tasks WHERE id=?),
 (SELECT COUNT(*) FROM workspace_file_publications),(SELECT COUNT(*) FROM artifacts))`, f.taskID, f.taskID).Scan(&state)
	if err != nil {
		t.Fatal(fmt.Errorf("read-state observation: %w", err))
	}
	return state
}
