package projectworkspace

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

const LibraryDocumentIndexLimit = 8 << 20
const LibraryDocumentMIME = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
const LibraryDocumentParserID = "docx-wordml-paragraphs-v2"
const LibraryDocumentCoverage = "stored paragraph text across declared Word stories; no layout/pagination/field evaluation/images/embedded documents"

var ErrLibraryDocumentIndex = errors.New("Library document index requires reconciliation")
var ErrLibraryDocumentNotIndexed = errors.New("Library document version has no ready document index")

type IndexLibraryDocumentCommand struct {
	Selection LibraryTextSelection
	Selected  LibraryVersion
	Content   []byte
}
type LibraryDocumentPart struct {
	PartName       string `json:"part_name"`
	SourceSHA      string `json:"source_sha"`
	ParagraphCount int    `json:"paragraph_count"`
}
type LibraryDocumentIndex struct {
	AssetID          string                `json:"asset_id"`
	Version          int64                 `json:"version"`
	ParserID         string                `json:"parser_id"`
	RawSourceSHA     string                `json:"raw_source_sha"`
	RawSourceSize    int64                 `json:"raw_source_size"`
	DerivedSHA       string                `json:"derived_sha"`
	DerivedUTF8Bytes int                   `json:"derived_utf8_bytes"`
	Status           string                `json:"status"`
	Coverage         string                `json:"coverage"`
	Parts            []LibraryDocumentPart `json:"parts"`
	UnitCount        int                   `json:"unit_count"`
	CreatedAt        int64                 `json:"created_at,omitempty"`
	manifestSHA      string
}
type LibraryDocumentUnit struct {
	Ordinal         int    `json:"ordinal"`
	PartName        string `json:"part_name"`
	ParagraphNumber int    `json:"paragraph_number"`
	Text            string `json:"text"`
	TextHash        string `json:"text_hash"`
}
type LibraryDocumentPage struct {
	Index     LibraryDocumentIndex  `json:"index"`
	Query     string                `json:"query"`
	Units     []LibraryDocumentUnit `json:"units"`
	HasMore   bool                  `json:"has_more"`
	NextAfter int                   `json:"next_after"`
}

func documentCanonical(units []LibraryDocumentUnit) (string, int) {
	var b bytes.Buffer
	size := 0
	for _, u := range units {
		b.WriteString(u.PartName)
		b.WriteByte(0)
		b.WriteString(strconv.Itoa(u.ParagraphNumber))
		b.WriteByte(0)
		b.WriteString(u.Text)
		b.WriteByte(0)
		size += len(u.Text)
	}
	return libraryTextDigest(b.Bytes()), size
}

// A separate bounded manifest seal binds part-byte receipts and coverage to the
// immutable source and unit digest without changing the unit citation encoding.
func documentManifest(idx LibraryDocumentIndex) string {
	idx.CreatedAt = 0
	b, _ := json.Marshal(idx)
	return libraryTextDigest(b)
}
func documentBase(v LibraryVersion) (LibraryDocumentIndex, error) {
	hash, ok := libraryTextCanonicalHash(v.ContentHash)
	if !ok || v.SizeBytes < 0 {
		return LibraryDocumentIndex{}, ErrLibraryDocumentIndex
	}
	return LibraryDocumentIndex{AssetID: v.AssetID, Version: v.Version, ParserID: LibraryDocumentParserID, RawSourceSHA: hash, RawSourceSize: v.SizeBytes, Status: "not_indexed", Parts: []LibraryDocumentPart{}}, nil
}
func parseLibraryDocument(v LibraryVersion, body []byte) (LibraryDocumentIndex, []LibraryDocumentUnit, error) {
	idx, err := documentBase(v)
	if err != nil {
		return idx, nil, err
	}
	if v.MIMEType != LibraryDocumentMIME {
		if body != nil {
			return LibraryDocumentIndex{}, nil, ErrInvalidCommand
		}
		idx.Status = "unsupported"
		idx.manifestSHA = documentManifest(idx)
		return idx, nil, nil
	}
	if v.SizeBytes > LibraryDocumentIndexLimit {
		if body != nil {
			return LibraryDocumentIndex{}, nil, ErrInvalidCommand
		}
		idx.Status = "too_large"
		idx.manifestSHA = documentManifest(idx)
		return idx, nil, nil
	}
	if int64(len(body)) != v.SizeBytes || libraryTextDigest(body) != idx.RawSourceSHA {
		return LibraryDocumentIndex{}, nil, ErrLibraryDocumentIndex
	}
	parts, units, status := parseDOCX(body)
	idx.Status = status
	if status == "ready" {
		idx.Parts = parts
		idx.UnitCount = len(units)
		idx.Coverage = LibraryDocumentCoverage
		idx.DerivedSHA, idx.DerivedUTF8Bytes = documentCanonical(units)
	}
	idx.manifestSHA = documentManifest(idx)
	return idx, units, nil
}

