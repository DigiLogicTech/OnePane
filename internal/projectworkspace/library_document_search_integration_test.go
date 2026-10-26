//go:build integration

package projectworkspace

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func addDocumentSearchAsset(t *testing.T, f *libraryTextFixture, id string, readable bool) {
	t.Helper()
	story := `<w:document xmlns:w="` + wordML + `"><w:body>` + strings.Repeat(`<w:p><w:r><w:t>needle café &lt;script&gt;literal&lt;/script&gt;</w:t></w:r></w:p>`, 10) + `</w:body></w:document>`
	body := documentFixtureZIP(t, map[string]string{"word/document.xml": story})
	f.exec(`INSERT INTO project_library_assets(id,project_id,name,asset_type,current_version,created_at,updated_at) VALUES(?,'p',?,'document',1,1,1)`, id, id)
	f.exec(`INSERT INTO project_library_asset_versions(asset_id,version,content_hash,size_bytes,mime_type,storage_uri,created_at) VALUES(?,1,?,?,?,?,1)`, id, libraryTextDigest(body), len(body), LibraryDocumentMIME, "artifact:"+id)
	f.exec(`INSERT INTO workspace_library_grants(id,project_workspace_id,asset_id,enabled,version_policy,pinned_version,permissions_json,created_at,updated_at) VALUES(?,'target',?,1,'pinned',1,'{"read":true}',1,1)`, "doc-grant-"+id, id)
	c := f.selection
	c.AssetID = id
	selected, err := f.service.ResolveBoundedWorkspaceLibraryVersion(context.Background(), "p", "target", id, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.IndexWorkspaceLibraryDocument(context.Background(), IndexLibraryDocumentCommand{Selection: c, Selected: selected, Content: body}); err != nil {
		t.Fatal(err)
	}
	if !readable {
		f.exec(`UPDATE workspace_library_grants SET enabled=0 WHERE asset_id=?`, id)
	}
}

func TestLibraryDocumentSearchBoundedDeniedAndUnindexedPages(t *testing.T) {
	f := newLibraryTextFixture(t)
	for i := 0; i < 8; i++ {
		id := fmt.Sprintf("a-unindexed-%02d", i)
		f.exec(`INSERT INTO project_library_assets(id,project_id,name,asset_type,current_version,created_at,updated_at) VALUES(?,'p',?,'document',1,1,1)`, id, id)
		addDocumentSearchAsset(t, f, fmt.Sprintf("b-denied-%02d", i), false)
	}
	addDocumentSearchAsset(t, f, "z-readable", true)
	f.exec(`PRAGMA query_only=ON`)
	defer f.exec(`PRAGMA query_only=OFF`)
	position := LibraryDocumentSearchPosition{}
	seen := map[int]bool{}
	empty := 0
	for page := 0; page < 16; page++ {
		out, err := f.service.SearchWorkspaceIndexedLibraryDocuments(context.Background(), f.selection, "needle", position)
		if err != nil {
			t.Fatal(err)
		}
		if out.ScannedAssets > 8 || out.ScannedVersions > 2 || len(out.Candidates) > 2 || len(out.Matches) > 8 {
			t.Fatal("unbounded document discovery", out)
		}
		if out.HasMore && len(out.Matches) == 0 {
			empty++
		}
		for _, match := range out.Matches {
			if match.Index.AssetID != "z-readable" || match.Index.ParserID != LibraryDocumentParserID || match.Unit.PartName != "word/document.xml" || seen[match.Unit.Ordinal] || match.Unit.TextHash != libraryTextDigest([]byte(match.Unit.Text)) {
				t.Fatal("private, duplicate or invalid paragraph citation", match)
			}
			seen[match.Unit.Ordinal] = true
		}
		body, err := json.Marshal(out)
		if err != nil || strings.Contains(string(body), "b-denied") || strings.Contains(string(body), "Candidates") || strings.Contains(string(body), "storage_uri") || strings.Contains(string(body), "Scanned") || strings.Contains(string(body), "NextPosition") {
			t.Fatal("private document scan navigation/verification leaked", string(body), err)
		}
		if !out.HasMore {
			if len(seen) != 10 || empty < 5 {
				t.Fatal("late paragraphs or honest partial pages lost", seen, empty)
			}
			return
		}
		if position == out.NextPosition {
			t.Fatal("document discovery failed to advance")
		}
		position = out.NextPosition
	}
	t.Fatal("document discovery did not exhaust bounded forward scan")
}

func TestLibraryDocumentSearchVerifiesNonmatchesAndCurrentAuthority(t *testing.T) {
	f := newLibraryTextFixture(t)
	addDocumentSearchAsset(t, f, "readable", true)
	out, err := f.service.SearchWorkspaceIndexedLibraryDocuments(context.Background(), f.selection, "no such phrase", LibraryDocumentSearchPosition{})
	if err != nil || len(out.Matches) != 0 || len(out.Candidates) != 1 || out.Candidates[0].Version.AssetID != "readable" {
		t.Fatal("authorized nonmatches omitted from raw verification", out, err)
	}
	f.exec(`UPDATE workspace_library_grants SET enabled=0 WHERE asset_id='readable'`)
	out, err = f.service.SearchWorkspaceIndexedLibraryDocuments(context.Background(), f.selection, "needle", LibraryDocumentSearchPosition{})
	if err != nil || len(out.Matches) != 0 || len(out.Candidates) != 0 {
		t.Fatal("revoked source remains a result/verification candidate", out, err)
	}
	f.exec(`UPDATE project_workspaces SET status='archived' WHERE id='target'`)
	if _, err = f.service.SearchWorkspaceIndexedLibraryDocuments(context.Background(), f.selection, "needle", LibraryDocumentSearchPosition{}); err == nil {
		t.Fatal("empty result bypassed current Workspace authority")
	}
}

func TestLibraryDocumentSearchParserFirstPersistentPlan(t *testing.T) {
	f := newLibraryTextFixture(t)
	addDocumentSearchAsset(t, f, "readable", true)
	// A dense retained older-parser history must not become a filter scan on
	// (asset,version,parser). These rows are deliberately never content-read.
	f.exec(`WITH RECURSIVE n(x) AS (VALUES(2) UNION ALL SELECT x+1 FROM n WHERE x<1025) INSERT INTO project_library_asset_versions(asset_id,version,content_hash,size_bytes,mime_type,storage_uri,created_at) SELECT asset_id,x,content_hash,size_bytes,mime_type,storage_uri,created_at FROM project_library_asset_versions,n WHERE asset_id='readable' AND version=1`)
	f.exec(`INSERT INTO project_library_document_indexes SELECT h.asset_id,v.version,'docx-wordml-paragraphs-v1',h.raw_source_sha,h.raw_source_size,'',h.manifest_sha,0,'invalid_document','','[]',0,h.created_by,h.created_at FROM project_library_document_indexes h JOIN project_library_asset_versions v ON v.asset_id=h.asset_id WHERE h.asset_id='readable' AND v.version>1`)
	for _, analyze := range []bool{false, true} {
		if analyze {
			f.exec(`ANALYZE`)
		}
		for _, query := range []string{libraryWorkspaceDocumentVersionsSQL, libraryWorkspaceDocumentVersionsResumeSQL} {
			rows, err := f.db.SQL().Query("EXPLAIN QUERY PLAN "+query, "readable", 0, 2)
			if err != nil {
				t.Fatal(err)
			}
			var details strings.Builder
			for rows.Next() {
				var id, parent, unused int
				var detail string
				if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
					t.Fatal(err)
				}
				details.WriteString(detail)
			}
			err = rows.Err()
			rows.Close()
			if err != nil || !strings.Contains(details.String(), "SEARCH") || !strings.Contains(details.String(), "library_document_indexes_by_parser_version") || strings.Contains(details.String(), "TEMP B-TREE") {
				t.Fatal("parser history caused unbounded navigation", details.String(), err)
			}
		}
		out, err := f.service.SearchWorkspaceIndexedLibraryDocuments(context.Background(), f.selection, "needle", LibraryDocumentSearchPosition{})
		if err != nil || len(out.Matches) != 8 || out.ScannedVersions != 1 {
			t.Fatal("retained v1 history changed v2 discovery", out, err)
		}
	}
}
