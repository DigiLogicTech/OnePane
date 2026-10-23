package team

import (
	"context"
	"database/sql"
	"errors"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

func cancellationMissing(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}

func teamCancellationRootPristine(ctx context.Context, tx storage.Tx, id string) (bool, error) {
	for _, table := range []string{"task_attempts", "agent_worker_runs", "operations", "tool_invocations", "inference_sessions", "agent_runtime_invocations", "capability_leases", "resource_leases", "checkpoints", "verifications", "recovery_snapshots", "continuation_capsules", "assurance_runs", "workspace_file_publications", "node_power_holds", "node_wake_attempts", "edit_proposals", "project_change_proposals"} {
		var present bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM `+table+` WHERE task_id=?)`, id).Scan(&present); err != nil {
			return false, err
		}
		if present {
			return false, nil
		}
	}
	var dirty bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM global_resource_leases WHERE json_extract(owner_json,'$.task_id')=?) OR EXISTS(SELECT 1 FROM events WHERE aggregate_type='task' AND aggregate_id=? AND event_type IN ('task.started','task.attempt_interrupted','task.completed'))`, id, id).Scan(&dirty)
	return !dirty, err
}

func (s *Service) cancellationReasoning(ctx context.Context, tx storage.Tx, root, session, ws, manifestSHA string) (bool, error) {
	// Bound full histories as well as active work. Oversized evidence defers;
	// partial evidence is never sufficient for terminal cancellation.
	var turnCount, requestCount int
	const ownedRequests = `task_id=? OR json_extract(context_manifest_json,'$.session_id')=? OR json_extract(context_manifest_json,'$.turn_id') IN (SELECT id FROM team_turn_requests WHERE session_id=?)`
	for _, probe := range []struct {
		query string
		id    string
		out   *int
	}{
		{`SELECT COUNT(*) FROM (SELECT id FROM team_turn_requests WHERE session_id=? LIMIT 129)`, session, &turnCount},
	} {
		if err := tx.QueryRowContext(ctx, probe.query, probe.id).Scan(probe.out); err != nil {
			return false, err
		}
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM (SELECT id FROM inference_requests WHERE `+ownedRequests+` LIMIT 129)`, root, session, session).Scan(&requestCount); err != nil {
		return false, err
	}
	if turnCount > 128 || requestCount > 128 {
		return false, nil
	}
	var bad bool
	// External harnesses may have performed effects even when their requested
	// role was reasoning-only. Follow session/turn attribution as well as Task.
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM agent_runtime_invocations
 WHERE task_id=? OR json_extract(request_json,'$.context_manifest.session_id')=? OR
 json_extract(request_json,'$.context_manifest.turn_id') IN (SELECT id FROM team_turn_requests WHERE session_id=?))`, root, session, session).Scan(&bad)
	if err != nil || bad {
		return false, err
	}
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM team_turn_requests WHERE session_id=? AND (workspace_id<>? OR status NOT IN ('pending','succeeded') OR (status='pending' AND (selected_candidate_id IS NOT NULL OR selected_candidate_kind IS NOT NULL OR budget_reservation_id IS NOT NULL OR response_message_id IS NOT NULL OR completed_at IS NOT NULL OR attempt_count<>0)) OR (status='succeeded' AND (completed_at IS NULL OR response_message_id IS NULL OR selected_candidate_kind<>'model_deployment' OR selected_candidate_id IS NULL))))`, session, ws).Scan(&bad)
	if err != nil || bad {
		return false, err
	}
	// Exactly one successful persisted request owns each completed turn, and
	// every request must have that owner. Session-only caller metadata is never
	// enough: require worker, capability, manifest, candidate, message, artifact,
	// historical local route and completion ledger evidence together.
	rows, err := tx.QueryContext(ctx, `SELECT id FROM inference_requests WHERE `+ownedRequests+` LIMIT 129`, root, session, session)
	if err != nil {
		return false, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, id)
	}
	if err == nil {
		err = rows.Err()
	}
	closeErr := rows.Close()
	if err != nil {
		return false, err
	}
	if closeErr != nil {
		return false, closeErr
	}
	for _, id := range ids {
		var matched bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM inference_requests q
   JOIN model_deployments d ON d.id=q.deployment_id AND d.provider_connection_id IS NULL AND d.node_id=q.execution_node_id
   JOIN harness_nodes n ON n.id=q.origin_node_id AND n.local=1 AND n.trust_state='local'
   JOIN team_turn_requests tr ON tr.id=json_extract(q.context_manifest_json,'$.turn_id') AND tr.session_id=? AND tr.workspace_id=? AND tr.status='succeeded'
    AND tr.member_id=json_extract(q.context_manifest_json,'$.member_id') AND tr.round_number=json_extract(q.context_manifest_json,'$.round')
    AND tr.selected_candidate_kind='model_deployment' AND tr.selected_candidate_id=q.deployment_id
   JOIN team_members member ON member.id=tr.member_id AND member.workspace_id=tr.workspace_id AND member.member_kind!='human'
   JOIN team_messages msg ON msg.id=tr.response_message_id AND msg.session_id=tr.session_id AND msg.workspace_id=tr.workspace_id AND msg.author_member_id=tr.member_id AND msg.author_principal_id='system:team-worker' AND msg.message_kind='agent' AND msg.round_number=tr.round_number
   JOIN artifacts a ON a.id=q.response_artifact_id AND a.workspace_id=q.workspace_id AND a.status='active' AND a.created_by='system:team-worker' AND a.media_type='application/json' AND length(a.content_hash)=71 AND substr(a.content_hash,1,7)='sha256:' AND substr(a.content_hash,8) NOT GLOB '*[^0-9a-f]*'
   WHERE q.id=? AND q.task_id=? AND q.workspace_id=? AND q.session_id IS NULL AND q.principal_id='system:team-worker'
    AND q.status='succeeded' AND q.completed_at IS NOT NULL AND q.error_code IS NULL AND q.provider_connection_id IS NULL
    AND q.origin_node_id IS NOT NULL AND q.execution_node_id=q.origin_node_id
    AND json_extract(q.capability_json,'$.capability_id')='team.deliberate'
    AND json_extract(q.classification_json,'$.metadata.team_mode')=1 AND json_extract(q.classification_json,'$.metadata.deliberation_only')=1
    AND json_extract(q.context_manifest_json,'$.session_id')=? AND json_extract(q.context_manifest_json,'$.session_manifest_sha256')=?
    AND json_extract(q.context_manifest_json,'$.candidate_kind')='model_deployment' AND json_extract(q.context_manifest_json,'$.candidate_id')=q.deployment_id
    AND json_extract(msg.content_json,'$.candidate_id')=q.deployment_id AND json_extract(msg.content_json,'$.candidate_kind')='model_deployment'
    AND json_extract(a.metadata_json,'$.inference_request_id')=q.id AND json_extract(a.metadata_json,'$.deployment_id')=q.deployment_id AND json_extract(a.metadata_json,'$.model_id')=d.model_id
    AND json_type(a.metadata_json,'$.provider_connection_id')='null' AND json_extract(a.metadata_json,'$.transport') IN ('llamacpp','colibri')
    AND EXISTS(SELECT 1 FROM events WHERE workspace_id=q.workspace_id AND aggregate_type='inference_request' AND aggregate_id=q.id AND event_type='inference_request.routed' AND json_extract(payload_json,'$.inference_request_id')=q.id AND json_extract(payload_json,'$.transport')=json_extract(a.metadata_json,'$.transport') AND json_extract(payload_json,'$.to')='routed')
    AND EXISTS(SELECT 1 FROM events WHERE workspace_id=q.workspace_id AND aggregate_type='inference_request' AND aggregate_id=q.id AND event_type='inference_request.succeeded' AND json_extract(payload_json,'$.inference_request_id')=q.id AND json_extract(payload_json,'$.response_artifact_id')=a.id AND json_extract(payload_json,'$.to')='succeeded')
    AND (SELECT COUNT(*) FROM inference_requests other WHERE json_extract(other.context_manifest_json,'$.turn_id')=tr.id)=1
  )`, session, ws, id, root, ws, session, manifestSHA).Scan(&matched)
		if err != nil || !matched {
			return false, err
		}
	}
	var succeeded int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM team_turn_requests WHERE session_id=? AND status='succeeded'`, session).Scan(&succeeded); err != nil {
		return false, err
	}
	if succeeded != requestCount {
		return false, nil
	}
	// Validate both directions: a turn can retain a reservation whose Task or
	// request linkage is absent or foreign. A root-only scan cannot see it.
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM team_turn_requests tr
 LEFT JOIN budget_reservations r ON r.id=tr.budget_reservation_id
 LEFT JOIN inference_requests q ON q.id=r.inference_request_id
 WHERE tr.session_id=? AND tr.budget_reservation_id IS NOT NULL AND
 (r.id IS NULL OR r.task_id IS NOT ? OR q.task_id IS NOT ? OR
 json_extract(q.context_manifest_json,'$.turn_id') IS NOT tr.id OR
 r.status NOT IN ('committed','released') OR r.closed_at IS NULL))`, session, root, root).Scan(&bad)
	if err != nil || bad {
		return false, err
	}
	// Retained queue/accounting history is accepted only when every row has
	// a positively matched request and a recorded closed outcome. No cleanup,
	// budget refund or external cancellation is fabricated here.
	for _, table := range []string{"runtime_execution_queue", "budget_reservations"} {
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM (SELECT id FROM `+table+` WHERE task_id=? OR inference_request_id IN (SELECT id FROM inference_requests WHERE task_id=?) LIMIT 129)`, root, root).Scan(&count); err != nil {
			return false, err
		}
		if count > 128 {
			return false, nil
		}
	}
	for _, query := range []string{
		`SELECT EXISTS(SELECT 1 FROM runtime_execution_queue r LEFT JOIN inference_requests q ON q.id=r.inference_request_id AND q.task_id=r.task_id WHERE (r.task_id=? OR r.inference_request_id IN (SELECT id FROM inference_requests WHERE task_id=?)) AND (q.id IS NULL OR r.workspace_id IS NOT q.workspace_id OR r.deployment_id IS NOT q.deployment_id OR r.node_id IS NOT q.execution_node_id OR r.state IS NOT 'completed' OR r.safe_boundary IS NOT 'complete' OR r.completed_at IS NULL OR r.failure_reason IS NOT NULL))`,
		`SELECT EXISTS(SELECT 1 FROM budget_reservations r LEFT JOIN inference_requests q ON q.id=r.inference_request_id AND q.task_id=r.task_id LEFT JOIN budget_accounts b ON b.id=r.budget_account_id LEFT JOIN team_turn_requests tr ON tr.id=json_extract(q.context_manifest_json,'$.turn_id') WHERE (r.task_id=? OR r.inference_request_id IN (SELECT id FROM inference_requests WHERE task_id=?)) AND (q.id IS NULL OR b.workspace_id IS NOT q.workspace_id OR tr.budget_reservation_id IS NOT r.id OR r.agent_runtime_invocation_id IS NOT NULL OR r.status NOT IN ('committed','released') OR r.closed_at IS NULL))`,
	} {
		if err = tx.QueryRowContext(ctx, query, root, root).Scan(&bad); err != nil || bad {
			return false, err
		}
	}
	return true, nil
}