// VerifyLibraryDocumentContent independently rederives the retained extraction
// receipt from already verified original source bytes. It grants no access.
func VerifyLibraryDocumentContent(idx LibraryDocumentIndex, body []byte) error {
	if idx.Status == "not_indexed" {
		return nil
	}
	if idx.RawSourceSize > LibraryDocumentIndexLimit || len(body) > LibraryDocumentIndexLimit {
		return ErrLibraryDocumentIndex
	}
	parsed, _, err := parseLibraryDocument(LibraryVersion{AssetID: idx.AssetID, Version: idx.Version, ContentHash: idx.RawSourceSHA, SizeBytes: idx.RawSourceSize, MIMEType: LibraryDocumentMIME}, body)
	if err != nil {
		return err
	}
	parsed.CreatedAt = idx.CreatedAt
	// Public receipts do not expose the internal persisted manifest seal.
	parsed.manifestSHA = ""
	idx.manifestSHA = ""
	if !reflect.DeepEqual(parsed, idx) {
		return ErrLibraryDocumentIndex
	}
	return nil
}

func readLibraryDocumentTx(ctx context.Context, tx storage.Tx, v LibraryVersion) (LibraryDocumentIndex, []LibraryDocumentUnit, error) {
	idx, err := documentBase(v)
	if err != nil {
		return idx, nil, err
	}
	var parts sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT CASE WHEN length(raw_source_sha)=71 THEN raw_source_sha ELSE '' END,raw_source_size,CASE WHEN length(derived_sha)<=71 THEN derived_sha ELSE '' END,derived_utf8_bytes,CASE WHEN length(status)<=16 THEN status ELSE '' END,CASE WHEN length(coverage)<=256 THEN coverage ELSE '' END,CASE WHEN length(CAST(parts_json AS BLOB))<=32768 THEN parts_json ELSE NULL END,unit_count,created_at,CASE WHEN length(manifest_sha)=71 THEN manifest_sha ELSE '' END FROM project_library_document_indexes WHERE asset_id=? AND version=? AND parser_id=?`, v.AssetID, v.Version, LibraryDocumentParserID).Scan(&idx.RawSourceSHA, &idx.RawSourceSize, &idx.DerivedSHA, &idx.DerivedUTF8Bytes, &idx.Status, &idx.Coverage, &parts, &idx.UnitCount, &idx.CreatedAt, &idx.manifestSHA)
	if errors.Is(err, sql.ErrNoRows) {
		var orphan bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM project_library_document_units WHERE asset_id=? AND version=? AND parser_id=?)`, v.AssetID, v.Version, LibraryDocumentParserID).Scan(&orphan)
		if orphan {
			return idx, nil, ErrLibraryDocumentIndex
		}
		return idx, nil, err
	}
	if err != nil {
		return idx, nil, err
	}
	raw, _ := libraryTextCanonicalHash(v.ContentHash)
	if idx.RawSourceSHA != raw || idx.RawSourceSize != v.SizeBytes || idx.CreatedAt <= 0 || idx.UnitCount < 0 || idx.UnitCount > 8192 || idx.DerivedUTF8Bytes < 0 || idx.DerivedUTF8Bytes > 1<<20 || !parts.Valid || json.Unmarshal([]byte(parts.String), &idx.Parts) != nil || idx.Parts == nil || len(idx.Parts) > 64 {
		return idx, nil, ErrLibraryDocumentIndex
	}
	canonicalParts, _ := json.Marshal(idx.Parts)
	if string(canonicalParts) != parts.String {
		return idx, nil, ErrLibraryDocumentIndex
	}
	if idx.manifestSHA != documentManifest(idx) {
		return idx, nil, ErrLibraryDocumentIndex
	}
	switch idx.Status {
	case "ready":
		if v.MIMEType != LibraryDocumentMIME || v.SizeBytes > LibraryDocumentIndexLimit || idx.Coverage != LibraryDocumentCoverage || len(idx.Parts) == 0 {
			return idx, nil, ErrLibraryDocumentIndex
		}
	case "unsupported":
	case "too_large":
		if v.MIMEType != LibraryDocumentMIME {
			return idx, nil, ErrLibraryDocumentIndex
		}
	case "invalid_document":
		if v.MIMEType != LibraryDocumentMIME || v.SizeBytes > LibraryDocumentIndexLimit {
			return idx, nil, ErrLibraryDocumentIndex
		}
	default:
		return idx, nil, ErrLibraryDocumentIndex
	}
	if idx.Status != "ready" && (idx.UnitCount != 0 || idx.DerivedSHA != "" || idx.DerivedUTF8Bytes != 0 || idx.Coverage != "" || len(idx.Parts) != 0) {
		return idx, nil, ErrLibraryDocumentIndex
	}
	rows, err := tx.QueryContext(ctx, `SELECT ordinal,CASE WHEN length(CAST(part_name AS BLOB))<=256 THEN part_name ELSE '' END,paragraph_number,CASE WHEN length(CAST(content AS BLOB))<=16384 THEN content ELSE NULL END,CASE WHEN length(text_hash)=71 THEN text_hash ELSE '' END FROM project_library_document_units WHERE asset_id=? AND version=? AND parser_id=? ORDER BY ordinal LIMIT 8193`, v.AssetID, v.Version, LibraryDocumentParserID)
	if err != nil {
		return idx, nil, err
	}
	defer rows.Close()
	units := []LibraryDocumentUnit{}
	total := 0
	for rows.Next() {
		var u LibraryDocumentUnit
		var text sql.NullString
		if err = rows.Scan(&u.Ordinal, &u.PartName, &u.ParagraphNumber, &text, &u.TextHash); err != nil {
			return idx, nil, err
		}
		if !text.Valid || idx.Status != "ready" || len(units) >= idx.UnitCount || u.Ordinal != len(units)+1 || !utf8.ValidString(text.String) || strings.ContainsRune(text.String, 0) || libraryTextDigest([]byte(text.String)) != u.TextHash {
			return idx, nil, ErrLibraryDocumentIndex
		}
		u.Text = text.String
		total += len(u.Text)
		if total > 1<<20 {
			return idx, nil, ErrLibraryDocumentIndex
		}
		units = append(units, u)
	}
	if err = rows.Err(); err != nil {
		return idx, nil, err
	}
	if len(units) != idx.UnitCount {
		return idx, nil, ErrLibraryDocumentIndex
	}
	if idx.Status == "ready" {
		pos := 0
		previous := ""
		for _, p := range idx.Parts {
			if !validDOCXPath(p.PartName) || !strings.HasPrefix(p.PartName, "word/") || p.PartName <= previous || p.ParagraphCount < 0 || p.ParagraphCount > 8192 {
				return idx, nil, ErrLibraryDocumentIndex
			}
			if _, ok := libraryTextCanonicalHash(p.SourceSHA); !ok {
				return idx, nil, ErrLibraryDocumentIndex
			}
			previous = p.PartName
			for n := 1; n <= p.ParagraphCount; n++ {
				if pos >= len(units) || units[pos].PartName != p.PartName || units[pos].ParagraphNumber != n {
					return idx, nil, ErrLibraryDocumentIndex
				}
				pos++
			}
		}
		hash, size := documentCanonical(units)
		if pos != len(units) || hash != idx.DerivedSHA || size != idx.DerivedUTF8Bytes {
			return idx, nil, ErrLibraryDocumentIndex
		}
	}
	return idx, units, nil
}

