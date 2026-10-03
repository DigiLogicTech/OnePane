package projectworkspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type WorkspaceView struct {
	ID                string          `json:"id"`
	ProjectID         string          `json:"project_id"`
	Name              string          `json:"name"`
	Description       *string         `json:"description,omitempty"`
	Status            string          `json:"status"`
	LayoutJSON        json.RawMessage `json:"layout"`
	AISettingsJSON    json.RawMessage `json:"ai_settings"`
	ResourceScopeJSON json.RawMessage `json:"resource_scope"`
	StateJSON         json.RawMessage `json:"state"`
	StorageRoot       *string         `json:"storage_root,omitempty"`
	Revision          int64           `json:"revision"`
	CreatedAt         int64           `json:"created_at"`
	UpdatedAt         int64           `json:"updated_at"`
}

type CreateWorkspaceViewCommand struct {
	ProjectID string
	Name string
	Description *string
	LayoutJSON json.RawMessage
	AISettingsJSON json.RawMessage
	ResourceScopeJSON json.RawMessage
	StateJSON json.RawMessage
	StorageRoot *string
	ActorPrincipalID string
}

type UpdateWorkspaceViewCommand struct {
	WorkspaceID string
	ExpectedRevision int64
	Name string
	Description *string
	LayoutJSON json.RawMessage
	AISettingsJSON json.RawMessage
	ResourceScopeJSON json.RawMessage
	StateJSON json.RawMessage
	StorageRoot *string
	ActorPrincipalID string
}

func canonicalObjectOr(raw json.RawMessage, fallback string) (json.RawMessage, error) {
	if len(raw) == 0 { raw = json.RawMessage(fallback) }
	var v any
	if err := json.Unmarshal(raw, &v); err != nil { return nil, err }
	b, err := json.Marshal(v)
	if err != nil { return nil, err }
	return json.RawMessage(b), nil
}

func scanWorkspaceView(row scanner) (WorkspaceView, error) {
	var x WorkspaceView
	var desc, root sql.NullString
	var layout, ai, scope, state string
	err := row.Scan(&x.ID, &x.ProjectID, &x.Name, &desc, &x.Status, &layout, &ai, &scope, &state, &root, &x.Revision, &x.CreatedAt, &x.UpdatedAt)
	if err != nil { return x, err }
	if desc.Valid { v:=desc.String; x.Description=&v }
	if root.Valid { v:=root.String; x.StorageRoot=&v }
	x.LayoutJSON, x.AISettingsJSON, x.ResourceScopeJSON, x.StateJSON = json.RawMessage(layout), json.RawMessage(ai), json.RawMessage(scope), json.RawMessage(state)
	return x, nil
}

const workspaceViewSelect = `SELECT id,project_id,name,description,status,layout_json,ai_settings_json,resource_scope_json,state_json,storage_root,revision,created_at,updated_at FROM project_workspaces WHERE id=?`

func (s *Service) WorkspaceView(ctx context.Context, id string) (WorkspaceView, error) {
	if s == nil || s.db == nil || strings.TrimSpace(id) == "" { return WorkspaceView{}, ErrInvalidCommand }
	return scanWorkspaceView(s.db.QueryRowContext(ctx, workspaceViewSelect, strings.TrimSpace(id)))
}

func (s *Service) WorkspaceViews(ctx context.Context, projectID string) ([]WorkspaceView, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" { return nil, ErrInvalidCommand }
	if err := s.ensureWorkspaceViews(ctx, projectID); err != nil { return nil, err }
	rows, err := s.db.QueryContext(ctx, `SELECT id,project_id,name,description,status,layout_json,ai_settings_json,resource_scope_json,state_json,storage_root,revision,created_at,updated_at FROM project_workspaces WHERE project_id=? AND status<>'archived' ORDER BY created_at,id`, projectID)
	if err != nil { return nil, err }
	defer rows.Close()
	out := []WorkspaceView{}
	for rows.Next() { x,err:=scanWorkspaceView(rows); if err!=nil{return nil,err}; out=append(out,x) }
	return out, rows.Err()
}

