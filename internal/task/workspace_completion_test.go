package task

import (
 "context"
 "encoding/json"
 "errors"
 "testing"
)

func TestWorkspaceTaskCreationPersistsLocalFirstBrokeredPolicy(t *testing.T) {
 service,repo,events,jobs:=newTestService(1700000000000)
 projectID, workspaceID:="project-1","world-1"
 original:=json.RawMessage(`{"type":"operator_review","onepane_routing":{"agent_profile":"agent.md","candidate_id":"local-llama"}}`)
 got,err:=service.Create(context.Background(),CreateCommand{
  WorkspaceID:"tenant",ProjectID:&projectID,ProjectWorkspaceID:&workspaceID,
  Objective:"Build the world",Completion:original,
 })
 if err!=nil{t.Fatal(err)}
 if len(events.events)!=1||len(jobs.jobs)!=1||len(repo.tasks)!=1{
  t.Fatalf("Task was not durably queued once: events=%d jobs=%d tasks=%d",len(events.events),len(jobs.jobs),len(repo.tasks))
 }
 var envelope struct{
  Type string `json:"type"`
  OnePaneRouting struct{
   AgentProfile string `json:"agent_profile"`
   CandidateID string `json:"candidate_id"`
   ProjectWorkspaceID string `json:"project_workspace_id"`
   WorkspaceAccess struct{
    Mode string `json:"mode"`
    ProjectWorkspaceID string `json:"project_workspace_id"`
    RemoteModels *bool `json:"remote_models"`
    Filesystem string `json:"filesystem"`
    Internet bool `json:"internet"`
    LAN bool `json:"lan"`
    Browser bool `json:"browser"`
    Computer bool `json:"computer"`
    Secrets string `json:"secrets"`
   } `json:"workspace_access"`
  } `json:"onepane_routing"`
 }
 if err:=json.Unmarshal(got.Completion,&envelope);err!=nil{t.Fatal(err)}
 access:=envelope.OnePaneRouting.WorkspaceAccess
 if envelope.Type!="operator_review"||
  envelope.OnePaneRouting.AgentProfile!="agent.md"||
  envelope.OnePaneRouting.CandidateID!="local-llama"||
  envelope.OnePaneRouting.ProjectWorkspaceID!=workspaceID||
  access.ProjectWorkspaceID!=workspaceID||access.Mode!="brokered"||
  access.RemoteModels==nil||*access.RemoteModels||
  access.Filesystem!="workspace-only"||
  access.Internet||access.LAN||access.Browser||access.Computer||access.Secrets!="none"{
  t.Fatalf("unsafe or incomplete persisted Workspace completion: %s",got.Completion)
 }
 if string(original)==string(got.Completion){t.Fatal("Workspace policy not normalised")}
}

func TestWorkspaceTaskExplicitCloudOptInStillRequiresBrokeredScope(t *testing.T) {
 service,_,_,_:=newTestService(1700000000000)
 pid,wid:="project-1","world-1"
 got,err:=service.Create(context.Background(),CreateCommand{
  WorkspaceID:"tenant",ProjectID:&pid,ProjectWorkspaceID:&wid,Objective:"Cloud-authorised Task",
  Completion:json.RawMessage(`{"onepane_routing":{"workspace_access":{"remote_models":true}}}`),
 })
 if err!=nil{t.Fatal(err)}
 var envelope struct{OnePaneRouting struct{WorkspaceAccess struct{
  RemoteModels bool `json:"remote_models"`
  Internet bool `json:"internet"`
  Secrets string `json:"secrets"`
 } `json:"workspace_access"`} `json:"onepane_routing"`}
 if err:=json.Unmarshal(got.Completion,&envelope);err!=nil{t.Fatal(err)}
 a:=envelope.OnePaneRouting.WorkspaceAccess
 if !a.RemoteModels||a.Internet||a.Secrets!="none"{t.Fatalf("cloud opt-in widened other permissions: %s",got.Completion)}
}

