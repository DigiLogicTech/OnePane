package assistant

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/artifact"
	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/id"
	"github.com/DigiLogicTech/OnePane/internal/inference"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	"github.com/DigiLogicTech/OnePane/internal/projectorchestrator"
	"github.com/DigiLogicTech/OnePane/internal/scheduler"
)

var (
	ErrInvalid  = errors.New("invalid assistant command")
	ErrNotFound = errors.New("assistant thread not found")
)

type Thread struct {
	ID              string  `json:"id"`
	WorkspaceID     string  `json:"workspace_id"`
	Title           string  `json:"title"`
	Status          string  `json:"status"`
	ActiveProjectID *string `json:"active_project_id,omitempty"`
	PreferredModelDeploymentID *string `json:"preferred_model_deployment_id,omitempty"`
	CreatedBy       string  `json:"created_by"`
	Revision        int64   `json:"revision"`
	CreatedAt       int64   `json:"created_at"`
	UpdatedAt       int64   `json:"updated_at"`
}

type Turn struct {
	ID             string          `json:"id"`
	ThreadID       string          `json:"thread_id"`
	Role           string          `json:"role"`
	Content        string          `json:"content"`
	ProjectID      *string         `json:"project_id,omitempty"`
	TaskID         *string         `json:"task_id,omitempty"`
	ProvenanceJSON json.RawMessage `json:"provenance"`
	CreatedAt      int64           `json:"created_at"`
}

type SubmitCommand struct {
	ThreadID        string
	Content         string
	ActorPrincipalID string
	ProjectID       *string
	ForceTask       bool
	AllowTaskCreation bool
}

type Handoff struct {
	ID                   string  `json:"id"`
	AssistantThreadID    string  `json:"assistant_thread_id"`
	AssistantTurnID      *string `json:"assistant_turn_id,omitempty"`
	ProjectID            string  `json:"project_id"`
	OrchestratorID       string  `json:"orchestrator_id"`
	OrchestratorTurnID   *string `json:"orchestrator_turn_id,omitempty"`
	TaskID               *string `json:"task_id,omitempty"`
	Status               string  `json:"status"`
	Objective            string  `json:"objective"`
	CreatedAt            int64   `json:"created_at"`
	UpdatedAt            int64   `json:"updated_at"`
}

type SubmitResult struct {
	Turn       Turn                               `json:"turn"`
	Delegated  bool                               `json:"delegated"`
	HandoffID  *string                            `json:"handoff_id,omitempty"`
	Project    *string                            `json:"project_id,omitempty"`
	Orchestrator *projectorchestrator.TurnResult  `json:"orchestrator,omitempty"`
}

type schedulerService interface { Route(context.Context, scheduler.RouteRequest) (scheduler.Decision, error) }
type inferenceService interface { Execute(context.Context, inference.ExecuteCommand) (inference.InferenceRequest, error) }
type artifactReader interface { Open(context.Context, string) (io.ReadCloser, artifact.Artifact, error) }
type orchestratorService interface {
	Ensure(context.Context,string) (projectorchestrator.Orchestrator,error)
	Turn(context.Context,projectorchestrator.TurnCommand) (projectorchestrator.TurnResult,error)
}

type Service struct {
	db *sql.DB
	ids id.Generator
	clock clock.Clock
	scheduler schedulerService
	inference inferenceService
	artifacts artifactReader
	orchestrators orchestratorService
}

func NewService(db *sql.DB, clk clock.Clock, sched schedulerService, inf inferenceService, artifacts artifactReader, orchestrators orchestratorService) *Service {
	return &Service{db:db,ids:id.Generator{},clock:clk,scheduler:sched,inference:inf,artifacts:artifacts,orchestrators:orchestrators}
}

