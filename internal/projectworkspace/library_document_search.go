package projectworkspace

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/DigiLogicTech/OnePane/internal/storage"
)

// A position is private navigation state, never read or execution authority.
// API callers retain it behind an opaque bounded, scope/query/credential-bound
// cursor. It must not be serialized to Workspace clients: raw asset/version
// keys can include candidates for which that Workspace has no content grant.
// AfterParagraph>0 resumes that exact version after the last emitted unit; zero
// advances to the following version. AssetComplete advances past the asset.
type LibraryDocumentSearchPosition struct {
	AssetID        string `json:"-"`
	Version        int64  `json:"-"`
	AfterParagraph int    `json:"-"`
	AssetComplete  bool   `json:"-"`
}

type LibraryWorkspaceDocumentMatch struct {
	Index LibraryDocumentIndex `json:"index"`
	Unit  LibraryDocumentUnit  `json:"unit"`
}

// Private verification work includes authorized nonmatching candidates too:
// self-consistent forged derived data must not silently hide a real match.
type LibraryDocumentSearchCandidate struct {
	Version LibraryVersion
	Index   LibraryDocumentIndex
}

type LibraryWorkspaceDocumentSearch struct {
	Query           string                           `json:"query"`
	Matches         []LibraryWorkspaceDocumentMatch  `json:"matches"`
	HasMore         bool                             `json:"has_more"`
	ScannedVersions int                              `json:"-"`
	ScannedAssets   int                              `json:"-"`
	NextPosition    LibraryDocumentSearchPosition    `json:"-"`
	Candidates      []LibraryDocumentSearchCandidate `json:"-"`
}

// The Project/archived/identity index is deliberately independent of mutable
// updated_at and global history. Each page inspects at most eight raw assets
// and two raw indexed versions; an empty page may still have more work.
const libraryWorkspaceDocumentAssetsSQL = `SELECT CASE WHEN length(CAST(id AS BLOB))<=256 THEN id ELSE NULL END
 FROM project_library_assets WHERE project_id=? AND archived=0 AND id>=? ORDER BY id LIMIT 8`
const libraryWorkspaceDocumentAssetsAfterSQL = `SELECT CASE WHEN length(CAST(id AS BLOB))<=256 THEN id ELSE NULL END
 FROM project_library_assets WHERE project_id=? AND archived=0 AND id>? ORDER BY id LIMIT 8`
const libraryWorkspaceDocumentVersionsSQL = `SELECT version FROM project_library_document_indexes INDEXED BY library_document_indexes_by_parser_version
 WHERE asset_id=? AND parser_id='` + LibraryDocumentParserID + `' AND version>? ORDER BY version LIMIT ?`
const libraryWorkspaceDocumentVersionsResumeSQL = `SELECT version FROM project_library_document_indexes INDEXED BY library_document_indexes_by_parser_version
 WHERE asset_id=? AND parser_id='` + LibraryDocumentParserID + `' AND version>=? ORDER BY version LIMIT ?`

func libraryDocumentSearchPositionValid(p LibraryDocumentSearchPosition) bool {
	if p.AssetID == "" {
		return p.Version == 0 && p.AfterParagraph == 0 && !p.AssetComplete
	}
	if len(p.AssetID) > 256 || !utf8.ValidString(p.AssetID) || strings.TrimSpace(p.AssetID) == "" || p.Version < 0 || p.AfterParagraph < 0 || p.AfterParagraph > 8192 {
		return false
	}
	return p.AfterParagraph == 0 || p.Version > 0 && !p.AssetComplete
}

