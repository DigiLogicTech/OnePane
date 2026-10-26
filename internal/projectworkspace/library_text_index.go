package projectworkspace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"strings"
	"unicode/utf8"

	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

const LibraryTextIndexLimit = 1 << 20
const libraryTextLineLimit = 16384
const libraryTextLineCount = 16384
const LibraryTextParserVersion = 1

var ErrLibraryTextIndex = errors.New("Library text index requires reconciliation")
var ErrLibraryTextNotIndexed = errors.New("Library text version has no ready text index")

type LibraryTextSelection struct {
	ProjectID, WorkspaceID, AssetID, ActorPrincipalID string
	Version                                           int64
	// Optional domain collaborator. HTTP callers supply original credential and
	// policy verification inside this transaction, including identical retries.
	Authorize func(context.Context, storage.Tx) error
}

type IndexLibraryTextCommand struct {
	Selection LibraryTextSelection
	Selected  LibraryVersion
	Content   []byte
}

type LibraryTextIndex struct {
	AssetID       string `json:"asset_id"`
	Version       int64  `json:"version"`
	ParserVersion int    `json:"parser_version"`
	ContentHash   string `json:"content_hash"`
	SizeBytes     int64  `json:"size_bytes"`
	Status        string `json:"status"`
	LineCount     int    `json:"line_count"`
	CreatedAt     int64  `json:"created_at,omitempty"`
}

// Byte offsets are zero-based, end-exclusive offsets into the original bytes.
// LF is the separator and is excluded; any preceding CR is retained verbatim.
// Empty files have no lines; a final LF creates no phantom empty line.
type LibraryTextLine struct {
	LineNumber int    `json:"line_number"`
	ByteStart  int64  `json:"byte_start"`
	ByteEnd    int64  `json:"byte_end"`
	Text       string `json:"text"`
	LineHash   string `json:"line_hash"`
}

type LibraryTextSearch struct {
	Index   LibraryTextIndex  `json:"index"`
	Query   string            `json:"query"`
	Lines   []LibraryTextLine `json:"lines"`
	HasMore bool              `json:"has_more"`
}

func LibraryTextMIME(value string) bool {
	typ, params, err := mime.ParseMediaType(strings.TrimSpace(value))
	if err != nil {
		return false
	}
	if charset := strings.ToLower(params["charset"]); charset != "" && charset != "utf-8" && charset != "us-ascii" {
		return false
	}
	if strings.HasPrefix(strings.ToLower(typ), "text/") {
		return true
	}
	switch strings.ToLower(typ) {
	case "application/json", "application/xml", "application/javascript", "application/x-yaml", "application/yaml", "application/toml":
		return true
	}
	return false
}

func libraryTextDigest(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}
func libraryTextCanonicalHash(value string) (string, bool) {
	digest := strings.TrimPrefix(value, "sha256:")
	if len(digest) != 64 {
		return "", false
	}
	for _, c := range digest {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return "", false
		}
	}
	return "sha256:" + digest, true
}

func libraryTextParse(selected LibraryVersion, body []byte) (LibraryTextIndex, []LibraryTextLine, error) {
	hash, ok := libraryTextCanonicalHash(selected.ContentHash)
	if !ok || selected.SizeBytes < 0 {
		return LibraryTextIndex{}, nil, ErrLibraryTextIndex
	}
	idx := LibraryTextIndex{AssetID: selected.AssetID, Version: selected.Version, ParserVersion: LibraryTextParserVersion, ContentHash: hash, SizeBytes: selected.SizeBytes}
	if !LibraryTextMIME(selected.MIMEType) {
		idx.Status = "unsupported"
		return idx, nil, nil
	}
	if selected.SizeBytes > LibraryTextIndexLimit {
		idx.Status = "too_large"
		return idx, nil, nil
	}
	if int64(len(body)) != selected.SizeBytes || libraryTextDigest(body) != hash {
		return LibraryTextIndex{}, nil, ErrLibraryTextIndex
	}
	if !utf8.Valid(body) || bytes.IndexByte(body, 0) >= 0 {
		idx.Status = "invalid_text"
		return idx, nil, nil
	}
	lines := make([]LibraryTextLine, 0)
	for start := 0; start < len(body); {
		end := bytes.IndexByte(body[start:], '\n')
		if end < 0 {
			end = len(body)
		} else {
			end += start
		}
		if end-start > libraryTextLineLimit || len(lines) >= libraryTextLineCount {
			idx.Status = "too_large"
			return idx, nil, nil
		}
		line := body[start:end]
		lines = append(lines, LibraryTextLine{LineNumber: len(lines) + 1, ByteStart: int64(start), ByteEnd: int64(end), Text: string(line), LineHash: libraryTextDigest(line)})
		start = end + 1
	}
	idx.Status = "ready"
	idx.LineCount = len(lines)
	return idx, lines, nil
}

