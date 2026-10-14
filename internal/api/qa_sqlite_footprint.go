package api

import (
 "context"
 "database/sql"
 "errors"
 "net/http"
 "os"
 "time"
)

// Node-admin-only point-in-time accounting for the canonical local DB.
// Global SQLite bytes must not be exposed through a Project-reader endpoint.
type qaSQLiteFootprint struct {
 Schema string `json:"schema"`
 Scope string `json:"scope"`
 ObservedAtMS int64 `json:"observed_at_ms"`
 DatabaseBytes int64 `json:"database_bytes"`
 WALBytes int64 `json:"wal_bytes"`
 SHMBytes int64 `json:"shared_memory_bytes"`
 PageSizeBytes int64 `json:"page_size_bytes"`
 PageCount int64 `json:"page_count"`
 FreePageCount int64 `json:"freelist_pages"`
 AllocationBytes int64 `json:"page_allocation_bytes"`
 DatabaseHealthy bool `json:"database_healthy"`
 ReadOnly bool `json:"read_only"`
 AutoPruneEnabled bool `json:"automatic_pruning_enabled"`
 Limitations []string `json:"limitations"`
}

// No checkpoint, VACUUM, deletion, history prune, export or network action.
// Filenames remain strictly private to the server process.
func qaReadSQLiteFootprint(ctx context.Context,db *sql.DB)(qaSQLiteFootprint,error){
 if db==nil{return qaSQLiteFootprint{},errors.New("canonical SQLite unavailable")}
 out:=qaSQLiteFootprint{
  Schema:"onepane.qa.sqlite_footprint.v1",Scope:"canonical_local_database",
  ObservedAtMS:time.Now().UTC().UnixMilli(),ReadOnly:true,
  Limitations:[]string{
   "point-in-time local file lengths are not a durable disk quota or backup",
   "counts and lengths can change while the database is writing",
   "global database storage cannot be attributed to one Project or Workspace",
   "no automatic checkpoint, VACUUM, deletion or remediation was run",
   "filesystem free space and physical device health were not attested",
  },
 }
 rows,err:=db.QueryContext(ctx,"PRAGMA database_list")
 if err!=nil{return qaSQLiteFootprint{},err}
 var path string
 for rows.Next(){
  var seq int64
  var name,filename string
  if err=rows.Scan(&seq,&name,&filename);err!=nil{break}
  if name=="main"{path=filename;break}
 }
 closeErr:=rows.Close()
 if err!=nil{return qaSQLiteFootprint{},err}
 if closeErr!=nil{return qaSQLiteFootprint{},closeErr}
 if path==""{return qaSQLiteFootprint{},errors.New("canonical SQLite not file-backed")}
 for _,q:=range []struct{sql string;value *int64}{
  {"PRAGMA page_size",&out.PageSizeBytes},
  {"PRAGMA page_count",&out.PageCount},
  {"PRAGMA freelist_count",&out.FreePageCount},
 }{
  if err=db.QueryRowContext(ctx,q.sql).Scan(q.value);err!=nil{return qaSQLiteFootprint{},err}
 }
 if out.PageSizeBytes<1||out.PageCount<0||
  out.FreePageCount<0||out.FreePageCount>out.PageCount||
  out.PageCount>(1<<40)/out.PageSizeBytes{
  return qaSQLiteFootprint{},errors.New("invalid SQLite page metrics")
 }
 out.AllocationBytes=out.PageSizeBytes*out.PageCount
 for _,slot:=range []struct{suffix string;value *int64}{
  {"",&out.DatabaseBytes},
  {"-wal",&out.WALBytes},
  {"-shm",&out.SHMBytes},
 }{
  st,e:=os.Stat(path+slot.suffix)
  if errors.Is(e,os.ErrNotExist)&&slot.suffix!=""{continue}
  if e!=nil{return qaSQLiteFootprint{},e}
  if !st.Mode().IsRegular(){return qaSQLiteFootprint{},errors.New("SQLite storage not regular")}
  *slot.value=st.Size()
 }
 // Readable page metadata is not a full integrity check.
 out.DatabaseHealthy=true
 return out,nil
}

func(s *Server)qaSQLiteFootprintHandler(w http.ResponseWriter,r *http.Request){
 identity,ok:=s.requireNodeOperator(w,r);if !ok{return}
 // A privileged Agent should not receive global SQLite data merely
 // because a role or downstream federation checker was misconfigured.
 if identity.PrincipalType!="human"{
  writeError(w,http.StatusForbidden,"human Node operator required")
  return
 }
 if s.attentionDB==nil{
  writeError(w,http.StatusServiceUnavailable,"SQLite diagnostics unavailable")
  return
 }
 ctx,cancel:=context.WithTimeout(r.Context(),1500*time.Millisecond)
 defer cancel()
 view,err:=qaReadSQLiteFootprint(ctx,s.attentionDB)
 if err!=nil{
  writeError(w,http.StatusServiceUnavailable,"SQLite footprint unavailable")
  return
 }
 w.Header().Set("Cache-Control","no-store")
 writeJSON(w,http.StatusOK,view)
}