func scanThread(row interface{ Scan(...any) error }) (Thread,error) {
	var t Thread; var project,model sql.NullString
	if err:=row.Scan(&t.ID,&t.WorkspaceID,&t.Title,&t.Status,&project,&t.CreatedBy,&t.Revision,&t.CreatedAt,&t.UpdatedAt,&model);err!=nil{
		if errors.Is(err,sql.ErrNoRows){return Thread{},ErrNotFound};return Thread{},err
	}
	if project.Valid{t.ActiveProjectID=&project.String};if model.Valid{t.PreferredModelDeploymentID=&model.String};return t,nil
}
func (s *Service) Thread(ctx context.Context,idv string)(Thread,error){
	return scanThread(s.db.QueryRowContext(ctx,`SELECT id,workspace_id,title,status,active_project_id,created_by,revision,created_at,updated_at,(SELECT deployment_id FROM assistant_model_preferences WHERE thread_id=assistant_threads.id) FROM assistant_threads WHERE id=?`,strings.TrimSpace(idv)))
}
func (s *Service) Threads(ctx context.Context,workspaceID string,limit int)([]Thread,error){
	if limit<=0||limit>100{limit=50};rows,err:=s.db.QueryContext(ctx,`SELECT id,workspace_id,title,status,active_project_id,created_by,revision,created_at,updated_at,(SELECT deployment_id FROM assistant_model_preferences WHERE thread_id=assistant_threads.id) FROM assistant_threads WHERE workspace_id=? ORDER BY updated_at DESC,id DESC LIMIT ?`,strings.TrimSpace(workspaceID),limit);if err!=nil{return nil,err};defer rows.Close();out:=[]Thread{};for rows.Next(){t,e:=scanThread(rows);if e!=nil{return nil,e};out=append(out,t)};return out,rows.Err()
}
// SetModel pins a deployment for global Assistant reasoning on this thread.
// Nil or empty resets to normal automatic scheduler routing. An explicit pin
// cannot fall back to a different model when the deployment is unavailable.
func (s *Service) SetModel(ctx context.Context, threadID string, deploymentID *string) (Thread,error) {
 t,err:=s.Thread(ctx,threadID);if err!=nil{return Thread{},err}
 selected:=""
 if deploymentID!=nil{selected=strings.TrimSpace(*deploymentID)}
 if selected!=""{
  listing,ok:=s.scheduler.(interface{
   Candidates(context.Context,string,string,string)([]scheduler.Candidate,error)
  })
  if !ok{return Thread{},fmt.Errorf("%w: scheduler model inventory unavailable",ErrInvalid)}
  candidates,err:=listing.Candidates(ctx,t.WorkspaceID,"inference.general","onepane-assistant")
  if err!=nil{return Thread{},err}
  found:=false
  for _,candidate:=range candidates {
   if candidate.ID!=selected||candidate.Kind!=scheduler.CandidateModel{continue}
   if !candidate.Schedulable||candidate.Qualification==scheduler.QualIncompatible{
    return Thread{},fmt.Errorf("%w: selected model is not admitted or available for Assistant inference",ErrInvalid)
   }
   found=true;break
  }
  if !found{return Thread{},fmt.Errorf("%w: selected model is not available to this workspace",ErrInvalid)}
 }
 now:=s.clock.UnixMilli()
 tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return Thread{},err}
 defer tx.Rollback()
 if selected==""{
  _,err=tx.ExecContext(ctx,`DELETE FROM assistant_model_preferences WHERE thread_id=?`,t.ID)
 }else{
  _,err=tx.ExecContext(ctx,`INSERT INTO assistant_model_preferences(thread_id,deployment_id,updated_at) VALUES(?,?,?) ON CONFLICT(thread_id) DO UPDATE SET deployment_id=excluded.deployment_id,updated_at=excluded.updated_at`,t.ID,selected,now)
 }
 if err!=nil{return Thread{},err}
 _,err=tx.ExecContext(ctx,`UPDATE assistant_threads SET revision=revision+1,updated_at=? WHERE id=?`,now,t.ID)
 if err!=nil{return Thread{},err}
 if err=tx.Commit();err!=nil{return Thread{},err}
 return s.Thread(ctx,t.ID)
}