func (s *Service) libraryTextVersionTx(ctx context.Context, tx storage.Tx, c LibraryTextSelection) (LibraryVersion, error) {
	for _, id := range []string{c.ProjectID, c.WorkspaceID, c.AssetID, c.ActorPrincipalID} {
		if len(id) == 0 || len(id) > 256 || !utf8.ValidString(id) || strings.TrimSpace(id) == "" {
			return LibraryVersion{}, ErrInvalidCommand
		}
	}
	if c.Version < 1 {
		return LibraryVersion{}, ErrInvalidCommand
	}
	p, err := s.repo.ProjectTx(ctx, tx, c.ProjectID)
	if err != nil {
		return LibraryVersion{}, err
	}
	if p.Status != "active" {
		return LibraryVersion{}, ErrProjectInactive
	}
	if err = s.requireActor(ctx, tx, p.WorkspaceID, c.ActorPrincipalID); err != nil {
		return LibraryVersion{}, err
	}
	if c.Authorize != nil {
		if err = c.Authorize(ctx, tx); err != nil {
			return LibraryVersion{}, err
		}
	}
	var selected LibraryVersion
	var permission int
	err = tx.QueryRowContext(ctx, boundedWorkspaceLibraryVersionSQL, c.WorkspaceID, c.WorkspaceID, c.ProjectID, c.AssetID, c.Version, c.WorkspaceID, s.clock.UnixMilli()).Scan(&selected.AssetID, &selected.Version, &selected.ContentHash, &selected.SizeBytes, &selected.MIMEType, &selected.StorageURI, &selected.CreatedAt, &permission)
	if errors.Is(err, sql.ErrNoRows) {
		return LibraryVersion{}, ErrCrossWorkspace
	}
	if err != nil {
		return LibraryVersion{}, err
	}
	if permission == -1 {
		return LibraryVersion{}, ErrLibraryPublicationLimit
	}
	if permission == -2 {
		return LibraryVersion{}, ErrLibraryMetadataLimit
	}
	if permission != 1 {
		return LibraryVersion{}, ErrCrossWorkspace
	}
	if _, ok := libraryTextCanonicalHash(selected.ContentHash); !ok {
		return LibraryVersion{}, ErrLibraryTextIndex
	}
	return selected, nil
}

const libraryTextIndexReadSQL = `SELECT
 CASE WHEN length(CAST(content_hash AS BLOB))=71 THEN content_hash ELSE '' END,
 size_bytes,CASE WHEN length(CAST(status AS BLOB))<=16 THEN status ELSE '' END,
 line_count,created_at FROM project_library_text_indexes WHERE asset_id=? AND version=? AND parser_version=1`
const libraryTextLinesReadSQL = `SELECT line_number,byte_start,byte_end,
 CASE WHEN length(CAST(content AS BLOB))<=16384 THEN content ELSE NULL END,
 CASE WHEN length(CAST(line_hash AS BLOB))=71 THEN line_hash ELSE '' END
 FROM project_library_text_lines WHERE asset_id=? AND version=? AND parser_version=1 ORDER BY line_number LIMIT 16385`

