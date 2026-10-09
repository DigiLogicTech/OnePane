package api

import (
 "archive/zip"
 "bytes"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "io"
 "strings"
 "testing"
 "time"

 "github.com/DigiLogicTech/OnePane/internal/task"
)

func TestQASnapshotIsStrictlyAllowlistedAndBundleIsBounded(t *testing.T){
 now:=time.Date(2026,time.October,10,2,3,4,0,time.UTC)
 secret:="VERY_SECRET_BEARER_CANARY"
 project,workspace:="project","world"
 rows:=[]task.Task{{
  ID:"task-safe",WorkspaceID:"tenant",ProjectID:&project,ProjectWorkspaceID:&workspace,
  Objective:"private task instructions "+secret,State:task.StateBlocked,Revision:12,
  UpdatedAt:1001,Completion:json.RawMessage(`{"authorization":"VERY_SECRET_BEARER_CANARY"}`),
  Result:json.RawMessage(`{"password":"VERY_SECRET_BEARER_CANARY"}`),
 },{
  ID:"url?token="+secret,WorkspaceID:"tenant",ProjectID:&project,ProjectWorkspaceID:&workspace,
  State:task.State("running-"+secret),Revision:4,
 }}
 progress:=map[string]taskExecutionProgress{
  "task-safe":{
   RunID:"run-safe",Status:"interrupted",StepsUsed:3,MaxSteps:10,
   LastStepKind:"tool-output-"+secret,LastStepStatus:"unknown",ReviewRequired:true,
  },
  rows[1].ID:{RunID:"Bearer "+secret,Status:"status-"+secret,StepsUsed:1,MaxSteps:2},
 }
 dependencies:=map[string]taskDependencyEvidence{"task-safe":{
  Total:3,Completed:1,Failed:1,Blocked:0,Restricted:1,Remaining:2,
 }}
 snap:=makeQASnapshot(now,rows,progress,dependencies)
 if snap.CapturedTasks!=2||snap.Truncated||len(snap.Tasks)!=2{
  t.Fatalf("invalid snapshot inventory: %+v",snap)
 }
 if snap.Tasks[0].State!="blocked"||snap.Tasks[0].Execution==nil||
   snap.Tasks[0].Execution.LastStepKind!="unavailable"||
   !snap.Tasks[0].Execution.ReviewRequired{
   t.Fatalf("unsafe or missing Worker projection: %+v",snap.Tasks[0])
 }
 if snap.Tasks[1].ID!="unavailable"||snap.Tasks[1].State!="unavailable"||
   snap.Tasks[1].Execution.RunID!="unavailable"||
   snap.Tasks[1].Execution.Status!="unavailable"{
  t.Fatalf("unknown DB text escaped sanitisation: %+v",snap.Tasks[1])
 }
 raw,err:=json.Marshal(snap)
 if err!=nil{t.Fatal(err)}
 if bytes.Contains(raw,[]byte(secret)){t.Fatal("secret leaked in preview")}
 b,err:=qaBundle(snap)
 if err!=nil{t.Fatal(err)}
 if len(b)>qaSnapshotArchiveCap{t.Fatalf("bundle exceeded cap: %d",len(b))}
 if bytes.Contains(b,[]byte(secret)){t.Fatal("secret leaked in compressed bytes")}
 z,err:=zip.NewReader(bytes.NewReader(b),int64(len(b)))
 if err!=nil{t.Fatal(err)}
 if len(z.File)!=3{t.Fatalf("expected three fixed archive files, got %d",len(z.File))}
 contents:=map[string][]byte{}
 for _,f:=range z.File{
  if strings.ContainsAny(f.Name,"/\\"){t.Fatalf("nonfixed archive path: %s",f.Name)}
  rd,err:=f.Open()
  if err!=nil{t.Fatal(err)}
  data,err:=io.ReadAll(io.LimitReader(rd,qaSnapshotArchiveCap+1))
  _=rd.Close()
  if err!=nil{t.Fatal(err)}
  if bytes.Contains(data,[]byte(secret)){t.Fatalf("secret leaked in ZIP entry %s",f.Name)}
  contents[f.Name]=data
 }
 var manifest qaBundleManifest
 if err:=json.Unmarshal(contents["manifest.json"],&manifest);err!=nil{t.Fatal(err)}
 if manifest.SchemaVersion!=qaSnapshotVersion||len(manifest.Contents)!=2 {
  t.Fatalf("unexpected report manifest: %+v",manifest)
 }
 for name,expected:=range manifest.Contents {
  h:=sha256.Sum256(contents[name])
  if hex.EncodeToString(h[:])!=expected{t.Fatalf("bad ZIP SHA-256 for %s",name)}
 }
 if !bytes.Equal(contents["snapshot.json"],raw) {
  // MarshalIndent is allowed to differ from Marshal, but decoded structures must match.
  var got qaSnapshot
  if err:=json.Unmarshal(contents["snapshot.json"],&got);err!=nil{t.Fatal(err)}
  if got.CapturedTasks!=snap.CapturedTasks||got.Tasks[0].ID!=snap.Tasks[0].ID{
   t.Fatal("ZIP does not contain previewed snapshot")
  }
 }
}

func TestQASnapshotCapsRowsRejectsBadCountsAndNeverCopiesTaskBodies(t *testing.T){
 token:="SECRET_SHOULD_NOT_BE_VISIBLE"
 rows:=make([]task.Task,qaSnapshotTaskCap+1)
 for idx:=range rows{
  rows[idx]=task.Task{
   ID:"safe-task",Objective:token,Completion:json.RawMessage(`{"key":"SECRET_SHOULD_NOT_BE_VISIBLE"}`),
   State:task.StateRunning,
  }
 }
 out:=makeQASnapshot(time.Unix(0,0),rows,map[string]taskExecutionProgress{
  "safe-task":{RunID:token,Status:"running",StepsUsed:999,MaxSteps:4},
 },map[string]taskDependencyEvidence{
  "safe-task":{Total:2,Completed:3,Remaining:-1},
 })
 if !out.Truncated||out.CapturedTasks!=qaSnapshotTaskCap||out.MaxTasks!=qaSnapshotTaskCap{
  t.Fatalf("unbounded or incorrectly counted Task inventory: %+v",out)
 }
 if out.Tasks[0].Execution!=nil||out.Tasks[0].Dependencies!=nil{
  t.Fatal("invalid diagnostic counters were exported")
 }
 b,err:=qaBundle(out)
 if err!=nil{t.Fatal(err)}
 z,err:=zip.NewReader(bytes.NewReader(b),int64(len(b)))
 if err!=nil{t.Fatal(err)}
 for _,f:=range z.File{
  rd,err:=f.Open();if err!=nil{t.Fatal(err)}
  x,err:=io.ReadAll(rd);_=rd.Close();if err!=nil{t.Fatal(err)}
  if bytes.Contains(x,[]byte(token)){t.Fatalf("unallowlisted Task body leaked in %s",f.Name)}
 }
}