func (s *Service) SetScope(ctx context.Context, threadID string, projectID *string) (Thread,error) {
	t,err:=s.Thread(ctx,threadID);if err!=nil{return Thread{},err}
	var value any=nil
	if projectID!=nil&&strings.TrimSpace(*projectID)!="" {
		var idv string
		if err:=s.db.QueryRowContext(ctx,`SELECT id FROM projects WHERE id=? AND workspace_id=? AND status='active'`,strings.TrimSpace(*projectID),t.WorkspaceID).Scan(&idv);err!=nil{return Thread{},err}
		value=idv
	}
	now:=s.clock.UnixMilli()
	res,err:=s.db.ExecContext(ctx,`UPDATE assistant_threads SET active_project_id=?,revision=revision+1,updated_at=? WHERE id=? AND revision=?`,value,now,t.ID,t.Revision)
	if err!=nil{return Thread{},err};n,_:=res.RowsAffected();if n!=1{return Thread{},ErrInvalid}
	return s.Thread(ctx,t.ID)
}

func (s *Service) CreateThread(ctx context.Context,workspaceID,title,actor string)(Thread,error){
	workspaceID,title,actor=strings.TrimSpace(workspaceID),strings.TrimSpace(title),strings.TrimSpace(actor);if workspaceID==""||actor==""{return Thread{},ErrInvalid};if title==""{title="OnePane Assistant"}
	idv,_:=s.ids.New("athread");now:=s.clock.UnixMilli();_,err:=s.db.ExecContext(ctx,`INSERT INTO assistant_threads(id,workspace_id,title,status,active_project_id,created_by,revision,created_at,updated_at) VALUES(?,?,?,'active',NULL,?,1,?,?)`,idv,workspaceID,title,actor,now,now);if err!=nil{return Thread{},err};return s.Thread(ctx,idv)
}
func scanTurn(row interface{Scan(...any)error})(Turn,error){var t Turn;var project,taskID sql.NullString;var prov string;if err:=row.Scan(&t.ID,&t.ThreadID,&t.Role,&t.Content,&project,&taskID,&prov,&t.CreatedAt);err!=nil{return Turn{},err};if project.Valid{t.ProjectID=&project.String};if taskID.Valid{t.TaskID=&taskID.String};t.ProvenanceJSON=json.RawMessage(prov);return t,nil}
func(s *Service) Turns(ctx context.Context,threadID string,limit int)([]Turn,error){if limit<=0||limit>200{limit=100};rows,err:=s.db.QueryContext(ctx,`SELECT id,thread_id,role,content,project_id,task_id,provenance_json,created_at FROM assistant_turns WHERE thread_id=? ORDER BY created_at DESC,id DESC LIMIT ?`,threadID,limit);if err!=nil{return nil,err};defer rows.Close();out:=[]Turn{};for rows.Next(){t,e:=scanTurn(rows);if e!=nil{return nil,e};out=append(out,t)};for i,j:=0,len(out)-1;i<j;i,j=i+1,j-1{out[i],out[j]=out[j],out[i]};return out,rows.Err()}

func extractText(raw []byte) string {
	var v map[string]any;if json.Unmarshal(raw,&v)!=nil{return strings.TrimSpace(string(raw))}
	if s,ok:=v["output_text"].(string);ok{return strings.TrimSpace(s)}
	if s,ok:=v["response"].(string);ok{return strings.TrimSpace(s)}
	if choices,ok:=v["choices"].([]any);ok&&len(choices)>0{if c,ok:=choices[0].(map[string]any);ok{if m,ok:=c["message"].(map[string]any);ok{if s,ok:=m["content"].(string);ok{return strings.TrimSpace(s)}};if s,ok:=c["text"].(string);ok{return strings.TrimSpace(s)}}}
	if content,ok:=v["content"].([]any);ok{for _,item:=range content{if m,ok:=item.(map[string]any);ok{if s,ok:=m["text"].(string);ok{return strings.TrimSpace(s)}}}}
	b,_:=json.Marshal(v);return string(b)
}

