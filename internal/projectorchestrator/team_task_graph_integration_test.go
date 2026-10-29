//go:build integration

package projectorchestrator

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/projectworkspace"
	sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/team"
)

func TestProjectTaskGraphRecognizesReviewedTeamContributionsAndDetectsMissingEdges(t *testing.T) {
	ctx := context.Background()
	db, err := sqlitestore.Open(t.TempDir() + "/team-project.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	db.SQL().SetMaxOpenConns(1)
	for _, q := range []string{
		`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)`,
		`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('human','human','Human','active',1,1,1)`,
		`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','human','active',1,1)`,
	} {
		if _, err = db.SQL().ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	projects := projectworkspace.NewService(db.SQL(), db, clock.Real{})
	project, err := projects.CreateProject(ctx, projectworkspace.CreateProjectCommand{WorkspaceID: "tenant", Name: "Team Project", CreatedBy: "human"})
	if err != nil {
		t.Fatal(err)
	}
	world, err := projects.CreateWorkspaceView(ctx, projectworkspace.CreateWorkspaceViewCommand{ProjectID: project.ID, Name: "World", ActorPrincipalID: "human"})
	if err != nil {
		t.Fatal(err)
	}
	tasks := task.NewService(db.SQL(), db, clock.Real{})
	orchestrator := NewService(db.SQL(), db, clock.Real{}, nil, nil, nil, tasks, nil)
	graph, err := orchestrator.CreateTaskGraph(ctx, CreateTaskGraphCommand{ProjectID: project.ID, Name: "Creation", IdempotencyKey: "team-project", ActorPrincipalID: "human", Nodes: []TaskGraphNodeSpec{{Key: "source", ProjectWorkspaceID: world.ID, Objective: "Prepare source"}, {Key: "deliver", ProjectWorkspaceID: world.ID, Objective: "Consolidate output", DependsOn: []string{"source"}}}})
	if err != nil {
		t.Fatal(err)
	}
	var root string
	for _, node := range graph.Nodes {
		if node.Key == "deliver" {
			root = node.TaskID
		}
	}
	teams := team.NewService(db.SQL(), db, clock.Real{}, tasks)
	tasks.SetAdmissionGuard(teams)
	group, err := teams.CreateTeam(ctx, team.CreateTeamCommand{WorkspaceID: "tenant", Name: "Build team", CreatedBy: "human"})
	if err != nil {
		t.Fatal(err)
	}
	member, err := teams.AddMember(ctx, team.AddMemberCommand{TeamID: group.ID, MemberKind: "agent", DisplayName: "Builder", RoleName: "Builder", CreatedBy: "human"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := teams.StartSession(ctx, team.StartSessionCommand{TaskID: root, TeamID: group.ID, CreatedBy: "human", ExecutionMode: "team"})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"assignments": []team.AssignmentSpec{{Key: "build", MemberID: member.ID, Objective: "Contribute artifact"}}})
	plan, err := teams.ProposePlan(ctx, team.ProposePlanCommand{SessionID: session.ID, ProposedByPrincipalID: "human", Plan: body})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = teams.AcceptPlan(ctx, team.AcceptPlanCommand{SessionID: session.ID, PlanID: plan.ID, AcceptedByPrincipalID: "human"}); err != nil {
		t.Fatal(err)
	}
	graph, err = orchestrator.TaskGraph(ctx, project.ID, graph.ID)
	if err != nil {
		t.Fatal(err)
	}
	var child string
	if err = db.SQL().QueryRowContext(ctx, `SELECT task_id FROM team_execution_assignments WHERE parent_task_id=?`, root).Scan(&child); err != nil {
		t.Fatal(err)
	}
	contribution, err := tasks.Get(ctx, child)
	if err != nil {
		t.Fatal(err)
	}
	if contribution.ProjectID == nil || *contribution.ProjectID != project.ID || contribution.ProjectWorkspaceID == nil || *contribution.ProjectWorkspaceID != world.ID {
		t.Fatalf("Team escaped Project scope: %+v", contribution)
	}
	var inherited int
	if err = db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM task_dependencies d JOIN project_orchestrator_task_graph_nodes n ON n.task_id=d.depends_on_task_id WHERE d.task_id=? AND n.graph_id=? AND n.node_key='source' AND d.dependency_type='hard' AND d.dependency_mode='all'`, child, graph.ID).Scan(&inherited); err != nil || inherited != 1 {
		t.Fatalf("Team child bypassed the original Project prerequisite: %d %v", inherited, err)
	}
	for _, node := range graph.Nodes {
		if node.Key == "deliver" {
			if node.DependencyDrift || node.ScopeDrift || len(node.TeamContributions) != 1 || node.TeamContributions[0] != "build" || len(node.DependsOn) != 1 || node.DependsOn[0] != "source" {
				t.Fatalf("reviewed Team contribution marked as foreign: %+v", node)
			}
			found := false
			for _, blocker := range node.BlockedBy {
				if blocker == "team:build" {
					found = true
				}
			}
			if !found {
				t.Fatalf("Team contribution wait hidden: %+v", node)
			}
		}
	}
	// Removing the separately accepted contribution edge must not make the
	// original graph ready or quietly discard the Team assignment.
	if _, err = db.SQL().ExecContext(ctx, `DELETE FROM task_dependencies WHERE task_id=? AND depends_on_task_id=?`, root, child); err != nil {
		t.Fatal(err)
	}
	graph, err = orchestrator.TaskGraph(ctx, project.ID, graph.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range graph.Nodes {
		if node.Key == "deliver" && !node.DependencyDrift {
			t.Fatal("deleted Team edge was not reported as drift")
		}
	}
}
