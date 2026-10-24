//go:build integration

package projectworkspace

import (
	"context"
	"database/sql"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	sqlitestore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

// Extract the actual shipped statement: a copied, simplified query would not
// establish the production join order or preservation of permission predicates.
func workspaceLibraryResolverSQL(t *testing.T) string {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), "library.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var query string
	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "ResolveWorkspaceLibraryVersion" {
			continue
		}
		ast.Inspect(fn, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) < 2 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "QueryRowContext" {
				return true
			}
			lit, ok := call.Args[1].(*ast.BasicLit)
			if !ok {
				return true
			}
			value, e := strconv.Unquote(lit.Value)
			if e == nil && strings.HasPrefix(value, "SELECT v.asset_id") {
				if query != "" {
					t.Fatal("ambiguous production resolver statement")
				}
				query = value
			}
			return true
		})
	}
	if query == "" {
		t.Fatal("actual production Library resolver statement not found")
	}
	return query
}

func workspaceLibraryResolverPlan(t *testing.T, db *sql.DB, query string, args ...any) string {
	t.Helper()
	rows, err := db.Query("EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatal(err)
	}
	var plan strings.Builder
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		fmt.Fprintln(&plan, detail)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	if err = rows.Close(); err != nil {
		t.Fatal(err)
	}
	return plan.String()
}

func TestWorkspaceLibraryPublicationLookupUsesExactVersionIndex(t *testing.T) {
	ctx := context.Background()
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "library-plan.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := db.SQL().Exec(q, args...); e != nil {
			t.Fatal(e)
		}
	}
	for _, q := range []string{
		`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('tenant','Tenant','active',1,1,1)`,
		`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('human','human','Owner','active',1,1,1)`,
		`INSERT INTO projects(id,workspace_id,name,status,project_policy_json,indexing_config_json,created_by,created_at,updated_at) VALUES('p','tenant','Project','active','{}','{}','human',1,1)`,
		`INSERT INTO project_workspaces(id,project_id,name,created_at,updated_at) VALUES('target','p','Target',1,1)`,
		`INSERT INTO project_library_assets(id,project_id,name,asset_type,current_version,created_at,updated_at) VALUES('wanted','p','Wanted','text/plain',1,1,1)`,
		`INSERT INTO project_library_asset_versions(asset_id,version,content_hash,size_bytes,mime_type,storage_uri,created_at) VALUES('wanted',1,printf('%064x',2048),3,'text/plain','artifact:wanted',1)`,
		`WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<1024) INSERT INTO project_workspaces(id,project_id,name,created_at,updated_at) SELECT 'history-'||x,'p','Source',1,1 FROM n`,
		`INSERT INTO project_workspace_links(id,project_id,source_workspace_id,target_workspace_id,name,enabled,created_by,created_at,updated_at) SELECT id,'p',id,'target','Source',1,'human',1,1 FROM project_workspaces WHERE id<>'target'`,
		`INSERT INTO project_library_assets(id,project_id,name,asset_type,current_version,created_at,updated_at) SELECT id,'p','Other','text/plain',1,1,1 FROM project_workspace_links`,
		`INSERT INTO project_library_asset_versions(asset_id,version,content_hash,size_bytes,mime_type,storage_uri,created_at) SELECT id,1,printf('%064x',2048),3,'text/plain','artifact:other',1 FROM project_workspace_links`,
		`INSERT INTO project_workspace_publications(id,link_id,asset_id,asset_version,content_hash,published_by,published_at) SELECT id,id,id,1,printf('%064x',2048),'human',1 FROM project_workspace_links`,
	} {
		exec(q)
	}
	query := workspaceLibraryResolverSQL(t)
	args := []any{"p", "wanted", int64(1), "target", "target", int64(2)}
	svc := NewService(db.SQL(), db, clock.Real{})
	denied := func() {
		t.Helper()
		if _, e := svc.ResolveWorkspaceLibraryVersion(ctx, "p", "target", "wanted", 1); e != ErrCrossWorkspace {
			t.Fatalf("unpublished version was readable: %v", e)
		}
	}
	assertIndexed := func() {
		t.Helper()
		plan := workspaceLibraryResolverPlan(t, db.SQL(), query, args...)
		if !regexp.MustCompile(`SEARCH pub USING (?:COVERING )?INDEX [^ ]+ \(asset_id=\? AND asset_version=\? AND content_hash=\?\)`).MatchString(plan) || !regexp.MustCompile(`SEARCH l USING (?:COVERING )?INDEX [^ ]+ \(id=\?\)`).MatchString(plan) || strings.Contains(plan, "SCAN ") || strings.Contains(plan, "TEMP B-TREE") || strings.Contains(plan, "AUTOMATIC") {
			t.Fatalf("unrelated history can dominate actual resolver:\n%s", plan)
		}
	}
	assertIndexed()
	denied()
	// Reproduce the historical negative lookup against identical rows and SQL.
	exec(`DROP INDEX idx_workspace_publications_asset_version_hash`)
	exec(`ANALYZE`)
	baseline := workspaceLibraryResolverPlan(t, db.SQL(), query, args...)
	if !strings.Contains(baseline, "SCAN pub") && !strings.Contains(baseline, "SEARCH l USING INDEX idx_project_workspace_links_expiry (project_id=? AND enabled=?)") {
		t.Fatalf("historical unrelated-history cost not reproduced:\n%s", baseline)
	}
	denied()
	exec(`CREATE INDEX idx_workspace_publications_asset_version_hash ON project_workspace_publications(asset_id,asset_version,content_hash,link_id)`)
	assertIndexed()
	exec(`ANALYZE`)
	assertIndexed()
	denied()
	exec(`INSERT INTO project_workspace_publications(id,link_id,asset_id,asset_version,content_hash,published_by,published_at) VALUES('wanted-publication','history-1','wanted',1,printf('%064x',2048),'human',1)`)
	if _, e := svc.ResolveWorkspaceLibraryVersion(ctx, "p", "target", "wanted", 1); e != nil {
		t.Fatalf("exact authorized publication denied: %v", e)
	}
	exec(`UPDATE project_workspace_publications SET content_hash=printf('%064x',4096) WHERE id='wanted-publication'`)
	denied()
	exec(`UPDATE project_workspace_publications SET content_hash=printf('%064x',2048) WHERE id='wanted-publication'`)
	exec(`UPDATE project_workspace_links SET enabled=0 WHERE id='history-1'`)
	denied()
	exec(`UPDATE project_workspace_links SET enabled=1,expires_at_ms=1 WHERE id='history-1'`)
	denied()
	exec(`INSERT INTO workspace_library_grants(id,project_workspace_id,asset_id,enabled,version_policy,permissions_json,created_at,updated_at) VALUES('direct','target','wanted',1,'pinned','{"read":true}',1,1)`)
	exec(`UPDATE workspace_library_grants SET pinned_version=1 WHERE id='direct'`)
	if _, e := svc.ResolveWorkspaceLibraryVersion(ctx, "p", "target", "wanted", 1); e != nil {
		t.Fatalf("exact direct grant denied: %v", e)
	}
	exec(`UPDATE workspace_library_grants SET enabled=0 WHERE id='direct'`)
	denied()
	// The index excludes unrelated versions/history. Multiple publications of
	// this same exact version/hash remain fan-out, not a global work bound.
}
