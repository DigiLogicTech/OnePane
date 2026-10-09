package projectworkspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type sqlRepository struct{ db *sql.DB }

func newSQLRepository(db *sql.DB) repository { return &sqlRepository{db: db} }

type scanner interface{ Scan(...any) error }

const projectSelect = `SELECT id,workspace_id,name,description,status,project_policy_json,indexing_config_json,revision,created_by,created_at,updated_at FROM projects WHERE id=?`

func scanProject(row scanner) (Project, error) {
	var p Project
	var d sql.NullString
	var pol, idx string
	if err := row.Scan(&p.ID, &p.WorkspaceID, &p.Name, &d, &p.Status, &pol, &idx, &p.Revision, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return Project{}, err
	}
	if d.Valid {
		v := d.String
		p.Description = &v
	}
	p.ProjectPolicyJSON = json.RawMessage(pol)
	p.IndexingConfigJSON = json.RawMessage(idx)
	return p, nil
}
func (r *sqlRepository) Project(ctx context.Context, id string) (Project, error) {
	return scanProject(r.db.QueryRowContext(ctx, projectSelect, id))
}
func (r *sqlRepository) Projects(ctx context.Context, workspaceID string) ([]Project, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,workspace_id,name,description,status,project_policy_json,indexing_config_json,revision,created_by,created_at,updated_at FROM projects WHERE workspace_id=? AND status='active' ORDER BY updated_at DESC`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()
	out := make([]Project, 0)
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
func (r *sqlRepository) ProjectTx(ctx context.Context, tx storage.Tx, id string) (Project, error) {
	return scanProject(tx.QueryRowContext(ctx, projectSelect, id))
}
func (r *sqlRepository) InsertProject(ctx context.Context, tx storage.Tx, p Project) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO projects(id,workspace_id,name,description,status,project_policy_json,indexing_config_json,revision,created_by,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, p.ID, p.WorkspaceID, p.Name, p.Description, p.Status, string(p.ProjectPolicyJSON), string(p.IndexingConfigJSON), p.Revision, p.CreatedBy, p.CreatedAt, p.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert project: %w", err)
	}
	return nil
}

func (r *sqlRepository) UpdateProjectPolicy(ctx context.Context, tx storage.Tx, p Project, policy json.RawMessage, now int64) error {
	res, err := tx.ExecContext(ctx, `UPDATE projects SET project_policy_json=?,revision=revision+1,updated_at=? WHERE id=? AND revision=?`, string(policy), now, p.ID, p.Revision)
	if err != nil {
		return fmt.Errorf("update project policy: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrRevisionConflict
	}
	return nil
}

func (r *sqlRepository) ArchiveProject(ctx context.Context, tx storage.Tx, p Project, now int64) error {
	res, err := tx.ExecContext(ctx, `UPDATE projects SET status='archived',revision=revision+1,updated_at=? WHERE id=? AND revision=? AND status='active'`, now, p.ID, p.Revision)
	if err != nil {
		return fmt.Errorf("archive project: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrRevisionConflict
	}
	return nil
}

const runtimeColumns = "id,project_id,project_workspace_id,node_id,isolation_mode,backend,desired_state,status,runtime_spec_json,resource_limits_json,network_policy_json,filesystem_policy_json,environment_bindings_json,revision,created_by,created_at,updated_at"
const runtimeSelect = "SELECT "+runtimeColumns+" FROM project_runtimes WHERE id=?"

func scanRuntime(row scanner) (ProjectRuntime,error) {
 var x ProjectRuntime
 var node,projectWorkspace sql.NullString
 var rs,rl,np,fp,eb string
 if err:=row.Scan(&x.ID,&x.ProjectID,&projectWorkspace,&node,&x.IsolationMode,&x.Backend,&x.DesiredState,&x.Status,&rs,&rl,&np,&fp,&eb,&x.Revision,&x.CreatedBy,&x.CreatedAt,&x.UpdatedAt);err!=nil{
  return ProjectRuntime{},err
 }
 if node.Valid{v:=node.String;x.NodeID=&v}
 if projectWorkspace.Valid{v:=projectWorkspace.String;x.ProjectWorkspaceID=&v}
 x.RuntimeSpecJSON=json.RawMessage(rs)
 x.ResourceLimitsJSON=json.RawMessage(rl)
 x.NetworkPolicyJSON=json.RawMessage(np)
 x.FilesystemPolicyJSON=json.RawMessage(fp)
 x.EnvironmentBindingsJSON=json.RawMessage(eb)
 return x,nil
}
func (r *sqlRepository) Runtime(ctx context.Context,id string)(ProjectRuntime,error){
 return scanRuntime(r.db.QueryRowContext(ctx,runtimeSelect,id))
}
func (r *sqlRepository) RuntimeTx(ctx context.Context,tx storage.Tx,id string)(ProjectRuntime,error){
 return scanRuntime(tx.QueryRowContext(ctx,runtimeSelect,id))
}
func (r *sqlRepository) RuntimeByProject(ctx context.Context,projectID string)(ProjectRuntime,error){
 return scanRuntime(r.db.QueryRowContext(ctx,"SELECT "+runtimeColumns+" FROM project_runtimes WHERE project_id=? AND project_workspace_id IS NULL",projectID))
}
func (r *sqlRepository) RuntimeByProjectWorkspace(ctx context.Context,projectID,workspaceID string)(ProjectRuntime,error){
 return scanRuntime(r.db.QueryRowContext(ctx,"SELECT "+runtimeColumns+" FROM project_runtimes WHERE project_id=? AND project_workspace_id=?",projectID,workspaceID))
}
func (r *sqlRepository) InsertRuntime(ctx context.Context,tx storage.Tx,x ProjectRuntime)error{
 _,err:=tx.ExecContext(ctx,`INSERT INTO project_runtimes(
 id,project_id,project_workspace_id,node_id,isolation_mode,backend,desired_state,status,
 runtime_spec_json,resource_limits_json,network_policy_json,filesystem_policy_json,
 environment_bindings_json,revision,created_by,created_at,updated_at)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,x.ID,x.ProjectID,x.ProjectWorkspaceID,x.NodeID,x.IsolationMode,x.Backend,x.DesiredState,x.Status,
 string(x.RuntimeSpecJSON),string(x.ResourceLimitsJSON),string(x.NetworkPolicyJSON),string(x.FilesystemPolicyJSON),
 string(x.EnvironmentBindingsJSON),x.Revision,x.CreatedBy,x.CreatedAt,x.UpdatedAt)
 if err!=nil{return fmt.Errorf("insert project runtime: %w",err)}
 return nil
}
func (r *sqlRepository) UpdateRuntimeDesired(ctx context.Context, tx storage.Tx, x ProjectRuntime, to RuntimeDesiredState, now int64) error {
	res, err := tx.ExecContext(ctx, `UPDATE project_runtimes SET desired_state=?,revision=revision+1,updated_at=? WHERE id=? AND revision=?`, to, now, x.ID, x.Revision)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrRevisionConflict
	}
	return nil
}
func (r *sqlRepository) UpdateRuntimePolicy(ctx context.Context, tx storage.Tx, x ProjectRuntime, networkPolicy, filesystemPolicy json.RawMessage, now int64) error {
	res, err := tx.ExecContext(ctx, `UPDATE project_runtimes SET network_policy_json=?,filesystem_policy_json=?,revision=revision+1,updated_at=? WHERE id=? AND revision=?`, string(networkPolicy), string(filesystemPolicy), now, x.ID, x.Revision)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrRevisionConflict
	}
	return nil
}

