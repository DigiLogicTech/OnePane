//go:build integration

package projectworkspace

import (
	"bytes"
	"context"
	"errors"

	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/storage"
	sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

type libraryTextFixture struct {
	db        *sqlitestore.DB
	service   *Service
	exec      func(string, ...any)
	selection LibraryTextSelection
}

func newLibraryTextFixture(t *testing.T) *libraryTextFixture {
	t.Helper()
	db, s, exec := boundedLibraryFixture(t)
	exec(`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('tenant','human','active',1,1)`)
	exec(`INSERT INTO workspace_library_grants(id,project_workspace_id,asset_id,enabled,version_policy,pinned_version,permissions_json,created_at,updated_at) VALUES('text-direct','target','wanted',1,'pinned',1,'{"read":true}',1,1)`)
	return &libraryTextFixture{db: db, service: s, exec: exec, selection: LibraryTextSelection{ProjectID: "p", WorkspaceID: "target", AssetID: "wanted", ActorPrincipalID: "human", Version: 1}}
}

func (f *libraryTextFixture) command(t *testing.T, body []byte, media string) IndexLibraryTextCommand {
	t.Helper()
	f.exec(`UPDATE project_library_asset_versions SET content_hash=?,size_bytes=?,mime_type=? WHERE asset_id='wanted' AND version=1`, libraryTextDigest(body), len(body), media)
	selected, err := f.service.ResolveBoundedWorkspaceLibraryVersion(context.Background(), "p", "target", "wanted", 1)
	if err != nil {
		t.Fatal(err)
	}
	return IndexLibraryTextCommand{Selection: f.selection, Selected: selected, Content: body}
}

func TestLibraryTextIndexExactLinesSearchRestartAndIdempotency(t *testing.T) {
	f := newLibraryTextFixture(t)
	ctx := context.Background()
	cmd := f.command(t, []byte("Castle café\r\n<script>Castle</script>\n城 Castle\n"), "text/markdown; charset=utf-8")
	absent, err := f.service.WorkspaceLibraryTextIndex(ctx, f.selection)
	if err != nil || absent.Status != "not_indexed" {
		t.Fatalf("missing %+v %v", absent, err)
	}
	idx, err := f.service.IndexWorkspaceLibraryText(ctx, cmd)
	if err != nil || idx.Status != "ready" || idx.LineCount != 3 || idx.ContentHash != libraryTextDigest(cmd.Content) {
		t.Fatalf("index %+v %v", idx, err)
	}
	rebuilt := NewService(f.db.SQL(), f.db, clock.Real{})
	retry, err := rebuilt.IndexWorkspaceLibraryText(ctx, cmd)
	if err != nil || retry != idx {
		t.Fatalf("retry %+v %v", retry, err)
	}
	var audits int
	if err = f.db.SQL().QueryRow(`SELECT COUNT(*) FROM events WHERE event_type='project.library_text_indexed'`).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("retry audit=%d %v", audits, err)
	}
	f.exec(`PRAGMA query_only=ON`)
	defer f.exec(`PRAGMA query_only=OFF`)
	found, err := rebuilt.SearchWorkspaceLibraryText(ctx, f.selection, "Castle")
	if err != nil || len(found.Lines) != 3 || found.HasMore {
		t.Fatalf("search %+v %v", found, err)
	}
	for i, line := range found.Lines {
		if line.LineNumber != i+1 || line.LineHash != libraryTextDigest([]byte(line.Text)) || string(cmd.Content[line.ByteStart:line.ByteEnd]) != line.Text {
			t.Fatalf("unanchored citation %+v", line)
		}
	}
	if found.Lines[0].Text != "Castle café\r" || found.Lines[0].ByteStart != 0 || found.Lines[1].ByteStart != int64(len("Castle café\r\n")) {
		t.Fatalf("CRLF/UTF8 offsets changed %+v", found)
	}
	literal, err := rebuilt.SearchWorkspaceLibraryText(ctx, f.selection, "<script>")
	if err != nil || len(literal.Lines) != 1 || literal.Lines[0].Text != "<script>Castle</script>" {
		t.Fatalf("markup was interpreted %+v %v", literal, err)
	}
	empty, err := rebuilt.SearchWorkspaceLibraryText(ctx, f.selection, "castle")
	if err != nil || len(empty.Lines) != 0 {
		t.Fatalf("literal case-sensitive query changed %+v %v", empty, err)
	}
}

