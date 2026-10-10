package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/agentprofile"
	"github.com/DigiLogicTech/OnePane/internal/assistant"
	"github.com/DigiLogicTech/OnePane/internal/projectorchestrator"
)

type assistantService interface {
	Thread(context.Context,string) (assistant.Thread,error)
	Threads(context.Context,string,int) ([]assistant.Thread,error)
	CreateThread(context.Context,string,string,string) (assistant.Thread,error)
	SetScope(context.Context,string,*string) (assistant.Thread,error)
	SetModel(context.Context,string,*string) (assistant.Thread,error)
	Turns(context.Context,string,int) ([]assistant.Turn,error)
	Submit(context.Context,assistant.SubmitCommand) (assistant.SubmitResult,error)
	ProjectHandoffs(context.Context,string,int) ([]assistant.Handoff,error)
}

type projectOrchestratorService interface {
	Ensure(context.Context,string) (projectorchestrator.Orchestrator,error)
	Get(context.Context,string) (projectorchestrator.Orchestrator,error)
	Turns(context.Context,string,int) ([]projectorchestrator.Turn,error)
	Turn(context.Context,projectorchestrator.TurnCommand) (projectorchestrator.TurnResult,error)
}

type agentProfileService interface {
	Get(context.Context,string) (agentprofile.Profile,error)
	List(context.Context,string) ([]agentprofile.Profile,error)
	Create(context.Context,agentprofile.CreateCommand) (agentprofile.Profile,error)
	Update(context.Context,agentprofile.UpdateCommand) (agentprofile.Profile,error)
	Archive(context.Context,string,string,string) (agentprofile.Profile,error)
	ListSessions(context.Context,string,int) ([]agentprofile.Session,error)
}

func parseLimit(r *http.Request, fallback int) int {
	v:=fallback
	if raw:=strings.TrimSpace(r.URL.Query().Get("limit"));raw!="" {
		if n,err:=strconv.Atoi(raw);err==nil&&n>0&&n<=500{v=n}
	}
	return v
}

func (s *Server) assistantOverview(w http.ResponseWriter,r *http.Request){
	i,ok:=s.authenticate(w,r);if !ok{return}
	workspaceID:=strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	if workspaceID==""{writeError(w,http.StatusBadRequest,"workspace_id is required");return}
	if !s.authorize(w,r,i,workspaceID,"project.read"){return}
	if s.assistant==nil{writeError(w,http.StatusServiceUnavailable,"assistant unavailable");return}
	writeJSON(w,http.StatusOK,map[string]any{"status":"ready","scope":"global","workspace_id":workspaceID})
}

