package projectorchestrator

import (
 "context"
 "crypto/sha256"
 "database/sql"
 "encoding/hex"
 "encoding/json"
 "errors"
 "regexp"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/event"
 "github.com/DigiLogicTech/OnePane/internal/storage"
 "github.com/DigiLogicTech/OnePane/internal/task"
)

// Explicit operator-reviewed DAG, not a model-authored request to execute.
// The graph controls *scheduling order*, not Library, network or secret access.
type TaskGraphNodeSpec struct {
 Key string `json:"key"`
 ProjectWorkspaceID string `json:"project_workspace_id"`
 Objective string `json:"objective"`
 DependsOn []string `json:"depends_on,omitempty"`
 Priority int `json:"priority,omitempty"`
}
type CreateTaskGraphCommand struct {
 ProjectID string `json:"project_id"`
 Name string `json:"name"`
 IdempotencyKey string `json:"idempotency_key"`
 Nodes []TaskGraphNodeSpec `json:"nodes"`
 ActorPrincipalID string `json:"-"`
}
type TaskGraphNode struct {
 Key string `json:"key"`
 TaskID string `json:"task_id"`
 ProjectWorkspaceID string `json:"project_workspace_id"`
 DependsOn []string `json:"depends_on"`
 State task.State `json:"state"`
}
type TaskGraph struct {
 ID string `json:"id"`
 ProjectID string `json:"project_id"`
 WorkspaceID string `json:"workspace_id"`
 Name string `json:"name"`
 IdempotencyKey string `json:"idempotency_key"`
 ManifestSHA256 string `json:"manifest_sha256"`
 CreatedBy string `json:"created_by"`
 CreatedAt int64 `json:"created_at"`
 Nodes []TaskGraphNode `json:"nodes"`
}

var graphKeyRE=regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,39}$`)
var idempotencyRE=regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,119}$`)
var ErrGraphConflict=errors.New("Project Task graph idempotency key used with different plan")

// graphCreationPlan rejects cycles and unknown/duplicate dependencies before
// any Task is inserted; independent branches can be admitted by the Worker.
func graphCreationPlan(c CreateTaskGraphCommand)([]int,string,error){
 if c.ProjectID==""||c.ActorPrincipalID==""||len(c.Name)<1||
  len(c.Name)>120||!idempotencyRE.MatchString(c.IdempotencyKey)||
  len(c.Nodes)<1||len(c.Nodes)>32{return nil,"",ErrInvalid}
 indexes:=map[string]int{}
 for i,n:=range c.Nodes{
  if !graphKeyRE.MatchString(n.Key)||n.ProjectWorkspaceID==""||
   n.Objective==""||len(n.Objective)>4096||n.Priority<0||n.Priority>100||
   len(n.DependsOn)>8{return nil,"",ErrInvalid}
  if _,exists:=indexes[n.Key];exists{return nil,"",ErrInvalid}
  indexes[n.Key]=i
 }
 indegree:=make([]int,len(c.Nodes))
 dependents:=make([][]int,len(c.Nodes))
 for i,n:=range c.Nodes{
  seen:=map[string]bool{}
  for _,key:=range n.DependsOn{
   j,exists:=indexes[key]
   if !exists||j==i||seen[key]{return nil,"",ErrInvalid}
   seen[key]=true
   indegree[i]++
   dependents[j]=append(dependents[j],i)
  }
 }
 order:=make([]int,0,len(c.Nodes))
 // A stable topological order is kept in the canonical persisted manifest.
 for len(order)<len(c.Nodes){
  next:=-1
  for i:=range c.Nodes{
   if indegree[i]==0{next=i;break}
  }
  if next<0{return nil,"",ErrInvalid}
  indegree[next]=-1
  order=append(order,next)
  for _,dependent:=range dependents[next]{indegree[dependent]--}
 }
 canonical:=make([]TaskGraphNodeSpec,0,len(c.Nodes))
 for _,i:=range order{
  n:=c.Nodes[i]
  n.DependsOn=append([]string{},n.DependsOn...)
  // Topological order and the caller's explicit dependency ordering are
  // part of the reviewed manifest; retrying the same plan is idempotent.
  canonical=append(canonical,n)
 }
 raw,_:=json.Marshal(struct{
  ProjectID string `json:"project_id"`
  Name string `json:"name"`
  Nodes []TaskGraphNodeSpec `json:"nodes"`
 }{c.ProjectID,c.Name,canonical})
 digest:=sha256.Sum256(raw)
 return order,"sha256:"+hex.EncodeToString(digest[:]),nil
}

