package api

import (
 "context"
 "database/sql"
 "encoding/json"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "testing"

 _ "modernc.org/sqlite"
)

func TestQASQLiteFootprintReadOnlyAndNoPaths(t *testing.T){
 path:=filepath.Join(t.TempDir(),"PRIVATE_DB_PATH_CANARY.db")
 db,e:=sql.Open("sqlite",path);if e!=nil{t.Fatal(e)}
 defer db.Close()
 for _,q:=range []string{
  "PRAGMA journal_mode=WAL",
  "CREATE TABLE footprint_example(id INTEGER PRIMARY KEY, token TEXT)",
  "INSERT INTO footprint_example(token) VALUES('PRIVATE_TOKEN_CANARY')",
 }{
  if _,e:=db.Exec(q);e!=nil{t.Fatal(e)}
 }
 before,err:=os.Stat(path);if err!=nil{t.Fatal(err)}
 got,e:=qaReadSQLiteFootprint(context.Background(),db)
 if e!=nil{t.Fatal(e)}
 if !got.DatabaseHealthy||!got.ReadOnly||got.AutoPruneEnabled||
  got.DatabaseBytes<1||got.PageSizeBytes<512||got.PageCount<1||
  got.FreePageCount>got.PageCount||got.AllocationBytes<got.PageSizeBytes{
  t.Fatalf("misleading SQLite footprint: %+v",got)
 }
 raw,_:=json.Marshal(got)
 for _,secret:=range []string{path,"PRIVATE_DB_PATH_CANARY","PRIVATE_TOKEN_CANARY",
  "journal_mode","sqlite_master","pragma_database_list"}{
  if strings.Contains(string(raw),secret){t.Fatalf("leak %q: %s",secret,raw)}
 }
 after,err:=os.Stat(path);if err!=nil{t.Fatal(err)}
 if after.Size()!=before.Size(){t.Fatal("footprint mutated database length")}
 if _,err:=db.Exec("INSERT INTO footprint_example(token) VALUES('still_writable')");
  err!=nil{t.Fatal(err)}
 var n int
 if err:=db.QueryRow("SELECT COUNT(*) FROM footprint_example").Scan(&n);
  err!=nil||n!=2{t.Fatalf("footprint blocked writers: %d %v",n,err)}
}

func TestQASQLiteFootprintMissingAndClosedFailClosed(t *testing.T){
 if _,err:=qaReadSQLiteFootprint(context.Background(),nil);err==nil{
  t.Fatal("nil DB reported available")
 }
 db:=qaReadinessTestDB(t)
 _=db.Close()
 if _,err:=qaReadSQLiteFootprint(context.Background(),db);err==nil{
  t.Fatal("closed DB reported available")
 }
}

func TestQASQLiteFootprintHTTPRequiresNodeOperator(t *testing.T){
 db:=qaReadinessTestDB(t);defer db.Close()
 s:=NewServer(nil,nil,qaCaptureTestAuth{})
 s.SetFederation(&qaCaptureTestFederation{})
 s.SetAttentionDB(db)
 for _,tc:=range []struct{name,bearer string;code int}{
  {"anonymous","",401},{"outsider","outsider",403},{"operator","adminA",200},
 }{
  t.Run(tc.name,func(t *testing.T){
   req:=httptest.NewRequest("GET","/v1/qa/sqlite-footprint",nil)
   if tc.bearer!=""{req.Header.Set("Authorization","Bearer "+tc.bearer)}
   rw:=httptest.NewRecorder()
   s.Handler().ServeHTTP(rw,req)
   if rw.Code!=tc.code{t.Fatalf("want %d got %d: %s",tc.code,rw.Code,rw.Body)}
   if rw.Code==200{
    if rw.Header().Get("Cache-Control")!="no-store"||
     !strings.Contains(rw.Body.String(),"\"scope\":\"canonical_local_database\""){
     t.Fatalf("unsafe operator response: %s",rw.Body)
    }
    for _,secret:=range []string{"readiness.db","PRIVATE_NODE_IDENTITY_CANARY","local-fp"}{
     if strings.Contains(rw.Body.String(),secret){t.Fatalf("leak: %s",secret)}
    }
   }
  })
 }
 s.SetAttentionDB(nil)
 req:=httptest.NewRequest("GET","/v1/qa/sqlite-footprint",nil)
 req.Header.Set("Authorization","Bearer adminA")
 rw:=httptest.NewRecorder();s.Handler().ServeHTTP(rw,req)
 if rw.Code!=503{t.Fatalf("missing canonical DB not blocked: %d",rw.Code)}
}
