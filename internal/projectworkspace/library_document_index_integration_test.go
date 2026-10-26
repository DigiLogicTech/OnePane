//go:build integration

package projectworkspace

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/storage"
)

func documentCommand(t *testing.T, f *libraryTextFixture, body []byte) IndexLibraryDocumentCommand {
	c := f.command(t, body, LibraryDocumentMIME)
	return IndexLibraryDocumentCommand{Selection: c.Selection, Selected: c.Selected, Content: c.Content}
}
func TestLibraryDocumentIndexPersistedGrantsAndRetry(t *testing.T) {
	f := newLibraryTextFixture(t)
	ctx := context.Background()
	cmd := documentCommand(t, f, documentFixtureZIP(t, nil))
	absent, err := f.service.WorkspaceLibraryDocumentIndex(ctx, f.selection)
	if err != nil || absent.Status != "not_indexed" {
		t.Fatal(absent, err)
	}
	idx, err := f.service.IndexWorkspaceLibraryDocument(ctx, cmd)
	if err != nil || idx.Status != "ready" || idx.UnitCount != 5 {
		t.Fatal(idx, err)
	}
	retry, err := f.service.IndexWorkspaceLibraryDocument(ctx, cmd)
	if err != nil || !reflect.DeepEqual(idx, retry) {
		t.Fatal(retry, err)
	}
	var n int
	if err = f.db.SQL().QueryRow(`SELECT count(*) FROM events WHERE event_type='project.library_document_indexed'`).Scan(&n); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	f.exec(`PRAGMA query_only=ON`)
	page, err := f.service.ReadWorkspaceLibraryDocument(ctx, f.selection, "", 0)
	f.exec(`PRAGMA query_only=OFF`)
	if err != nil || len(page.Units) != 5 || page.HasMore || page.NextAfter != 5 || VerifyLibraryDocumentContent(page.Index, cmd.Content) != nil {
		t.Fatal(page, err)
	}
	query, err := f.service.ReadWorkspaceLibraryDocument(ctx, f.selection, "<tag>", 0)
	if err != nil || len(query.Units) != 1 || query.Units[0].Ordinal != 1 {
		t.Fatal(query, err)
	}
	f.exec(`UPDATE workspace_library_grants SET enabled=0 WHERE id='text-direct'`)
	// Existing directional publication has the previous hash and cannot grant this version.
	if _, err = f.service.ReadWorkspaceLibraryDocument(ctx, f.selection, "", 0); !errors.Is(err, ErrCrossWorkspace) {
		t.Fatal(err)
	}
	f.exec(`UPDATE project_workspace_publications SET content_hash=? WHERE id='source-128'`, cmd.Selected.ContentHash)
	if _, err = f.service.ReadWorkspaceLibraryDocument(ctx, f.selection, "", 0); err != nil {
		t.Fatal(err)
	}
	f.exec(`UPDATE project_workspace_links SET enabled=0 WHERE id='source-128'`)
	if _, err = f.service.WorkspaceLibraryDocumentIndex(ctx, f.selection); !errors.Is(err, ErrCrossWorkspace) {
		t.Fatal(err)
	}
}
func TestLibraryDocumentIndexAtomicAndCurrentAuthority(t *testing.T) {
	for _, target := range []string{"project_library_document_units", "events"} {
		t.Run(target, func(t *testing.T) {
			f := newLibraryTextFixture(t)
			cmd := documentCommand(t, f, documentFixtureZIP(t, nil))
			f.exec(`CREATE TRIGGER reject_doc BEFORE INSERT ON ` + target + ` BEGIN SELECT RAISE(ABORT,'rejected'); END`)
			if _, err := f.service.IndexWorkspaceLibraryDocument(context.Background(), cmd); err == nil {
				t.Fatal("expected rejection")
			}
			var n int
			f.db.SQL().QueryRow(`SELECT count(*) FROM project_library_document_indexes`).Scan(&n)
			if n != 0 {
				t.Fatal("partial header")
			}
		})
	}
	for _, change := range []string{"source", "actor", "tenant", "workspace", "authorize"} {
		t.Run(change, func(t *testing.T) {
			f := newLibraryTextFixture(t)
			cmd := documentCommand(t, f, documentFixtureZIP(t, nil))
			switch change {
			case "source":
				f.exec(`UPDATE project_library_asset_versions SET storage_uri='artifact:changed' WHERE asset_id='wanted' AND version=1`)
			case "actor":
				cmd.Selection.ActorPrincipalID = "unknown"
			case "tenant":
				f.exec(`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant2','Other','active',1,1,1)`)
				f.exec(`UPDATE projects SET workspace_id='tenant2' WHERE id='p'`)
			case "workspace":
				cmd.Selection.WorkspaceID = "other-target"
			case "authorize":
				cmd.Selection.Authorize = func(context.Context, storage.Tx) error { return errors.New("original credential revoked") }
			}
			if _, err := f.service.IndexWorkspaceLibraryDocument(context.Background(), cmd); err == nil {
				t.Fatal("changed authority accepted")
			}
		})
	}
}
func TestLibraryDocumentRetainedCorruptionFailsClosed(t *testing.T) {
	for _, change := range []string{"selfhashedunit", "missing", "orphan", "parts", "header", "oversized"} {
		t.Run(change, func(t *testing.T) {
			f := newLibraryTextFixture(t)
			cmd := documentCommand(t, f, documentFixtureZIP(t, nil))
			if _, err := f.service.IndexWorkspaceLibraryDocument(context.Background(), cmd); err != nil {
				t.Fatal(err)
			}
			f.exec(`DROP TRIGGER library_document_unit_immutable`)
			f.exec(`DROP TRIGGER library_document_unit_retained`)
			f.exec(`DROP TRIGGER library_document_index_immutable`)
			f.exec(`DROP TRIGGER library_document_index_retained`)
			switch change {
			case "selfhashedunit":
				f.exec(`UPDATE project_library_document_units SET content='forged',text_hash=? WHERE ordinal=1`, libraryTextDigest([]byte("forged")))
			case "missing":
				f.exec(`DELETE FROM project_library_document_units WHERE ordinal=1`)
			case "orphan":
				f.exec(`PRAGMA foreign_keys=OFF`)
				f.exec(`DELETE FROM project_library_document_indexes`)
			case "parts":
				f.exec(`UPDATE project_library_document_indexes SET parts_json=json_set(parts_json,'$[0].source_sha',?)`, libraryTextDigest([]byte("forged")))
			case "header":
				f.exec(`UPDATE project_library_document_indexes SET raw_source_size=raw_source_size+1`)
			case "oversized":
				f.exec(`PRAGMA ignore_check_constraints=ON`)
				f.exec(`UPDATE project_library_document_units SET content=? WHERE ordinal=1`, strings.Repeat("x", 16385))
			}
			if _, err := f.service.ReadWorkspaceLibraryDocument(context.Background(), f.selection, "", 0); !errors.Is(err, ErrLibraryDocumentIndex) {
				t.Fatal(err)
			}
		})
	}
}