// SearchWorkspaceIndexedLibraryDocuments performs current, explicitly paged keyword
// discovery in one canonical Workspace. Outer selection uses ProjectID,
// WorkspaceID, ActorPrincipalID and Authorize; AssetID/Version are deliberately
// unused until independently authorized candidates supply them. There is no
// Project-wide content privilege and no latest-N history filter.
//
// Every page rechecks current authority. Index/source hashes remain immutable,
// but this is live keyset pagination, not a cross-request snapshot: new keys
// before the cursor are visible on a fresh search. HasMore is conservative;
// only exhaustion with HasMore=false establishes that the current forward scan
// has ended. Eight denied or unindexed assets can produce an empty partial page.
// No more than two ready 1MiB derived versions are materialized in a page.
// API verification reparses up to two 8MiB original sources, including authorized
// candidates with no matches. Citations retain source/derived/part/paragraph
// identity; raw ZIP byte offsets are never paragraph coordinates.
func (s *Service) SearchWorkspaceIndexedLibraryDocuments(ctx context.Context, c LibraryTextSelection, query string, position LibraryDocumentSearchPosition) (LibraryWorkspaceDocumentSearch, error) {
	if len(query) == 0 || len(query) > 256 || !utf8.ValidString(query) || strings.TrimSpace(query) == "" || strings.ContainsRune(query, 0) || !libraryDocumentSearchPositionValid(position) {
		return LibraryWorkspaceDocumentSearch{}, ErrInvalidCommand
	}
	for _, id := range []string{c.ProjectID, c.WorkspaceID, c.ActorPrincipalID} {
		if len(id) == 0 || len(id) > 256 || !utf8.ValidString(id) || strings.TrimSpace(id) == "" {
			return LibraryWorkspaceDocumentSearch{}, ErrInvalidCommand
		}
	}
	out := LibraryWorkspaceDocumentSearch{Query: query, Matches: []LibraryWorkspaceDocumentMatch{}}
	err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		p, err := s.repo.ProjectTx(ctx, tx, c.ProjectID)
		if err != nil {
			return err
		}
		if p.Status != "active" {
			return ErrProjectInactive
		}
		if err = s.requireActor(ctx, tx, p.WorkspaceID, c.ActorPrincipalID); err != nil {
			return err
		}
		if c.Authorize != nil {
			if err = c.Authorize(ctx, tx); err != nil {
				return err
			}
		}
		var canonical bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM project_workspaces WHERE id=? AND project_id=? AND status='active')`, c.WorkspaceID, c.ProjectID).Scan(&canonical); err != nil {
			return err
		}
		if !canonical {
			return ErrCrossWorkspace
		}
		assetSQL := libraryWorkspaceDocumentAssetsSQL
		if position.AssetComplete {
			assetSQL = libraryWorkspaceDocumentAssetsAfterSQL
		}
		rows, err := tx.QueryContext(ctx, assetSQL, c.ProjectID, position.AssetID)
		if err != nil {
			return err
		}
		assets := make([]string, 0, 8)
		for rows.Next() {
			var id sql.NullString
			if err = rows.Scan(&id); err != nil {
				break
			}
			if !id.Valid || !utf8.ValidString(id.String) || strings.TrimSpace(id.String) == "" {
				err = ErrLibraryMetadataLimit
				break
			}
			assets = append(assets, id.String)
		}
		if err == nil {
			err = rows.Err()
		}
		closeErr := rows.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
		out.ScannedAssets = len(assets)
		for _, asset := range assets {
			remaining := 2 - out.ScannedVersions
			after := int64(0)
			afterParagraph := 0
			if asset == position.AssetID && !position.AssetComplete {
				after = position.Version
				afterParagraph = position.AfterParagraph
			}
			versionSQL := libraryWorkspaceDocumentVersionsSQL
			if afterParagraph > 0 {
				versionSQL = libraryWorkspaceDocumentVersionsResumeSQL
			}
			versions, err := libraryDocumentCandidateVersions(ctx, tx, versionSQL, asset, after, remaining)
			if err != nil {
				return err
			}
			for _, version := range versions {
				out.ScannedVersions++
				scoped := c
				scoped.AssetID = asset
				scoped.Version = version
				selected, err := s.libraryTextVersionTx(ctx, tx, scoped)
				if errors.Is(err, ErrCrossWorkspace) {
					out.NextPosition = LibraryDocumentSearchPosition{AssetID: asset, Version: version}
					continue
				}
				if err != nil {
					return err
				}
				idx, units, err := readLibraryDocumentTx(ctx, tx, selected)
				if err != nil {
					return err
				}
				if idx.Status == "not_indexed" {
					return ErrLibraryDocumentIndex
				}
				out.Candidates = append(out.Candidates, LibraryDocumentSearchCandidate{Version: selected, Index: idx})
				resume := 0
				if version == after {
					resume = afterParagraph
				}
				if resume > idx.UnitCount {
					return ErrInvalidCommand
				}
				if idx.Status == "ready" {
					for _, unit := range units {
						if unit.Ordinal <= resume || !strings.Contains(unit.Text, query) {
							continue
						}
						if len(out.Matches) == 8 {
							out.HasMore = true
							return nil
						}
						out.Matches = append(out.Matches, LibraryWorkspaceDocumentMatch{Index: idx, Unit: unit})
						out.NextPosition = LibraryDocumentSearchPosition{AssetID: asset, Version: version, AfterParagraph: unit.Ordinal}
					}
				}
				out.NextPosition = LibraryDocumentSearchPosition{AssetID: asset, Version: version}
				if len(out.Matches) == 8 {
					out.HasMore = true
					return nil
				}
			}
			if len(versions) == remaining {
				out.HasMore = true
				return nil
			}
			// Every indexed version of this asset is exhausted. Unindexed assets must
			// advance the private key too; otherwise an empty page can never progress.
			out.NextPosition = LibraryDocumentSearchPosition{AssetID: asset, AssetComplete: true}
		}
		out.HasMore = len(assets) == 8
		if !out.HasMore {
			out.NextPosition = LibraryDocumentSearchPosition{}
		}
		return nil
	})
	if err != nil {
		return LibraryWorkspaceDocumentSearch{}, err
	}
	return out, nil
}

func libraryDocumentCandidateVersions(ctx context.Context, tx storage.Tx, query, asset string, after int64, limit int) ([]int64, error) {
	if limit < 1 || limit > 2 {
		return nil, ErrInvalidCommand
	}
	rows, err := tx.QueryContext(ctx, query, asset, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]int64, 0, limit)
	for rows.Next() {
		var version int64
		if err = rows.Scan(&version); err != nil {
			return nil, err
		}
		if version < 1 {
			return nil, ErrLibraryDocumentIndex
		}
		out = append(out, version)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