func (r *sqlRepository) UpdateRuntimeObserved(ctx context.Context, tx storage.Tx, x ProjectRuntime, to RuntimeStatus, now int64) error {
	res, err := tx.ExecContext(ctx, `UPDATE project_runtimes SET status=?,revision=revision+1,updated_at=? WHERE id=? AND revision=?`, to, now, x.ID, x.Revision)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrRevisionConflict
	}
	return nil
}

const appSelect = `SELECT id,project_runtime_id,name,source_kind,source_ref,version_ref,install_spec_json,runtime_spec_json,environment_bindings_json,desired_state,status,trust,revision,created_by,created_at,updated_at FROM project_applications WHERE id=?`

func scanApp(row scanner) (Application, error) {
	var a Application
	var vr sql.NullString
	var i, rn, e string
	if err := row.Scan(&a.ID, &a.ProjectRuntimeID, &a.Name, &a.SourceKind, &a.SourceRef, &vr, &i, &rn, &e, &a.DesiredState, &a.Status, &a.Trust, &a.Revision, &a.CreatedBy, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return Application{}, err
	}
	if vr.Valid {
		v := vr.String
		a.VersionRef = &v
	}
	a.InstallSpecJSON = json.RawMessage(i)
	a.RuntimeSpecJSON = json.RawMessage(rn)
	a.EnvironmentBindingsJSON = json.RawMessage(e)
	return a, nil
}
func (r *sqlRepository) Application(ctx context.Context, id string) (Application, error) {
	return scanApp(r.db.QueryRowContext(ctx, appSelect, id))
}
func (r *sqlRepository) ApplicationTx(ctx context.Context, tx storage.Tx, id string) (Application, error) {
	return scanApp(tx.QueryRowContext(ctx, appSelect, id))
}
func (r *sqlRepository) InsertApplication(ctx context.Context, tx storage.Tx, a Application) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO project_applications(id,project_runtime_id,name,source_kind,source_ref,version_ref,install_spec_json,runtime_spec_json,environment_bindings_json,desired_state,status,trust,revision,created_by,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, a.ID, a.ProjectRuntimeID, a.Name, a.SourceKind, a.SourceRef, a.VersionRef, string(a.InstallSpecJSON), string(a.RuntimeSpecJSON), string(a.EnvironmentBindingsJSON), a.DesiredState, a.Status, a.Trust, a.Revision, a.CreatedBy, a.CreatedAt, a.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert project application: %w", err)
	}
	return nil
}
func (r *sqlRepository) UpdateApplicationObserved(ctx context.Context, tx storage.Tx, a Application, to AppStatus, now int64) error {
	res, err := tx.ExecContext(ctx, `UPDATE project_applications SET status=?,revision=revision+1,updated_at=? WHERE id=? AND revision=?`, to, now, a.ID, a.Revision)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrRevisionConflict
	}
	return nil
}

