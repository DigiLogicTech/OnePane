//go:build integration

package team

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/task"
)

func TestTeamAssistanceReadProductionPlans(t *testing.T) {
	s, db, ss, specs := assignmentFixture(t)
	ctx := context.Background()
	ids := acceptAssignments(t, s, ss, specs)
	sql := db.SQL()
	worker := "system:agent-worker"
	for _, statement := range []string{
		`INSERT INTO principals(id,principal_type,display_name,status,created_at,updated_at) VALUES('system:agent-worker','system','Worker','active',1,1)`,
		`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','system:agent-worker','active',1,1)`,
	} {
		if _, err := sql.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	current, err := s.tasks.Get(ctx, ids["build"])
	if err != nil {
		t.Fatal(err)
	}
	current, err = s.tasks.MarkReady(ctx, task.TransitionCommand{TaskID: current.ID, ExpectedRevision: current.Revision})
	if err != nil {
		t.Fatal(err)
	}
	current, attempt, err := s.tasks.Start(ctx, task.StartCommand{TaskID: current.ID, ExpectedRevision: current.Revision, WorkerPrincipalID: &worker, Metadata: json.RawMessage(`{"agent_worker":"v1"}`)})
	if err != nil {
		t.Fatal(err)
	}
	current, err = s.tasks.WaitApproval(ctx, task.TransitionCommand{TaskID: current.ID, ExpectedRevision: current.Revision})
	if err != nil {
		t.Fatal(err)
	}
	// The Task/Attempt transitions above use the actual service. The following
	// waiting Worker/journal/notice/request rows are synthetic query-plan inputs;
	// actual Worker handler->request->read evidence lives in agentworker tests.
	for _, statement := range []string{
		fmt.Sprintf(`INSERT INTO agent_worker_runs(id,workspace_id,task_id,attempt_id,worker_principal_id,status,role_name,capability_id,protocol_level,max_steps,max_replans,max_escalations,route_policy_json,continuation_json,started_at,updated_at) VALUES('observed-run','tenant','%s','%s','system:agent-worker','waiting','Build','inference.general','L1',8,0,0,'{}','{"human_input_required":"Review this?"}',1,2)`, current.ID, attempt.ID),
		`INSERT INTO agent_worker_steps(id,run_id,step_number,step_kind,status,proposal_type,detail_json,started_at,completed_at) VALUES('observed-step','observed-run',1,'human','waiting','human','{}',1,2)`,
		fmt.Sprintf(`INSERT INTO team_messages(id,workspace_id,session_id,author_member_id,author_principal_id,message_kind,content_json,round_number,created_at) VALUES('observed-notice','tenant','%s','%s','system:agent-worker','system','{"text":"Assignment build needs human input.","assistance_request_id":"observed-help","task_id":"%s","target_member_id":"%s"}',1,2)`, ss.ID, specs[0].MemberID, current.ID, specs[0].MemberID),
		fmt.Sprintf(`INSERT INTO team_assistance_requests(id,workspace_id,session_id,plan_id,assignment_key,member_id,task_id,attempt_id,run_id,wait_step_id,notice_message_id,author_principal_id,question,created_at) SELECT 'observed-help','tenant',session_id,plan_id,assignment_key,member_id,task_id,'%s','observed-run','observed-step','observed-notice','system:agent-worker','Review this?',2 FROM team_execution_assignments WHERE task_id='%s'`, attempt.ID, current.ID),
		`INSERT INTO team_assistance_request_scopes(request_id,project_id,project_workspace_id,artifact_session_id) VALUES('observed-help',NULL,NULL,NULL)`,
	} {
		if _, err = sql.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	want, err := s.AssistanceRequest(ctx, "tenant", ss.ID, "observed-help", "human")
	if err != nil {
		t.Fatalf("healthy fixture: %v", err)
	}
	for _, statement := range []string{
		`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<1024) INSERT INTO tasks(id,workspace_id,objective,state,completion_json,created_at,updated_at) SELECT 'history-'||i,'tenant','history','created','{}',i,i FROM n`,
		`INSERT INTO team_sessions(id,workspace_id,team_id,task_id,status,accepted_plan_id,config_json,created_by,created_at,updated_at) SELECT t.id,s.workspace_id,s.team_id,t.id,'executing',s.accepted_plan_id,'{}',s.created_by,t.created_at,t.updated_at FROM tasks t JOIN team_sessions s ON s.id=? WHERE t.id LIKE 'history-%'`,
		`INSERT INTO team_execution_assignments(session_id,plan_id,assignment_key,workspace_id,member_id,task_id,parent_task_id,depends_on_json,completion_sha256,profile_id,profile_revision,created_at) SELECT t.id,a.plan_id,'history',a.workspace_id,a.member_id,t.id,a.parent_task_id,a.depends_on_json,a.completion_sha256,a.profile_id,a.profile_revision,t.created_at FROM tasks t JOIN team_execution_assignments a ON a.task_id=? WHERE t.id LIKE 'history-%'`,
		`INSERT INTO task_attempts(id,task_id,attempt_number,status,metadata_json) SELECT id,id,1,'waiting','{}' FROM tasks WHERE id LIKE 'history-%'`,
		`INSERT INTO agent_worker_runs(id,workspace_id,task_id,attempt_id,worker_principal_id,status,role_name,capability_id,protocol_level,max_steps,max_replans,max_escalations,route_policy_json,continuation_json,started_at,updated_at) SELECT t.id,r.workspace_id,t.id,t.id,r.worker_principal_id,'waiting',r.role_name,r.capability_id,r.protocol_level,r.max_steps,r.max_replans,r.max_escalations,r.route_policy_json,'{}',t.created_at,t.updated_at FROM tasks t JOIN agent_worker_runs r ON r.id='observed-run' WHERE t.id LIKE 'history-%'`,
		`INSERT INTO agent_worker_steps(id,run_id,step_number,step_kind,status,proposal_type,detail_json,started_at,completed_at) SELECT id,id,1,'human','waiting','human','{}',started_at,updated_at FROM agent_worker_runs WHERE id LIKE 'history-%'`,
		`INSERT INTO team_messages(id,workspace_id,session_id,author_member_id,author_principal_id,message_kind,content_json,round_number,created_at) SELECT t.id,r.workspace_id,t.id,r.member_id,r.author_principal_id,'system','{}',1,t.created_at FROM tasks t JOIN team_assistance_requests r ON r.id='observed-help' WHERE t.id LIKE 'history-%'`,
		`INSERT INTO team_assistance_requests(id,workspace_id,session_id,plan_id,assignment_key,member_id,task_id,attempt_id,run_id,wait_step_id,notice_message_id,author_principal_id,question,created_at) SELECT t.id,r.workspace_id,t.id,r.plan_id,'history',r.member_id,t.id,t.id,t.id,t.id,t.id,r.author_principal_id,'history question',t.created_at FROM tasks t JOIN team_assistance_requests r ON r.id='observed-help' WHERE t.id LIKE 'history-%'`,
		`INSERT INTO team_assistance_request_scopes(request_id,project_id,project_workspace_id,artifact_session_id) SELECT id,NULL,NULL,NULL FROM team_assistance_requests WHERE id LIKE 'history-%'`,
	} {
		var err error
		if strings.Contains(statement, "s.id=?") {
			_, err = sql.ExecContext(ctx, statement, ss.ID)
		} else if strings.Contains(statement, "a.task_id=?") {
			_, err = sql.ExecContext(ctx, statement, current.ID)
		} else {
			_, err = sql.ExecContext(ctx, statement)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, analyze := range []bool{false, true} {
		if analyze {
			if _, err = sql.ExecContext(ctx, `ANALYZE`); err != nil {
				t.Fatal(err)
			}
		}
		observed, err := s.AssistanceRequest(ctx, "tenant", ss.ID, "observed-help", "human")
		if err != nil || observed != want {
			t.Fatalf("history changed exact read: %+v %v", observed, err)
		}
		for _, query := range []struct {
			sql      string
			args     []any
			searches []string
		}{
			{assistanceRequestReadSQL, []any{"human", "observed-help", "tenant", ss.ID}, []string{"h", "pinned", "a", "t", "attempt", "r", "step", "notice"}},
			{assistanceGraphReadSQL, []any{ss.ID}, []string{"a"}},
		} {
			rows, err := sql.QueryContext(ctx, `EXPLAIN QUERY PLAN `+query.sql, query.args...)
			if err != nil {
				t.Fatal(err)
			}
			details := []string{}
			for rows.Next() {
				var id, parent, unused int
				var detail string
				if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
					t.Fatal(err)
				}
				details = append(details, detail)
			}
			if err = rows.Err(); err != nil {
				t.Fatal(err)
			}
			if err = rows.Close(); err != nil {
				t.Fatal(err)
			}
			text := strings.Join(details, "\n")
			if strings.Contains(text, "AUTOMATIC") || strings.Contains(text, "TEMP B-TREE") {
				t.Fatalf("ephemeral plan after analyze=%v:\n%s", analyze, text)
			}
			// Small one-row roster/principal tables may legitimately be scanned after
			// ANALYZE. Every populated history relation must use a persistent search.
			for _, alias := range query.searches {
				found := false
				for _, detail := range details {
					if strings.HasPrefix(detail, "SEARCH "+alias+" USING ") {
						found = true
					}
				}
				if !found {
					t.Fatalf("missing bounded lookup %s after analyze=%v:\n%s", alias, analyze, text)
				}
			}
		}
	}
}