func (s *Service) globalSnapshot(ctx context.Context,workspaceID string) json.RawMessage {
	var projectsCount,tasksCount,nodesCount,providersCount int64
	_ = s.db.QueryRowContext(ctx,`SELECT COUNT(*) FROM projects WHERE workspace_id=? AND status='active'`,workspaceID).Scan(&projectsCount)
	_ = s.db.QueryRowContext(ctx,`SELECT COUNT(*) FROM tasks WHERE workspace_id=? AND state NOT IN ('complete','cancelled','failed')`,workspaceID).Scan(&tasksCount)
	_ = s.db.QueryRowContext(ctx,`SELECT COUNT(*) FROM harness_nodes WHERE trust_state IN ('local','paired')`).Scan(&nodesCount)
	_ = s.db.QueryRowContext(ctx,`SELECT COUNT(*) FROM provider_connections WHERE (workspace_id=? OR workspace_id IS NULL) AND status IN ('connected','degraded')`,workspaceID).Scan(&providersCount)
	counts:=map[string]any{"projects":projectsCount,"active_tasks":tasksCount,"nodes":nodesCount,"providers":providersCount}
	projects:=[]map[string]any{};rows,_:=s.db.QueryContext(ctx,`SELECT id,name,status,updated_at FROM projects WHERE workspace_id=? ORDER BY updated_at DESC LIMIT 20`,workspaceID);if rows!=nil{defer rows.Close();for rows.Next(){var idv,name,status string;var updated int64;if rows.Scan(&idv,&name,&status,&updated)==nil{projects=append(projects,map[string]any{"id":idv,"name":name,"status":status,"updated_at":updated})}}}
	raw,_:=json.Marshal(map[string]any{"scope":"global","counts":counts,"projects":projects});return raw
}

func(s *Service) reasonGlobal(ctx context.Context,workspaceID,actor,content string,pin *string)(string,*string,error){
	label:=policy.DataLabel{WorkspaceID:workspaceID,Confidentiality:policy.ConfidentialityInternal,Residency:policy.ResidencyAny,Trust:policy.TrustUserInstruction}
	selected:=""
 if pin!=nil{selected=strings.TrimSpace(*pin)}
 route:=scheduler.RouteRequest{WorkspaceID:workspaceID,CapabilityID:"inference.general",RoleName:"onepane-assistant",ProtocolLevel:"L1",ContextTokens:8192,DataLabel:label,AllowUntested:true,AllowLimited:true,AllowMediated:true,AllowDegraded:true,PreferZeroIncrementalCost:true,AllowedKinds:[]scheduler.CandidateKind{scheduler.CandidateModel}}
 if selected!=""{
  route.IncludeCandidateIDs=[]string{selected}
  // Manual model selection may use a smaller verified context than the
  // automatic route. Keep at least 2k for the bounded Assistant snapshot.
  route.ContextTokens=2048
 }
 decision,err:=s.scheduler.Route(ctx,route)
	if err!=nil||decision.Selected==nil{
  if selected!=""{return "",nil,fmt.Errorf("selected Assistant model is unavailable or disallowed (no substitution): %w",err)}
  return "",nil,fmt.Errorf("route OnePane Assistant: %w",err)
 }
	dep:=decision.Selected.Candidate.ID;snapshot:=s.globalSnapshot(ctx,workspaceID)
	system:=`You are the global OnePane Assistant. You may explain global OnePane state and help navigate. Do not claim direct tool, filesystem, secret, node, or approval authority. Project-specific work must be delegated to that Project's Orchestrator. Use only the supplied bounded global context.`
	req,_:=json.Marshal(map[string]any{"messages":[]map[string]string{{"role":"system","content":system},{"role":"user","content":"Global context:\n"+string(snapshot)+"\n\nUser:\n"+content}},"max_tokens":1000,"temperature":0.2})
	inf,err:=s.inference.Execute(ctx,inference.ExecuteCommand{WorkspaceID:workspaceID,PrincipalID:actor,DeploymentID:dep,CapabilityJSON:json.RawMessage(`{"id":"inference.general"}`),ContextManifestJSON:snapshot,ClassificationMetadata:json.RawMessage(`{"source":"onepane-assistant"}`),InputLabel:label,RequestJSON:req,ActorPrincipalID:&actor});if err!=nil{return "",&dep,err}
	if inf.ResponseArtifactID==nil{return "",&dep,fmt.Errorf("assistant inference returned no response artifact")}
	rc,_,err:=s.artifacts.Open(ctx,*inf.ResponseArtifactID);if err!=nil{return "",&dep,err};defer rc.Close();b,err:=io.ReadAll(io.LimitReader(rc,4<<20));if err!=nil{return "",&dep,err};return extractText(b),&dep,nil
}

