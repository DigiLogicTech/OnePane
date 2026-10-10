package projectorchestrator

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
	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/id"
	"github.com/DigiLogicTech/OnePane/internal/inference"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	"github.com/DigiLogicTech/OnePane/internal/scheduler"
	"github.com/DigiLogicTech/OnePane/internal/storage"
	"github.com/DigiLogicTech/OnePane/internal/task"
	"github.com/DigiLogicTech/OnePane/internal/team"
)

var (
	ErrInvalid  = errors.New("invalid project orchestrator command")
	ErrNotFound = errors.New("project orchestrator not found")
)

type Orchestrator struct {
	ID                string          `json:"id"`
	ProjectID         string          `json:"project_id"`
	WorkspaceID       string          `json:"workspace_id"`
	Status            string          `json:"status"`
	ConfigurationJSON json.RawMessage `json:"configuration"`
	LastEventSequence int64           `json:"last_event_sequence"`
	Revision          int64           `json:"revision"`
	CreatedAt         int64           `json:"created_at"`
	UpdatedAt         int64           `json:"updated_at"`
}

type Turn struct {
	ID                string          `json:"id"`
	OrchestratorID    string          `json:"orchestrator_id"`
	AssistantThreadID *string         `json:"assistant_thread_id,omitempty"`
	TaskID            *string         `json:"task_id,omitempty"`
	Role              string          `json:"role"`
	Content           string          `json:"content"`
	ContextJSON       json.RawMessage `json:"context"`
	ProvenanceJSON    json.RawMessage `json:"provenance"`
	CreatedAt         int64           `json:"created_at"`
}

type TurnCommand struct {
	ProjectID          string
	AssistantThreadID  *string
	Objective          string
	ActorPrincipalID   string
	ProjectWorkspaceID string
	ForceTask          bool
	AllowTaskCreation  bool
}

type TurnResult struct {
	Turn               Turn    `json:"turn"`
	Disposition        string  `json:"disposition"`
	TaskID             *string `json:"task_id,omitempty"`
	ProjectWorkspaceID *string `json:"project_workspace_id,omitempty"`
	Mode               *string `json:"mode,omitempty"`
	TeamSessionID      *string `json:"team_session_id,omitempty"`
}

type schedulerService interface {
	Route(context.Context, scheduler.RouteRequest) (scheduler.Decision, error)
}
type inferenceService interface {
	Execute(context.Context, inference.ExecuteCommand) (inference.InferenceRequest, error)
}
type artifactReader interface {
	Open(context.Context, string) (io.ReadCloser, artifact.Artifact, error)
}
type taskService interface {
	Create(context.Context, task.CreateCommand) (task.Task, error)
}
type teamService interface {
	StartSession(context.Context, team.StartSessionCommand) (team.Session, error)
}

type Service struct {
	db        *sql.DB
	tx        storage.Transactor
	events    event.Store
	ids       id.Generator
	clock     clock.Clock
	scheduler schedulerService
	inference inferenceService
	artifacts artifactReader
	tasks     taskService
	teams     teamService
}

func NewService(db *sql.DB, tx storage.Transactor, clk clock.Clock, sched schedulerService, inf inferenceService, artifacts artifactReader, tasks taskService, teams teamService) *Service {
	return &Service{db: db, tx: tx, events: event.Store{}, ids: id.Generator{}, clock: clk, scheduler: sched, inference: inf, artifacts: artifacts, tasks: tasks, teams: teams}
}

func scanOrchestrator(row interface{ Scan(...any) error }) (Orchestrator, error) {
	var o Orchestrator
	var cfg string
	if err := row.Scan(&o.ID, &o.ProjectID, &o.WorkspaceID, &o.Status, &cfg, &o.LastEventSequence, &o.Revision, &o.CreatedAt, &o.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) { return Orchestrator{}, ErrNotFound }
		return Orchestrator{}, err
	}
	o.ConfigurationJSON = json.RawMessage(cfg)
	return o, nil
}