func (s *Service) ensureWorkspaceViews(ctx context.Context, projectID string) error {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM project_workspaces WHERE project_id=?`, projectID).Scan(&n); err != nil { return err }
	if n > 0 { return nil }
	p, err := s.repo.Project(ctx, projectID); if err != nil { return err }
	var doc map[string]any
	_ = json.Unmarshal(p.ProjectPolicyJSON, &doc)
	var legacy []any
	if ui, ok := doc["onepane_ui"].(map[string]any); ok { legacy, _ = ui["workspaces"].([]any) }
	if len(legacy)==0 { legacy=[]any{map[string]any{"id":"main","name":"Main"}} }
	now := s.clock.UnixMilli()
	return s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		for i,item:=range legacy {
			m,_:=item.(map[string]any)
			name,_:=m["name"].(string); if strings.TrimSpace(name)==""{name="Main"}
			legacyID,_:=m["id"].(string)
			idv,_:=s.ids.New("pws")
			if i==0 && strings.TrimSpace(legacyID)!="" {}
			state,_:=json.Marshal(map[string]any{"legacy_workspace":m,"legacy_workspace_id":legacyID})
			layout:=json.RawMessage(`{"schema_version":1,"components":[]}`)
			if widgets,ok:=m["widgets"];ok{b,_:=json.Marshal(map[string]any{"schema_version":1,"components":widgets});layout=b}
			ai:=json.RawMessage(`{"version":1,"routing":{"enabled":true,"strategy":"hybrid"},"direct":{},"team":{},"council":{}}`)
			if orch,ok:=m["orchestration"];ok{b,_:=json.Marshal(map[string]any{"version":1,"legacy_orchestration":orch,"routing":m["routing"]});ai=b}
			_,err:=tx.ExecContext(ctx,`INSERT INTO project_workspaces(id,project_id,name,status,layout_json,ai_settings_json,resource_scope_json,state_json,revision,created_at,updated_at) VALUES(?,?,?,'active',?,?, '{}',?,1,?,?)`,idv,projectID,name,string(layout),string(ai),string(state),now,now)
			if err!=nil{return err}
		}
		return nil
	})
}

func (s *Service) CreateWorkspaceView(ctx context.Context, cmd CreateWorkspaceViewCommand) (WorkspaceView, error) {
	cmd.ProjectID,cmd.Name,cmd.ActorPrincipalID=strings.TrimSpace(cmd.ProjectID),strings.TrimSpace(cmd.Name),strings.TrimSpace(cmd.ActorPrincipalID)
	if cmd.ProjectID==""||cmd.Name==""||cmd.ActorPrincipalID==""{return WorkspaceView{},ErrInvalidCommand}
	p,err:=s.repo.Project(ctx,cmd.ProjectID);if err!=nil{return WorkspaceView{},err};if p.Status!="active"{return WorkspaceView{},ErrProjectInactive}
	layout,err:=canonicalObjectOr(cmd.LayoutJSON,`{"schema_version":1,"components":[]}`);if err!=nil{return WorkspaceView{},ErrInvalidCommand}
	ai,err:=canonicalObjectOr(cmd.AISettingsJSON,`{"version":1,"routing":{"enabled":true,"strategy":"hybrid"},"direct":{},"team":{},"council":{}}`);if err!=nil{return WorkspaceView{},ErrInvalidCommand}
	scope,err:=canonicalObjectOr(cmd.ResourceScopeJSON,`{}`);if err!=nil{return WorkspaceView{},ErrInvalidCommand}
	state,err:=canonicalObjectOr(cmd.StateJSON,`{}`);if err!=nil{return WorkspaceView{},ErrInvalidCommand}
	idv,_:=s.ids.New("pws");now:=s.clock.UnixMilli()
	err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
		if _,err:=tx.ExecContext(ctx,`INSERT INTO project_workspaces(id,project_id,name,description,status,layout_json,ai_settings_json,resource_scope_json,state_json,storage_root,revision,created_at,updated_at) VALUES(?,?,?,?,'active',?,?,?,?,?,1,?,?)`,idv,cmd.ProjectID,cmd.Name,cmd.Description,string(layout),string(ai),string(scope),string(state),cmd.StorageRoot,now,now);err!=nil{return err}
		eid,_:=s.ids.New("evt");payload,_:=json.Marshal(map[string]any{"project_id":cmd.ProjectID,"project_workspace_id":idv,"name":cmd.Name})
		return s.events.Append(ctx,tx,event.Event{ID:eid,WorkspaceID:&p.WorkspaceID,Type:"project_workspace.created",AggregateType:"project_workspace",AggregateID:idv,ActorPrincipalID:&cmd.ActorPrincipalID,Payload:payload,OccurredAt:now})
	})
	if err!=nil{return WorkspaceView{},err}
	return s.WorkspaceView(ctx,idv)
}

func (s *Service) UpdateWorkspaceView(ctx context.Context, cmd UpdateWorkspaceViewCommand) (WorkspaceView, error) {
	cmd.WorkspaceID,cmd.Name,cmd.ActorPrincipalID=strings.TrimSpace(cmd.WorkspaceID),strings.TrimSpace(cmd.Name),strings.TrimSpace(cmd.ActorPrincipalID)
	if cmd.WorkspaceID==""||cmd.Name==""||cmd.ExpectedRevision<1||cmd.ActorPrincipalID==""{return WorkspaceView{},ErrInvalidCommand}
	current,err:=s.WorkspaceView(ctx,cmd.WorkspaceID);if err!=nil{return WorkspaceView{},err};if current.Revision!=cmd.ExpectedRevision{return WorkspaceView{},ErrRevisionConflict}
	layout,err:=canonicalObjectOr(cmd.LayoutJSON,string(current.LayoutJSON));if err!=nil{return WorkspaceView{},ErrInvalidCommand}
	ai,err:=canonicalObjectOr(cmd.AISettingsJSON,string(current.AISettingsJSON));if err!=nil{return WorkspaceView{},ErrInvalidCommand}
	scope,err:=canonicalObjectOr(cmd.ResourceScopeJSON,string(current.ResourceScopeJSON));if err!=nil{return WorkspaceView{},ErrInvalidCommand}
	state,err:=canonicalObjectOr(cmd.StateJSON,string(current.StateJSON));if err!=nil{return WorkspaceView{},ErrInvalidCommand}
	now:=s.clock.UnixMilli()
	res,err:=s.db.ExecContext(ctx,`UPDATE project_workspaces SET name=?,description=?,layout_json=?,ai_settings_json=?,resource_scope_json=?,state_json=?,storage_root=?,revision=revision+1,updated_at=? WHERE id=? AND revision=? AND status='active'`,cmd.Name,cmd.Description,string(layout),string(ai),string(scope),string(state),cmd.StorageRoot,now,cmd.WorkspaceID,cmd.ExpectedRevision)
	if err!=nil{return WorkspaceView{},err};n,_:=res.RowsAffected();if n!=1{return WorkspaceView{},ErrRevisionConflict};return s.WorkspaceView(ctx,cmd.WorkspaceID)
}

func (s *Service) ArchiveWorkspaceView(ctx context.Context,id string,expected int64)error{
	if strings.TrimSpace(id)==""||expected<1{return ErrInvalidCommand}
	res,err:=s.db.ExecContext(ctx,`UPDATE project_workspaces SET status='archived',revision=revision+1,updated_at=? WHERE id=? AND revision=? AND status='active'`,s.clock.UnixMilli(),strings.TrimSpace(id),expected)
	if err!=nil{return err};n,_:=res.RowsAffected();if n!=1{return ErrRevisionConflict};return nil
}

func (s *Service) MoveWorkspaceStoragePointer(ctx context.Context,id string,expected int64,newRoot string)(WorkspaceView,error){
	newRoot=strings.TrimSpace(newRoot);if newRoot==""{return WorkspaceView{},ErrInvalidCommand}
	current,err:=s.WorkspaceView(ctx,id);if err!=nil{return WorkspaceView{},err};if current.Revision!=expected{return WorkspaceView{},ErrRevisionConflict}
	res,err:=s.db.ExecContext(ctx,`UPDATE project_workspaces SET storage_root=?,revision=revision+1,updated_at=? WHERE id=? AND revision=?`,newRoot,s.clock.UnixMilli(),id,expected)
	if err!=nil{return WorkspaceView{},err};n,_:=res.RowsAffected();if n!=1{return WorkspaceView{},ErrRevisionConflict};return s.WorkspaceView(ctx,id)
}
