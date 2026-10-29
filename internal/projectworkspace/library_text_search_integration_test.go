//go:build integration

package projectworkspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

func addWorkspaceTextIndex(t *testing.T, f *libraryTextFixture, id, body string, readable bool) {
	t.Helper()
	f.exec(`INSERT INTO project_library_assets(id,project_id,name,asset_type,current_version,created_at,updated_at) VALUES(?,'p',?,'text/plain',1,1,1)`, id, id)
	f.exec(`INSERT INTO project_library_asset_versions(asset_id,version,content_hash,size_bytes,mime_type,storage_uri,created_at) VALUES(?,1,?,?,'text/plain',?,1)`, id, libraryTextDigest([]byte(body)), len(body), "artifact:"+id)
	f.exec(`INSERT INTO workspace_library_grants(id,project_workspace_id,asset_id,enabled,version_policy,pinned_version,permissions_json,created_at,updated_at) VALUES(?,'target',?,1,'pinned',1,'{"read":true}',1,1)`, "grant-"+id, id)
	selection := f.selection
	selection.AssetID = id
	selected, err := f.service.ResolveBoundedWorkspaceLibraryVersion(context.Background(), "p", "target", id, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.IndexWorkspaceLibraryText(context.Background(), IndexLibraryTextCommand{Selection: selection, Selected: selected, Content: []byte(body)}); err != nil {
		t.Fatal(err)
	}
	if !readable {
		f.exec(`UPDATE workspace_library_grants SET enabled=0 WHERE asset_id=?`, id)
	}
}

func TestWorkspaceLibraryTextSearchBoundedEmptyPagesAndExactLineContinuation(t *testing.T) {
	f := newLibraryTextFixture(t)
	for i := 0; i < 8; i++ {
		id := fmt.Sprintf("a-unindexed-%02d", i)
		f.exec(`INSERT INTO project_library_assets(id,project_id,name,asset_type,current_version,created_at,updated_at) VALUES(?,'p',?,'text/plain',1,1,1)`, id, id)
	}
	for i := 0; i < 8; i++ {
		addWorkspaceTextIndex(t, f, fmt.Sprintf("b-denied-%02d", i), "PRIVATE DENIED needle", false)
	}
	addWorkspaceTextIndex(t, f, "z-readable", strings.Repeat("needle café <script>literal</script>\r\n", 10), true)
	ctx := context.Background()
	position := LibraryTextSearchPosition{}
	seen := map[int]bool{}
	empty := 0
	f.exec(`PRAGMA query_only=ON`)
	defer f.exec(`PRAGMA query_only=OFF`)
	for page := 0; page < 8; page++ {
		s := NewService(f.db.SQL(), f.db, clock.Real{})
		out, err := s.SearchWorkspaceIndexedLibraryText(ctx, f.selection, "needle", position)
		if err != nil {
			t.Fatal(err)
		}
		if out.ScannedAssets > 8 || out.ScannedVersions > 8 || len(out.Matches) > 8 {
			t.Fatalf("unbounded page %+v", out)
		}
		if len(out.Matches) == 0 && out.HasMore {
			empty++
		}
		for _, m := range out.Matches {
			if m.Index.AssetID != "z-readable" || seen[m.Line.LineNumber] || m.Line.LineHash != libraryTextDigest([]byte(m.Line.Text)) {
				t.Fatalf("unauthorised/duplicate citation %+v", m)
			}
			seen[m.Line.LineNumber] = true
		}
		serialized, _ := json.Marshal(out)
		if strings.Contains(string(serialized), "denied-") || strings.Contains(string(serialized), "Scanned") || strings.Contains(string(serialized), "scanned_") {
			t.Fatalf("private navigation exposed %s", serialized)
		}
		if !out.HasMore {
			if len(seen) != 10 || empty < 2 {
				t.Fatalf("lost late matches seen=%v empty=%d", seen, empty)
			}
			return
		}
		if position == out.NextPosition {
			t.Fatalf("empty page did not advance %+v", out)
		}
		position = out.NextPosition
	}
	t.Fatal("bounded forward scan never exhausted")
}

func TestWorkspaceLibraryTextSearchCurrentAuthorityAndMalformedRows(t *testing.T) {
	for _, scenario := range []string{"grant", "principal", "membership", "project", "workspace", "callback", "forged_line"} {
		t.Run(scenario, func(t *testing.T) {
			f := newLibraryTextFixture(t)
			addWorkspaceTextIndex(t, f, "readable", "needle protected", true)
			c := f.selection
			switch scenario {
			case "grant":
				f.exec(`UPDATE workspace_library_grants SET enabled=0 WHERE asset_id='readable'`)
			case "principal":
				f.exec(`UPDATE principals SET status='disabled' WHERE id='human'`)
			case "membership":
				f.exec(`UPDATE workspace_memberships SET status='suspended' WHERE principal_id='human'`)
			case "project":
				f.exec(`UPDATE projects SET status='archived' WHERE id='p'`)
			case "workspace":
				f.exec(`UPDATE project_workspaces SET status='archived' WHERE id='target'`)
			case "callback":
				c.Authorize = func(context.Context, storage.Tx) error { return ErrCrossWorkspace }
			case "forged_line":
				f.exec(`DROP TRIGGER library_text_line_immutable`)
				f.exec(`UPDATE project_library_text_lines SET content='needle corrupted',line_hash=? WHERE asset_id='readable'`, libraryTextDigest([]byte("needle corrupted")))
			}
			out, err := f.service.SearchWorkspaceIndexedLibraryText(context.Background(), c, "needle", LibraryTextSearchPosition{})
			if scenario == "grant" {
				if err != nil || len(out.Matches) != 0 {
					t.Fatalf("grant retained %+v %v", out, err)
				}
			} else if err == nil || len(out.Matches) != 0 {
				t.Fatalf("authority/malformed data accepted %+v %v", out, err)
			}
		})
	}
	f := newLibraryTextFixture(t)
	for _, q := range []string{"", " ", strings.Repeat("x", 257), "\x00", string([]byte{0xff})} {
		if _, err := f.service.SearchWorkspaceIndexedLibraryText(context.Background(), f.selection, q, LibraryTextSearchPosition{}); !errors.Is(err, ErrInvalidCommand) {
			t.Fatalf("invalid query accepted %q %v", q, err)
		}
	}
	for _, p := range []LibraryTextSearchPosition{{Version: 1}, {AssetID: "wanted", AfterLine: 1}, {AssetID: "wanted", Version: -1}, {AssetID: "wanted", Version: 1, AfterLine: 16385}} {
		if _, err := f.service.SearchWorkspaceIndexedLibraryText(context.Background(), f.selection, "needle", p); !errors.Is(err, ErrInvalidCommand) {
			t.Fatalf("invalid navigation accepted %+v %v", p, err)
		}
	}
}

func TestWorkspaceLibraryTextSearchPersistentProjectAndVersionPlans(t *testing.T) {
	f := newLibraryTextFixture(t)
	addWorkspaceTextIndex(t, f, "zz-readable", "needle", true)
	f.exec(`WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<1024) INSERT INTO project_library_assets(id,project_id,name,asset_type,current_version,created_at,updated_at) SELECT printf('foreign-%04d',x),'other','Foreign','text/plain',1,1,1 FROM n`)
	for _, analyze := range []bool{false, true} {
		if analyze {
			f.exec(`ANALYZE`)
		}
		for _, q := range []struct {
			sql   string
			args  []any
			index string
		}{{libraryWorkspaceTextAssetsSQL, []any{"p", "wanted"}, "idx_library_assets_project_identity"}, {libraryWorkspaceTextAssetsAfterSQL, []any{"p", "wanted"}, "idx_library_assets_project_identity"}, {libraryWorkspaceTextVersionsSQL, []any{"zz-readable", 0, 8}, "sqlite_autoindex_project_library_text_indexes"}} {
			rows, err := f.db.SQL().Query("EXPLAIN QUERY PLAN "+q.sql, q.args...)
			if err != nil {
				t.Fatal(err)
			}
			details := ""
			for rows.Next() {
				var id, parent, unused int
				var detail string
				if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
					t.Fatal(err)
				}
				details += detail + "\n"
			}
			if err = rows.Err(); err != nil {
				t.Fatal(err)
			}
			rows.Close()
			if !strings.Contains(details, "SEARCH") || !strings.Contains(details, q.index) || strings.Contains(details, "TEMP B-TREE") {
				t.Fatalf("unbounded production plan afterANALYZE=%v: %s", analyze, details)
			}
		}
	}
}