type graphTaskCreator interface{
 CreateInTransaction(context.Context,storage.Tx,task.CreateCommand)(task.Task,error)
}

// CreateTaskGraph atomically persists Task+event+outbox for every node, all
// dependency edges, graph identity and provenance. No inference or runtime
// execution happens here. A missing Task implementation fails closed.
func(s *Service) CreateTaskGraph(ctx context.Context,c CreateTaskGraphCommand)(TaskGraph,error){
 c.ProjectID=strings.TrimSpace(c.ProjectID)
 c.Name=strings.TrimSpace(c.Name)
 c.IdempotencyKey=strings.TrimSpace(c.IdempotencyKey)
 c.ActorPrincipalID=strings.TrimSpace(c.ActorPrincipalID)
 for i:=range c.Nodes{c.Nodes[i].Objective=strings.TrimSpace(c.Nodes[i].Objective)}
 order,hash,err:=graphCreationPlan(c)
 if err!=nil{return TaskGraph{},err}
 if s==nil||s.db==nil||s.tx==nil{return TaskGraph{},ErrInvalid}
 creator,ok:=s.tasks.(graphTaskCreator)
 if !ok{return TaskGraph{},ErrInvalid}
 graphID,err:=s.ids.New("ptgraph")
 if err!=nil{return TaskGraph{},err}
 eventID,err:=s.ids.New("evt")
 if err!=nil{return TaskGraph{},err}
 now:=s.clock.UnixMilli()
 resolvedID:=""
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  var tenant,principalType string
  err:=tx.QueryRowContext(ctx,`SELECT p.workspace_id,principals.principal_type
   FROM projects p
   JOIN workspaces w ON w.id=p.workspace_id AND w.status='active'
   JOIN workspace_memberships m ON m.workspace_id=w.id
    AND m.principal_id=? AND m.status='active'
   JOIN principals ON principals.id=m.principal_id AND principals.status='active'
   WHERE p.id=? AND p.status='active'`,c.ActorPrincipalID,c.ProjectID).
    Scan(&tenant,&principalType)
  if err!=nil{return err}
  if principalType!="human"{return ErrInvalid}
  var existingID,existingDigest string
  lookup:=tx.QueryRowContext(ctx,`SELECT id,manifest_sha256
   FROM project_orchestrator_task_graphs WHERE project_id=? AND idempotency_key=?`,
   c.ProjectID,c.IdempotencyKey).Scan(&existingID,&existingDigest)
  if lookup==nil{
   if existingDigest!=hash{return ErrGraphConflict}
   resolvedID=existingID
   return nil
  }
  if !errors.Is(lookup,sql.ErrNoRows){return lookup}
  for _,n:=range c.Nodes{
   var count int
   if err=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM project_workspaces
    WHERE id=? AND project_id=? AND status='active'`,n.ProjectWorkspaceID,c.ProjectID).
    Scan(&count);err!=nil{return err}
   if count!=1{return ErrInvalid}
  }
  _,err=tx.ExecContext(ctx,`INSERT INTO project_orchestrator_task_graphs(
   id,project_id,workspace_id,name,idempotency_key,manifest_sha256,
   node_count,created_by,created_at) VALUES(?,?,?,?,?,?,?,?,?)`,
   graphID,c.ProjectID,tenant,c.Name,c.IdempotencyKey,hash,
   len(c.Nodes),c.ActorPrincipalID,now)
  if err!=nil{return err}
  tasksByKey:=map[string]string{}
  for position,index:=range order{
   n:=c.Nodes[index]
   completion,_:=json.Marshal(map[string]any{
    "source":"project-orchestrator-task-graph",
    "graph_id":graphID,"node_key":n.Key,
    "dependency_policy":"hard_all",
    "operator_approved":true,
    "routing_intent":"local_first",
   })
   created,createErr:=creator.CreateInTransaction(ctx,tx,task.CreateCommand{
    WorkspaceID:tenant,ProjectID:&c.ProjectID,
    ProjectWorkspaceID:&n.ProjectWorkspaceID,Objective:n.Objective,
    SchedulingClass:task.ClassNormal,Priority:n.Priority,Completion:completion,
    ActorPrincipalID:&c.ActorPrincipalID,
   })
   if createErr!=nil{return createErr}
   tasksByKey[n.Key]=created.ID
   _,err=tx.ExecContext(ctx,`INSERT INTO project_orchestrator_task_graph_nodes(
    graph_id,node_key,task_id,project_workspace_id,position)
    VALUES(?,?,?,?,?)`,graphID,n.Key,created.ID,n.ProjectWorkspaceID,position)
   if err!=nil{return err}
  }
  for _,n:=range c.Nodes{
   for _,predecessor:=range n.DependsOn{
    _,err=tx.ExecContext(ctx,`INSERT INTO task_dependencies(
      task_id,depends_on_task_id,dependency_type,dependency_mode,metadata_json)
      VALUES(?,?,'hard','all',?)`,tasksByKey[n.Key],tasksByKey[predecessor],
      `{"source":"operator_approved_project_graph"}`)
    if err!=nil{return err}
   }
  }
  payload,_:=json.Marshal(map[string]any{
   "graph_id":graphID,"project_id":c.ProjectID,"manifest_sha256":hash,
   "node_count":len(c.Nodes),"operator_approved":true,
  })
  if err=s.events.Append(ctx,tx,event.Event{
   ID:eventID,WorkspaceID:&tenant,Type:"project.task_graph.created",
   AggregateType:"project_task_graph",AggregateID:graphID,
   ActorPrincipalID:&c.ActorPrincipalID,Payload:payload,OccurredAt:now,
  });err!=nil{return err}
  resolvedID=graphID
  return nil
 })
 if err!=nil{return TaskGraph{},err}
 return s.TaskGraph(ctx,c.ProjectID,resolvedID)
}

// TaskGraph returns canonical node ownership, completion state and dependency
// keys to authorized callers (the API requires project.read). It never
// exposes another Project's graph even if the opaque ID is guessed.
func(s *Service) TaskGraph(ctx context.Context,projectID,graphID string)(TaskGraph,error){
 if s==nil||s.db==nil||projectID==""||graphID==""{return TaskGraph{},ErrInvalid}
 var x TaskGraph
 err:=s.db.QueryRowContext(ctx,`SELECT g.id,g.project_id,g.workspace_id,
  g.name,g.idempotency_key,g.manifest_sha256,g.created_by,g.created_at
  FROM project_orchestrator_task_graphs g
  JOIN projects p ON p.id=g.project_id AND p.status='active'
  WHERE g.id=? AND g.project_id=?`,graphID,projectID).
  Scan(&x.ID,&x.ProjectID,&x.WorkspaceID,&x.Name,&x.IdempotencyKey,
   &x.ManifestSHA256,&x.CreatedBy,&x.CreatedAt)
 if err!=nil{return TaskGraph{},err}
 rows,err:=s.db.QueryContext(ctx,`SELECT n.node_key,n.task_id,
  n.project_workspace_id,t.state
  FROM project_orchestrator_task_graph_nodes n
  JOIN tasks t ON t.id=n.task_id AND t.project_id=?
  WHERE n.graph_id=? ORDER BY n.position`,projectID,graphID)
 if err!=nil{return TaskGraph{},err}
 x.Nodes=[]TaskGraphNode{}
 for rows.Next(){
  var n TaskGraphNode
  if err=rows.Scan(&n.Key,&n.TaskID,&n.ProjectWorkspaceID,&n.State);err!=nil{break}
  n.DependsOn=[]string{}
  x.Nodes=append(x.Nodes,n)
 }
 if err==nil{err=rows.Err()}
 _=rows.Close()
 if err!=nil{return TaskGraph{},err}
 for i:=range x.Nodes{
  depRows,depErr:=s.db.QueryContext(ctx,`SELECT pred.node_key
   FROM task_dependencies d
   JOIN project_orchestrator_task_graph_nodes pred
    ON pred.task_id=d.depends_on_task_id AND pred.graph_id=?
   WHERE d.task_id=? AND d.dependency_type='hard'
   ORDER BY pred.position`,graphID,x.Nodes[i].TaskID)
  if depErr!=nil{return TaskGraph{},depErr}
  for depRows.Next(){
   var key string
   if depErr=depRows.Scan(&key);depErr!=nil{break}
   x.Nodes[i].DependsOn=append(x.Nodes[i].DependsOn,key)
  }
  if depErr==nil{depErr=depRows.Err()}
  _=depRows.Close()
  if depErr!=nil{return TaskGraph{},depErr}
 }
 return x,nil
}
