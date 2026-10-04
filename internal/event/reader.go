package event

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

type Reader struct{ db *sql.DB }

func NewReader(db *sql.DB) *Reader { return &Reader{db: db} }

func (r *Reader) After(ctx context.Context, workspaceID string, after int64, limit int) ([]Stored, error) {
	if workspaceID == "" || after < 0 {
		return nil, fmt.Errorf("workspace and non-negative sequence required")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `SELECT sequence,id,workspace_id,event_type,aggregate_type,aggregate_id,actor_principal_id,request_id,trace_id,payload_json,occurred_at FROM events WHERE workspace_id=? AND sequence>? ORDER BY sequence ASC LIMIT ?`, workspaceID, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Stored, 0)
	for rows.Next() {
		var s Stored
		var wid, actor, req, trace sql.NullString
		var payload string
		if err := rows.Scan(&s.Sequence, &s.ID, &wid, &s.Type, &s.AggregateType, &s.AggregateID, &actor, &req, &trace, &payload, &s.OccurredAt); err != nil {
			return nil, err
		}
		if wid.Valid {
			v := wid.String
			s.WorkspaceID = &v
		}
		if actor.Valid {
			v := actor.String
			s.ActorPrincipalID = &v
		}
		if req.Valid {
			v := req.String
			s.RequestID = &v
		}
		if trace.Valid {
			v := trace.String
			s.TraceID = &v
		}
		s.Payload = json.RawMessage(payload)
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *Reader) Recent(ctx context.Context, workspaceID string, limit int) ([]Stored, error) {
	if workspaceID == "" {
		return nil, fmt.Errorf("workspace required")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, `SELECT sequence,id,workspace_id,event_type,aggregate_type,aggregate_id,actor_principal_id,request_id,trace_id,payload_json,occurred_at FROM events WHERE workspace_id=? ORDER BY sequence DESC LIMIT ?`, workspaceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Stored, 0)
	for rows.Next() {
		var st Stored
		var wid, actor, req, trace sql.NullString
		var payload string
		if err := rows.Scan(&st.Sequence, &st.ID, &wid, &st.Type, &st.AggregateType, &st.AggregateID, &actor, &req, &trace, &payload, &st.OccurredAt); err != nil {
			return nil, err
		}
		if wid.Valid {
			v := wid.String
			st.WorkspaceID = &v
		}
		if actor.Valid {
			v := actor.String
			st.ActorPrincipalID = &v
		}
		if req.Valid {
			v := req.String
			st.RequestID = &v
		}
		if trace.Valid {
			v := trace.String
			st.TraceID = &v
		}
		st.Payload = json.RawMessage(payload)
		out = append(out, st)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}