func (r *sqlRepository) InsertEndpoint(ctx context.Context, tx storage.Tx, e Endpoint) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO project_runtime_endpoints(id,project_runtime_id,application_id,name,protocol,internal_port,exposure,path_prefix,desired_state,status,external_url,revision,created_by,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, e.ID, e.ProjectRuntimeID, e.ApplicationID, e.Name, e.Protocol, e.InternalPort, e.Exposure, e.PathPrefix, e.DesiredState, e.Status, e.ExternalURL, e.Revision, e.CreatedBy, e.CreatedAt, e.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert project endpoint: %w", err)
	}
	return nil
}

const endpointSelect = `SELECT id,project_runtime_id,application_id,name,protocol,internal_port,exposure,path_prefix,desired_state,status,external_url,revision,created_by,created_at,updated_at FROM project_runtime_endpoints WHERE id=?`

func (r *sqlRepository) Endpoint(ctx context.Context, id string) (Endpoint, error) {
	return scanEndpoint(r.db.QueryRowContext(ctx, endpointSelect, id))
}
func (r *sqlRepository) EndpointTx(ctx context.Context, tx storage.Tx, id string) (Endpoint, error) {
	return scanEndpoint(tx.QueryRowContext(ctx, endpointSelect, id))
}

const endpointRouteSelect = `SELECT endpoint_id,project_runtime_id,application_id,host_ip,host_port,transport_protocol,observation_id,verification_id,application_revision,endpoint_revision,container_spec_hash,status,updated_at FROM project_endpoint_routes WHERE endpoint_id=?`