func (s *Service) IndexWorkspaceLibraryDocument(ctx context.Context, c IndexLibraryDocumentCommand) (LibraryDocumentIndex, error) {
	if len(c.Content) > LibraryDocumentIndexLimit {
		return LibraryDocumentIndex{}, ErrInvalidCommand
	}
	idx, units, err := parseLibraryDocument(c.Selected, c.Content)
	if err != nil {
		return idx, err
	}
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		v, err := s.libraryTextVersionTx(ctx, tx, c.Selection)
		if err != nil {
			return err
		}
		if v != c.Selected {
			return ErrLibraryDocumentIndex
		}
		prior, _, err := readLibraryDocumentTx(ctx, tx, v)
		if err != nil {
			return err
		}
		if prior.Status != "not_indexed" {
			copy := idx
			copy.CreatedAt = prior.CreatedAt
			if !reflect.DeepEqual(copy, prior) {
				return ErrLibraryDocumentIndex
			}
			idx = prior
			return nil
		}
		idx.CreatedAt = s.clock.UnixMilli()
		parts, _ := json.Marshal(idx.Parts)
		_, err = tx.ExecContext(ctx, `INSERT INTO project_library_document_indexes(asset_id,version,parser_id,raw_source_sha,raw_source_size,derived_sha,manifest_sha,derived_utf8_bytes,status,coverage,parts_json,unit_count,created_by,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, idx.AssetID, idx.Version, idx.ParserID, idx.RawSourceSHA, idx.RawSourceSize, idx.DerivedSHA, idx.manifestSHA, idx.DerivedUTF8Bytes, idx.Status, idx.Coverage, string(parts), idx.UnitCount, c.Selection.ActorPrincipalID, idx.CreatedAt)
		if err != nil {
			return err
		}
		for _, u := range units {
			if _, err = tx.ExecContext(ctx, `INSERT INTO project_library_document_units(asset_id,version,parser_id,ordinal,part_name,paragraph_number,content,text_hash) VALUES(?,?,?,?,?,?,?,?)`, idx.AssetID, idx.Version, idx.ParserID, u.Ordinal, u.PartName, u.ParagraphNumber, u.Text, u.TextHash); err != nil {
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
		payload, _ := json.Marshal(map[string]any{"asset_id": idx.AssetID, "version": idx.Version, "parser_id": idx.ParserID, "raw_source_sha": idx.RawSourceSHA, "derived_sha": idx.DerivedSHA, "status": idx.Status, "unit_count": idx.UnitCount, "project_workspace_id": c.Selection.WorkspaceID})
		return s.events.Append(ctx, tx, event.Event{ID: id, WorkspaceID: &p.WorkspaceID, Type: "project.library_document_indexed", AggregateType: "library_asset", AggregateID: idx.AssetID, ActorPrincipalID: &c.Selection.ActorPrincipalID, Payload: payload, OccurredAt: idx.CreatedAt})
	})
	if err != nil {
		return LibraryDocumentIndex{}, err
	}
	return idx, nil
}
func (s *Service) WorkspaceLibraryDocumentIndex(ctx context.Context, c LibraryTextSelection) (LibraryDocumentIndex, error) {
	var out LibraryDocumentIndex
	err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		v, err := s.libraryTextVersionTx(ctx, tx, c)
		if err != nil {
			return err
		}
		out, _, err = readLibraryDocumentTx(ctx, tx, v)
		return err
	})
	if err != nil {
		return LibraryDocumentIndex{}, err
	}
	return out, nil
}
func (s *Service) ReadWorkspaceLibraryDocument(ctx context.Context, c LibraryTextSelection, query string, after int) (LibraryDocumentPage, error) {
	if len(query) > 256 || !utf8.ValidString(query) || strings.ContainsRune(query, 0) || after < 0 {
		return LibraryDocumentPage{}, ErrInvalidCommand
	}
	out := LibraryDocumentPage{Query: query, Units: []LibraryDocumentUnit{}, NextAfter: after}
	err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		v, err := s.libraryTextVersionTx(ctx, tx, c)
		if err != nil {
			return err
		}
		var units []LibraryDocumentUnit
		out.Index, units, err = readLibraryDocumentTx(ctx, tx, v)
		if err != nil {
			return err
		}
		if out.Index.Status != "ready" {
			return fmt.Errorf("%w: %s", ErrLibraryDocumentNotIndexed, out.Index.Status)
		}
		if after > out.Index.UnitCount {
			return ErrInvalidCommand
		}
		for _, u := range units {
			if u.Ordinal > after && strings.Contains(u.Text, query) {
				if len(out.Units) == 8 {
					out.HasMore = true
					break
				}
				out.Units = append(out.Units, u)
				out.NextAfter = u.Ordinal
			}
		}
		return nil
	})
	if err != nil {
		return LibraryDocumentPage{}, err
	}
	return out, nil
}
