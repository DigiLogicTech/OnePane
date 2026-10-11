package projectorchestrator

import (
	"context"
	"database/sql"
	"fmt"
)

// workspaceOrchestratorActivity is the only Task/event projection shared with
// a Project Orchestrator reasoning model. Cross-Workspace collaboration uses
// explicitly published and granted artifacts; membership in the same Project
// is not an implicit grant for private Task objectives or event identities.
func workspaceOrchestratorActivity(ctx context.Context, db *sql.DB,
	tenantID, projectID, selectedWorkspaceID string,
) ([]map[string]any, []map[string]any, error) {
	if db == nil {
		return nil, nil, fmt.Errorf("Project activity database unavailable")
	}
	tasks := []map[string]any{}
	events := []map[string]any{}
	if selectedWorkspaceID != "" {
		rows, err := db.QueryContext(ctx,`SELECT id,objective,state,scheduling_class,
			priority,created_at,updated_at FROM tasks
			WHERE workspace_id=? AND project_id=? AND project_workspace_id=?
			ORDER BY updated_at DESC,id DESC LIMIT 24`,
			tenantID,projectID,selectedWorkspaceID)
		if err != nil { return nil,nil,err }
		for rows.Next() {
			var id,objective,state,schedule string
			var priority int
			var created,updated int64
			if err := rows.Scan(&id,&objective,&state,&schedule,&priority,&created,&updated);err!=nil{
				rows.Close()
				return nil,nil,err
			}
			tasks=append(tasks,map[string]any{
				"id":id,"objective":objective,"state":state,
				"scheduling_class":schedule,"priority":priority,
				"created_at":created,"updated_at":updated,
			})
		}
		err=rows.Err()
		closeErr:=rows.Close()
		if err!=nil {return nil,nil,err}
		if closeErr!=nil {return nil,nil,closeErr}
	}
	// Only Project aggregate events and the selected Workspace's Task
	// events. Payload JSON is never copied into model context. In particular,
	// event payloads with a matching Project ID are not an access grant.
	rows,err:=db.QueryContext(ctx,`SELECT e.sequence,e.event_type,
		e.aggregate_type,e.aggregate_id,e.occurred_at
		FROM events e
		WHERE e.workspace_id=? AND (
			(e.aggregate_type='project' AND e.aggregate_id=?)
			OR (e.aggregate_type='task' AND e.aggregate_id IN (
				SELECT t.id FROM tasks t WHERE t.workspace_id=?
				AND t.project_id=? AND t.project_workspace_id=?
			))
		)
		ORDER BY e.sequence DESC LIMIT 20`,
		tenantID,projectID,tenantID,projectID,selectedWorkspaceID)
	if err!=nil{return nil,nil,err}
	for rows.Next(){
		var sequence,occurred int64
		var eventType,aggregateType,aggregateID string
		if err:=rows.Scan(&sequence,&eventType,&aggregateType,&aggregateID,&occurred);err!=nil{
			rows.Close()
			return nil,nil,err
		}
		events=append(events,map[string]any{
			"sequence":sequence,"type":eventType,
			"aggregate_type":aggregateType,"aggregate_id":aggregateID,
			"occurred_at":occurred,
		})
	}
	err=rows.Err()
	closeErr:=rows.Close()
	if err!=nil{return nil,nil,err}
	if closeErr!=nil{return nil,nil,closeErr}
	return tasks,events,nil
}