func TestWorkspaceTaskNeverPersistsPrivilegeEscalationOrForeignScope(t *testing.T) {
 project,world:="project-1","world-1"
 cases:=[]struct{name string;completion string;projectID,workspaceID *string}{
  {"missing_project",`{}`,nil,&world},
  {"foreign_route",`{"onepane_routing":{"project_workspace_id":"other"}}`,&project,&world},
  {"foreign_access",`{"onepane_routing":{"workspace_access":{"project_workspace_id":"other"}}}`,&project,&world},
  {"direct_mode",`{"onepane_routing":{"workspace_access":{"mode":"direct"}}}`,&project,&world},
  {"host_filesystem",`{"onepane_routing":{"workspace_access":{"filesystem":"host"}}}`,&project,&world},
  {"internet",`{"onepane_routing":{"workspace_access":{"internet":true}}}`,&project,&world},
  {"browser",`{"onepane_routing":{"workspace_access":{"browser":true}}}`,&project,&world},
  {"lan",`{"onepane_routing":{"workspace_access":{"lan":true}}}`,&project,&world},
  {"computer",`{"onepane_routing":{"workspace_access":{"computer":true}}}`,&project,&world},
  {"vault",`{"onepane_routing":{"workspace_access":{"secrets":"all"}}}`,&project,&world},
  {"wrong_remote_type",`{"onepane_routing":{"workspace_access":{"remote_models":"true"}}}`,&project,&world},
  {"bad_routing_type",`{"onepane_routing":"brokered"}`,&project,&world},
  {"bad_access_type",`{"onepane_routing":{"workspace_access":"all"}}`,&project,&world},
  {"scalar",`"ok"`,&project,&world},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   svc,repo,events,jobs:=newTestService(1700000000000)
   _,err:=svc.Create(context.Background(),CreateCommand{
    WorkspaceID:"tenant",ProjectID:tc.projectID,ProjectWorkspaceID:tc.workspaceID,
    Objective:"Malicious scope",Completion:json.RawMessage(tc.completion),
   })
   if !errors.Is(err,ErrInvalidCommand){t.Fatalf("expected domain denial, got %v",err)}
   if len(repo.tasks)!=0||len(events.events)!=0||len(jobs.jobs)!=0{
    t.Fatalf("denied Task partially persisted: tasks=%d events=%d jobs=%d",len(repo.tasks),len(events.events),len(jobs.jobs))
   }
  })
 }
}

func TestLegacyProjectTasksKeepExistingCompletionSemantics(t *testing.T) {
 svc,_,_,_:=newTestService(1700000000000)
 original:=json.RawMessage(`{"type":"operator_review","onepane_routing":{"candidate_id":"cloud-1"}}`)
 got,err:=svc.Create(context.Background(),CreateCommand{
  WorkspaceID:"tenant",Objective:"Legacy maintenance",Completion:original,
 })
 if err!=nil{t.Fatal(err)}
 if string(got.Completion)!=string(original){
  t.Fatalf("legacy Task was rewritten: got %s",got.Completion)
 }
}

func TestWorkspaceTaskComputePlacementIsPinnedAndValidated(t *testing.T){
 pid,wid:="project","cpu-workspace"
 for _,place:=range []string{"auto","prefer_cpu","cpu_only","prefer_gpu","gpu_only"}{
  svc,_,_,_:=newTestService(1700000000000)
  input,_:=json.Marshal(map[string]any{
   "onepane_routing":map[string]any{"compute_preference":place},
  })
  created,err:=svc.Create(context.Background(),CreateCommand{
   WorkspaceID:"tenant",ProjectID:&pid,ProjectWorkspaceID:&wid,
   Objective:"Run on approved placement",Completion:input,
  })
  if err!=nil{t.Fatalf("valid placement %s rejected: %v",place,err)}
  var body struct{Routing struct{
   Compute string `json:"compute_preference"`
   Access struct{ Remote bool `json:"remote_models"` } `json:"workspace_access"`
  } `json:"onepane_routing"`}
  if err=json.Unmarshal(created.Completion,&body);err!=nil||
   body.Routing.Compute!=place||body.Routing.Access.Remote{
   t.Fatalf("placement changed remote authority: %s %v",created.Completion,err)
  }
 }
 for _,raw:=range []string{
  `{"onepane_routing":{"compute_preference":"cloud_only"}}`,
  `{"onepane_routing":{"compute_preference":123}}`,
  `{"onepane_routing":{"compute_preference":null}}`,
 }{
  svc,_,_,_:=newTestService(1700000000000)
  if _,err:=svc.Create(context.Background(),CreateCommand{
   WorkspaceID:"tenant",ProjectID:&pid,ProjectWorkspaceID:&wid,
   Objective:"Fail closed",Completion:json.RawMessage(raw),
  });err==nil{t.Fatalf("invalid compute authority persisted: %s",raw)}
 }
}