func (s *Server) listAssistantThreads(w http.ResponseWriter,r *http.Request){
	i,ok:=s.authenticate(w,r);if !ok{return}
	workspaceID:=strings.TrimSpace(r.URL.Query().Get("workspace_id"));if workspaceID==""{writeError(w,http.StatusBadRequest,"workspace_id is required");return}
	if !s.authorize(w,r,i,workspaceID,"project.read"){return}
	rows,err:=s.assistant.Threads(r.Context(),workspaceID,parseLimit(r,50));respondDomain(w,rows,err,http.StatusOK)
}
func (s *Server) createAssistantThread(w http.ResponseWriter,r *http.Request){
	i,ok:=s.authenticate(w,r);if !ok{return}
	var in struct{WorkspaceID string `json:"workspace_id"`;Title string `json:"title"`}
	if !decodeJSON(w,r,&in){return};if !s.authorize(w,r,i,in.WorkspaceID,"project.read"){return}
	x,err:=s.assistant.CreateThread(r.Context(),in.WorkspaceID,in.Title,i.PrincipalID);respondDomain(w,x,err,http.StatusCreated)
}
func (s *Server) setAssistantScope(w http.ResponseWriter,r *http.Request){
	i,ok:=s.authenticate(w,r);if !ok{return}
	if s.assistant==nil{writeError(w,http.StatusServiceUnavailable,"assistant unavailable");return}
	t,err:=s.assistant.Thread(r.Context(),r.PathValue("threadID"));if err!=nil{respondDomain(w,nil,err,0);return}
	if !s.authorize(w,r,i,t.WorkspaceID,"project.read"){return}
	var in struct{ProjectID *string `json:"project_id"`}
	if !decodeJSON(w,r,&in){return}
	out,err:=s.assistant.SetScope(r.Context(),t.ID,in.ProjectID);respondDomain(w,out,err,http.StatusOK)
}
// Configure the global Assistant model on this specific thread. The
// Project Orchestrator has its own routing and is not affected.
func (s *Server) setAssistantModel(w http.ResponseWriter,r *http.Request){
 i,ok:=s.authenticate(w,r);if !ok{return}
 if s.assistant==nil{writeError(w,http.StatusServiceUnavailable,"assistant unavailable");return}
 t,err:=s.assistant.Thread(r.Context(),r.PathValue("threadID"))
 if err!=nil{respondDomain(w,nil,err,0);return}
 if !s.authorize(w,r,i,t.WorkspaceID,"project.read"){return}
 if !s.authorize(w,r,i,t.WorkspaceID,"model.read"){return}
 var in struct{DeploymentID *string `json:"deployment_id"`}
 if !decodeJSON(w,r,&in){return}
 out,err:=s.assistant.SetModel(r.Context(),t.ID,in.DeploymentID)
 if err!=nil{respondDomain(w,nil,err,http.StatusOK);return}
 writeJSON(w,http.StatusOK,out)
}
func (s *Server) listAssistantTurns(w http.ResponseWriter,r *http.Request){
	i,ok:=s.authenticate(w,r);if !ok{return};t,err:=s.assistant.Thread(r.Context(),r.PathValue("threadID"));if err!=nil{respondDomain(w,nil,err,0);return}
	if !s.authorize(w,r,i,t.WorkspaceID,"project.read"){return}
	rows,err:=s.assistant.Turns(r.Context(),t.ID,parseLimit(r,100));respondDomain(w,rows,err,http.StatusOK)
}
func (s *Server) submitAssistantTurn(w http.ResponseWriter,r *http.Request){
	i,ok:=s.authenticate(w,r);if !ok{return};t,err:=s.assistant.Thread(r.Context(),r.PathValue("threadID"));if err!=nil{respondDomain(w,nil,err,0);return}
	if !s.authorize(w,r,i,t.WorkspaceID,"project.read"){return}
	var in struct{Content string `json:"content"`;ProjectID *string `json:"project_id"`;AllowTaskCreation *bool `json:"allow_task_creation"`;ForceTask bool `json:"force_task"`}
	if !decodeJSON(w,r,&in){return}
	allow:=false;if in.AllowTaskCreation!=nil{allow=*in.AllowTaskCreation}
	if allow||in.ForceTask{if !s.authorize(w,r,i,t.WorkspaceID,"task.write"){return}}
	out,err:=s.assistant.Submit(r.Context(),assistant.SubmitCommand{ThreadID:t.ID,Content:in.Content,ActorPrincipalID:i.PrincipalID,ProjectID:in.ProjectID,ForceTask:in.ForceTask,AllowTaskCreation:allow})
	respondDomain(w,out,err,http.StatusOK)
}

