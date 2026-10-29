//go:build integration

package agentworker

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/DigiLogicTech/OnePane/internal/agentprotocol"
)

func TestTeamAssistanceHumanWaitCommitsExactOriginalOwner(t *testing.T) {
	w, ctx, run, _, session, plan := prepareTeamProposalContinuation(t, "human")
	result := applyWorkerWait(t, w, ctx, run, "human")
	if result.Status != "waiting_human" {
		t.Fatalf("wait: %+v", result)
	}
	var request, ws, gotSession, gotPlan, key, member, taskID, attempt, worker, step, message, author, question string
	if err := w.db.QueryRowContext(ctx, `SELECT id,workspace_id,session_id,plan_id,assignment_key,member_id,task_id,attempt_id,run_id,wait_step_id,notice_message_id,author_principal_id,question FROM team_assistance_requests`).Scan(&request, &ws, &gotSession, &gotPlan, &key, &member, &taskID, &attempt, &worker, &step, &message, &author, &question); err != nil {
		t.Fatal(err)
	}
	if request == "" || ws != run.WorkspaceID || gotSession != session || gotPlan != plan || key != "build" || member == "" || taskID != run.TaskID || attempt != run.AttemptID || worker != run.ID || author != WorkerPrincipal || question != "Review the recorded contribution?" {
		t.Fatalf("wrong exact assistance owner: %s %s %s %s %s %s %s %s %s %q", ws, gotSession, gotPlan, key, member, taskID, attempt, worker, author, question)
	}
	var stepRun, kind, status, proposal, noticeKind, noticeAuthor, noticeMember, noticeSession, content string
	if err := w.db.QueryRowContext(ctx, `SELECT run_id,step_kind,status,proposal_type FROM agent_worker_steps WHERE id=?`, step).Scan(&stepRun, &kind, &status, &proposal); err != nil {
		t.Fatal(err)
	}
	if stepRun != run.ID || kind != "human" || status != "waiting" || proposal != "human" {
		t.Fatal("request did not bind exact waiting journal")
	}
	if err := w.db.QueryRowContext(ctx, `SELECT message_kind,author_principal_id,author_member_id,session_id,content_json FROM team_messages WHERE id=?`, message).Scan(&noticeKind, &noticeAuthor, &noticeMember, &noticeSession, &content); err != nil {
		t.Fatal(err)
	}
	var body map[string]string
	if err := json.Unmarshal([]byte(content), &body); err != nil {
		t.Fatal(err)
	}
	if noticeKind != "system" || noticeAuthor != WorkerPrincipal || noticeMember != member || noticeSession != session || body["assistance_request_id"] != request || strings.Contains(content, question) {
		t.Fatal("notice impersonates authored question or wrong identity")
	}
	var state, attemptState, workerState string
	var events, steps int
	if err := w.db.QueryRowContext(ctx, `SELECT t.state,a.status,r.status,(SELECT COUNT(*) FROM events WHERE event_type='team.assistance_requested' AND json_extract(payload_json,'$.request_id')=?),(SELECT COUNT(*) FROM agent_worker_steps WHERE run_id=? AND step_kind='human') FROM tasks t JOIN task_attempts a ON a.task_id=t.id JOIN agent_worker_runs r ON r.attempt_id=a.id WHERE r.id=?`, request, run.ID, run.ID).Scan(&state, &attemptState, &workerState, &events, &steps); err != nil {
		t.Fatal(err)
	}
	if state != "waiting_approval" || attemptState != "waiting" || workerState != "waiting" || events != 1 || steps != 1 {
		t.Fatalf("partial wait %s %s %s %d %d", state, attemptState, workerState, events, steps)
	}
	// Reconstructed service reads durable data; old proposal redelivery cannot
	// produce another request, notice, Attempt or execution.
	restarted := New(w.db, w.tx, w.clock, w.localNodeID, w.tasks, w.authority, w.budgets, w.scheduler, w.inference, w.runtimes, w.artifacts, w.tools, w.operations, w.observations, w.verification)
	if r := applyWorkerWait(t, restarted, ctx, run, "human"); r.Status != "failed" {
		t.Fatal("redelivery accepted")
	}
	var count int
	if err := w.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM team_assistance_requests`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate requests count=%d err=%v", count, err)
	}
	var scopeCount int
	if err := w.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM team_assistance_request_scopes WHERE request_id=? AND project_id IS NULL AND project_workspace_id IS NULL AND artifact_session_id IS NULL`, request).Scan(&scopeCount); err != nil || scopeCount != 1 {
		t.Fatalf("original NULL scope not captured: %d %v", scopeCount, err)
	}
	for _, statement := range []string{`UPDATE team_assistance_request_scopes SET project_id=NULL WHERE request_id=?`, `DELETE FROM team_assistance_request_scopes WHERE request_id=?`} {
		if _, err := w.db.ExecContext(ctx, statement, request); err == nil {
			t.Fatal("immutable scope mutation accepted")
		}
	}
	for _, sql := range []string{`UPDATE team_assistance_requests SET question='replacement' WHERE id=?`, `DELETE FROM team_assistance_requests WHERE id=?`, `INSERT INTO team_assistance_requests SELECT 'duplicate',workspace_id,session_id,plan_id,assignment_key,member_id,task_id,attempt_id,run_id,wait_step_id,notice_message_id,author_principal_id,question,created_at FROM team_assistance_requests WHERE id=?`} {
		if _, err := w.db.ExecContext(ctx, sql, request); err == nil {
			t.Fatalf("immutable/unique request allowed %s", sql)
		}
	}
}

