package sqlite

import (
 "context"
 "path/filepath"
 "strings"
 "testing"
)

// The fit-level constraint must not force operators to falsify an actual
// too-tight estimate as marginal just to request an experimental install.
func TestOperatorOverrideMigrationKeepsHonestFitAndForeignKeys(t *testing.T) {
 db,err:=Open(filepath.Join(t.TempDir(),"onepane-rc3.sqlite"))
 if err!=nil {t.Fatal(err)}
 defer db.Close()
 if err:=db.Migrate(context.Background());err!=nil {t.Fatalf("migrate RC3 schema: %v",err)}
 var ddl string
 if err:=db.SQL().QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='local_model_install_plans'").Scan(&ddl);err!=nil {t.Fatal(err)}
 if !strings.Contains(ddl,"'too_tight'") {t.Fatalf("RC3 plan constraint doesn't preserve too-tight estimates: %s",ddl)}
 if strings.Contains(ddl,"local_model_install_plans_rebuilt") {t.Fatal("temporary table name leaked into durable schema")}
 var n int
 if err:=db.SQL().QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version=35").Scan(&n);err!=nil||n!=1 {t.Fatalf("RC3 migration missing: %d, %v",n,err)}
 rows,err:=db.SQL().Query("PRAGMA foreign_key_check")
 if err!=nil {t.Fatal(err)}
 defer rows.Close()
 if rows.Next(){t.Fatal("foreign key violation after RC3 plan migration")}
 if err:=rows.Err();err!=nil {t.Fatal(err)}
 var integrity string
 if err:=db.SQL().QueryRow("PRAGMA quick_check").Scan(&integrity);err!=nil||integrity!="ok" {t.Fatalf("post-migrate quick_check=%s: %v",integrity,err)}
}
