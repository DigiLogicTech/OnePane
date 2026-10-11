package task

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/storage"
)

func hasTaskGraphProvenance(raw json.RawMessage) bool {
	var provenance struct {
		Source string `json:"source"`
		GraphID string `json:"graph_id"`
	}
	return json.Unmarshal(raw,&provenance)==nil &&
		(provenance.Source=="project-orchestrator-task-graph"||
			strings.TrimSpace(provenance.GraphID)!="")
}

// approvedTaskGraphDependenciesTx enforces the graph's independently
// operator-attested predecessor list at the actual Task/Attempt admission
// boundary. Graph readiness/reporting is only advisory; deleted SQL edges
// MUST NOT make previously blocked Tasks executable.
//
// Legacy/unattested graph nodes fail closed. Ordinary non-graph Tasks retain
// their historical dependency policy and are not impacted.
func approvedTaskGraphDependenciesTx(ctx context.Context, tx storage.Tx, t Task) error {
	var graphID,nodeKey,nodeWorkspace,projectID,tenant,approvedJSON string
	var recorded int
	var activeWorkspace sql.NullString
	err:=tx.QueryRowContext(ctx,`SELECT n.graph_id,n.node_key,
		n.project_workspace_id,g.project_id,g.workspace_id,
		n.approved_dependencies_json,n.approved_dependencies_recorded,pw.id
		FROM project_orchestrator_task_graph_nodes n
		JOIN project_orchestrator_task_graphs g ON g.id=n.graph_id
		LEFT JOIN project_workspaces pw ON pw.id=n.project_workspace_id
			AND pw.project_id=g.project_id AND pw.status='active'
		WHERE n.task_id=?`,t.ID).Scan(&graphID,&nodeKey,&nodeWorkspace,
			&projectID,&tenant,&approvedJSON,&recorded,&activeWorkspace)
	if errors.Is(err,sql.ErrNoRows){
		// A Task created as part of an approved graph must not turn into
		// an ordinary Task if its canonical graph-node row is missing.
		if hasTaskGraphProvenance(t.Completion) {
			return ErrHardDependencyUnsatisfied
		}
		return nil
	}
	if err!=nil{return err}
	if t.ProjectID==nil||t.ProjectWorkspaceID==nil||
		*t.ProjectID!=projectID||*t.ProjectWorkspaceID!=nodeWorkspace||
		t.WorkspaceID!=tenant||t.ArchivedAt!=nil||!activeWorkspace.Valid||
		recorded!=1||strings.TrimSpace(approvedJSON)=="null"{
		return ErrHardDependencyUnsatisfied
	}
	// Preserve bounded DAG validation even if a persisted row is corrupted.
	var expected []string
	if json.Unmarshal([]byte(approvedJSON),&expected)!=nil||len(expected)>8 {
		return ErrHardDependencyUnsatisfied
	}
	expectedKeys:=make(map[string]bool,len(expected))
	for _,key:=range expected{
		if key==""||key==nodeKey||expectedKeys[key]{
			return ErrHardDependencyUnsatisfied
		}
		expectedKeys[key]=true
	}
	rows,err:=tx.QueryContext(ctx,`SELECT COALESCE(pred.node_key,''),
		d.dependency_type,d.dependency_mode,
		COALESCE(pred.approved_dependencies_recorded,0),
		COALESCE(pw.status,''),dep.workspace_id,dep.project_id,
		dep.project_workspace_id,pred.project_workspace_id,
		dep.archived_at
		FROM task_dependencies d
		JOIN tasks dep ON dep.id=d.depends_on_task_id
		LEFT JOIN project_orchestrator_task_graph_nodes pred
			ON pred.task_id=dep.id AND pred.graph_id=?
		LEFT JOIN project_workspaces pw ON pw.id=pred.project_workspace_id
			AND pw.project_id=?
		WHERE d.task_id=?`,graphID,projectID,t.ID)
	if err!=nil{return err}
	seen:=map[string]bool{}
	for rows.Next(){
		var key,kind,mode,predTenant string
		var recordedPred int
		var status string
		var predProject,predActual,predCanonical sql.NullString
		var archived sql.NullInt64
		if err=rows.Scan(&key,&kind,&mode,&recordedPred,&status,
			&predTenant,&predProject,&predActual,&predCanonical,&archived);err!=nil{break}
		if kind!="hard"||mode!="all"||!expectedKeys[key]||seen[key]||
			recordedPred!=1||status!="active"||predTenant!=tenant||
			!predProject.Valid||predProject.String!=projectID||
			!predActual.Valid||!predCanonical.Valid||
			predActual.String!=predCanonical.String||archived.Valid{
			err=ErrHardDependencyUnsatisfied
			break
		}
		seen[key]=true
	}
	if err==nil{err=rows.Err()}
	closeErr:=rows.Close()
	if err!=nil{return err}
	if closeErr!=nil{return closeErr}
	if len(seen)!=len(expectedKeys){return ErrHardDependencyUnsatisfied}
	return nil
}