func (s *Service) resolveProject(ctx context.Context,t Thread,content string,explicit *string)(*string,error){
	if explicit!=nil&&strings.TrimSpace(*explicit)!=""{var idv string;err:=s.db.QueryRowContext(ctx,`SELECT id FROM projects WHERE id=? AND workspace_id=? AND status='active'`,strings.TrimSpace(*explicit),t.WorkspaceID).Scan(&idv);if err!=nil{return nil,err};return &idv,nil}
	if t.ActiveProjectID!=nil{return t.ActiveProjectID,nil}
	rows,err:=s.db.QueryContext(ctx,`SELECT id,name FROM projects WHERE workspace_id=? AND status='active' ORDER BY length(name) DESC`,t.WorkspaceID);if err!=nil{return nil,err};defer rows.Close();low:=strings.ToLower(content);for rows.Next(){var idv,name string;if rows.Scan(&idv,&name)==nil&&strings.Contains(low,strings.ToLower(name)){return &idv,nil}};return nil,nil
}

func (s *Service) ProjectHandoffs(ctx context.Context, projectID string, limit int) ([]Handoff,error) {
	if limit<=0||limit>200{limit=100}
	rows,err:=s.db.QueryContext(ctx,`SELECT id,assistant_thread_id,assistant_turn_id,project_id,orchestrator_id,orchestrator_turn_id,task_id,status,objective,created_at,updated_at FROM assistant_project_handoffs WHERE project_id=? ORDER BY updated_at DESC,id DESC LIMIT ?`,strings.TrimSpace(projectID),limit)
	if err!=nil{return nil,err}
	defer rows.Close()
	out:=[]Handoff{}
	for rows.Next(){
		var h Handoff;var assistantTurn,orchestratorTurn,taskID sql.NullString
		if err:=rows.Scan(&h.ID,&h.AssistantThreadID,&assistantTurn,&h.ProjectID,&h.OrchestratorID,&orchestratorTurn,&taskID,&h.Status,&h.Objective,&h.CreatedAt,&h.UpdatedAt);err!=nil{return nil,err}
		if assistantTurn.Valid{h.AssistantTurnID=&assistantTurn.String};if orchestratorTurn.Valid{h.OrchestratorTurnID=&orchestratorTurn.String};if taskID.Valid{h.TaskID=&taskID.String};out=append(out,h)
	}
	return out,rows.Err()
}

