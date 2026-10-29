package recoveryinventory

import (
	"context"
	"database/sql"
	"errors"
)

type provenanceQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// HasKnownTaskEffectProvenance is a negative-only cancellation guard. A match
// means the Task must be deferred for reconciliation, including when direct
// Task ownership is missing or contradicts the persisted reverse provenance.
// False is NOT an inventory, proof of no effects, or permission to settle: all
// existing history/control/ownership guards still apply. Unsupported model
// encodings cannot be cleared by these reviewed candidate lookups.
//
// These exact expressions have unfiltered leading indexes in migrations0104
// and0105. Neither status nor tenant filters may hide historical/foreign rows.
// Evidence payloads are never returned or changed, and no adapter is invoked.
func HasKnownTaskEffectProvenance(ctx context.Context, db provenanceQueryer, taskID string) (bool, error) {
	if taskID == "" {
		return false, errors.New("missing provenance Task identity")
	}
	for _, query := range []string{
		`SELECT EXISTS(SELECT 1 FROM agent_runtime_invocations WHERE json_extract(request_json,'$.task_id')=?)`,
		`SELECT EXISTS(SELECT 1 FROM artifacts WHERE json_extract(metadata_json,'$.task_id')=?)`,
		`SELECT EXISTS(SELECT 1 FROM project_library_asset_versions WHERE json_extract(provenance_json,'$.task_id')=?)`,
		`SELECT EXISTS(SELECT 1 FROM inference_requests WHERE
 CASE WHEN json_extract(request_json,'$.messages[1].role')='user'
 AND json_type(request_json,'$.messages[1].content')='text'
 THEN CASE WHEN json_valid(json_extract(request_json,'$.messages[1].content'))
 THEN CASE WHEN json_type(json_extract(request_json,'$.messages[1].content'))='object'
 AND json_extract(json_extract(request_json,'$.messages[1].content'),'$.protocol_version')='v1'
 THEN CASE WHEN json_type(json_extract(request_json,'$.messages[1].content'),'$.task_id')='text'
 THEN json_extract(json_extract(request_json,'$.messages[1].content'),'$.task_id')
 END END END END=?)`,
	} {
		var present bool
		if err := db.QueryRowContext(ctx, query, taskID).Scan(&present); err != nil {
			return false, err
		}
		if present {
			return true, nil
		}
	}
	return false, nil
}