func (s *Server) getProjectOrchestrator(w http.ResponseWriter,r *http.Request){
	i,ok:=s.authenticate(w,r);if !ok{return};p,err:=s.projects.Project(r.Context(),r.PathValue("projectID"));if err!=nil{respondDomain(w,nil,err,0);return}
	if !s.authorize(w,r,i,p.WorkspaceID,"project.read"){return};x,err:=s.projectOrchestrator.Ensure(r.Context(),p.ID);respondDomain(w,x,err,http.StatusOK)
}
func (s *Server) listProjectOrchestratorTurns(w http.ResponseWriter,r *http.Request){
	i,ok:=s.authenticate(w,r);if !ok{return};p,err:=s.projects.Project(r.Context(),r.PathValue("projectID"));if err!=nil{respondDomain(w,nil,err,0);return}
	if !s.authorize(w,r,i,p.WorkspaceID,"project.read"){return};rows,err:=s.projectOrchestrator.Turns(r.Context(),p.ID,parseLimit(r,100));respondDomain(w,rows,err,http.StatusOK)
}
func (s *Server) submitProjectOrchestratorTurn(w http.ResponseWriter,r *http.Request){
	i,ok:=s.authenticate(w,r);if !ok{return};p,err:=s.projects.Project(r.Context(),r.PathValue("projectID"));if err!=nil{respondDomain(w,nil,err,0);return}
	if !s.authorize(w,r,i,p.WorkspaceID,"project.read"){return}
	var in struct{Objective string `json:"objective"`;ProjectWorkspaceID string `json:"project_workspace_id"`;AllowTaskCreation *bool `json:"allow_task_creation"`;ForceTask bool `json:"force_task"`}
	if !decodeJSON(w,r,&in){return};allow:=false;if in.AllowTaskCreation!=nil{allow=*in.AllowTaskCreation}
	if allow||in.ForceTask{if !s.authorize(w,r,i,p.WorkspaceID,"task.write"){return}}
	out,err:=s.projectOrchestrator.Turn(r.Context(),projectorchestrator.TurnCommand{ProjectID:p.ID,Objective:in.Objective,ActorPrincipalID:i.PrincipalID,ProjectWorkspaceID:in.ProjectWorkspaceID,ForceTask:in.ForceTask,AllowTaskCreation:allow})
	respondDomain(w,out,err,http.StatusOK)
}
// Graph writes require a human-authored request and task.write, independent
// of the Orchestrator's text-generation turn. The Task graph service validates
// canonical per-Workspace ownership and atomically persists all hard edges.
type projectTaskGraphService interface {
 CreateTaskGraph(context.Context,projectorchestrator.CreateTaskGraphCommand)(projectorchestrator.TaskGraph,error)
 TaskGraph(context.Context,string,string)(projectorchestrator.TaskGraph,error)
}
func(s *Server) projectTaskGraphService(w http.ResponseWriter)(projectTaskGraphService,bool){
 graph,ok:=s.projectOrchestrator.(projectTaskGraphService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"Project Task graph unavailable")}
 return graph,ok
}
func(s *Server) createProjectTaskGraph(w http.ResponseWriter,r *http.Request){
 i,ok:=s.authenticate(w,r);if !ok{return}
 p,err:=s.projects.Project(r.Context(),r.PathValue("projectID"))
 if err!=nil{respondDomain(w,nil,err,0);return}
 if !s.authorize(w,r,i,p.WorkspaceID,"task.write"){return}
 graph,ok:=s.projectTaskGraphService(w);if !ok{return}
 var input projectorchestrator.CreateTaskGraphCommand
 if !decodeJSON(w,r,&input){return}
 input.ProjectID=p.ID
 input.ActorPrincipalID=i.PrincipalID
 created,err:=graph.CreateTaskGraph(r.Context(),input)
 respondDomain(w,created,err,http.StatusCreated)
}
func(s *Server) getProjectTaskGraph(w http.ResponseWriter,r *http.Request){
 i,ok:=s.authenticate(w,r);if !ok{return}
 p,err:=s.projects.Project(r.Context(),r.PathValue("projectID"))
 if err!=nil{respondDomain(w,nil,err,0);return}
 if !s.authorize(w,r,i,p.WorkspaceID,"project.read"){return}
 graph,ok:=s.projectTaskGraphService(w);if !ok{return}
 out,err:=graph.TaskGraph(r.Context(),p.ID,r.PathValue("graphID"))
 respondDomain(w,out,err,http.StatusOK)
}

func (s *Server) listProjectHandoffs(w http.ResponseWriter,r *http.Request){
	i,ok:=s.authenticate(w,r);if !ok{return};p,err:=s.projects.Project(r.Context(),r.PathValue("projectID"));if err!=nil{respondDomain(w,nil,err,0);return}
	if !s.authorize(w,r,i,p.WorkspaceID,"project.read"){return};rows,err:=s.assistant.ProjectHandoffs(r.Context(),p.ID,parseLimit(r,100));respondDomain(w,rows,err,http.StatusOK)
}