func (s *Service) Ensure(ctx context.Context, projectID string) (Orchestrator, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" { return Orchestrator{}, ErrInvalid }
	if o, err := s.Get(ctx, projectID); err == nil { return o, nil } else if !errors.Is(err, ErrNotFound) { return Orchestrator{}, err }
	var workspaceID string
	if err := s.db.QueryRowContext(ctx, `SELECT workspace_id FROM projects WHERE id=? AND status='active'`, projectID).Scan(&workspaceID); err != nil { return Orchestrator{}, err }
	oid, _ := s.ids.New("porch")
	now := s.clock.UnixMilli()
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO project_orchestrators(id,project_id,workspace_id,status,configuration_json,last_event_sequence,revision,created_at,updated_at) VALUES(?,?,?,'ready','{}',0,1,?,?)`, oid, projectID, workspaceID, now, now)
	if err != nil { return Orchestrator{}, err }
	return s.Get(ctx, projectID)
}

func (s *Service) Get(ctx context.Context, projectID string) (Orchestrator, error) {
	return scanOrchestrator(s.db.QueryRowContext(ctx, `SELECT id,project_id,workspace_id,status,configuration_json,last_event_sequence,revision,created_at,updated_at FROM project_orchestrators WHERE project_id=?`, strings.TrimSpace(projectID)))
}

func scanTurn(row interface{ Scan(...any) error }) (Turn, error) {
	var t Turn
	var aid, taskID sql.NullString
	var ctxRaw, provRaw string
	if err := row.Scan(&t.ID, &t.OrchestratorID, &aid, &taskID, &t.Role, &t.Content, &ctxRaw, &provRaw, &t.CreatedAt); err != nil { return Turn{}, err }
	if aid.Valid { t.AssistantThreadID = &aid.String }
	if taskID.Valid { t.TaskID = &taskID.String }
	t.ContextJSON, t.ProvenanceJSON = json.RawMessage(ctxRaw), json.RawMessage(provRaw)
	return t, nil
}

func (s *Service) Turns(ctx context.Context, projectID string, limit int) ([]Turn, error) {
	o, err := s.Ensure(ctx, projectID); if err != nil { return nil, err }
	if limit <= 0 || limit > 200 { limit = 100 }
	rows, err := s.db.QueryContext(ctx, `SELECT id,orchestrator_id,assistant_thread_id,task_id,role,content,context_json,provenance_json,created_at FROM project_orchestrator_turns WHERE orchestrator_id=? ORDER BY created_at DESC,id DESC LIMIT ?`, o.ID, limit)
	if err != nil { return nil, err }
	defer rows.Close()
	out := []Turn{}
	for rows.Next() { t, err := scanTurn(rows); if err != nil { return nil, err }; out = append(out, t) }
	for i,j:=0,len(out)-1;i<j;i,j=i+1,j-1 { out[i],out[j]=out[j],out[i] }
	return out, rows.Err()
}

func (s *Service) contextSnapshot(ctx context.Context, projectID, requestedPWS string) (json.RawMessage, string, string, string, error) {
	var projectName, workspaceID, projectPolicy string
	if err := s.db.QueryRowContext(ctx, `SELECT name,workspace_id,project_policy_json FROM projects WHERE id=? AND status='active'`, projectID).Scan(&projectName, &workspaceID, &projectPolicy); err != nil { return nil,"","","",err }
	type pwsRow struct{ ID, Name, Settings string }
	pwsRows := []pwsRow{}
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,ai_settings_json FROM project_workspaces WHERE project_id=? AND status='active' ORDER BY updated_at DESC,id`, projectID)
	if err != nil { return nil,"","","",err }
	for rows.Next(){ var x pwsRow; if err:=rows.Scan(&x.ID,&x.Name,&x.Settings);err!=nil{rows.Close();return nil,"","","",err};pwsRows=append(pwsRows,x) }; rows.Close()
	selected := pwsRow{}
	for _,x:=range pwsRows { if requestedPWS!=""&&x.ID==requestedPWS { selected=x;break } }
	if selected.ID==""&&len(pwsRows)>0 { selected=pwsRows[0] }
	mode := "direct"; teamID := ""
	if selected.Settings!="" {
		var cfg map[string]any
		if json.Unmarshal([]byte(selected.Settings), &cfg)==nil {
			if o,ok:=cfg["orchestration"].(map[string]any);ok {
				if m,ok:=o["mode"].(string);ok { switch strings.ToLower(m){case "team","council":mode=strings.ToLower(m)} }
				if r,ok:=cfg["routing"].(map[string]any);ok { if enabled,ok:=r["enabled"].(bool);ok&&!enabled { mode="direct" } }
				if role,ok:=o[mode].(map[string]any);ok { if v,ok:=role["team_id"].(string);ok { teamID=v } }
				if v,ok:=o["team_id"].(string);ok&&teamID=="" { teamID=v }
			}
		}
	}
	tasks := []map[string]any{}
	tr, _ := s.db.QueryContext(ctx, `SELECT id,objective,state,scheduling_class,priority,created_at,updated_at FROM tasks WHERE project_id=? ORDER BY updated_at DESC LIMIT 24`, projectID)
	if tr!=nil { defer tr.Close(); for tr.Next(){var idv,obj,st,sc string;var pr int;var ca,ua int64;if tr.Scan(&idv,&obj,&st,&sc,&pr,&ca,&ua)==nil{tasks=append(tasks,map[string]any{"id":idv,"objective":obj,"state":st,"scheduling_class":sc,"priority":pr,"created_at":ca,"updated_at":ua})}} }
	events := []map[string]any{}
	er,_:=s.db.QueryContext(ctx, `SELECT sequence,event_type,aggregate_type,aggregate_id,occurred_at FROM events WHERE workspace_id=? AND (aggregate_id=? OR json_extract(payload_json,'$.project_id')=? OR aggregate_id IN (SELECT id FROM tasks WHERE project_id=?)) ORDER BY sequence DESC LIMIT 20`,workspaceID,projectID,projectID,projectID)
	if er!=nil { defer er.Close(); for er.Next(){var seq,at int64;var et,ag,aid string;if er.Scan(&seq,&et,&ag,&aid,&at)==nil{events=append(events,map[string]any{"sequence":seq,"type":et,"aggregate_type":ag,"aggregate_id":aid,"occurred_at":at})}} }
	runtime := map[string]any{}
	var runtimeID,runtimeStatus,runtimeDesired,runtimeBackend string
	var runtimeUpdated int64
	if s.db.QueryRowContext(ctx, `SELECT id,status,desired_state,backend,updated_at FROM project_runtimes WHERE project_id=? ORDER BY updated_at DESC LIMIT 1`, projectID).Scan(&runtimeID,&runtimeStatus,&runtimeDesired,&runtimeBackend,&runtimeUpdated)==nil {
		runtime=map[string]any{"id":runtimeID,"status":runtimeStatus,"desired_state":runtimeDesired,"backend":runtimeBackend,"updated_at":runtimeUpdated}
	}
	pwss:=[]map[string]any{};for _,x:=range pwsRows{pwss=append(pwss,map[string]any{"id":x.ID,"name":x.Name,"ai_settings":json.RawMessage(x.Settings)})}
	snapshot:=map[string]any{"project":map[string]any{"id":projectID,"name":projectName,"policy":json.RawMessage(projectPolicy)},"project_workspaces":pwss,"selected_project_workspace_id":selected.ID,"execution_mode":mode,"tasks":tasks,"recent_events":events,"runtime":runtime}
	if selected.ID!=""{
		readiness,err:=inspectWorkspaceToolchain(ctx,s.db,projectID,selected.ID)
		if err!=nil{return nil,"","","",fmt.Errorf("Project Workspace toolchain readiness: %w",err)}
		snapshot["toolchain_readiness"]=readiness
	}
	raw,_:=json.Marshal(snapshot)
	return raw,selected.ID,mode,teamID,nil
}