func scanEndpointRoute(row scanner) (EndpointRoute, error) {
	var x EndpointRoute
	err := row.Scan(&x.EndpointID, &x.ProjectRuntimeID, &x.ApplicationID, &x.HostIP, &x.HostPort, &x.TransportProtocol, &x.ObservationID, &x.VerificationID, &x.ApplicationRevision, &x.EndpointRevision, &x.ContainerSpecHash, &x.Status, &x.UpdatedAt)
	return x, err
}
func (r *sqlRepository) EndpointRoute(ctx context.Context, endpointID string) (EndpointRoute, error) {
	return scanEndpointRoute(r.db.QueryRowContext(ctx, endpointRouteSelect, endpointID))
}
func (r *sqlRepository) IngressRoute(ctx context.Context, endpointID string) (IngressRoute, error) {
	var x IngressRoute
	var e Endpoint
	var route EndpointRoute
	var appID, path, external sql.NullString
	err := r.db.QueryRowContext(ctx, `
SELECT p.workspace_id,p.id,
       e.id,e.project_runtime_id,e.application_id,e.name,e.protocol,e.internal_port,e.exposure,e.path_prefix,e.desired_state,e.status,e.external_url,e.revision,e.created_by,e.created_at,e.updated_at,
       er.endpoint_id,er.project_runtime_id,er.application_id,er.host_ip,er.host_port,er.transport_protocol,er.observation_id,er.verification_id,er.application_revision,er.endpoint_revision,er.container_spec_hash,er.status,er.updated_at
FROM project_runtime_endpoints e
JOIN project_endpoint_routes er ON er.endpoint_id=e.id
JOIN project_applications a ON a.id=er.application_id AND a.id=e.application_id
JOIN project_runtimes r ON r.id=e.project_runtime_id AND r.id=er.project_runtime_id AND r.id=a.project_runtime_id
JOIN projects p ON p.id=r.project_id
WHERE e.id=?
  AND e.desired_state='enabled' AND e.status='ready'
  AND er.status='verified' AND er.host_ip='127.0.0.1'
  AND er.endpoint_revision=e.revision
  AND er.application_revision=a.revision
  AND a.status='running' AND r.status='running' AND p.status='active'`, endpointID).Scan(
		&x.WorkspaceID, &x.ProjectID,
		&e.ID, &e.ProjectRuntimeID, &appID, &e.Name, &e.Protocol, &e.InternalPort, &e.Exposure, &path, &e.DesiredState, &e.Status, &external, &e.Revision, &e.CreatedBy, &e.CreatedAt, &e.UpdatedAt,
		&route.EndpointID, &route.ProjectRuntimeID, &route.ApplicationID, &route.HostIP, &route.HostPort, &route.TransportProtocol, &route.ObservationID, &route.VerificationID, &route.ApplicationRevision, &route.EndpointRevision, &route.ContainerSpecHash, &route.Status, &route.UpdatedAt)
	if err != nil {
		return IngressRoute{}, err
	}
	if appID.Valid {
		v := appID.String
		e.ApplicationID = &v
	}
	if path.Valid {
		v := path.String
		e.PathPrefix = &v
	}
	if external.Valid {
		v := external.String
		e.ExternalURL = &v
	}
	x.Endpoint = e
	x.Route = route
	return x, nil
}
func (r *sqlRepository) ListEndpointRoutes(ctx context.Context, runtimeID string) ([]EndpointRoute, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT endpoint_id,project_runtime_id,application_id,host_ip,host_port,transport_protocol,observation_id,verification_id,application_revision,endpoint_revision,container_spec_hash,status,updated_at FROM project_endpoint_routes WHERE project_runtime_id=? ORDER BY endpoint_id`, runtimeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EndpointRoute{}
	for rows.Next() {
		x, err := scanEndpointRoute(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
func (r *sqlRepository) UpsertEndpointRoute(ctx context.Context, tx storage.Tx, x EndpointRoute) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO project_endpoint_routes(endpoint_id,project_runtime_id,application_id,host_ip,host_port,transport_protocol,observation_id,verification_id,application_revision,endpoint_revision,container_spec_hash,status,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(endpoint_id) DO UPDATE SET project_runtime_id=excluded.project_runtime_id,application_id=excluded.application_id,host_ip=excluded.host_ip,host_port=excluded.host_port,transport_protocol=excluded.transport_protocol,observation_id=excluded.observation_id,verification_id=excluded.verification_id,application_revision=excluded.application_revision,endpoint_revision=excluded.endpoint_revision,container_spec_hash=excluded.container_spec_hash,status=excluded.status,updated_at=excluded.updated_at`, x.EndpointID, x.ProjectRuntimeID, x.ApplicationID, x.HostIP, x.HostPort, x.TransportProtocol, x.ObservationID, x.VerificationID, x.ApplicationRevision, x.EndpointRevision, x.ContainerSpecHash, x.Status, x.UpdatedAt)
	return err
}
func (r *sqlRepository) MarkApplicationEndpointRoutesStale(ctx context.Context, tx storage.Tx, applicationID string, now int64) error {
	_, err := tx.ExecContext(ctx, `UPDATE project_endpoint_routes SET status='stale',updated_at=? WHERE application_id=? AND status='verified'`, now, applicationID)
	return err
}
func (r *sqlRepository) MarkApplicationEndpointsUnready(ctx context.Context, tx storage.Tx, applicationID string, now int64) error {
	_, err := tx.ExecContext(ctx, `UPDATE project_runtime_endpoints SET status=CASE WHEN desired_state='disabled' THEN 'disabled' ELSE 'declared' END,external_url=NULL,updated_at=? WHERE application_id=?`, now, applicationID)
	return err
}
func (r *sqlRepository) SetEndpointObservedRoute(ctx context.Context, tx storage.Tx, endpointID string, now int64) error {
	// Authenticated preview URLs are short-lived and live on a distinct browser
	// origin, so they are minted on demand rather than persisted here.
	res, err := tx.ExecContext(ctx, `UPDATE project_runtime_endpoints SET status='ready',external_url=NULL,updated_at=? WHERE id=? AND desired_state='enabled'`, now, endpointID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrInvalidTransition
	}
	return nil
}