func (s *Server) listAgentProfiles(w http.ResponseWriter,r *http.Request){
	i,ok:=s.authenticate(w,r);if !ok{return};workspaceID:=strings.TrimSpace(r.URL.Query().Get("workspace_id"));if workspaceID==""{writeError(w,http.StatusBadRequest,"workspace_id is required");return}
	if !s.authorize(w,r,i,workspaceID,"project.read"){return};rows,err:=s.agentProfiles.List(r.Context(),workspaceID);respondDomain(w,rows,err,http.StatusOK)
}
func (s *Server) getAgentProfile(w http.ResponseWriter,r *http.Request){
	i,ok:=s.authenticate(w,r);if !ok{return};workspaceID:=strings.TrimSpace(r.URL.Query().Get("workspace_id"));if workspaceID==""{writeError(w,http.StatusBadRequest,"workspace_id is required");return}
	if !s.authorize(w,r,i,workspaceID,"project.read"){return};x,err:=s.agentProfiles.Get(r.Context(),r.PathValue("profileID"));respondDomain(w,x,err,http.StatusOK)
}
func (s *Server) createAgentProfile(w http.ResponseWriter,r *http.Request){
	i,ok:=s.authenticate(w,r);if !ok{return}
	var in struct{
		WorkspaceID string `json:"workspace_id"`
		Name string `json:"name"`
		Description string `json:"description"`
		InstructionsMD string `json:"instructions_md"`
		DefaultRole string `json:"default_role"`
		CapabilityID string `json:"capability_id"`
		ProtocolLevel string `json:"protocol_level"`
		Metadata json.RawMessage `json:"metadata"`
	}
	if !decodeJSON(w,r,&in){return};if !s.authorize(w,r,i,in.WorkspaceID,"project.write"){return}
	x,err:=s.agentProfiles.Create(r.Context(),agentprofile.CreateCommand{WorkspaceID:in.WorkspaceID,Name:in.Name,Description:in.Description,InstructionsMD:in.InstructionsMD,DefaultRole:in.DefaultRole,CapabilityID:in.CapabilityID,ProtocolLevel:in.ProtocolLevel,Metadata:in.Metadata,CreatedBy:i.PrincipalID});respondDomain(w,x,err,http.StatusCreated)
}
func (s *Server) updateAgentProfile(w http.ResponseWriter,r *http.Request){
	i,ok:=s.authenticate(w,r);if !ok{return}
	var in struct{
		WorkspaceID string `json:"workspace_id"`
		ExpectedRevision int64 `json:"expected_revision"`
		Name string `json:"name"`
		Description string `json:"description"`
		InstructionsMD string `json:"instructions_md"`
		DefaultRole string `json:"default_role"`
		CapabilityID string `json:"capability_id"`
		ProtocolLevel string `json:"protocol_level"`
		Metadata json.RawMessage `json:"metadata"`
	}
	if !decodeJSON(w,r,&in){return};if !s.authorize(w,r,i,in.WorkspaceID,"project.write"){return}
	x,err:=s.agentProfiles.Update(r.Context(),agentprofile.UpdateCommand{ID:r.PathValue("profileID"),WorkspaceID:in.WorkspaceID,ExpectedRevision:in.ExpectedRevision,Name:in.Name,Description:in.Description,InstructionsMD:in.InstructionsMD,DefaultRole:in.DefaultRole,CapabilityID:in.CapabilityID,ProtocolLevel:in.ProtocolLevel,Metadata:in.Metadata,ActorPrincipalID:i.PrincipalID});respondDomain(w,x,err,http.StatusOK)
}
func (s *Server) archiveAgentProfile(w http.ResponseWriter,r *http.Request){
	i,ok:=s.authenticate(w,r);if !ok{return};var in struct{WorkspaceID string `json:"workspace_id"`};if !decodeJSON(w,r,&in){return};if !s.authorize(w,r,i,in.WorkspaceID,"project.write"){return}
	x,err:=s.agentProfiles.Archive(r.Context(),r.PathValue("profileID"),in.WorkspaceID,i.PrincipalID);respondDomain(w,x,err,http.StatusOK)
}
func (s *Server) listAgentSessions(w http.ResponseWriter,r *http.Request){
	i,ok:=s.authenticate(w,r);if !ok{return};workspaceID:=strings.TrimSpace(r.URL.Query().Get("workspace_id"));if workspaceID==""{writeError(w,http.StatusBadRequest,"workspace_id is required");return}
	if !s.authorize(w,r,i,workspaceID,"project.read"){return};rows,err:=s.agentProfiles.ListSessions(r.Context(),workspaceID,parseLimit(r,100));respondDomain(w,rows,err,http.StatusOK)
}