func extractText(raw []byte) string {
	var v map[string]any
	if json.Unmarshal(raw,&v)!=nil { return strings.TrimSpace(string(raw)) }
	if s,ok:=v["output_text"].(string);ok { return strings.TrimSpace(s) }
	if s,ok:=v["response"].(string);ok { return strings.TrimSpace(s) }
	if choices,ok:=v["choices"].([]any);ok&&len(choices)>0 {
		if c,ok:=choices[0].(map[string]any);ok {
			if msg,ok:=c["message"].(map[string]any);ok { if s,ok:=msg["content"].(string);ok{return strings.TrimSpace(s)} }
			if s,ok:=c["text"].(string);ok{return strings.TrimSpace(s)}
		}
	}
	if content,ok:=v["content"].([]any);ok { for _,item:=range content{if m,ok:=item.(map[string]any);ok{if s,ok:=m["text"].(string);ok{return strings.TrimSpace(s)}}} }
	b,_:=json.Marshal(v);return string(b)
}

func (s *Service) reason(ctx context.Context, workspaceID, actor, objective string, snapshot json.RawMessage) (string, map[string]any, *string, error) {
	label:=policy.DataLabel{WorkspaceID:workspaceID,Confidentiality:policy.ConfidentialityInternal,Residency:policy.ResidencyAny,Trust:policy.TrustUserInstruction}
	decision,err:=s.scheduler.Route(ctx,scheduler.RouteRequest{WorkspaceID:workspaceID,CapabilityID:"inference.general",RoleName:"project-orchestrator",ProtocolLevel:"L1",ContextTokens:8192,DataLabel:label,AllowUntested:true,AllowLimited:true,AllowMediated:true,AllowDegraded:true,PreferZeroIncrementalCost:true,AllowedKinds:[]scheduler.CandidateKind{scheduler.CandidateModel},ComputePreference:"auto"})
	if err!=nil||decision.Selected==nil{return "",nil,nil,fmt.Errorf("route project orchestrator: %w",err)}
	dep:=decision.Selected.Candidate.ID
	system:=`You are the logical OnePane Project Orchestrator. Coordinate project work but never claim direct tool, filesystem, secret, node, or approval authority. Use only the supplied bounded Project context. Return one JSON object: {"answer":"...", "action":"answer|task", "objective":"...", "project_workspace_id":"...", "mode":"direct|team|council"}. Use action=task only when the user is asking OnePane to perform work rather than explain or report state.`
	req,_:=json.Marshal(map[string]any{"messages":[]map[string]string{{"role":"system","content":system},{"role":"user","content":"Project context:\n"+string(snapshot)+"\n\nUser objective:\n"+objective}},"max_tokens":1200,"temperature":0.2})
	manifest,_:=json.Marshal(map[string]any{"project_context":json.RawMessage(snapshot),"logical_role":"project-orchestrator"})
	inf,err:=s.inference.Execute(ctx,inference.ExecuteCommand{WorkspaceID:workspaceID,PrincipalID:actor,DeploymentID:dep,CapabilityJSON:json.RawMessage(`{"id":"inference.general"}`),ContextManifestJSON:manifest,ClassificationMetadata:json.RawMessage(`{"source":"project-orchestrator"}`),InputLabel:label,RequestJSON:req,ActorPrincipalID:&actor})
	if err!=nil{return "",nil,&dep,err}
	if inf.ResponseArtifactID==nil{return "",nil,&dep,fmt.Errorf("orchestrator inference returned no response artifact")}
	rc,_,err:=s.artifacts.Open(ctx,*inf.ResponseArtifactID);if err!=nil{return "",nil,&dep,err};defer rc.Close()
	body,err:=io.ReadAll(io.LimitReader(rc,4<<20));if err!=nil{return "",nil,&dep,err}
	text:=extractText(body)
	plan:=map[string]any{};trim:=strings.TrimSpace(text);trim=strings.TrimPrefix(trim,"```json");trim=strings.TrimPrefix(trim,"```");trim=strings.TrimSuffix(trim,"```");_ = json.Unmarshal([]byte(strings.TrimSpace(trim)),&plan)
	return text,plan,&dep,nil
}