func TestWorkspaceLibraryTextSearchPreservesPinnedAndDirectionalVersionAuthority(t *testing.T) {
	f := newLibraryTextFixture(t)
	ctx := context.Background()
	cmd := f.command(t, []byte("needle pinned first version"), "text/plain")
	if _, err := f.service.IndexWorkspaceLibraryText(ctx, cmd); err != nil {
		t.Fatal(err)
	}
	body := []byte("needle PRIVATE newer version")
	f.exec(`UPDATE project_library_asset_versions SET content_hash=?,size_bytes=? WHERE asset_id='wanted' AND version=2`, libraryTextDigest(body), len(body))
	f.exec(`UPDATE workspace_library_grants SET pinned_version=2 WHERE id='text-direct'`)
	c := f.selection
	c.Version = 2
	v, err := f.service.ResolveBoundedWorkspaceLibraryVersion(ctx, "p", "target", "wanted", 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.IndexWorkspaceLibraryText(ctx, IndexLibraryTextCommand{Selection: c, Selected: v, Content: body}); err != nil {
		t.Fatal(err)
	}
	f.exec(`UPDATE workspace_library_grants SET pinned_version=1 WHERE id='text-direct'`)
	page, err := f.service.SearchWorkspaceIndexedLibraryText(ctx, f.selection, "needle", LibraryTextSearchPosition{})
	if err != nil || len(page.Matches) != 1 || page.Matches[0].Index.Version != 1 {
		t.Fatalf("pinned grant widened %+v %v", page, err)
	}
	f.exec(`UPDATE workspace_library_grants SET enabled=0 WHERE id='text-direct'`)
	f.exec(`UPDATE project_workspace_links SET enabled=1 WHERE id='source-129'`)
	f.exec(`INSERT INTO project_workspace_publications(id,link_id,asset_id,asset_version,content_hash,published_by,published_at) VALUES('current-publication','source-129','wanted',1,?,'human',1)`, cmd.Selected.ContentHash)
	page, err = f.service.SearchWorkspaceIndexedLibraryText(ctx, f.selection, "needle", LibraryTextSearchPosition{})
	if err != nil || len(page.Matches) != 1 || page.Matches[0].Index.Version != 1 {
		t.Fatalf("directional grant lost or widened %+v %v", page, err)
	}
	f.exec(`UPDATE project_workspace_links SET enabled=0 WHERE id='source-129'`)
	page, err = f.service.SearchWorkspaceIndexedLibraryText(ctx, f.selection, "needle", LibraryTextSearchPosition{})
	if err != nil || len(page.Matches) != 0 {
		t.Fatalf("revoked publication retained text %+v %v", page, err)
	}
}
