//go:build integration

package projectworkspace

import (
	"context"
	"testing"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	sqliteStore "github.com/DigiLogicTech/OnePane/internal/storage/sqlite"
)

func TestProjectWorkspaceLifecycleSQLite(t *testing.T) {
	ctx := context.Background()
	db, err := sqliteStore.Open(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().UnixMilli()
	seed := []struct {
		q    string
		args []any
	}{
		{`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('ws','Workspace','active',1,?,?)`, []any{now, now}},
		{`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('admin','human','Admin','active',1,?,?)`, []any{now, now}},
		{`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('reviewer','human','Reviewer','active',1,?,?)`, []any{now, now}},
		{`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('ws','admin','active',?,?)`, []any{now, now}},
		{`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('ws','reviewer','active',?,?)`, []any{now, now}},
		{`INSERT INTO routines(id,workspace_id,name,definition_version,status,trigger_json,policy_json,timezone,created_by,created_at,updated_at) VALUES('routine','ws','Nightly',1,'active','{}','{}','Australia/Brisbane','admin',?,?)`, []any{now, now}},
	}
	for _, s := range seed {
		if _, err := db.SQL().ExecContext(ctx, s.q, s.args...); err != nil {
			t.Fatal(err)
		}
	}
	svc := NewService(db.SQL(), db, clock.Real{})
	p, err := svc.CreateProject(ctx, CreateProjectCommand{WorkspaceID: "ws", Name: "Demo", CreatedBy: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := svc.CreateRuntime(ctx, CreateRuntimeCommand{ProjectID: p.ID, IsolationMode: IsolationSandboxedContainer, DesiredState: RuntimeDesiredRunning, CreatedBy: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != RuntimeDefined || r.DesiredState != RuntimeDesiredRunning {
		t.Fatalf("runtime=%+v", r)
	}
	app, err := svc.DeclareApplication(ctx, DeclareApplicationCommand{RuntimeID: r.ID, Name: "web", SourceKind: AppOCIImage, SourceRef: "ghcr.io/example/web@sha256:abc", DesiredState: AppDesiredRunning, CreatedBy: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if app.Trust != "untrusted_content" {
		t.Fatalf("trust=%s", app.Trust)
	}
	_, err = svc.BindRoutine(ctx, BindRoutineCommand{ProjectID: p.ID, RuntimeID: r.ID, ApplicationID: &app.ID, RoutineID: "routine", ActionKind: ActionAppCommand, ActionRef: "refresh", ActionSpecJSON: []byte(`{"command":["./refresh"]}`), CreatedBy: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := svc.ProposeChange(ctx, ProposeChangeCommand{ProjectID: p.ID, RuntimeID: &r.ID, Kind: ProposalRuntimeConfig, Summary: "Raise preview memory", ProposedBy: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	c, err = svc.ReviewChange(ctx, ReviewChangeCommand{ProposalID: c.ID, ExpectedRevision: c.Revision, Decision: ProposalApproved, ReviewedBy: "reviewer"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != ProposalApproved {
		t.Fatalf("status=%s", c.Status)
	}
	var jobs int
	if err := db.SQL().QueryRowContext(ctx, `SELECT count(*) FROM outbox_jobs WHERE job_type='project_runtime.reconcile'`).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if jobs < 2 {
		t.Fatalf("expected runtime/app reconcile jobs, got %d", jobs)
	}
}

func TestVerifiedEndpointRouteSQLite(t *testing.T) {
	ctx := context.Background()
	db, err := sqliteStore.Open(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().UnixMilli()
	seed := []struct {
		q    string
		args []any
	}{
		{`INSERT INTO workspaces(id,name,status,revision,created_at,updated_at) VALUES('ws','Workspace','active',1,?,?)`, []any{now, now}},
		{`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('admin','human','Admin','active',1,?,?)`, []any{now, now}},
		{`INSERT INTO principals(id,principal_type,display_name,status,revision,created_at,updated_at) VALUES('observer','system','Observer','active',1,?,?)`, []any{now, now}},
		{`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('ws','admin','active',?,?)`, []any{now, now}},
		{`INSERT INTO workspace_memberships(workspace_id,principal_id,status,created_at,updated_at) VALUES('ws','observer','active',?,?)`, []any{now, now}},
	}
	for _, s := range seed {
		if _, err := db.SQL().ExecContext(ctx, s.q, s.args...); err != nil {
			t.Fatal(err)
		}
	}
	svc := NewService(db.SQL(), db, clock.Real{})
	p, err := svc.CreateProject(ctx, CreateProjectCommand{WorkspaceID: "ws", Name: "Preview", CreatedBy: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := svc.CreateRuntime(ctx, CreateRuntimeCommand{ProjectID: p.ID, IsolationMode: IsolationSandboxedContainer, DesiredState: RuntimeDesiredRunning, CreatedBy: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	app, err := svc.DeclareApplication(ctx, DeclareApplicationCommand{RuntimeID: r.ID, Name: "web", SourceKind: AppOCIImage, SourceRef: "ghcr.io/example/web@sha256:abc", DesiredState: AppDesiredRunning, CreatedBy: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	ep, err := svc.DeclareEndpoint(ctx, DeclareEndpointCommand{RuntimeID: r.ID, ApplicationID: &app.ID, Name: "web", Protocol: "http", InternalPort: 8080, Exposure: ExposurePreview, CreatedBy: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `UPDATE project_runtimes SET status='running' WHERE id=?`, r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `UPDATE project_applications SET status='running' WHERE id=?`, app.ID); err != nil {
		t.Fatal(err)
	}
	value := `{"container":{"application_id":"` + app.ID + `","status":"running","isolation_verified":true,"spec_hash":"spec123","ports":[{"internal_port":8080,"protocol":"tcp","host_ip":"127.0.0.1","host_port":49152}]}}`
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO observations(id,workspace_id,subject_ref,observation_type,probe_tool_id,probe_tool_version,source_principal_id,adapter_id,adapter_version,value_json,confidentiality,residency,trust,integrity_hash,observed_at,created_at) VALUES('obs','ws',?,'project_application_state','project.app.inspect','1','observer','sandbox_runner','1',?,'internal','origin_node','unverified_derived','hash',?,?)`, "project_app:"+app.ID, value, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO verifications(id,workspace_id,subject_ref,required_level,achieved_level,status,spec_json,result_json,verified_by,started_at,completed_at,revision) VALUES('ver','ws',?,'V2','V2','pass','{}','{"observation_id":"obs"}','observer',?,?,1)`, "project_app:"+app.ID, now, now); err != nil {
		t.Fatal(err)
	}

	route, err := svc.ApplyEndpointRoute(ctx, ApplyEndpointRouteCommand{EndpointID: ep.ID, ApplicationID: app.ID, ApplicationRevision: app.Revision, HostIP: "127.0.0.1", HostPort: 49152, TransportProtocol: "tcp", ContainerSpecHash: "spec123", ObservationID: "obs", VerificationID: "ver", ActorPrincipalID: "observer"})
	if err != nil {
		t.Fatal(err)
	}
	if route.Status != "verified" || route.HostPort != 49152 {
		t.Fatalf("route=%+v", route)
	}
	resolved, err := svc.ResolveIngressRoute(ctx, ep.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.WorkspaceID != "ws" || resolved.Route.HostIP != "127.0.0.1" {
		t.Fatalf("resolved=%+v", resolved)
	}
	if _, err := svc.ApplyEndpointRoute(ctx, ApplyEndpointRouteCommand{EndpointID: ep.ID, ApplicationID: app.ID, ApplicationRevision: app.Revision, HostIP: "127.0.0.1", HostPort: 49153, TransportProtocol: "tcp", ContainerSpecHash: "spec123", ObservationID: "obs", VerificationID: "ver", ActorPrincipalID: "observer"}); err == nil {
		t.Fatal("unobserved host port accepted")
	}
	if _, err := db.SQL().ExecContext(ctx, `UPDATE project_applications SET revision=revision+1 WHERE id=?`, app.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResolveIngressRoute(ctx, ep.ID); err == nil {
		t.Fatal("stale application revision route resolved")
	}
}
