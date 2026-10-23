package team

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/teamgate"
	"github.com/DigiLogicTech/OnePane/internal/verification"
)

func assignmentFixture(t *testing.T) (*Service, *sqlitestore.DB, Session, []AssignmentSpec) {
	t.Helper()
	ctx := context.Background()
	s, db, old, first := liveRoomFixture(t)
	second, err := s.AddMember(ctx, AddMemberCommand{TeamID: old.TeamID, MemberKind: "agent", DisplayName: "Reviewer", RoleName: "Review", CreatedBy: "human"})
	if err != nil {
		t.Fatal(err)
	}
	parent, err := s.tasks.Create(ctx, task.CreateCommand{WorkspaceID: "tenant", Objective: "Deliver a checked result", Completion: json.RawMessage(`{"required_verification":"V1","onepane_routing":{"candidate_id":"pinned-model","workspace_access":{"remote_models":false}},"toolchain_manifest_sha256":"reviewed-toolchain"}`)})
	if err != nil {
		t.Fatal(err)
	}
	ss, err := s.StartSession(ctx, StartSessionCommand{TaskID: parent.ID, TeamID: old.TeamID, CreatedBy: "human", ExecutionMode: "team"})
	if err != nil {
		t.Fatal(err)
	}
	s.tasks.SetAdmissionGuard(s)
	return s, db, ss, []AssignmentSpec{{Key: "build", MemberID: first.ID, Objective: "Produce the artifact"}, {Key: "review", MemberID: second.ID, Objective: "Check the builder result", DependsOn: []string{"build"}}}
}