func TestTeamAssistanceProductionBindingPlan(t *testing.T) {
	w, ctx, run, _, _, _ := prepareTeamProposalContinuation(t, "human")
	if result := applyWorkerWait(t, w, ctx, run, "human"); result.Status != "waiting_human" {
		t.Fatalf("wait %+v", result)
	}
	var stepID string
	if err := w.db.QueryRowContext(ctx, `SELECT wait_step_id FROM team_assistance_requests WHERE run_id=?`, run.ID).Scan(&stepID); err != nil {
		t.Fatal(err)
	}
	// Populate assignment history without claiming these synthetic entries are
	// an accepted execution graph. The plan is for the unchanged production
	// binding query; it must not scan that history to find one exact Task.
	for _, statement := range []string{
		`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<1024) INSERT INTO tasks(id,workspace_id,objective,state,completion_json,created_at,updated_at) SELECT 'history-task-'||i,'tenant','unrelated history','created','{}',i,i FROM n`,
		`INSERT INTO team_members(id,team_id,workspace_id,member_kind,display_name,role_name,route_policy_json,status,config_json,created_at,updated_at) SELECT t.id,a.team_id,a.workspace_id,'agent','history','history','{}','active','{}',t.created_at,t.updated_at FROM tasks t JOIN team_members a ON a.id=(SELECT member_id FROM team_execution_assignments WHERE task_id=?) WHERE t.id LIKE 'history-task-%'`,
		`INSERT INTO team_sessions(id,workspace_id,team_id,task_id,status,accepted_plan_id,config_json,created_by,created_at,updated_at) SELECT t.id,s.workspace_id,s.team_id,t.id,'executing',s.accepted_plan_id,'{}',s.created_by,t.created_at,t.updated_at FROM tasks t JOIN team_sessions s ON s.id=(SELECT session_id FROM team_execution_assignments WHERE task_id=?) WHERE t.id LIKE 'history-task-%'`,
		`INSERT INTO task_execution_profiles(task_id,workspace_id,execution_mode,team_id,team_session_id,config_json,created_by,created_at,updated_at) SELECT t.id,ep.workspace_id,'team',ep.team_id,ep.team_session_id,'{}',ep.created_by,t.created_at,t.updated_at FROM tasks t JOIN task_execution_profiles ep ON ep.task_id=? WHERE t.id LIKE 'history-task-%'`,
		`INSERT INTO team_execution_assignments(session_id,plan_id,assignment_key,workspace_id,member_id,task_id,parent_task_id,depends_on_json,completion_sha256,profile_id,profile_revision,created_at) SELECT a.session_id,a.plan_id,t.id,a.workspace_id,a.member_id,t.id,a.parent_task_id,a.depends_on_json,a.completion_sha256,a.profile_id,a.profile_revision,t.created_at FROM tasks t JOIN team_execution_assignments a ON a.task_id=? WHERE t.id LIKE 'history-task-%'`,
		`INSERT INTO task_attempts(id,task_id,attempt_number,status,metadata_json) SELECT id,id,1,'waiting','{}' FROM tasks WHERE id LIKE 'history-task-%'`,
		`INSERT INTO agent_worker_runs(id,workspace_id,task_id,attempt_id,worker_principal_id,status,role_name,capability_id,protocol_level,max_steps,max_replans,max_escalations,route_policy_json,continuation_json,started_at,updated_at) SELECT t.id,r.workspace_id,t.id,t.id,r.worker_principal_id,'waiting',r.role_name,r.capability_id,r.protocol_level,r.max_steps,r.max_replans,r.max_escalations,r.route_policy_json,'{}',t.created_at,t.updated_at FROM tasks t JOIN agent_worker_runs r ON r.task_id=? WHERE t.id LIKE 'history-task-%'`,
		`INSERT INTO agent_worker_steps(id,run_id,step_number,step_kind,status,proposal_type,detail_json,started_at,completed_at) SELECT id,id,1,'human','waiting','human','{}',started_at,updated_at FROM agent_worker_runs WHERE id LIKE 'history-task-%'`,
		`INSERT INTO team_messages(id,workspace_id,session_id,author_member_id,author_principal_id,message_kind,content_json,round_number,created_at) SELECT t.id,r.workspace_id,r.session_id,r.member_id,r.author_principal_id,'system','{}',1,t.created_at FROM tasks t JOIN team_assistance_requests r ON r.task_id=? WHERE t.id LIKE 'history-task-%'`,
		`INSERT INTO team_assistance_requests(id,workspace_id,session_id,plan_id,assignment_key,member_id,task_id,attempt_id,run_id,wait_step_id,notice_message_id,author_principal_id,question,created_at) SELECT t.id,r.workspace_id,r.session_id,r.plan_id,t.id,r.member_id,t.id,t.id,t.id,t.id,t.id,r.author_principal_id,'history question',t.created_at FROM tasks t JOIN team_assistance_requests r ON r.task_id=? WHERE t.id LIKE 'history-task-%'`,
	} {
		var err error
		if strings.Contains(statement, "?") {
			_, err = w.db.ExecContext(ctx, statement, run.TaskID)
		} else {
			_, err = w.db.ExecContext(ctx, statement)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, analyze := range []bool{false, true} {
		if analyze {
			if _, err := w.db.ExecContext(ctx, `ANALYZE`); err != nil {
				t.Fatal(err)
			}
		}
		rows, err := w.db.QueryContext(ctx, `EXPLAIN QUERY PLAN `+teamAssistanceBindingSQL, stepID, run.ID, run.TaskID, run.WorkspaceID)
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
		joined := strings.Join(details, "\n")
		if strings.Contains(joined, "SCAN ") || strings.Contains(joined, "AUTOMATIC") || strings.Contains(joined, "TEMP B-TREE") {
			t.Fatalf("unbounded binding plan after analyze=%v:\n%s", analyze, joined)
		}
		for _, alias := range []string{"a", "s", "m", "ep", "st"} {
			found := false
			for _, detail := range details {
				if strings.HasPrefix(detail, "SEARCH "+alias+" USING ") {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing persistent exact lookup for %s:\n%s", alias, joined)
			}
		}
		// These are bounded index/identity probes, not a recovery collector or
		// authority to inspect/resume the synthetic historical owners.
		for _, lookup := range []struct{ column, key string }{{"task_id", run.TaskID}, {"attempt_id", run.AttemptID}, {"run_id", run.ID}, {"wait_step_id", stepID}} {
			query := `SELECT id FROM team_assistance_requests WHERE ` + lookup.column + `=?`
			if lookup.column != "wait_step_id" {
				query += ` ORDER BY id`
			}
			query += ` LIMIT 2`
			planRows, err := w.db.QueryContext(ctx, `EXPLAIN QUERY PLAN `+query, lookup.key)
			if err != nil {
				t.Fatal(err)
			}
			detailText := ""
			for planRows.Next() {
				var id, parent, unused int
				var detail string
				if err = planRows.Scan(&id, &parent, &unused, &detail); err != nil {
					t.Fatal(err)
				}
				detailText += detail + "\n"
			}
			if err = planRows.Err(); err != nil {
				t.Fatal(err)
			}
			if err = planRows.Close(); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(detailText, "SEARCH team_assistance_requests USING ") || !strings.Contains(detailText, lookup.column+"=?") || strings.Contains(detailText, "SCAN ") || strings.Contains(detailText, "AUTOMATIC") || strings.Contains(detailText, "TEMP B-TREE") {
				t.Fatalf("unbounded %s reverse plan after analyze=%v: %s", lookup.column, analyze, detailText)
			}
			for _, key := range []string{lookup.key, "missing-owner"} {
				rows, err := w.db.QueryContext(ctx, query, key)
				if err != nil {
					t.Fatal(err)
				}
				ids := []string{}
				for rows.Next() {
					var id string
					if err = rows.Scan(&id); err != nil {
						t.Fatal(err)
					}
					ids = append(ids, id)
				}
				if err = rows.Err(); err != nil {
					t.Fatal(err)
				}
				if err = rows.Close(); err != nil {
					t.Fatal(err)
				}
				if key == "missing-owner" {
					if len(ids) != 0 {
						t.Fatal("foreign reverse request returned")
					}
				} else {
					var expected string
					if err = w.db.QueryRowContext(ctx, `SELECT id FROM team_assistance_requests WHERE wait_step_id=?`, stepID).Scan(&expected); err != nil {
						t.Fatal(err)
					}
					if len(ids) != 1 || ids[0] != expected {
						t.Fatalf("wrong %s owner: %v", lookup.column, ids)
					}
				}
			}
		}
	}
}

func TestTeamAssistanceModelCannotChooseOwnership(t *testing.T) {
	w, ctx, run, _, session, plan := prepareTeamProposalContinuation(t, "human")
	current, err := w.tasks.Get(ctx, run.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	response := agentprotocol.Response{Proposal: json.RawMessage(`{"question":"Please review <script>untrusted</script>","session_id":"foreign","plan_id":"foreign","member_id":"foreign","task_id":"foreign","attempt_id":"foreign","run_id":"foreign","author_principal_id":"proposal-human"}`)}
	if result := w.handleHuman(ctx, run, current, response, TickResult{RunID: run.ID, TaskID: run.TaskID}); result.Status != "waiting_human" {
		t.Fatalf("wait %+v", result)
	}
	var gotSession, gotPlan, taskID, attempt, worker, author, question string
	if err = w.db.QueryRowContext(ctx, `SELECT session_id,plan_id,task_id,attempt_id,run_id,author_principal_id,question FROM team_assistance_requests`).Scan(&gotSession, &gotPlan, &taskID, &attempt, &worker, &author, &question); err != nil {
		t.Fatal(err)
	}
	if gotSession != session || gotPlan != plan || taskID != run.TaskID || attempt != run.AttemptID || worker != run.ID || author != WorkerPrincipal || question != "Please review <script>untrusted</script>" {
		t.Fatal("model changed ownership or question was treated as authority")
	}
}

func TestTeamAssistanceWritesRollbackWithOriginalWait(t *testing.T) {
	for _, failure := range []string{"request", "notice", "scope", "audit"} {
		t.Run(failure, func(t *testing.T) {
			w, ctx, run, _, _, _ := prepareTeamProposalContinuation(t, "human")
			statement := `CREATE TRIGGER reject_help BEFORE INSERT ON team_assistance_requests BEGIN SELECT RAISE(ABORT,'request rejected'); END`
			if failure == "notice" {
				statement = `CREATE TRIGGER reject_help BEFORE INSERT ON team_messages WHEN json_extract(NEW.content_json,'$.assistance_request_id') IS NOT NULL BEGIN SELECT RAISE(ABORT,'notice rejected'); END`
			}
			if failure == "audit" {
				statement = `CREATE TRIGGER reject_help BEFORE INSERT ON events WHEN NEW.event_type='team.assistance_requested' BEGIN SELECT RAISE(ABORT,'help audit rejected'); END`
			}
			if failure == "scope" {
				statement = `CREATE TRIGGER reject_help BEFORE INSERT ON team_assistance_request_scopes BEGIN SELECT RAISE(ABORT,'scope rejected'); END`
			}
			if _, err := w.db.ExecContext(ctx, statement); err != nil {
				t.Fatal(err)
			}
			before, err := w.getRun(ctx, run.ID)
			if err != nil {
				t.Fatal(err)
			}
			beforeTask, err := w.tasks.Get(ctx, run.TaskID)
			if err != nil {
				t.Fatal(err)
			}
			var beforeEvents, beforeMessages int
			if err = w.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM events),(SELECT COUNT(*) FROM team_messages)`).Scan(&beforeEvents, &beforeMessages); err != nil {
				t.Fatal(err)
			}
			result := applyWorkerWait(t, w, ctx, run, "human")
			after, err := w.getRun(ctx, run.ID)
			if err != nil {
				t.Fatal(err)
			}
			afterTask, err := w.tasks.Get(ctx, run.TaskID)
			if err != nil {
				t.Fatal(err)
			}
			var requests, scopes, events, messages, steps int
			var attempt string
			if err = w.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM team_assistance_requests),(SELECT COUNT(*) FROM team_assistance_request_scopes),(SELECT COUNT(*) FROM events),(SELECT COUNT(*) FROM team_messages),(SELECT COUNT(*) FROM agent_worker_steps WHERE run_id=? AND step_kind='human'),status FROM task_attempts WHERE id=?`, run.ID, run.AttemptID).Scan(&requests, &scopes, &events, &messages, &steps, &attempt); err != nil {
				t.Fatal(err)
			}
			if result.Status != "failed" || !w.dispatchReconciliationFailed || !reflect.DeepEqual(before, after) || !reflect.DeepEqual(beforeTask, afterTask) || requests != 0 || scopes != 0 || steps != 0 || events != beforeEvents || messages != beforeMessages || attempt != "running" {
				t.Fatalf("partial %s: result=%+v requests=%d steps=%d events=%d->%d messages=%d->%d attempt=%s", failure, result, requests, steps, beforeEvents, events, beforeMessages, messages, attempt)
			}
		})
	}
}

func TestTeamAssistanceQuestionBytesAndOrdinaryWait(t *testing.T) {
	for _, kind := range []string{"team_human", "ordinary_human"} {
		t.Run(kind, func(t *testing.T) {
			w, ctx, run, _, _, _ := prepareTeamProposalContinuation(t, "human")
			if kind == "ordinary_human" {
				w, ctx, run = prepareProposalContinuation(t, "human")
			}
			current, err := w.tasks.Get(ctx, run.TaskID)
			if err != nil {
				t.Fatal(err)
			}
			question := strings.Repeat("a", 511) + "界"
			raw, _ := json.Marshal(map[string]string{"question": question, "reason": "manual hold"})
			response := agentprotocol.Response{Proposal: raw}
			result := w.handleHuman(ctx, run, current, response, TickResult{RunID: run.ID, TaskID: run.TaskID})
			if result.Status != "waiting_human" {
				t.Fatalf("bounded wait: %+v", result)
			}
			var count int
			if err = w.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM team_assistance_requests`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if kind == "ordinary_human" {
				if count != 0 {
					t.Fatal("ordinary human became Team request")
				}
				return
			}
			var got string
			if err = w.db.QueryRowContext(ctx, `SELECT question FROM team_assistance_requests`).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if !utf8.ValidString(got) || len(got) > 512 || got != strings.Repeat("a", 511) {
				t.Fatalf("invalid bounded UTF8 %d %q", len(got), got)
			}
		})
	}
	w, ctx, run, _, _, _ := prepareTeamProposalContinuation(t, "wait")
	if result := applyWorkerWait(t, w, ctx, run, "wait"); result.Status != "paused" {
		t.Fatalf("ordinary Team wait: %+v", result)
	}
	var count int
	if err := w.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM team_assistance_requests`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("wait generated help: %d %v", count, err)
	}
}

func TestTeamAssistanceChangedAuthorityLeavesNoRequest(t *testing.T) {
	for _, change := range []string{"inactive_member", "human_member", "root_criteria", "child_criteria", "tenant", "stale_worker", "empty_question"} {
		t.Run(change, func(t *testing.T) {
			w, ctx, run, _, session, _ := prepareTeamProposalContinuation(t, "human")
			sql := ""
			switch change {
			case "inactive_member":
				sql = `UPDATE team_members SET status='disabled' WHERE id=(SELECT member_id FROM team_execution_assignments WHERE task_id='` + run.TaskID + `')`
			case "human_member":
				sql = `UPDATE team_members SET member_kind='human' WHERE id=(SELECT member_id FROM team_execution_assignments WHERE task_id='` + run.TaskID + `')`
			case "root_criteria":
				sql = fmt.Sprintf(`UPDATE tasks SET completion_json='{"changed_criteria":true}' WHERE id=(SELECT task_id FROM team_sessions WHERE id='%s')`, session)
			case "child_criteria":
				sql = fmt.Sprintf(`UPDATE tasks SET completion_json='{}' WHERE id='%s'`, run.TaskID)
			case "tenant":
				sql = `UPDATE workspaces SET status='suspended' WHERE id='tenant'`
			case "stale_worker":
				run.Revision--
			}
			if sql != "" {
				if _, err := w.db.ExecContext(ctx, sql); err != nil {
					t.Fatal(err)
				}
			}
			before, err := w.getRun(ctx, run.ID)
			if err != nil {
				t.Fatal(err)
			}
			var result TickResult
			if change == "empty_question" {
				current, err := w.tasks.Get(ctx, run.TaskID)
				if err != nil {
					t.Fatal(err)
				}
				result = w.handleHuman(ctx, run, current, agentprotocol.Response{Proposal: json.RawMessage(`{"question":" "}`)}, TickResult{RunID: run.ID, TaskID: run.TaskID})
			} else {
				result = applyWorkerWait(t, w, ctx, run, "human")
			}
			after, err := w.getRun(ctx, run.ID)
			if err != nil {
				t.Fatal(err)
			}
			var count int
			if err = w.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM team_assistance_requests`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if result.Status != "failed" || count != 0 || !reflect.DeepEqual(before, after) {
				t.Fatalf("changed authority request: %+v count=%d", result, count)
			}
		})
	}
}
