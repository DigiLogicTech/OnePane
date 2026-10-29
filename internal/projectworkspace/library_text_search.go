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
// AfterLine>0 resumes that exact version after the last emitted line; zero
// advances to the following version. AssetComplete advances past the asset.
type LibraryTextSearchPosition struct {
	AssetID       string `json:"-"`
	Version       int64  `json:"-"`
	AfterLine     int    `json:"-"`
	AssetComplete bool   `json:"-"`
}

type LibraryWorkspaceTextMatch struct {
	Index LibraryTextIndex `json:"index"`
	Line  LibraryTextLine  `json:"line"`
}

type LibraryWorkspaceTextSearch struct {
	Query           string                      `json:"query"`
	Matches         []LibraryWorkspaceTextMatch `json:"matches"`
	HasMore         bool                        `json:"has_more"`
	ScannedVersions int                         `json:"-"`
	ScannedAssets   int                         `json:"-"`
	NextPosition    LibraryTextSearchPosition   `json:"-"`
}

// The Project/archived/identity index is deliberately independent of mutable
// updated_at and global history. Each page inspects at most eight raw assets
// and eight raw indexed versions; an empty page may still have more work.
const libraryWorkspaceTextAssetsSQL = `SELECT CASE WHEN length(CAST(id AS BLOB))<=256 THEN id ELSE NULL END
 FROM project_library_assets WHERE project_id=? AND archived=0 AND id>=? ORDER BY id LIMIT 8`
const libraryWorkspaceTextAssetsAfterSQL = `SELECT CASE WHEN length(CAST(id AS BLOB))<=256 THEN id ELSE NULL END
 FROM project_library_assets WHERE project_id=? AND archived=0 AND id>? ORDER BY id LIMIT 8`
const libraryWorkspaceTextVersionsSQL = `SELECT version FROM project_library_text_indexes
 WHERE asset_id=? AND version>? AND parser_version=1 ORDER BY version LIMIT ?`
const libraryWorkspaceTextVersionsResumeSQL = `SELECT version FROM project_library_text_indexes
 WHERE asset_id=? AND version>=? AND parser_version=1 ORDER BY version LIMIT ?`

func libraryTextSearchPositionValid(p LibraryTextSearchPosition) bool {
	if p.AssetID == "" {
		return p.Version == 0 && p.AfterLine == 0 && !p.AssetComplete
	}
	if len(p.AssetID) > 256 || !utf8.ValidString(p.AssetID) || strings.TrimSpace(p.AssetID) == "" || p.Version < 0 || p.AfterLine < 0 || p.AfterLine > libraryTextLineCount {
		return false
	}
	return p.AfterLine == 0 || p.Version > 0 && !p.AssetComplete
}

// SearchWorkspaceIndexedLibraryText performs current, explicitly paged keyword
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
// No more than eight ready 1MiB versions are materialized in a page; citations
// retain their original version/source/line hashes. Search grants no execution.
func (s *Service) SearchWorkspaceIndexedLibraryText(ctx context.Context, c LibraryTextSelection, query string, position LibraryTextSearchPosition) (LibraryWorkspaceTextSearch, error) {
	if len(query) == 0 || len(query) > 256 || !utf8.ValidString(query) || strings.TrimSpace(query) == "" || strings.ContainsRune(query, 0) || !libraryTextSearchPositionValid(position) {
		return LibraryWorkspaceTextSearch{}, ErrInvalidCommand
	}
	for _, id := range []string{c.ProjectID, c.WorkspaceID, c.ActorPrincipalID} {
		if len(id) == 0 || len(id) > 256 || !utf8.ValidString(id) || strings.TrimSpace(id) == "" {
			return LibraryWorkspaceTextSearch{}, ErrInvalidCommand
		}
	}
	out := LibraryWorkspaceTextSearch{Query: query, Matches: []LibraryWorkspaceTextMatch{}}
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
		assetSQL := libraryWorkspaceTextAssetsSQL
		if position.AssetComplete {
			assetSQL = libraryWorkspaceTextAssetsAfterSQL
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
			remaining := 8 - out.ScannedVersions
			after := int64(0)
			afterLine := 0
			if asset == position.AssetID && !position.AssetComplete {
				after = position.Version
				afterLine = position.AfterLine
			}
			versionSQL := libraryWorkspaceTextVersionsSQL
			if afterLine > 0 {
				versionSQL = libraryWorkspaceTextVersionsResumeSQL
			}
			versions, err := libraryTextCandidateVersions(ctx, tx, versionSQL, asset, after, remaining)
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
					out.NextPosition = LibraryTextSearchPosition{AssetID: asset, Version: version}
					continue
				}
				if err != nil {
					return err
				}
				idx, lines, err := readLibraryTextIndexTx(ctx, tx, selected)
				if err != nil {
					return err
				}
				if idx.Status == "not_indexed" {
					return ErrLibraryTextIndex
				}
				resume := 0
				if version == after {
					resume = afterLine
				}
				if resume > idx.LineCount {
					return ErrInvalidCommand
				}
				if idx.Status == "ready" {
					for _, line := range lines {
						if line.LineNumber <= resume || !strings.Contains(line.Text, query) {
							continue
						}
						if len(out.Matches) == 8 {
							out.HasMore = true
							return nil
						}
						out.Matches = append(out.Matches, LibraryWorkspaceTextMatch{Index: idx, Line: line})
						out.NextPosition = LibraryTextSearchPosition{AssetID: asset, Version: version, AfterLine: line.LineNumber}
					}
				}
				out.NextPosition = LibraryTextSearchPosition{AssetID: asset, Version: version}
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
			out.NextPosition = LibraryTextSearchPosition{AssetID: asset, AssetComplete: true}
		}
		out.HasMore = len(assets) == 8
		if !out.HasMore {
			out.NextPosition = LibraryTextSearchPosition{}
		}
		return nil
	})
	if err != nil {
		return LibraryWorkspaceTextSearch{}, err
	}
	return out, nil
}

func libraryTextCandidateVersions(ctx context.Context, tx storage.Tx, query, asset string, after int64, limit int) ([]int64, error) {
	if limit < 1 || limit > 8 {
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
			return nil, ErrLibraryTextIndex
		}
		out = append(out, version)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