func (s *Service) Turn(ctx context.Context, c TurnCommand) (TurnResult, error) {
	c.ProjectID,c.Objective,c.ActorPrincipalID=strings.TrimSpace(c.ProjectID),strings.TrimSpace(c.Objective),strings.TrimSpace(c.ActorPrincipalID)
	if c.ProjectID==""||c.Objective==""||c.ActorPrincipalID==""{return TurnResult{},ErrInvalid}
	o,err:=s.Ensure(ctx,c.ProjectID);if err!=nil{return TurnResult{},err}
	snapshot,pwsID,mode,teamID,err:=s.contextSnapshot(ctx,c.ProjectID,c.ProjectWorkspaceID);if err!=nil{return TurnResult{},err}
	userTurnID,_:=s.ids.New("pturn");now:=s.clock.UnixMilli()
	_,_ = s.db.ExecContext(ctx,`INSERT INTO project_orchestrator_turns(id,orchestrator_id,assistant_thread_id,task_id,role,content,context_json,provenance_json,created_at) VALUES(?,?,?,NULL,'user',?,'{}','{}',?)`,userTurnID,o.ID,c.AssistantThreadID,c.Objective,now)
	_,_ = s.db.ExecContext(ctx,`UPDATE project_orchestrators SET status='working',revision=revision+1,updated_at=? WHERE id=?`,now,o.ID)
	text,plan,deploymentID,reasonErr:=s.reason(ctx,o.WorkspaceID,c.ActorPrincipalID,c.Objective,snapshot)
	answer:=strings.TrimSpace(text);action:="answer";objective:=c.Objective
	if v,ok:=plan["answer"].(string);ok&&strings.TrimSpace(v)!=""{answer=strings.TrimSpace(v)}
	if v,ok:=plan["action"].(string);ok{action=strings.ToLower(strings.TrimSpace(v))}
	if v,ok:=plan["objective"].(string);ok&&strings.TrimSpace(v)!=""{objective=strings.TrimSpace(v)}
	if v,ok:=plan["project_workspace_id"].(string);ok&&strings.TrimSpace(v)!="" {
		proposed:=strings.TrimSpace(v)
		var exists int
		if s.db.QueryRowContext(ctx,`SELECT COUNT(*) FROM project_workspaces WHERE id=? AND project_id=? AND status='active'`,proposed,c.ProjectID).Scan(&exists)==nil&&exists==1 { pwsID=proposed }
	}
	if v,ok:=plan["mode"].(string);ok {switch strings.ToLower(v){case "direct","team","council":mode=strings.ToLower(v)}}
	if reasonErr!=nil { answer="Project Orchestrator is available, but no eligible reasoning model could complete this turn: "+reasonErr.Error(); action="answer" }
	if c.ForceTask { action="task"; c.AllowTaskCreation=true }
	if action=="task" && !c.AllowTaskCreation { action="answer"; answer=answer+"\n\nThis turn was read-only; no Task was created." }
	var taskID,sessionID *string
	disposition:="answered"
	if action=="task" {
		// Use canonical relational ownership, not only model-supplied JSON.
		// When an approved toolchain exists, pin its exact digest to this Task,
		// so later approval changes cannot silently substitute a build image.
		completionState:=map[string]any{"source":"project-orchestrator","project_workspace_id":pwsID,"requested_mode":mode}
		var canonicalPWS *string
		if pwsID!=""{
			canonicalPWS=&pwsID
			readiness,checkErr:=inspectWorkspaceToolchain(ctx,s.db,c.ProjectID,pwsID)
			if checkErr!=nil{return TurnResult{},checkErr}
			if readiness.ManifestSHA256!=""{
				completionState["toolchain_manifest_sha256"]=readiness.ManifestSHA256
			}
			if readiness.Status!="ready_for_preflight"{
				answer=answer+"\\n\\nWorkspace toolchain: "+readiness.Status+". "+readiness.NextAction
			}
		}
		completion,_:=json.Marshal(completionState)
		t,terr:=s.tasks.Create(ctx,task.CreateCommand{WorkspaceID:o.WorkspaceID,ProjectID:&c.ProjectID,ProjectWorkspaceID:canonicalPWS,Objective:objective,SchedulingClass:task.ClassUserInteractive,Priority:10,Completion:completion,ActorPrincipalID:&c.ActorPrincipalID})
		if terr!=nil { disposition="blocked"; answer=answer+"\n\nTask creation was blocked: "+terr.Error() } else {
			taskID=&t.ID;disposition="task_created"
			if (mode=="team"||mode=="council")&&teamID!=""&&s.teams!=nil {
				cfg,_:=json.Marshal(map[string]any{"source":"project-orchestrator","project_workspace_id":pwsID})
				if ss,e:=s.teams.StartSession(ctx,team.StartSessionCommand{TaskID:t.ID,TeamID:teamID,CreatedBy:c.ActorPrincipalID,ExecutionMode:mode,Config:cfg});e==nil{sessionID=&ss.ID;disposition="team_session_started"}else{disposition="waiting";answer=answer+"\n\nTask was created, but the configured "+mode+" session could not start: "+e.Error()}
			} else if mode=="team"||mode=="council" {
				disposition="waiting";answer=answer+"\n\nTask was created and is waiting for a configured "+mode+" team."
			}
		}
	}
	if answer=="" { answer="Project context reviewed." }
	prov:=map[string]any{"project_id":c.ProjectID,"orchestrator_id":o.ID,"project_workspace_id":pwsID,"execution_mode":mode}
	if deploymentID!=nil{prov["candidate_kind"]="model_deployment";prov["candidate_id"]=*deploymentID}
	if taskID!=nil{prov["task_id"]=*taskID}
	if sessionID!=nil{prov["team_session_id"]=*sessionID}
	provRaw,_:=json.Marshal(prov)
	turnID,_:=s.ids.New("pturn");created:=s.clock.UnixMilli()
	_,err=s.db.ExecContext(ctx,`INSERT INTO project_orchestrator_turns(id,orchestrator_id,assistant_thread_id,task_id,role,content,context_json,provenance_json,created_at) VALUES(?,?,?,?, 'orchestrator',?,?,?,?)`,turnID,o.ID,c.AssistantThreadID,taskID,answer,string(snapshot),string(provRaw),created)
	if err!=nil{return TurnResult{},err}
	status:="ready";if disposition=="waiting"||disposition=="blocked"{status="waiting"};if reasonErr!=nil{status="degraded"}
	_,_ = s.db.ExecContext(ctx,`UPDATE project_orchestrators SET status=?,revision=revision+1,updated_at=? WHERE id=?`,status,created,o.ID)
	turn:=Turn{ID:turnID,OrchestratorID:o.ID,AssistantThreadID:c.AssistantThreadID,TaskID:taskID,Role:"orchestrator",Content:answer,ContextJSON:snapshot,ProvenanceJSON:provRaw,CreatedAt:created}
	return TurnResult{Turn:turn,Disposition:disposition,TaskID:taskID,ProjectWorkspaceID:&pwsID,Mode:&mode,TeamSessionID:sessionID},nil
}