func TestLibraryTextIndexRejectsIntegrityChangesAndAtomicWriteFailures(t *testing.T) {
	for _, scenario := range []string{"wrong_bytes", "wrong_size", "changed_metadata", "revoked_callback", "rejected_line", "rejected_audit"} {
		t.Run(scenario, func(t *testing.T) {
			f := newLibraryTextFixture(t)
			ctx := context.Background()
			cmd := f.command(t, []byte("first\nsecond\n"), "text/plain")
			switch scenario {
			case "wrong_bytes":
				cmd.Content = []byte("forged contents")
			case "wrong_size":
				cmd.Selected.SizeBytes++
			case "changed_metadata":
				f.exec(`UPDATE project_library_asset_versions SET storage_uri='artifact:changed' WHERE asset_id='wanted' AND version=1`)
			case "revoked_callback":
				cmd.Selection.Authorize = func(context.Context, storage.Tx) error { return errors.New("credential revoked") }
			case "rejected_line":
				f.exec(`CREATE TRIGGER reject_library_text_line BEFORE INSERT ON project_library_text_lines WHEN NEW.line_number=2 BEGIN SELECT RAISE(ABORT,'reject line'); END`)
			case "rejected_audit":
				f.exec(`CREATE TRIGGER reject_library_text_audit BEFORE INSERT ON events WHEN NEW.event_type='project.library_text_indexed' BEGIN SELECT RAISE(ABORT,'reject audit'); END`)
			}
			got, err := f.service.IndexWorkspaceLibraryText(ctx, cmd)
			if err == nil || got != (LibraryTextIndex{}) {
				t.Fatalf("unsafe index %+v %v", got, err)
			}
			var headers, lines, audits int
			if err = f.db.SQL().QueryRow(`SELECT (SELECT COUNT(*) FROM project_library_text_indexes),(SELECT COUNT(*) FROM project_library_text_lines),(SELECT COUNT(*) FROM events WHERE event_type='project.library_text_indexed')`).Scan(&headers, &lines, &audits); err != nil || headers != 0 || lines != 0 || audits != 0 {
				t.Fatalf("partial retained index %d/%d/%d %v", headers, lines, audits, err)
			}
		})
	}
}

