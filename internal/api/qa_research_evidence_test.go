package api

import (
 "context"
 "database/sql"
 "encoding/json"
 "path/filepath"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/task"
 _ "modernc.org/sqlite"
)

func TestQAResearchEvidenceOnlyExposesBoundedScopedApprovalState(t *testing.T){
 ctx:=context.Background()
 db,err:=sql.Open("sqlite",filepath.Join(t.TempDir(),"research-qa.db"))
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 for _,sqlText:=range []string{
  `CREATE TABLE tasks(id TEXT,workspace_id TEXT,project_id TEXT,project_workspace_id TEXT)`,
  `CREATE TABLE team_sessions(id TEXT,workspace_id TEXT,task_id TEXT,round_number INTEGER)`,
  `CREATE TABLE team_session_manifests(session_id TEXT,research_mode INTEGER,snapshot_sha256 TEXT)`,
  `CREATE TABLE research_session_evidence_metadata(
   session_id TEXT,workspace_id TEXT,project_id TEXT,project_workspace_id TEXT,
   entries_json TEXT,research_manifest_sha256 TEXT,evidence_manifest_sha256 TEXT,
   approved_at INTEGER)`,
  `INSERT INTO tasks VALUES('task-world','tenant','project','world')`,
  `INSERT INTO tasks VALUES('task-other','tenant','project','other')`,
  `INSERT INTO team_sessions VALUES('session-world','tenant','task-world',1)`,
  `INSERT INTO team_sessions VALUES('session-other','tenant','task-other',0)`,
 }{
  if _,err=db.ExecContext(ctx,sqlText);err!=nil{t.Fatal(err)}
 }
 digest:=strings.Repeat("a",64)
 for _,id:=range []string{"session-world","session-other"}{
  if _,err=db.ExecContext(ctx,`INSERT INTO team_session_manifests
   VALUES(?,1,?)`,id,digest);err!=nil{t.Fatal(err)}
 }
 for _,entry:=range []struct{session,ws string}{
  {"session-world","world"},{"session-other","other"},
 }{
  if _,err=db.ExecContext(ctx,`INSERT INTO research_session_evidence_metadata
    VALUES(?,?,?,?,?,?,?,1000)`,entry.session,"tenant","project",entry.ws,
    `[{"private_file":"do-not-export"}]`,digest,"sha256:"+digest);
   err!=nil{t.Fatal(err)}
 }
 project,world:="project","world"
 visible:=[]task.Task{
  {ID:"task-world",WorkspaceID:"tenant",ProjectID:&project,ProjectWorkspaceID:&world},
  {ID:"task-other",WorkspaceID:"tenant",ProjectID:&project,ProjectWorkspaceID:&world},
 }
 got,truncated,err:=loadQAResearchEvidence(ctx,db,"tenant","project","world",visible)
 if err!=nil||truncated||len(got)!=1||got[0].TaskRef!="task-world"||
  got[0].SessionRef!="session-world"||got[0].VersionCount!=1||
  got[0].ApprovalState!="metadata_approval_only"||
  !got[0].ResearchManifestRecorded||!got[0].EvidenceManifestRecorded||
  !got[0].RoundStarted{
  t.Fatalf("Research evidence scope drift or false provenance: %+v %v",got,err)
 }
 raw,_:=json.Marshal(got)
 if strings.Contains(string(raw),"private_file")||
  strings.Contains(string(raw),"do-not-export")||
  strings.Contains(string(raw),"sha256:aaaa")||
  strings.Contains(string(raw),digest){
  t.Fatalf("QA snapshot leaked private evidence provenance: %s",raw)
 }
 denied,_,err:=loadQAResearchEvidence(ctx,db,"tenant","project","other",
  []task.Task{{ID:"task-world",WorkspaceID:"tenant",ProjectID:&project,ProjectWorkspaceID:&world}})
 if err!=nil||len(denied)!=0{t.Fatalf("cross-Workspace QA scope leaked: %+v %v",denied,err)}
}