const changeSelect = `SELECT id,project_id,project_runtime_id,task_id,proposal_kind,status,summary,base_revision,patch_artifact_id,metadata_json,revision,proposed_by,reviewed_by,created_at,updated_at,reviewed_at FROM project_change_proposals WHERE id=?`

func scanChange(row scanner) (ChangeProposal, error) {
	var c ChangeProposal
	var rid, tid, aid, reviewer sql.NullString
	var base, reviewed sql.NullInt64
	var meta string
	if err := row.Scan(&c.ID, &c.ProjectID, &rid, &tid, &c.ProposalKind, &c.Status, &c.Summary, &base, &aid, &meta, &c.Revision, &c.ProposedBy, &reviewer, &c.CreatedAt, &c.UpdatedAt, &reviewed); err != nil {
		return ChangeProposal{}, err
	}
	if rid.Valid {
		v := rid.String
		c.ProjectRuntimeID = &v
	}
	if tid.Valid {
		v := tid.String
		c.TaskID = &v
	}
	if aid.Valid {
		v := aid.String
		c.PatchArtifactID = &v
	}
	if reviewer.Valid {
		v := reviewer.String
		c.ReviewedBy = &v
	}
	if base.Valid {
		v := base.Int64
		c.BaseRevision = &v
	}
	if reviewed.Valid {
		v := reviewed.Int64
		c.ReviewedAt = &v
	}
	c.MetadataJSON = json.RawMessage(meta)
	return c, nil
}
func (r *sqlRepository) Change(ctx context.Context, id string) (ChangeProposal, error) {
	return scanChange(r.db.QueryRowContext(ctx, changeSelect, id))
}
func (r *sqlRepository) ChangeTx(ctx context.Context, tx storage.Tx, id string) (ChangeProposal, error) {
	return scanChange(tx.QueryRowContext(ctx, changeSelect, id))
}
func (r *sqlRepository) InsertChange(ctx context.Context, tx storage.Tx, c ChangeProposal) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO project_change_proposals(id,project_id,project_runtime_id,task_id,proposal_kind,status,summary,base_revision,patch_artifact_id,metadata_json,revision,proposed_by,reviewed_by,created_at,updated_at,reviewed_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, c.ID, c.ProjectID, c.ProjectRuntimeID, c.TaskID, c.ProposalKind, c.Status, c.Summary, c.BaseRevision, c.PatchArtifactID, string(c.MetadataJSON), c.Revision, c.ProposedBy, c.ReviewedBy, c.CreatedAt, c.UpdatedAt, c.ReviewedAt)
	if err != nil {
		return err
	}
	return nil
}
func (r *sqlRepository) UpdateChangeStatus(ctx context.Context, tx storage.Tx, c ChangeProposal, to ProposalStatus, reviewer string, now int64) error {
	res, err := tx.ExecContext(ctx, `UPDATE project_change_proposals SET status=?,reviewed_by=?,reviewed_at=?,revision=revision+1,updated_at=? WHERE id=? AND revision=? AND status IN ('proposed','reviewing')`, to, reviewer, now, now, c.ID, c.Revision)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrRevisionConflict
	}
	return nil
}
func (r *sqlRepository) InsertRoutineBinding(ctx context.Context, tx storage.Tx, b RoutineBinding) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO project_routine_bindings(id,project_id,project_runtime_id,application_id,routine_id,action_kind,action_ref,action_spec_json,status,revision,created_by,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, b.ID, b.ProjectID, b.ProjectRuntimeID, b.ApplicationID, b.RoutineID, b.ActionKind, b.ActionRef, string(b.ActionSpecJSON), b.Status, b.Revision, b.CreatedBy, b.CreatedAt, b.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert routine binding: %w", err)
	}
	return nil
}