func readLibraryTextIndexTx(ctx context.Context, tx storage.Tx, selected LibraryVersion) (LibraryTextIndex, []LibraryTextLine, error) {
	hash, _ := libraryTextCanonicalHash(selected.ContentHash)
	idx := LibraryTextIndex{AssetID: selected.AssetID, Version: selected.Version, ParserVersion: LibraryTextParserVersion, ContentHash: hash, SizeBytes: selected.SizeBytes, Status: "not_indexed"}
	err := tx.QueryRowContext(ctx, libraryTextIndexReadSQL, selected.AssetID, selected.Version).Scan(&idx.ContentHash, &idx.SizeBytes, &idx.Status, &idx.LineCount, &idx.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		var orphan bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM project_library_text_lines WHERE asset_id=? AND version=? AND parser_version=1)`, selected.AssetID, selected.Version).Scan(&orphan); err != nil {
			return LibraryTextIndex{}, nil, err
		}
		if orphan {
			return LibraryTextIndex{}, nil, ErrLibraryTextIndex
		}
		return idx, nil, nil
	}
	if err != nil {
		return LibraryTextIndex{}, nil, err
	}
	if idx.ContentHash != hash || idx.SizeBytes != selected.SizeBytes || idx.CreatedAt <= 0 || idx.LineCount < 0 || idx.LineCount > libraryTextLineCount {
		return LibraryTextIndex{}, nil, ErrLibraryTextIndex
	}
	switch idx.Status {
	case "ready":
		if idx.SizeBytes > LibraryTextIndexLimit || !LibraryTextMIME(selected.MIMEType) {
			return LibraryTextIndex{}, nil, ErrLibraryTextIndex
		}
	case "unsupported":
		if LibraryTextMIME(selected.MIMEType) || idx.LineCount != 0 {
			return LibraryTextIndex{}, nil, ErrLibraryTextIndex
		}
	case "too_large":
		if !LibraryTextMIME(selected.MIMEType) || idx.LineCount != 0 {
			return LibraryTextIndex{}, nil, ErrLibraryTextIndex
		}
	case "invalid_text":
		if !LibraryTextMIME(selected.MIMEType) || idx.SizeBytes > LibraryTextIndexLimit || idx.LineCount != 0 {
			return LibraryTextIndex{}, nil, ErrLibraryTextIndex
		}
	default:
		return LibraryTextIndex{}, nil, ErrLibraryTextIndex
	}
	rows, err := tx.QueryContext(ctx, libraryTextLinesReadSQL, selected.AssetID, selected.Version)
	if err != nil {
		return LibraryTextIndex{}, nil, err
	}
	defer rows.Close()
	lines := make([]LibraryTextLine, 0, idx.LineCount)
	original := make([]byte, 0)
	for rows.Next() {
		var line LibraryTextLine
		var content sql.NullString
		if err = rows.Scan(&line.LineNumber, &line.ByteStart, &line.ByteEnd, &content, &line.LineHash); err != nil {
			return LibraryTextIndex{}, nil, err
		}
		if !content.Valid || idx.Status != "ready" || len(lines) >= idx.LineCount || line.LineNumber != len(lines)+1 || line.ByteStart != int64(len(original)) || line.ByteEnd < line.ByteStart || line.ByteEnd-line.ByteStart != int64(len(content.String)) || line.ByteEnd > idx.SizeBytes || !utf8.ValidString(content.String) || strings.ContainsAny(content.String, "\x00\n") || libraryTextDigest([]byte(content.String)) != line.LineHash {
			return LibraryTextIndex{}, nil, ErrLibraryTextIndex
		}
		line.Text = content.String
		original = append(original, line.Text...)
		if line.ByteEnd < idx.SizeBytes {
			original = append(original, '\n')
		}
		if len(original) > LibraryTextIndexLimit {
			return LibraryTextIndex{}, nil, ErrLibraryTextIndex
		}
		lines = append(lines, line)
	}
	if err = rows.Err(); err != nil {
		return LibraryTextIndex{}, nil, err
	}
	if len(lines) != idx.LineCount || idx.Status == "ready" && (int64(len(original)) != idx.SizeBytes || libraryTextDigest(original) != idx.ContentHash) {
		return LibraryTextIndex{}, nil, ErrLibraryTextIndex
	}
	return idx, lines, nil
}

func (s *Service) IndexWorkspaceLibraryText(ctx context.Context, c IndexLibraryTextCommand) (LibraryTextIndex, error) {
	// Parse/hash before taking the SQLite writer; the exact version is compared
	// again inside it, so bytes cannot be rebound to changed metadata/grants.
	if len(c.Content) > LibraryTextIndexLimit {
		return LibraryTextIndex{}, ErrInvalidCommand
	}
	idx, lines, err := libraryTextParse(c.Selected, c.Content)
	if err != nil {
		return LibraryTextIndex{}, err
	}
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		selected, err := s.libraryTextVersionTx(ctx, tx, c.Selection)
		if err != nil {
			return err
		}
		if selected != c.Selected {
			return ErrLibraryTextIndex
		}
		prior, _, err := readLibraryTextIndexTx(ctx, tx, selected)
		if err != nil {
			return err
		}
		if prior.Status != "not_indexed" {
			copy := idx
			copy.CreatedAt = prior.CreatedAt
			if copy != prior {
				return ErrLibraryTextIndex
			}
			idx = prior
			return nil
		}
		idx.CreatedAt = s.clock.UnixMilli()
		_, err = tx.ExecContext(ctx, `INSERT INTO project_library_text_indexes(asset_id,version,parser_version,content_hash,size_bytes,status,line_count,created_by,created_at) VALUES(?,?,1,?,?,?,?,?,?)`, idx.AssetID, idx.Version, idx.ContentHash, idx.SizeBytes, idx.Status, idx.LineCount, c.Selection.ActorPrincipalID, idx.CreatedAt)
		if err != nil {
			return err
		}
		for _, line := range lines {
			if _, err = tx.ExecContext(ctx, `INSERT INTO project_library_text_lines(asset_id,version,parser_version,line_number,byte_start,byte_end,content,line_hash) VALUES(?,?,1,?,?,?,?,?)`, idx.AssetID, idx.Version, line.LineNumber, line.ByteStart, line.ByteEnd, line.Text, line.LineHash); err != nil {
				return err
			}
		}
		p, err := s.repo.ProjectTx(ctx, tx, c.Selection.ProjectID)
		if err != nil {
			return err
		}
		id, err := s.ids.New("evt")
		if err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"asset_id": idx.AssetID, "version": idx.Version, "parser_version": idx.ParserVersion, "content_hash": idx.ContentHash, "status": idx.Status, "line_count": idx.LineCount, "project_workspace_id": c.Selection.WorkspaceID})
		return s.events.Append(ctx, tx, event.Event{ID: id, WorkspaceID: &p.WorkspaceID, Type: "project.library_text_indexed", AggregateType: "library_asset", AggregateID: idx.AssetID, ActorPrincipalID: &c.Selection.ActorPrincipalID, Payload: payload, OccurredAt: idx.CreatedAt})
	})
	if err != nil {
		return LibraryTextIndex{}, err
	}
	return idx, nil
}

func (s *Service) WorkspaceLibraryTextIndex(ctx context.Context, c LibraryTextSelection) (LibraryTextIndex, error) {
	var out LibraryTextIndex
	err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		selected, err := s.libraryTextVersionTx(ctx, tx, c)
		if err != nil {
			return err
		}
		out, _, err = readLibraryTextIndexTx(ctx, tx, selected)
		return err
	})
	if err != nil {
		return LibraryTextIndex{}, err
	}
	return out, nil
}

func (s *Service) SearchWorkspaceLibraryText(ctx context.Context, c LibraryTextSelection, query string) (LibraryTextSearch, error) {
	if len(query) == 0 || len(query) > 256 || !utf8.ValidString(query) || strings.TrimSpace(query) == "" || strings.ContainsRune(query, 0) {
		return LibraryTextSearch{}, ErrInvalidCommand
	}
	out := LibraryTextSearch{Query: query, Lines: []LibraryTextLine{}}
	err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		selected, err := s.libraryTextVersionTx(ctx, tx, c)
		if err != nil {
			return err
		}
		var lines []LibraryTextLine
		out.Index, lines, err = readLibraryTextIndexTx(ctx, tx, selected)
		if err != nil {
			return err
		}
		if out.Index.Status != "ready" {
			return fmt.Errorf("%w: %s", ErrLibraryTextNotIndexed, out.Index.Status)
		}
		for _, line := range lines {
			if strings.Contains(line.Text, query) {
				if len(out.Lines) == 8 {
					out.HasMore = true
					break
				}
				out.Lines = append(out.Lines, line)
			}
		}
		return nil
	})
	if err != nil {
		return LibraryTextSearch{}, err
	}
	return out, nil
}
