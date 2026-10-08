package sqlite

import (
 "context"
 "database/sql"
 "path/filepath"
 "testing"
)

func TestManualWebChairMigrationCreatesAuditableTurnSchema(t *testing.T){
 db,err:=Open(filepath.Join(t.TempDir(),"chair-upgrade.sqlite"));if err!=nil{t.Fatal(err)}
 defer db.Close()
 if err:=db.Migrate(context.Background());err!=nil{t.Fatalf("migrate Chair schema: %v",err)}
 for _,name:=range []string{"manual_web_council_chair_turns","manual_web_council_turns","team_session_manifests"}{
  var actual string
  if err:=db.SQL().QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`,name).Scan(&actual);err!=nil{
   t.Fatalf("expected %s: %v",name,err)
  }
 }
 var version int
 if err:=db.SQL().QueryRow(`SELECT version FROM schema_migrations WHERE version=34`).Scan(&version);err!=nil{
  t.Fatalf("Chair migration was not recorded: %v",err)
 }
 columns:=map[string]bool{}
 rows,err:=db.SQL().Query(`PRAGMA table_info(manual_web_council_chair_turns)`);if err!=nil{t.Fatal(err)}
 for rows.Next(){
  var seq,notnull,pk int
  var name,typ string
  var defaultValue sql.NullString
  if err:=rows.Scan(&seq,&name,&typ,&notnull,&defaultValue,&pk);err!=nil{t.Fatal(err)}
  columns[name]=true
 }
 if err:=rows.Err();err!=nil{t.Fatal(err)}
 rows.Close()
 for _,col:=range []string{"session_id","member_id","stage","after_round","prompt_sha256","response_sha256","approved_sha256","approved_by","status"}{
  if !columns[col]{t.Fatalf("durable Chair schema missing %s",col)}
 }
}