func (s *Service) Submit(ctx context.Context,c SubmitCommand)(SubmitResult,error){
	c.ThreadID,c.Content,c.ActorPrincipalID=strings.TrimSpace(c.ThreadID),strings.TrimSpace(c.Content),strings.TrimSpace(c.ActorPrincipalID);if c.ThreadID==""||c.Content==""||c.ActorPrincipalID==""{return SubmitResult{},ErrInvalid}
	thread,err:=s.Thread(ctx,c.ThreadID);if err!=nil{return SubmitResult{},err}
	userID,_:=s.ids.New("aturn");now:=s.clock.UnixMilli();projectID,err:=s.resolveProject(ctx,thread,c.Content,c.ProjectID);if err!=nil{return SubmitResult{},err}
	_,err=s.db.ExecContext(ctx,`INSERT INTO assistant_turns(id,thread_id,role,content,project_id,task_id,provenance_json,created_at) VALUES(?,?,'user',?,?,NULL,'{}',?)`,userID,thread.ID,c.Content,projectID,now);if err!=nil{return SubmitResult{},err}
	if projectID!=nil{
		o,err:=s.orchestrators.Ensure(ctx,*projectID);if err!=nil{return SubmitResult{},err}
		hid,_:=s.ids.New("ahandoff");_,err=s.db.ExecContext(ctx,`INSERT INTO assistant_project_handoffs(id,assistant_thread_id,assistant_turn_id,project_id,orchestrator_id,orchestrator_turn_id,task_id,status,objective,created_at,updated_at) VALUES(?,?,?,?,?,NULL,NULL,'delegated',?,?,?)`,hid,thread.ID,userID,*projectID,o.ID,c.Content,now,now);if err!=nil{return SubmitResult{},err}
		res,err:=s.orchestrators.Turn(ctx,projectorchestrator.TurnCommand{ProjectID:*projectID,AssistantThreadID:&thread.ID,Objective:c.Content,ActorPrincipalID:c.ActorPrincipalID,ForceTask:c.ForceTask,AllowTaskCreation:c.AllowTaskCreation});if err!=nil{_,_=s.db.ExecContext(ctx,`UPDATE assistant_project_handoffs SET status='failed',updated_at=? WHERE id=?`,s.clock.UnixMilli(),hid);return SubmitResult{},err}
		turnID,_:=s.ids.New("aturn");prov:=map[string]any{"assistant_thread_id":thread.ID,"assistant_turn_id":userID,"project_id":*projectID,"orchestrator_id":o.ID,"orchestrator_turn_id":res.Turn.ID};if res.TaskID!=nil{prov["task_id"]=*res.TaskID};provRaw,_:=json.Marshal(prov);created:=s.clock.UnixMilli()
		_,err=s.db.ExecContext(ctx,`INSERT INTO assistant_turns(id,thread_id,role,content,project_id,task_id,provenance_json,created_at) VALUES(?,?,'assistant',?,?,?,?,?)`,turnID,thread.ID,res.Turn.Content,*projectID,res.TaskID,string(provRaw),created);if err!=nil{return SubmitResult{},err}
		status:="completed";if res.Disposition=="waiting"||res.Disposition=="blocked"{status="waiting"};_,_=s.db.ExecContext(ctx,`UPDATE assistant_project_handoffs SET orchestrator_turn_id=?,task_id=?,status=?,updated_at=? WHERE id=?`,res.Turn.ID,res.TaskID,status,created,hid)
		_,_=s.db.ExecContext(ctx,`UPDATE assistant_threads SET active_project_id=?,revision=revision+1,updated_at=? WHERE id=?`,*projectID,created,thread.ID)
		t:=Turn{ID:turnID,ThreadID:thread.ID,Role:"assistant",Content:res.Turn.Content,ProjectID:projectID,TaskID:res.TaskID,ProvenanceJSON:provRaw,CreatedAt:created};return SubmitResult{Turn:t,Delegated:true,HandoffID:&hid,Project:projectID,Orchestrator:&res},nil
	}
	answer,dep,reasonErr:=s.reasonGlobal(ctx,thread.WorkspaceID,c.ActorPrincipalID,c.Content,thread.PreferredModelDeploymentID);if reasonErr!=nil{answer="OnePane Assistant could not complete this turn: "+reasonErr.Error()}
	prov:=map[string]any{"assistant_thread_id":thread.ID,"scope":"global"};if dep!=nil{prov["candidate_kind"]="model_deployment";prov["candidate_id"]=*dep};if thread.PreferredModelDeploymentID!=nil{prov["configured_model_deployment_id"]=*thread.PreferredModelDeploymentID};provRaw,_:=json.Marshal(prov);turnID,_:=s.ids.New("aturn");created:=s.clock.UnixMilli();_,err=s.db.ExecContext(ctx,`INSERT INTO assistant_turns(id,thread_id,role,content,project_id,task_id,provenance_json,created_at) VALUES(?,?,'assistant',?,NULL,NULL,?,?)`,turnID,thread.ID,answer,string(provRaw),created);if err!=nil{return SubmitResult{},err};_,_=s.db.ExecContext(ctx,`UPDATE assistant_threads SET revision=revision+1,updated_at=? WHERE id=?`,created,thread.ID);return SubmitResult{Turn:Turn{ID:turnID,ThreadID:thread.ID,Role:"assistant",Content:answer,ProvenanceJSON:provRaw,CreatedAt:created}},nil
}