func TestLibraryTextIndexCurrentScopeAndGrantRevocation(t *testing.T) {
	f := newLibraryTextFixture(t)
	ctx := context.Background()
	cmd := f.command(t, []byte("private Castle"), "text/plain")
	if _, err := f.service.IndexWorkspaceLibraryText(ctx, cmd); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct{ name, change, restore string }{
		{"membership", `UPDATE workspace_memberships SET status='revoked' WHERE principal_id='human'`, `UPDATE workspace_memberships SET status='active' WHERE principal_id='human'`},
		{"principal", `UPDATE principals SET status='disabled' WHERE id='human'`, `UPDATE principals SET status='active' WHERE id='human'`},
		{"project", `UPDATE projects SET status='archived' WHERE id='p'`, `UPDATE projects SET status='active' WHERE id='p'`},
		{"workspace", `UPDATE project_workspaces SET status='archived' WHERE id='target'`, `UPDATE project_workspaces SET status='active' WHERE id='target'`},
		{"tenant", `UPDATE workspaces SET status='archived' WHERE id='tenant'`, `UPDATE workspaces SET status='active' WHERE id='tenant'`},
		{"asset", `UPDATE project_library_assets SET archived=1 WHERE id='wanted'`, `UPDATE project_library_assets SET archived=0 WHERE id='wanted'`},
		{"changed_hash", `UPDATE project_library_asset_versions SET content_hash=printf('%064x',9) WHERE asset_id='wanted' AND version=1`, `UPDATE project_library_asset_versions SET content_hash='` + cmd.Selected.ContentHash + `' WHERE asset_id='wanted' AND version=1`},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			f.exec(scenario.change)
			if got, err := f.service.WorkspaceLibraryTextIndex(ctx, f.selection); err == nil || got != (LibraryTextIndex{}) {
				t.Fatalf("revoked status %+v %v", got, err)
			}
			if got, err := f.service.SearchWorkspaceLibraryText(ctx, f.selection, "Castle"); err == nil || got.Index.Status != "" || len(got.Lines) != 0 {
				t.Fatalf("revoked text %+v %v", got, err)
			}
			if _, err := f.service.IndexWorkspaceLibraryText(ctx, cmd); err == nil {
				t.Fatal("revoked identical retry accepted")
			}
			f.exec(scenario.restore)
		})
	}
	// Disable direct authority; one current hash-pinned incoming publication is
	// enough, but revocation/expiry and stale pinned-version policy are not.
	f.exec(`UPDATE workspace_library_grants SET enabled=0 WHERE id='text-direct'`)
	f.exec(`UPDATE project_workspace_publications SET content_hash=? WHERE asset_id='wanted' AND asset_version=1`, cmd.Selected.ContentHash)
	if got, err := f.service.SearchWorkspaceLibraryText(ctx, f.selection, "Castle"); err != nil || len(got.Lines) != 1 {
		t.Fatalf("directional search %+v %v", got, err)
	}
	f.exec(`UPDATE project_workspace_links SET enabled=0 WHERE id='source-128'`)
	if _, err := f.service.SearchWorkspaceLibraryText(ctx, f.selection, "Castle"); err == nil {
		t.Fatal("revoked directional text leaked")
	}
	f.exec(`UPDATE workspace_library_grants SET enabled=1,version_policy='latest',pinned_version=NULL WHERE id='text-direct'`)
	if _, err := f.service.SearchWorkspaceLibraryText(ctx, f.selection, "Castle"); err == nil {
		t.Fatal("latest grant leaked older indexed version")
	}
	f.exec(`UPDATE workspace_library_grants SET version_policy='pinned',pinned_version=1 WHERE id='text-direct'`)
	if _, err := f.service.SearchWorkspaceLibraryText(ctx, f.selection, "Castle"); err != nil {
		t.Fatal(err)
	}
	for _, selection := range []LibraryTextSelection{
		{ProjectID: "other", WorkspaceID: "target", AssetID: "wanted", ActorPrincipalID: "human", Version: 1},
		{ProjectID: "p", WorkspaceID: "other-target", AssetID: "wanted", ActorPrincipalID: "human", Version: 1},
		{ProjectID: "p", WorkspaceID: "target", AssetID: "wanted", ActorPrincipalID: "absent", Version: 1},
	} {
		if _, err := f.service.WorkspaceLibraryTextIndex(ctx, selection); err == nil {
			t.Fatalf("foreign scope accepted %+v", selection)
		}
	}
}

