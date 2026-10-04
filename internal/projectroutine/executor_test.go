package projectroutine

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/projectworkspace"
	"github.com/DigiLogicTech/OnePane/internal/tool"
)

type fakeProjects struct {
	binding   projectworkspace.RoutineBinding
	runtime   projectworkspace.ProjectRuntime
	app       projectworkspace.Application
	workspace string
}

func (f *fakeProjects) RoutineBinding(context.Context, string) (projectworkspace.RoutineBinding, error) {
	return f.binding, nil
}
func (f *fakeProjects) Runtime(context.Context, string) (projectworkspace.ProjectRuntime, error) {
	return f.runtime, nil
}
func (f *fakeProjects) Application(context.Context, string) (projectworkspace.Application, error) {
	return f.app, nil
}
func (f *fakeProjects) ValidateTaskProject(context.Context, string, string) error { return nil }
func (f *fakeProjects) TaskWorkspace(context.Context, string) (string, error) {
	return f.workspace, nil
}

type fakeGateway struct{ cmd tool.InvokeCommand }

func (f *fakeGateway) Invoke(_ context.Context, cmd tool.InvokeCommand) (tool.Invocation, error) {
	f.cmd = cmd
	return tool.Invocation{ID: "inv", Status: tool.StatusSucceeded}, nil
}

func TestExecutorRunsBoundCommandThroughSandboxTool(t *testing.T) {
	appID := "app"
	p := &fakeProjects{workspace: "ws", binding: projectworkspace.RoutineBinding{ID: "bind", ProjectID: "proj", ProjectRuntimeID: "runtime", ApplicationID: &appID, RoutineID: "routine", ActionKind: projectworkspace.ActionAppCommand, Status: "active", ActionSpecJSON: json.RawMessage(`{"command":["backup","--compact"]}`)}, runtime: projectworkspace.ProjectRuntime{ID: "runtime", Status: projectworkspace.RuntimeRunning}, app: projectworkspace.Application{ID: "app", ProjectRuntimeID: "runtime", Status: projectworkspace.AppRunning}}
	g := &fakeGateway{}
	e := New(p, g)
	inv, err := e.Execute(context.Background(), ExecuteCommand{BindingID: "bind", TaskID: "task", PrincipalID: "worker", CapabilityLeaseID: "lease"})
	if err != nil {
		t.Fatal(err)
	}
	if inv.Status != tool.StatusSucceeded || g.cmd.ToolID != "project.app.exec" || g.cmd.ResourceRef != "project_runtime:runtime" || g.cmd.TaskID == nil || *g.cmd.TaskID != "task" {
		t.Fatalf("cmd=%+v inv=%+v", g.cmd, inv)
	}
}

func TestExecutorRequiresObservedRunningState(t *testing.T) {
	appID := "app"
	p := &fakeProjects{workspace: "ws", binding: projectworkspace.RoutineBinding{ID: "bind", ProjectID: "proj", ProjectRuntimeID: "runtime", ApplicationID: &appID, RoutineID: "routine", ActionKind: projectworkspace.ActionAppCommand, Status: "active", ActionSpecJSON: json.RawMessage(`{"command":["job"]}`)}, runtime: projectworkspace.ProjectRuntime{ID: "runtime", Status: projectworkspace.RuntimeDefined}, app: projectworkspace.Application{ID: "app", ProjectRuntimeID: "runtime", Status: projectworkspace.AppRunning}}
	_, err := New(p, &fakeGateway{}).Execute(context.Background(), ExecuteCommand{BindingID: "bind", TaskID: "task", PrincipalID: "worker", CapabilityLeaseID: "lease"})
	if err != ErrRuntimeNotReady {
		t.Fatalf("err=%v", err)
	}
}