func TestLibraryDocumentStatusesSourceBindingAndPagination(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, mime, status string
		body               []byte
		large              bool
	}{
		{"unsupported", "application/pdf", "unsupported", nil, false},
		{"raw-limit", LibraryDocumentMIME, "too_large", nil, true},
		{"invalid", LibraryDocumentMIME, "invalid_document", []byte("bad ZIP"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newLibraryTextFixture(t)
			base := f.command(t, tc.body, tc.mime)
			cmd := IndexLibraryDocumentCommand{Selection: base.Selection, Selected: base.Selected, Content: tc.body}
			if tc.large {
				cmd.Selected.SizeBytes = LibraryDocumentIndexLimit + 1
				f.exec(`UPDATE project_library_asset_versions SET size_bytes=? WHERE asset_id='wanted' AND version=1`, cmd.Selected.SizeBytes)
			}
			idx, err := f.service.IndexWorkspaceLibraryDocument(ctx, cmd)
			if err != nil || idx.Status != tc.status || len(idx.Parts) != 0 || idx.DerivedSHA != "" || idx.Coverage != "" {
				t.Fatal(idx, err)
			}
			if _, err = f.service.ReadWorkspaceLibraryDocument(ctx, f.selection, "", 0); !errors.Is(err, ErrLibraryDocumentNotIndexed) {
				t.Fatal(err)
			}
			if _, err = f.service.IndexWorkspaceLibraryDocument(ctx, cmd); err != nil {
				t.Fatal(err)
			}
		})
	}
	f := newLibraryTextFixture(t)
	body := documentFixtureZIP(t, map[string]string{"word/document.xml": `<w:document xmlns:w="` + wordML + `"><w:body>` + strings.Repeat(`<w:p><w:r><w:t>match %_</w:t></w:r></w:p>`, 19) + `</w:body></w:document>`})
	cmd := documentCommand(t, f, body)
	bad := cmd
	bad.Content = []byte("substitute")
	if _, err := f.service.IndexWorkspaceLibraryDocument(ctx, bad); !errors.Is(err, ErrLibraryDocumentIndex) {
		t.Fatal(err)
	}
	if _, err := f.service.IndexWorkspaceLibraryDocument(ctx, cmd); err != nil {
		t.Fatal(err)
	}
	after := 0
	seen := 0
	for {
		page, err := f.service.ReadWorkspaceLibraryDocument(ctx, f.selection, "%_", after)
		if err != nil || len(page.Units) > 8 {
			t.Fatal(page, err)
		}
		for _, u := range page.Units {
			if u.Ordinal <= after {
				t.Fatal("duplicate")
			}
			seen++
		}
		if !page.HasMore {
			break
		}
		if page.NextAfter <= after {
			t.Fatal("no progress")
		}
		after = page.NextAfter
	}
	if seen != 19 {
		t.Fatal(seen)
	}
	for _, q := range []string{strings.Repeat("x", 257), "\x00", string([]byte{0xff})} {
		if _, err := f.service.ReadWorkspaceLibraryDocument(ctx, f.selection, q, 0); !errors.Is(err, ErrInvalidCommand) {
			t.Fatal(err)
		}
	}
	for _, after := range []int{-1, 22} {
		if _, err := f.service.ReadWorkspaceLibraryDocument(ctx, f.selection, "", after); !errors.Is(err, ErrInvalidCommand) {
			t.Fatal(err)
		}
	}
	f.exec(`UPDATE workspace_library_grants SET pinned_version=2 WHERE id='text-direct'`)
	if _, err := f.service.WorkspaceLibraryDocumentIndex(ctx, f.selection); !errors.Is(err, ErrCrossWorkspace) {
		t.Fatal(err)
	}
}