func TestLibraryTextIndexStatusAndBoundedLiteralMatches(t *testing.T) {
	for _, scenario := range []struct {
		name          string
		body          []byte
		media, status string
	}{
		{"empty", []byte{}, "text/plain", "ready"},
		{"binary_mime", []byte("not parsed"), "application/pdf", "unsupported"},
		{"charset", []byte("abc"), "text/plain; charset=utf-16", "unsupported"},
		{"invalid_utf8", []byte{0xff}, "text/plain", "invalid_text"},
		{"nul", []byte("private\x00text"), "text/plain", "invalid_text"},
		{"long_line", bytes.Repeat([]byte("x"), 16385), "text/plain", "too_large"},
		{"many_lines", bytes.Repeat([]byte("\n"), 16385), "text/plain", "too_large"},
		{"line_boundary", bytes.Repeat([]byte("x"), 16384), "text/plain", "ready"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			f := newLibraryTextFixture(t)
			cmd := f.command(t, scenario.body, scenario.media)
			got, err := f.service.IndexWorkspaceLibraryText(context.Background(), cmd)
			if err != nil || got.Status != scenario.status {
				t.Fatalf("status %+v %v", got, err)
			}
			read, err := f.service.WorkspaceLibraryTextIndex(context.Background(), f.selection)
			if err != nil || read != got {
				t.Fatalf("retained %+v %v", read, err)
			}
			if scenario.status != "ready" {
				if _, err = f.service.SearchWorkspaceLibraryText(context.Background(), f.selection, "private"); !errors.Is(err, ErrLibraryTextNotIndexed) {
					t.Fatalf("unready index searched %v", err)
				}
			}
		})
	}
	f := newLibraryTextFixture(t)
	cmd := f.command(t, []byte(strings.Repeat("literal %_ SQL --\n", 12)), "text/plain")
	if _, err := f.service.IndexWorkspaceLibraryText(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	matches, err := f.service.SearchWorkspaceLibraryText(context.Background(), f.selection, "%_")
	if err != nil || len(matches.Lines) != 8 || !matches.HasMore || matches.Lines[7].LineNumber != 8 {
		t.Fatalf("bounded literal %+v %v", matches, err)
	}
	for _, q := range []string{"", " ", string([]byte{0xff}), "\x00", strings.Repeat("x", 257)} {
		if _, err = f.service.SearchWorkspaceLibraryText(context.Background(), f.selection, q); !errors.Is(err, ErrInvalidCommand) {
			t.Fatalf("invalid query %q %v", q, err)
		}
	}
}

func TestLibraryTextIndexPersistentPlansAndRetainedCorruption(t *testing.T) {
	f := newLibraryTextFixture(t)
	ctx := context.Background()
	cmd := f.command(t, []byte("one\ntwo\n"), "text/plain")
	if _, err := f.service.IndexWorkspaceLibraryText(ctx, cmd); err != nil {
		t.Fatal(err)
	}
	f.exec(`WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<1024) INSERT INTO project_library_assets(id,project_id,name,asset_type,current_version,created_at,updated_at) SELECT 'text-history-'||x,'p','History','text/plain',1,1,1 FROM n`)
	f.exec(`INSERT INTO project_library_asset_versions(asset_id,version,content_hash,size_bytes,mime_type,storage_uri,created_at) SELECT id,1,?,1,'text/plain','artifact:history',1 FROM project_library_assets WHERE id LIKE 'text-history-%'`, libraryTextDigest([]byte("x")))
	f.exec(`INSERT INTO project_library_text_indexes(asset_id,version,parser_version,content_hash,size_bytes,status,line_count,created_by,created_at) SELECT id,1,1,?,1,'ready',1,'human',1 FROM project_library_assets WHERE id LIKE 'text-history-%'`, libraryTextDigest([]byte("x")))
	f.exec(`INSERT INTO project_library_text_lines(asset_id,version,parser_version,line_number,byte_start,byte_end,content,line_hash) SELECT id,1,1,1,0,1,'x',? FROM project_library_assets WHERE id LIKE 'text-history-%'`, libraryTextDigest([]byte("x")))
	for _, stats := range []bool{false, true} {
		if stats {
			f.exec(`ANALYZE`)
		}
		for _, query := range []string{libraryTextIndexReadSQL, libraryTextLinesReadSQL} {
			plan := workspaceLibraryResolverPlan(t, f.db.SQL(), query, "wanted", int64(1))
			if !strings.Contains(plan, "SEARCH project_library_text_") || strings.Contains(plan, "SCAN project_library_text_") || strings.Contains(plan, "AUTOMATIC") || strings.Contains(plan, "TEMP B-TREE") {
				t.Fatalf("history-dependent plan stats=%v\n%s", stats, plan)
			}
		}
	}
	// Simulate historical corruption outside normal immutable write paths.
	f.exec(`DROP TRIGGER library_text_line_immutable`)
	f.exec(`UPDATE project_library_text_lines SET content='new',line_hash=? WHERE asset_id='wanted' AND line_number=1`, libraryTextDigest([]byte("new")))
	if _, err := f.service.SearchWorkspaceLibraryText(ctx, f.selection, "new"); !errors.Is(err, ErrLibraryTextIndex) {
		t.Fatalf("forged line passed source hash %v", err)
	}
	f.exec(`UPDATE project_library_text_lines SET content='one',line_hash=? WHERE asset_id='wanted' AND line_number=1`, libraryTextDigest([]byte("one")))
	f.exec(`DROP TRIGGER library_text_line_retained`)
	f.exec(`DELETE FROM project_library_text_lines WHERE asset_id='wanted' AND line_number=2`)
	if _, err := f.service.WorkspaceLibraryTextIndex(ctx, f.selection); !errors.Is(err, ErrLibraryTextIndex) {
		t.Fatalf("missing retained line passed %v", err)
	}
}

