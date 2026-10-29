package team

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/DigiLogicTech/OnePane/internal/storage"
	"strings"
)

func cancellationGraphIdentity(ctx context.Context, tx storage.Tx, root, session, plan, ws string, snapshot SessionSnapshot, graph []string) (bool, error) {
	var body, priorJSON string
	if err := tx.QueryRowContext(ctx, `SELECT p.plan_json,a.prior_dependencies_json FROM team_plans p JOIN team_assignment_roots a ON a.plan_id=p.id AND a.session_id=p.session_id WHERE p.id=? AND p.session_id=? AND a.task_id=?`, plan, session, root).Scan(&body, &priorJSON); err != nil {
		return false, err
	}
	specs, err := assignmentSpecs(json.RawMessage(body))
	if err != nil || len(specs) == 0 {
		return false, nil
	}
	if len(graph) != len(specs)+1 {
		return false, nil
	}
	graphSet := map[string]bool{}
	for _, id := range graph {
		graphSet[id] = true
	}
	members := map[string]SnapshotMember{}
	for _, member := range snapshot.Members {
		members[member.ID] = member
	}
	ids := map[string]string{}
	for _, spec := range specs {
		var id, member, objective, completion, hash, deps, profile string
		var revision int64
		err = tx.QueryRowContext(ctx, `SELECT a.task_id,a.member_id,t.objective,t.completion_json,a.completion_sha256,a.depends_on_json,a.profile_id,a.profile_revision FROM team_execution_assignments a JOIN tasks t ON t.id=a.task_id WHERE a.session_id=? AND a.plan_id=? AND a.assignment_key=? AND a.workspace_id=? AND a.parent_task_id=? AND t.parent_task_id=a.parent_task_id AND t.archived_at IS NULL`, session, plan, spec.Key, ws, root).Scan(&id, &member, &objective, &completion, &hash, &deps, &profile, &revision)
		if err != nil {
			return false, cancellationMissing(err)
		}
		frozen, ok := members[member]
		if !ok || frozen.MemberKind != "agent" || frozen.Status != "active" || member != spec.MemberID || objective != strings.TrimSpace(spec.Objective) || !graphSet[id] {
			return false, nil
		}
		expectedProfile, expectedRevision := profileIDFromMemberConfig(frozen.Config), int64(0)
		if frozen.Profile != nil {
			expectedProfile = frozen.Profile.ID
			expectedRevision = frozen.Profile.Revision
		}
		if profile != expectedProfile || revision != expectedRevision {
			return false, nil
		}
		sum := sha256.Sum256([]byte(completion))
		if hex.EncodeToString(sum[:]) != hash {
			return false, nil
		}
		var declared []string
		if json.Unmarshal([]byte(deps), &declared) != nil || len(declared) != len(spec.DependsOn) {
			return false, nil
		}
		for i, key := range declared {
			if key != spec.DependsOn[i] {
				return false, nil
			}
		}
		ids[spec.Key] = id
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM team_execution_assignments WHERE session_id=?`, session).Scan(&count); err != nil {
		return false, err
	}
	if count != len(specs) {
		return false, nil
	}
	var prior []string
	if json.Unmarshal([]byte(priorJSON), &prior) != nil || len(prior) > 32 {
		return false, nil
	}
	rootDeps := map[string]bool{}
	for _, id := range prior {
		if id == "" || rootDeps[id] {
			return false, nil
		}
		rootDeps[id] = true
	}
	for _, id := range ids {
		if rootDeps[id] {
			return false, nil
		}
		rootDeps[id] = true
	}
	if ok, e := cancellationDependencies(ctx, tx, root, ws, rootDeps); e != nil || !ok {
		return false, e
	}
	for _, spec := range specs {
		expected := map[string]bool{}
		for _, id := range prior {
			expected[id] = true
		}
		for _, key := range spec.DependsOn {
			expected[ids[key]] = true
		}
		if ok, e := cancellationDependencies(ctx, tx, ids[spec.Key], ws, expected); e != nil || !ok {
			return false, e
		}
	}
	return true, nil
}

func cancellationDependencies(ctx context.Context, tx storage.Tx, id, ws string, expected map[string]bool) (bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT d.depends_on_task_id,d.dependency_type,d.dependency_mode,t.workspace_id FROM task_dependencies d LEFT JOIN tasks t ON t.id=d.depends_on_task_id WHERE d.task_id=? LIMIT 41`, id)
	if err != nil {
		return false, err
	}
	seen := map[string]bool{}
	valid := true
	for rows.Next() {
		var dep, kind, mode, tenant string
		if err = rows.Scan(&dep, &kind, &mode, &tenant); err != nil {
			break
		}
		if !expected[dep] || seen[dep] || kind != "hard" || mode != "all" || tenant != ws {
			valid = false
		}
		seen[dep] = true
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
	return valid && len(seen) == len(expected), nil
}
