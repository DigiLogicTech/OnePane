package projectorchestrator

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/storage"
	"github.com/DigiLogicTech/OnePane/internal/task"
)

// ReattestLegacyTaskGraphCommand is an explicit, human-reviewed reissue of
// the ORIGINAL graph plan. It never accepts mutable SQL edges as a new plan.
type ReattestLegacyTaskGraphCommand struct {
	ProjectID string `json:"-"`
	GraphID string `json:"-"`
	ExpectedManifestSHA256 string `json:"expected_manifest_sha256"`
	Nodes []TaskGraphNodeSpec `json:"nodes"`
	ConfirmOperatorReview bool `json:"confirm_operator_review"`
	ActorPrincipalID string `json:"-"`
}

// ReattestLegacyTaskGraph never replays work, changes dependency rows, or
// attests a graph after any attempt or external operation was recorded.
// A historical graph can only be cleared when the human-supplied complete
// plan hashes to the ORIGINAL stored manifest and EXACT live edges and
// Task ownership match it in one transaction.
func (s *Service) ReattestLegacyTaskGraph(ctx context.Context, c ReattestLegacyTaskGraphCommand) (TaskGraph,error) {
	c.ProjectID=strings.TrimSpace(c.ProjectID)
	c.GraphID=strings.TrimSpace(c.GraphID)
	c.ActorPrincipalID=strings.TrimSpace(c.ActorPrincipalID)
	c.ExpectedManifestSHA256=strings.TrimSpace(c.ExpectedManifestSHA256)
	if s==nil||s.db==nil||s.tx==nil||!c.ConfirmOperatorReview||
		c.ProjectID==""||c.GraphID==""||c.ActorPrincipalID==""||
		len(c.Nodes)==0||len(c.Nodes)>32||len(c.ExpectedManifestSHA256)!=71 {
		return TaskGraph{},ErrInvalid
	}
	// Always normalize the same way as the original approved Create path.
	reviewed:=append([]TaskGraphNodeSpec(nil),c.Nodes...)
	for i:=range reviewed {
		reviewed[i].Objective=strings.TrimSpace(reviewed[i].Objective)
		if reviewed[i].ComputePreference==""{reviewed[i].ComputePreference="auto"}
	}
	eid,err:=s.ids.New("evt")
	if err!=nil{return TaskGraph{},err}
	err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
		var tenant,typ,graphName,idempotencyKey,originalHash string
		var nodeCount int
		err:=tx.QueryRowContext(ctx,`SELECT g.workspace_id,ppl.principal_type,
			g.name,g.idempotency_key,g.manifest_sha256,g.node_count
			FROM project_orchestrator_task_graphs g
			JOIN projects p ON p.id=g.project_id AND p.status='active'
				AND p.workspace_id=g.workspace_id
			JOIN workspaces w ON w.id=p.workspace_id AND w.status='active'
			JOIN workspace_memberships m ON m.workspace_id=w.id
				AND m.principal_id=? AND m.status='active'
			JOIN principals ppl ON ppl.id=m.principal_id AND ppl.status='active'
			WHERE g.id=? AND g.project_id=?`,c.ActorPrincipalID,c.GraphID,c.ProjectID).
			Scan(&tenant,&typ,&graphName,&idempotencyKey,&originalHash,&nodeCount)
		if err!=nil{return err}
		if typ!="human"||nodeCount!=len(reviewed)||originalHash!=c.ExpectedManifestSHA256 {
			return ErrInvalid
		}
		plan:=CreateTaskGraphCommand{
			ProjectID:c.ProjectID,Name:graphName,IdempotencyKey:idempotencyKey,
			ActorPrincipalID:c.ActorPrincipalID,Nodes:reviewed,
		}
		order,reviewHash,err:=graphCreationPlan(plan)
		if err!=nil||reviewHash!=originalHash{return ErrGraphConflict}
		// The original manifest includes topology and the relative order of
		// nodes. Preserve the same canonical topological position at review.
		type persistedNode struct{
			Key,TaskID,WorkspaceID,Tenant,Objective,CompletionState,Completion string
			Priority,Position,Recorded int
			ActualWorkspace,ActiveWorkspace sql.NullString
			Archived sql.NullInt64
		}
		rows,err:=tx.QueryContext(ctx,`SELECT n.node_key,n.task_id,n.project_workspace_id,
			n.position,n.approved_dependencies_recorded,
			t.workspace_id,t.project_workspace_id,t.objective,t.priority,t.state,
			t.archived_at,t.completion_json,pw.id
			FROM project_orchestrator_task_graph_nodes n
			JOIN tasks t ON t.id=n.task_id AND t.project_id=?
			LEFT JOIN project_workspaces pw ON pw.id=n.project_workspace_id
				AND pw.project_id=? AND pw.status='active'
			WHERE n.graph_id=? ORDER BY n.position`,c.ProjectID,c.ProjectID,c.GraphID)
		if err!=nil{return err}
		live:=make([]persistedNode,0,nodeCount)
		for rows.Next(){
			var n persistedNode
			if err=rows.Scan(&n.Key,&n.TaskID,&n.WorkspaceID,&n.Position,&n.Recorded,
				&n.Tenant,&n.ActualWorkspace,&n.Objective,&n.Priority,&n.CompletionState,
				&n.Archived,&n.Completion,&n.ActiveWorkspace);err!=nil{break}
			live=append(live,n)
		}
		if err==nil{err=rows.Err()}
		closeErr:=rows.Close()
		if err!=nil{return err}
		if closeErr!=nil{return closeErr}
		if len(live)!=nodeCount{return ErrGraphConflict}
		ids:=make(map[string]string,len(live))
		for i,n:=range live {
			spec:=reviewed[order[i]]
			if n.Recorded!=0||n.Position!=i||n.Key!=spec.Key||
				n.WorkspaceID!=spec.ProjectWorkspaceID||
				!n.ActualWorkspace.Valid||n.ActualWorkspace.String!=n.WorkspaceID||
				!n.ActiveWorkspace.Valid||n.Tenant!=tenant||
				n.Objective!=spec.Objective||n.Priority!=spec.Priority||
				n.Archived.Valid||
				(n.CompletionState!=string(task.StateCreated)&&n.CompletionState!=string(task.StateReady)){
				return ErrGraphConflict
			}
			var cfg struct{OnePaneRouting struct{ComputePreference string `json:"compute_preference"`} `json:"onepane_routing"`}
			if json.Unmarshal([]byte(n.Completion),&cfg)!=nil{return ErrGraphConflict}
			compute:=cfg.OnePaneRouting.ComputePreference
			if compute==""{compute="auto"}
			if compute!=spec.ComputePreference{return ErrGraphConflict}
			ids[n.Key]=n.TaskID
		}
		for i,n:=range live{
			spec:=reviewed[order[i]]
			var attempts,operations int
			if err=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM task_attempts WHERE task_id=?`,n.TaskID).Scan(&attempts);err!=nil{return err}
			if err=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM operations WHERE task_id=?`,n.TaskID).Scan(&operations);err!=nil{return err}
			if attempts!=0||operations!=0{return ErrGraphConflict}
			deps,err:=tx.QueryContext(ctx,`SELECT d.depends_on_task_id,d.dependency_type,d.dependency_mode
				FROM task_dependencies d WHERE d.task_id=?`,n.TaskID)
			if err!=nil{return err}
			seen:=map[string]bool{}
			for deps.Next(){
				var predecessorID,kind,mode string
				if err=deps.Scan(&predecessorID,&kind,&mode);err!=nil{break}
				if kind!="hard"||mode!="all"||seen[predecessorID]{err=ErrGraphConflict;break}
				seen[predecessorID]=true
			}
			if err==nil{err=deps.Err()}
			closeErr:=deps.Close()
			if err!=nil{return err}
			if closeErr!=nil{return closeErr}
			if len(seen)!=len(spec.DependsOn){return ErrGraphConflict}
			for _,key:=range spec.DependsOn{
				id,ok:=ids[key]
				if !ok||!seen[id]{return ErrGraphConflict}
				delete(seen,id)
			}
			if len(seen)!=0{return ErrGraphConflict}
		}
		// No silent adoption of a changed graph. At most one attestation
		// record, made by an authorized human, can unlock this legacy plan.
		var ledgerCount int
		if err=tx.QueryRowContext(ctx,`SELECT COUNT(*) FROM project_orchestrator_graph_reattestations WHERE graph_id=?`,c.GraphID).Scan(&ledgerCount);err!=nil{return err}
		if ledgerCount!=0{return ErrGraphConflict}
		for i,n:=range live{
			spec:=reviewed[order[i]]
			raw,_:=json.Marshal(append([]string{},spec.DependsOn...))
			result,e:=tx.ExecContext(ctx,`UPDATE project_orchestrator_task_graph_nodes
				SET approved_dependencies_json=?,approved_dependencies_recorded=1
				WHERE graph_id=? AND node_key=? AND approved_dependencies_recorded=0`,
				string(raw),c.GraphID,n.Key)
			if e!=nil{return e}
			count,e:=result.RowsAffected()
			if e!=nil{return e}
			if count!=1{return ErrGraphConflict}
		}
		now:=s.clock.UnixMilli()
		reviewBytes,_:=json.Marshal(struct{
			GraphID string `json:"graph_id"`
			Manifest string `json:"manifest"`
			Nodes []TaskGraphNodeSpec `json:"nodes"`
			Actor string `json:"actor"`
		}{c.GraphID,originalHash,reviewed,c.ActorPrincipalID})
		digest:=sha256.Sum256(reviewBytes)
		receipt:=hex.EncodeToString(digest[:])
		_,err=tx.ExecContext(ctx,`INSERT INTO project_orchestrator_graph_reattestations(
			graph_id,project_id,manifest_sha256,reviewed_by,reviewed_at,review_evidence_sha256
			) VALUES(?,?,?,?,?,?)`,c.GraphID,c.ProjectID,originalHash,c.ActorPrincipalID,now,receipt)
		if err!=nil{return err}
		actor:=c.ActorPrincipalID
		payload,_:=json.Marshal(map[string]any{
			"graph_id":c.GraphID,"project_id":c.ProjectID,
			"manifest_sha256":originalHash,"review_evidence_sha256":receipt,
			"node_count":nodeCount,"operator_reviewed":true,
			"effect":"metadata_only_no_task_replay",
		})
		return s.events.Append(ctx,tx,event.Event{
			ID:eid,WorkspaceID:&tenant,Type:"project.task_graph.legacy_reattested",
			AggregateType:"project_task_graph",AggregateID:c.GraphID,
			ActorPrincipalID:&actor,Payload:payload,OccurredAt:now,
		})
	})
	if err!=nil{
		if errors.Is(err,sql.ErrNoRows){return TaskGraph{},ErrInvalid}
		return TaskGraph{},fmt.Errorf("legacy Task graph reattestation: %w",err)
	}
	return s.TaskGraph(ctx,c.ProjectID,c.GraphID)
}