func TestLibraryTextIndexRequiresExactVerifiedBytesForReadyAndRejectsOversize(t *testing.T) {
	f := newLibraryTextFixture(t)
	ctx := context.Background()
	cmd := f.command(t, []byte("x"), "text/plain")
	f.exec(`UPDATE project_library_asset_versions SET size_bytes=? WHERE asset_id='wanted' AND version=1`, LibraryTextIndexLimit+1)
	cmd.Selected.SizeBytes = LibraryTextIndexLimit + 1
	cmd.Content = nil
	idx, err := f.service.IndexWorkspaceLibraryText(ctx, cmd)
	if err != nil || idx.Status != "too_large" {
		t.Fatalf("oversize metadata %+v %v", idx, err)
	}
	if _, err = f.service.IndexWorkspaceLibraryText(ctx, IndexLibraryTextCommand{Selection: f.selection, Selected: cmd.Selected, Content: make([]byte, LibraryTextIndexLimit+1)}); !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("unbounded content allocation accepted %v", err)
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = f.service.WorkspaceLibraryTextIndex(ctx, f.selection); err == nil {
		t.Fatal("cancelled observation succeeded")
	}

}

func TestLibraryTextIndexMissingHeaderWithRetainedLinesRequiresReconciliation(t *testing.T) {
	f := newLibraryTextFixture(t)
	ctx := context.Background()
	cmd := f.command(t, []byte("retained private text"), "text/plain")
	if _, err := f.service.IndexWorkspaceLibraryText(ctx, cmd); err != nil {
		t.Fatal(err)
	}
	// Simulate damaged historical storage, outside all ordinary write paths.
	f.exec(`DROP TRIGGER library_text_index_retained`)
	f.exec(`PRAGMA foreign_keys=OFF`)
	f.exec(`DELETE FROM project_library_text_indexes WHERE asset_id='wanted'`)
	f.exec(`PRAGMA foreign_keys=ON`)
	if idx, err := f.service.WorkspaceLibraryTextIndex(ctx, f.selection); !errors.Is(err, ErrLibraryTextIndex) || idx != (LibraryTextIndex{}) {
		t.Fatalf("orphan lines reported absent %+v %v", idx, err)
	}
	if _, err := f.service.IndexWorkspaceLibraryText(ctx, cmd); !errors.Is(err, ErrLibraryTextIndex) {
		t.Fatalf("orphan lines repaired implicitly %v", err)
	}
}