func acceptAssignments(t *testing.T, s *Service, ss Session, specs []AssignmentSpec) map[string]string {
	t.Helper()
	ctx := context.Background()
	body, _ := json.Marshal(map[string]any{"assignments": specs})
	plan, err := s.ProposePlan(ctx, ProposePlanCommand{SessionID: ss.ID, ProposedByPrincipalID: "human", Plan: body})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AcceptPlan(ctx, AcceptPlanCommand{SessionID: ss.ID, PlanID: plan.ID, AcceptedByPrincipalID: "human"}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.db.Query(`SELECT assignment_key,task_id FROM team_execution_assignments WHERE session_id=?`, ss.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var key, id string
		if err = rows.Scan(&key, &id); err != nil {
			t.Fatal(err)
		}
		out[key] = id
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func completeAssignment(t *testing.T, s *Service, db *sqlitestore.DB, id string) string {
	t.Helper()
	ctx := context.Background()
	human := "human"
	row, err := s.tasks.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	row, err = s.tasks.MarkReady(ctx, task.TransitionCommand{TaskID: id, ExpectedRevision: row.Revision})
	if err != nil {
		t.Fatal(err)
	}
	row, _, err = s.tasks.Start(ctx, task.StartCommand{TaskID: id, ExpectedRevision: row.Revision, WorkerPrincipalID: &human})
	if err != nil {
		t.Fatal(err)
	}
	row, err = s.tasks.RequestCompletion(ctx, task.TransitionCommand{TaskID: id, ExpectedRevision: row.Revision})
	if err != nil {
		t.Fatal(err)
	}
	row, err = s.tasks.BeginVerification(ctx, task.TransitionCommand{TaskID: id, ExpectedRevision: row.Revision})
	if err != nil {
		t.Fatal(err)
	}
	verify := verification.NewService(db.SQL(), db, clock.Real{})
	level := policy.VerificationV1
	v, err := verify.Create(ctx, verification.CreateCommand{WorkspaceID: "tenant", TaskID: &id, SubjectRef: "test-contribution:" + id, RequiredLevel: level, Spec: json.RawMessage(`{"fixture":true}`)})
	if err != nil {
		t.Fatal(err)
	}
	v, err = verify.Resolve(ctx, verification.ResolveCommand{VerificationID: v.ID, ExpectedRevision: v.Revision, Status: verification.StatusPass, AchievedLevel: &level, VerifiedBy: human})
	if err != nil {
		t.Fatal(err)
	}
	cp, err := verify.CreateCheckpoint(ctx, verification.CheckpointCommand{WorkspaceID: "tenant", TaskID: id, VerificationID: v.ID, State: json.RawMessage(`{"result":"synthetic reviewed contribution"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.tasks.CompleteVerified(ctx, task.CompleteCommand{TaskID: id, ExpectedRevision: row.Revision, CheckpointID: cp.ID}); err != nil {
		t.Fatal(err)
	}
	return cp.ID
}

func TestAcceptedTeamAssignmentsUseGovernedTasksAndExplicitVerifiedHandoffs(t *testing.T) {
	s, db, ss, specs := assignmentFixture(t)
	ctx := context.Background()
	ids := acceptAssignments(t, s, ss, specs)
	if len(ids) != 2 {
		t.Fatalf("assignments: %v", ids)
	}
	for _, id := range ids {
		child, err := s.tasks.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if child.ParentTaskID == nil || *child.ParentTaskID != ss.TaskID || child.WorkspaceID != ss.WorkspaceID || child.State != task.StateCreated {
			t.Fatalf("wrong child: %+v", child)
		}
		var body map[string]json.RawMessage
		if json.Unmarshal(child.Completion, &body) != nil || string(body["required_verification"]) != `"V1"` || string(body["toolchain_manifest_sha256"]) != `"reviewed-toolchain"` {
			t.Fatalf("parent assurance/pin lost: %s", child.Completion)
		}
	}
	if _, err := teamgate.Require(ctx, db.SQL(), "tenant", ids["build"]); err != nil {
		t.Fatal(err)
	}
	if _, err := teamgate.Require(ctx, db.SQL(), "tenant", ids["review"]); err == nil {
		t.Fatal("review ran before predecessor verification")
	}
	root, _ := s.tasks.Get(ctx, ss.TaskID)
	if _, _, err := s.tasks.Start(ctx, task.StartCommand{TaskID: root.ID, ExpectedRevision: root.Revision}); err == nil {
		t.Fatal("consolidator ran before contributors")
	}
	cp := completeAssignment(t, s, db, ids["build"])
	contributions, err := teamgate.Require(ctx, db.SQL(), "tenant", ids["review"])
	if err != nil || len(contributions) != 1 || contributions[0].CheckpointID != cp || contributions[0].AchievedLevel != "V1" || contributions[0].MemberID != specs[0].MemberID {
		t.Fatalf("handoff: %+v %v", contributions, err)
	}
	completeAssignment(t, s, db, ids["review"])
	if err = s.SyncTaskStates(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.SyncTaskStates(ctx); err != nil {
		t.Fatal(err)
	}
	var notices int
	if err = db.SQL().QueryRow(`SELECT COUNT(*) FROM team_assignment_notices WHERE task_id IN (?,?)`, ids["build"], ids["review"]).Scan(&notices); err != nil || notices != 2 {
		t.Fatalf("completion notices were lost or duplicated: %d %v", notices, err)
	}
	contributions, err = teamgate.Require(ctx, db.SQL(), "tenant", ss.TaskID)
	if err != nil || len(contributions) != 2 {
		t.Fatalf("consolidation inputs: %+v %v", contributions, err)
	}
	root, _ = s.tasks.Get(ctx, ss.TaskID)
	if _, _, err = s.tasks.Start(ctx, task.StartCommand{TaskID: root.ID, ExpectedRevision: root.Revision}); err != nil {
		t.Fatalf("verified contributors did not unblock root: %v", err)
	}
	root, _ = s.tasks.Get(ctx, ss.TaskID)
	root, err = s.tasks.Pause(ctx, task.TransitionCommand{TaskID: root.ID, ExpectedRevision: root.Revision})
	if err != nil {
		t.Fatal(err)
	}
	root, err = s.tasks.Resume(ctx, task.TransitionCommand{TaskID: root.ID, ExpectedRevision: root.Revision})
	if err != nil || root.State != task.StateRunning {
		t.Fatalf("consolidator could not resume its original Attempt: %s %v", root.State, err)
	}
}

func TestTeamAssignmentPauseResumeRetainsAttemptAndRequiresActiveRoot(t *testing.T) {
	s, db, ss, specs := assignmentFixture(t)
	ctx := context.Background()
	ids := acceptAssignments(t, s, ss, specs)
	child, err := s.tasks.Get(ctx, ids["build"])
	if err != nil {
		t.Fatal(err)
	}
	child, err = s.tasks.MarkReady(ctx, task.TransitionCommand{TaskID: child.ID, ExpectedRevision: child.Revision})
	if err != nil {
		t.Fatal(err)
	}
	child, attempt, err := s.tasks.Start(ctx, task.StartCommand{TaskID: child.ID, ExpectedRevision: child.Revision})
	if err != nil {
		t.Fatal(err)
	}
	child, err = s.tasks.Pause(ctx, task.TransitionCommand{TaskID: child.ID, ExpectedRevision: child.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = teamgate.Require(ctx, db.SQL(), "tenant", child.ID); err == nil {
		t.Fatal("paused contribution still executable")
	}
	root, _ := s.tasks.Get(ctx, ss.TaskID)
	root, err = s.tasks.Pause(ctx, task.TransitionCommand{TaskID: root.ID, ExpectedRevision: root.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.tasks.Resume(ctx, task.TransitionCommand{TaskID: child.ID, ExpectedRevision: child.Revision}); err == nil {
		t.Fatal("child resumed while root paused")
	}
	if _, err = s.tasks.Resume(ctx, task.TransitionCommand{TaskID: root.ID, ExpectedRevision: root.Revision}); err != nil {
		t.Fatal(err)
	}
	child, err = s.tasks.Resume(ctx, task.TransitionCommand{TaskID: child.ID, ExpectedRevision: child.Revision})
	if err != nil || child.State != task.StateRunning {
		t.Fatalf("contribution resume: %s %v", child.State, err)
	}
	var active string
	if err = db.SQL().QueryRow(`SELECT id FROM task_attempts WHERE task_id=? AND status='running'`, child.ID).Scan(&active); err != nil || active != attempt.ID {
		t.Fatalf("resume replaced the original Attempt: %s %v", active, err)
	}
}

func TestTeamAssignmentAcceptanceIsAtomicAndRejectsInvalidOrForeignWork(t *testing.T) {
	for _, mutation := range []string{"cycle", "foreign_member", "unknown_dependency", "duplicate", "unknown_field", "failing_journal"} {
		t.Run(mutation, func(t *testing.T) {
			s, db, ss, specs := assignmentFixture(t)
			ctx := context.Background()
			switch mutation {
			case "cycle":
				specs[0].DependsOn = []string{"review"}
			case "foreign_member":
				specs[1].MemberID = "other-team-member"
			case "unknown_dependency":
				specs[1].DependsOn = []string{"absent"}
			case "duplicate":
				specs[1].Key = specs[0].Key
			case "failing_journal":
				if _, err := db.SQL().Exec(`CREATE TRIGGER fail_assignment_event BEFORE INSERT ON events WHEN NEW.event_type='team.assignment_created' BEGIN SELECT RAISE(ABORT,'fixture event fault'); END`); err != nil {
					t.Fatal(err)
				}
			}
			body, _ := json.Marshal(map[string]any{"assignments": specs})
			if mutation == "unknown_field" {
				body = json.RawMessage(strings.Replace(string(body), `"objective":`, `"tool_grant":true,"objective":`, 1))
			}
			plan, err := s.ProposePlan(ctx, ProposePlanCommand{SessionID: ss.ID, ProposedByPrincipalID: "human", Plan: body})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.AcceptPlan(ctx, AcceptPlanCommand{SessionID: ss.ID, PlanID: plan.ID, AcceptedByPrincipalID: "human"}); err == nil {
				t.Fatal("invalid plan accepted")
			}
			var n int
			if err = db.SQL().QueryRow(`SELECT COUNT(*) FROM tasks WHERE parent_task_id=?`, ss.TaskID).Scan(&n); err != nil || n != 0 {
				t.Fatalf("orphan Tasks after rejected acceptance: %d %v", n, err)
			}
			if err = db.SQL().QueryRow(`SELECT COUNT(*) FROM team_assignment_roots WHERE session_id=?`, ss.ID).Scan(&n); err != nil || n != 0 {
				t.Fatalf("partial root commit: %d %v", n, err)
			}
		})
	}
}

func TestTeamAssignmentFencesDeletedEdgesPausedRootAndChangedScope(t *testing.T) {
	for _, mutation := range []string{"edge", "root_pause", "scope", "completion", "objective", "checkpoint", "profile"} {
		t.Run(mutation, func(t *testing.T) {
			s, db, ss, specs := assignmentFixture(t)
			ctx := context.Background()
			ids := acceptAssignments(t, s, ss, specs)
			cp := completeAssignment(t, s, db, ids["build"])
			var err error
			switch mutation {
			case "edge":
				_, err = db.SQL().Exec(`DELETE FROM task_dependencies WHERE task_id=?`, ids["review"])
			case "root_pause":
				root, _ := s.tasks.Get(ctx, ss.TaskID)
				_, err = s.tasks.Pause(ctx, task.TransitionCommand{TaskID: root.ID, ExpectedRevision: root.Revision})
			case "scope":
				_, err = db.SQL().Exec(`UPDATE tasks SET parent_task_id=NULL WHERE id=?`, ids["review"])
			case "completion":
				_, err = db.SQL().Exec(`UPDATE tasks SET completion_json='{}' WHERE id=?`, ids["review"])
			case "objective":
				_, err = db.SQL().Exec(`UPDATE tasks SET objective='different unreviewed work' WHERE id=?`, ids["review"])
			case "checkpoint":
				_, err = db.SQL().Exec(`UPDATE checkpoints SET status='stale',invalidated_at=1 WHERE id=?`, cp)
			case "profile":
				_, err = db.SQL().Exec(`UPDATE agent_profiles SET revision=revision+1 WHERE id='agent.md'`)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = teamgate.Require(ctx, db.SQL(), "tenant", ids["review"]); err == nil {
				t.Fatal("changed authority allowed execution")
			}
		})
	}
}