func (r *sqlRepository) ActorStateTx(ctx context.Context, tx storage.Tx, workspaceID, principalID string) (actorState, error) {
	var s actorState
	var m sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT w.status,p.status,p.principal_type,wm.status FROM workspaces w JOIN principals p ON p.id=? LEFT JOIN workspace_memberships wm ON wm.workspace_id=w.id AND wm.principal_id=p.id WHERE w.id=?`, principalID, workspaceID).Scan(&s.WorkspaceStatus, &s.PrincipalStatus, &s.PrincipalType, &m)
	if m.Valid {
		v := m.String
		s.MembershipStatus = &v
	}
	return s, err
}
func (r *sqlRepository) NodeExistsTx(ctx context.Context, tx storage.Tx, id string) (bool, error) {
	var x int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM harness_nodes WHERE id=?`, id).Scan(&x)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}
func (r *sqlRepository) RoutineWorkspaceTx(ctx context.Context, tx storage.Tx, id string) (string, error) {
	var w string
	err := tx.QueryRowContext(ctx, `SELECT workspace_id FROM routines WHERE id=?`, id).Scan(&w)
	return w, err
}
func (r *sqlRepository) ArtifactProjectWorkspaceTx(ctx context.Context, tx storage.Tx, id string) (string, *string, error) {
	var w string
	var p sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT workspace_id,project_id FROM artifacts WHERE id=?`, id).Scan(&w, &p)
	if err != nil {
		return "", nil, err
	}
	var pp *string
	if p.Valid {
		v := p.String
		pp = &v
	}
	return w, pp, nil
}

func (r *sqlRepository) TaskProjectWorkspace(ctx context.Context, id string) (string, *string, error) {
	var w string
	var p sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT workspace_id,project_id FROM tasks WHERE id=?`, id).Scan(&w, &p)
	if err != nil {
		return "", nil, err
	}
	var pp *string
	if p.Valid {
		v := p.String
		pp = &v
	}
	return w, pp, nil
}

