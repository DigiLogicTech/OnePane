package api

import (
 "context"
 "database/sql"
 "encoding/json"
 "path/filepath"
 "strings"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/task"
 _ "modernc.org/sqlite"
)

func TestQATimelineScopesTaskAndWorkerEventsWithoutPayloadLeakage(t *testing.T){
 ctx:=context.Background()
 db,err:=sql.Open("sqlite",filepath.Join(t.TempDir(),"events.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 statements:=[]string{
  `CREATE TABLE tasks(id TEXT PRIMARY KEY,workspace_id TEXT,project_id TEXT,project_workspace_id TEXT)`,
  `CREATE TABLE agent_worker_runs(id TEXT PRIMARY KEY,workspace_id TEXT,task_id TEXT)`,
  `CREATE TABLE events(sequence INTEGER PRIMARY KEY,id TEXT,workspace_id TEXT,event_type TEXT,
   aggregate_type TEXT,aggregate_id TEXT,request_id TEXT,trace_id TEXT,payload_json TEXT,occurred_at INTEGER)`,
  `INSERT INTO tasks VALUES ('world','tenant','game','world'),('story','tenant','game','story'),
   ('another-project','tenant','different','world'),('other-tenant','elsewhere','game','world')`,
  `INSERT INTO agent_worker_runs VALUES
   ('run-world','tenant','world'),('run-story','tenant','story'),('run-other','elsewhere','world')`,
  `INSERT INTO events VALUES
   (1,'e1','tenant','task.created','task','world','request-s1','trace-s1','{"secret":"VERY_SECRET_CANARY"}',100),
   (2,'e2','tenant','agent_worker.started','agent_worker_run','run-world',NULL,'trace-s1','{"token":"VERY_SECRET_CANARY"}',110),
   (3,'e3','tenant','agent_worker.step','agent_worker_run','run-world','request-s1','trace-s1','{"message":"VERY_SECRET_CANARY"}',120),
   (4,'e4','tenant','task.failed','task','world','VERY_SECRET_CANARY',NULL,'{"secret":"VERY_SECRET_CANARY"}',130),
   (5,'e5','tenant','task.failed','task','story','request-story','trace-story','{"secret":"VERY_SECRET_CANARY"}',140),
   (6,'e6','tenant','task.failed','task','another-project',NULL,NULL,'{}',150),
   (7,'e7','elsewhere','task.failed','task','other-tenant',NULL,NULL,'{}',160),
   (8,'e8','tenant','agent_worker.step','agent_worker_run','run-story',NULL,NULL,'{}',170),
   (9,'e9','elsewhere','agent_worker.step','agent_worker_run','run-other',NULL,NULL,'{}',180),
   (10,'e10','tenant','task.VERY_SECRET_CANARY','task','world',NULL,NULL,'{}',190),
   (11,'e11','tenant','task.blocked','task','world','secret=VERY_SECRET_CANARY','trace-s1','{}',200)`,
 }
 for _,q:=range statements{if _,err:=db.ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 project,world,story,otherTenant:="game","world","story","elsewhere"
 visible:=[]task.Task{
  {ID:"world",WorkspaceID:"tenant",ProjectID:&project,ProjectWorkspaceID:&world},
  {ID:"story",WorkspaceID:"tenant",ProjectID:&project,ProjectWorkspaceID:&story},
  {ID:"other-tenant",WorkspaceID:otherTenant,ProjectID:&project,ProjectWorkspaceID:&world},
 }
 result,truncated,err:=loadQATimeline(ctx,db,"tenant",project,world,visible)
 if err!=nil{t.Fatal(err)}
 if truncated||len(result)!=5{t.Fatalf("expected 5 observed World events, got %d truncated=%t: %+v",len(result),truncated,result)}
 expected:=[]string{"task.blocked","task.failed","agent_worker.step","agent_worker.started","task.created"}
 for i,ev:=range result{
  if ev.EventType!=expected[i]{t.Fatalf("unexpected event order or scope: %+v",result)}
  if ev.TaskRef!=qaOpaqueRef("task","world"){t.Fatalf("Worker/Task event not linked to authorised Task: %+v",ev)}
  if strings.Contains(ev.TraceRef,"trace-s1")||strings.Contains(ev.RequestRef,"request-s1"){
   t.Fatalf("raw trace/request values leaked: %+v",ev)
  }
  if strings.Contains(ev.EventRef,"e1"){t.Fatalf("raw event ID leaked: %+v",ev)}
 }
 if result[2].RunRef!=qaOpaqueRef("run","run-world")||result[2].TraceRef!=result[4].TraceRef{
  t.Fatalf("Worker and Task trace must correlate by stable pseudonyms: %+v",result)
 }
 raw,err:=json.Marshal(result)
 if err!=nil{t.Fatal(err)}
 for _,s:=range []string{"VERY_SECRET_CANARY","request-s1","trace-s1","run-world","run-story","another-project","other-tenant","task.VERY_SECRET_CANARY"} {
  if strings.Contains(string(raw),s){t.Fatalf("private or untrusted source %q leaked: %s",s,raw)}
 }
 wrong,_,err:=loadQATimeline(ctx,db,"tenant",project,world,[]task.Task{
  {ID:"story",WorkspaceID:"tenant",ProjectID:&project,ProjectWorkspaceID:&world},
 })
 if err!=nil||len(wrong)!=0{t.Fatalf("forged visible World Task identity leaked Story events: %+v %v",wrong,err)}
 none,_,err:=loadQATimeline(ctx,db,"tenant",project,"",visible)
 if err!=nil||len(none)!=0{t.Fatalf("missing canonical Workspace must fail closed: %+v %v",none,err)}
}

func TestQATimelineBoundedWithoutRawEventDescriptions(t *testing.T){
 ctx:=context.Background()
 db,err:=sql.Open("sqlite",filepath.Join(t.TempDir(),"bounded-events.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 for _,q:=range []string{
  `CREATE TABLE tasks(id TEXT PRIMARY KEY,workspace_id TEXT,project_id TEXT,project_workspace_id TEXT)`,
  `CREATE TABLE agent_worker_runs(id TEXT PRIMARY KEY,workspace_id TEXT,task_id TEXT)`,
  `CREATE TABLE events(sequence INTEGER PRIMARY KEY,id TEXT,workspace_id TEXT,event_type TEXT,
   aggregate_type TEXT,aggregate_id TEXT,request_id TEXT,trace_id TEXT,payload_json TEXT,occurred_at INTEGER)`,
  `INSERT INTO tasks VALUES('world','tenant','game','world')`,
 }{if _,err:=db.ExecContext(ctx,q);err!=nil{t.Fatal(err)}}
 for i:=1;i<=qaTimelineEventCap+8;i++{
  if _,err:=db.ExecContext(ctx,`INSERT INTO events(id,workspace_id,event_type,aggregate_type,aggregate_id,payload_json,occurred_at)
    VALUES(?,'tenant','task.ready','task','world',?,?)`,
    "event-"+time.Unix(int64(i),0).UTC().Format(time.RFC3339),`{"raw":"CONFIDENTIAL"}`,i);err!=nil{t.Fatal(err)}
 }
 project,world:="game","world"
 v:=[]task.Task{{ID:"world",WorkspaceID:"tenant",ProjectID:&project,ProjectWorkspaceID:&world}}
 got,truncated,err:=loadQATimeline(ctx,db,"tenant",project,world,v)
 if err!=nil{t.Fatal(err)}
 if len(got)!=qaTimelineEventCap||!truncated{t.Fatalf("bounded timeline count=%d truncated=%t",len(got),truncated)}
 if got[0].OccurredAt!=int64(qaTimelineEventCap+8){t.Fatal("newest event not first")}
 s,err:=json.Marshal(got)
 if err!=nil{t.Fatal(err)}
 if strings.Contains(string(s),"CONFIDENTIAL"){t.Fatal("event payload escaped allowlist")}
}
