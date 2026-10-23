package team

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
)

// MessagePage is a bounded chronological window. Cursor IDs are resolved only
// inside the requested session; a cursor is never an authority-bearing token.
type MessagePage struct {
	Messages []Message `json:"messages"`
	HasMore  bool      `json:"has_more"`
	FirstID  string    `json:"first_id,omitempty"`
	LastID   string    `json:"last_id,omitempty"`
}

type MessagePageOptions struct {
	BeforeID, AfterID string
	Limit             int
}

func (s *Service) PageMessages(ctx context.Context, sessionID string, o MessagePageOptions) (MessagePage, error) {
	out := MessagePage{Messages: []Message{}}
	if sessionID == "" || len(o.BeforeID) > 128 || len(o.AfterID) > 128 ||
		(o.BeforeID != "" && o.AfterID != "") || strings.TrimSpace(o.BeforeID) != o.BeforeID || strings.TrimSpace(o.AfterID) != o.AfterID {
		return out, ErrInvalid
	}
	if o.Limit == 0 {
		o.Limit = 30
	}
	if o.Limit < 1 || o.Limit > 50 {
		return out, ErrInvalid
	}
	if _, err := s.Session(ctx, sessionID); err != nil {
		return out, err
	}
	args := []any{sessionID}
	where := ""
	order := "DESC"
	cursor := o.BeforeID
	if o.AfterID != "" {
		cursor = o.AfterID
		order = "ASC"
	}
	if cursor != "" {
		var at int64
		if err := s.db.QueryRowContext(ctx, `SELECT message_sequence FROM team_messages WHERE session_id=? AND id=?`, sessionID, cursor).Scan(&at); err != nil {
			if err == sql.ErrNoRows {
				return out, ErrInvalid
			}
			return out, err
		}
		op := "<"
		if o.AfterID != "" {
			op = ">"
		}
		where = " AND message_sequence " + op + " ?"
		args = append(args, at)
	}
	args = append(args, o.Limit+1)
	// Historical model output may be very large. The live view has an explicit
	// per-message bound and reports omission rather than truncating invalid JSON.
	rows, err := s.db.QueryContext(ctx, `SELECT id,workspace_id,session_id,author_member_id,author_principal_id,message_kind,
	 CASE WHEN length(CAST(content_json AS BLOB))<=16384 THEN content_json ELSE '{"text":"Message exceeds live view size limit.","omitted":true}' END,
	 reply_to_message_id,round_number,created_at,message_sequence FROM team_messages WHERE session_id=?`+where+` ORDER BY message_sequence `+order+` LIMIT ?`, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var m Message
		var member, principal, reply sql.NullString
		var raw string
		if err = rows.Scan(&m.ID, &m.WorkspaceID, &m.SessionID, &member, &principal, &m.Kind, &raw, &reply, &m.RoundNumber, &m.CreatedAt, &m.Sequence); err != nil {
			return out, err
		}
		if member.Valid {
			v := member.String
			m.AuthorMemberID = &v
		}
		if principal.Valid {
			v := principal.String
			m.AuthorPrincipalID = &v
		}
		if reply.Valid {
			v := reply.String
			m.ReplyToMessageID = &v
		}
		m.Content = json.RawMessage(raw)
		out.Messages = append(out.Messages, m)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	if len(out.Messages) > o.Limit {
		out.HasMore = true
		out.Messages = out.Messages[:o.Limit]
	}
	if order == "DESC" {
		for i, j := 0, len(out.Messages)-1; i < j; i, j = i+1, j-1 {
			out.Messages[i], out.Messages[j] = out.Messages[j], out.Messages[i]
		}
	}
	if len(out.Messages) > 0 {
		out.FirstID = out.Messages[0].ID
		out.LastID = out.Messages[len(out.Messages)-1].ID
	}
	return out, nil
}