func (r *sqlRepository) TaskProjectWorkspaceTx(ctx context.Context, tx storage.Tx, id string) (string, *string, error) {
	var w string
	var p sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT workspace_id,project_id FROM tasks WHERE id=?`, id).Scan(&w, &p)
	if err != nil {
		return "", nil, err
	}
	var pp *string
	if p.Valid {
		v := p.String
		pp = &v
	}
	return w, pp, nil
}

func (r *sqlRepository) ListApplications(ctx context.Context, runtimeID string) ([]Application, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,project_runtime_id,name,source_kind,source_ref,version_ref,install_spec_json,runtime_spec_json,environment_bindings_json,desired_state,status,trust,revision,created_by,created_at,updated_at FROM project_applications WHERE project_runtime_id=? ORDER BY name,id`, runtimeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Application{}
	for rows.Next() {
		a, err := scanApp(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func scanEndpoint(row scanner) (Endpoint, error) {
	var e Endpoint
	var app, path, url sql.NullString
	if err := row.Scan(&e.ID, &e.ProjectRuntimeID, &app, &e.Name, &e.Protocol, &e.InternalPort, &e.Exposure, &path, &e.DesiredState, &e.Status, &url, &e.Revision, &e.CreatedBy, &e.CreatedAt, &e.UpdatedAt); err != nil {
		return Endpoint{}, err
	}
	if app.Valid {
		v := app.String
		e.ApplicationID = &v
	}
	if path.Valid {
		v := path.String
		e.PathPrefix = &v
	}
	if url.Valid {
		v := url.String
		e.ExternalURL = &v
	}
	return e, nil
}
func (r *sqlRepository) ListEndpoints(ctx context.Context, runtimeID string) ([]Endpoint, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,project_runtime_id,application_id,name,protocol,internal_port,exposure,path_prefix,desired_state,status,external_url,revision,created_by,created_at,updated_at FROM project_runtime_endpoints WHERE project_runtime_id=? ORDER BY name,id`, runtimeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Endpoint{}
	for rows.Next() {
		e, err := scanEndpoint(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *sqlRepository) ListChanges(ctx context.Context, projectID string) ([]ChangeProposal, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,project_id,project_runtime_id,task_id,proposal_kind,status,summary,base_revision,patch_artifact_id,metadata_json,revision,proposed_by,reviewed_by,created_at,updated_at,reviewed_at FROM project_change_proposals WHERE project_id=? ORDER BY created_at DESC,id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChangeProposal{}
	for rows.Next() {
		c, err := scanChange(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func scanRoutineBinding(row scanner) (RoutineBinding, error) {
	var b RoutineBinding
	var app sql.NullString
	var spec string
	if err := row.Scan(&b.ID, &b.ProjectID, &b.ProjectRuntimeID, &app, &b.RoutineID, &b.ActionKind, &b.ActionRef, &spec, &b.Status, &b.Revision, &b.CreatedBy, &b.CreatedAt, &b.UpdatedAt); err != nil {
		return RoutineBinding{}, err
	}
	if app.Valid {
		v := app.String
		b.ApplicationID = &v
	}
	b.ActionSpecJSON = json.RawMessage(spec)
	return b, nil
}
func (r *sqlRepository) RoutineBinding(ctx context.Context, id string) (RoutineBinding, error) {
	return scanRoutineBinding(r.db.QueryRowContext(ctx, `SELECT id,project_id,project_runtime_id,application_id,routine_id,action_kind,action_ref,action_spec_json,status,revision,created_by,created_at,updated_at FROM project_routine_bindings WHERE id=?`, id))
}

func (r *sqlRepository) ListRoutineBindings(ctx context.Context, projectID string) ([]RoutineBinding, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,project_id,project_runtime_id,application_id,routine_id,action_kind,action_ref,action_spec_json,status,revision,created_by,created_at,updated_at FROM project_routine_bindings WHERE project_id=? ORDER BY created_at,id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RoutineBinding{}
	for rows.Next() {
		b, err := scanRoutineBinding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (r *sqlRepository) RequireVerifiedObservationTx(ctx context.Context, tx storage.Tx, workspaceID, subjectRef, verificationID, observationID string) error {
	var n int
	err := tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM verifications v
JOIN observations o ON o.id=?
WHERE v.id=? AND v.workspace_id=? AND v.subject_ref=? AND v.status='pass'
  AND v.achieved_level IN ('V2','V3','V4','V5')
  AND o.workspace_id=v.workspace_id AND o.subject_ref=v.subject_ref
  AND json_extract(v.result_json,'$.observation_id')=o.id`, observationID, verificationID, workspaceID, subjectRef).Scan(&n)
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrInvalidTransition
	}
	return nil
}

func (r *sqlRepository) RequireVerifiedEndpointRouteTx(ctx context.Context, tx storage.Tx, workspaceID, applicationID, verificationID, observationID string, internalPort, hostPort int, specHash string) error {
	var n int
	err := tx.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM verifications v
JOIN observations o ON o.id=?
JOIN json_each(o.value_json,'$.container.ports') port
WHERE v.id=? AND v.workspace_id=? AND v.subject_ref=? AND v.status='pass'
  AND v.achieved_level IN ('V2','V3','V4','V5')
  AND o.workspace_id=v.workspace_id AND o.subject_ref=v.subject_ref
  AND json_extract(v.result_json,'$.observation_id')=o.id
  AND json_extract(o.value_json,'$.container.application_id')=?
  AND json_extract(o.value_json,'$.container.status')='running'
  AND json_extract(o.value_json,'$.container.isolation_verified')=1
  AND json_extract(o.value_json,'$.container.spec_hash')=?
  AND CAST(json_extract(port.value,'$.internal_port') AS INTEGER)=?
  AND json_extract(port.value,'$.host_ip')='127.0.0.1'
  AND CAST(json_extract(port.value,'$.host_port') AS INTEGER)=?`, observationID, verificationID, workspaceID, "project_app:"+applicationID, applicationID, specHash, internalPort, hostPort).Scan(&n)
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrInvalidTransition
	}
	return nil
}
