package api

import (
 "net/http"
 "strings"
 "time"
)

// These endpoints are opt-in, local-only exports. They never persist captures,
// enumerate another Workspace, or echo untrusted HTTP/DB fields into a bundle.
// Full incident tracing and installer/Node diagnostics are separate #85 work.
type qaWorkspaceRequest struct {
 WorkspaceID string `json:"workspace_id"`
 ProjectID string `json:"project_id"`
 ProjectWorkspaceID string `json:"project_workspace_id"`
}

func (s *Server) qaWorkspaceSnapshot(w http.ResponseWriter,r *http.Request){
 scope:=qaWorkspaceRequest{
  WorkspaceID:r.URL.Query().Get("workspace_id"),
  ProjectID:r.URL.Query().Get("project_id"),
  ProjectWorkspaceID:r.URL.Query().Get("project_workspace_id"),
 }
 snapshot,ok:=s.loadQASnapshot(w,r,scope)
 if !ok{return}
 w.Header().Set("Cache-Control","no-store")
 writeJSON(w,http.StatusOK,snapshot)
}

func (s *Server) qaWorkspaceBundle(w http.ResponseWriter,r *http.Request){
 var scope qaWorkspaceRequest
 if !decodeJSON(w,r,&scope){return}
 snapshot,ok:=s.loadQASnapshot(w,r,scope)
 if !ok{return}
 b,err:=qaBundle(snapshot)
 if err!=nil{
  writeError(w,http.StatusUnprocessableEntity,"diagnostic bundle could not be safely generated")
  return
 }
 w.Header().Set("Cache-Control","no-store")
 w.Header().Set("Content-Type","application/zip")
 w.Header().Set("Content-Disposition",`attachment; filename="onepane-workspace-qa-snapshot.zip"`)
 w.Header().Set("X-Content-Type-Options","nosniff")
 w.WriteHeader(http.StatusOK)
 _,_=w.Write(b)
}

func (s *Server) loadQASnapshot(w http.ResponseWriter,r *http.Request,in qaWorkspaceRequest)(qaSnapshot,bool){
 // Authenticate BEFORE any tenant or ID based information is resolved.
 i,ok:=s.authenticate(w,r)
 if !ok{return qaSnapshot{},false}
 tenant:=strings.TrimSpace(in.WorkspaceID)
 projectID:=strings.TrimSpace(in.ProjectID)
 workspaceID:=strings.TrimSpace(in.ProjectWorkspaceID)
 if tenant==""||projectID==""||workspaceID==""{
  writeError(w,http.StatusBadRequest,"tenant, Project and canonical Workspace are required")
  return qaSnapshot{},false
 }
 if !s.authorize(w,r,i,tenant,"project.read")||!s.authorize(w,r,i,tenant,"task.read"){
  return qaSnapshot{},false
 }
 if s.projects==nil||s.tasks==nil||s.attentionDB==nil{
  writeError(w,http.StatusServiceUnavailable,"QA snapshot source unavailable")
  return qaSnapshot{},false
 }
 project,err:=s.projects.Project(r.Context(),projectID)
 if err!=nil||project.WorkspaceID!=tenant||project.Status=="archived" {
  writeError(w,http.StatusNotFound,"Project unavailable in the selected tenancy")
  return qaSnapshot{},false
 }
 views,ok:=s.projects.(projectWorkspaceViewReader)
 if !ok{
  writeError(w,http.StatusServiceUnavailable,"canonical Workspace reader unavailable")
  return qaSnapshot{},false
 }
 view,err:=views.WorkspaceView(r.Context(),workspaceID)
 if err!=nil||view.ProjectID!=projectID||view.Status=="archived"{
  writeError(w,http.StatusNotFound,"canonical Workspace unavailable in Project")
  return qaSnapshot{},false
 }
 reader,ok:=s.tasks.(scopedWorkspaceTaskReader)
 if !ok{
  writeError(w,http.StatusServiceUnavailable,"scoped Task reader unavailable")
  return qaSnapshot{},false
 }
 // Fetch one extra row to truthfully mark a truncated snapshot. Never scan
 // arbitrary unscoped Task lists then filter them after row limiting.
 rows,err:=reader.ListProjectWorkspace(r.Context(),tenant,projectID,workspaceID,qaSnapshotTaskCap+1)
 if err!=nil{
  writeError(w,http.StatusServiceUnavailable,"scoped Task snapshot unavailable")
  return qaSnapshot{},false
 }
 if len(rows)==0 {
  return makeQASnapshot(time.Now().UTC(),nil,nil,nil),true
 }
 progress,err:=loadTaskExecutionProgress(r.Context(),s.attentionDB,tenant,projectID,workspaceID,rows)
 if err!=nil{
  writeError(w,http.StatusServiceUnavailable,"Worker checkpoint evidence unavailable")
  return qaSnapshot{},false
 }
 dependencies,err:=loadWorkspaceTaskDependencies(r.Context(),s.attentionDB,tenant,projectID,workspaceID,rows)
 if err!=nil{
  writeError(w,http.StatusServiceUnavailable,"Task dependency evidence unavailable")
  return qaSnapshot{},false
 }
 // Use only the same newest 50 Tasks that the final snapshot exposes.
 // Event lookups independently recheck the canonical scope in SQL.
 timelineRows:=rows
 if len(timelineRows)>qaSnapshotTaskCap{timelineRows=timelineRows[:qaSnapshotTaskCap]}
 timeline,truncated,err:=loadQATimeline(r.Context(),s.attentionDB,tenant,projectID,workspaceID,timelineRows)
 if err!=nil{
  writeError(w,http.StatusServiceUnavailable,"Task/Worker event chronology unavailable")
  return qaSnapshot{},false
 }
 snapshot:=makeQASnapshot(time.Now().UTC(),rows,progress,dependencies)
 executionSources,err:=loadQAExecutionSources(r.Context(),s.attentionDB,tenant,projectID,workspaceID,timelineRows)
 if err!=nil{
  writeError(w,http.StatusServiceUnavailable,"scoped execution evidence unavailable")
  return qaSnapshot{},false
 }
 snapshot.ExecutionSources=executionSources
 snapshot.Timeline=timeline
 snapshot.CapturedTimelineEvents=len(timeline)
 snapshot.TimelineTruncated=truncated
 return snapshot,true
}
