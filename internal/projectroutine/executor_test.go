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

type fakeGateway struct{ cmd tool.InvokeCommand; calls int }

func (f *fakeGateway) Invoke(_ context.Context, cmd tool.InvokeCommand) (tool.Invocation, error) {
	f.cmd = cmd
	f.calls++
	return tool.Invocation{ID: "inv", Status: tool.StatusSucceeded}, nil
}

func TestExecutorRunsBoundCommandThroughSandboxTool(t *testing.T) {
	appID := "app"
	p := &fakeProjects{workspace: "ws", binding: projectworkspace.RoutineBinding{ID: "bind", ProjectID: "proj", ProjectRuntimeID: "runtime", ApplicationID: &appID, RoutineID: "routine", ActionKind: projectworkspace.ActionAppCommand, Status: "active", ActionSpecJSON: json.RawMessage(`{"command":["backup","--compact"]}`)}, runtime: projectworkspace.ProjectRuntime{ID: "runtime", ProjectID:"proj", Status: projectworkspace.RuntimeRunning}, app: projectworkspace.Application{ID: "app", ProjectRuntimeID: "runtime", Status: projectworkspace.AppRunning}}
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
	p := &fakeProjects{workspace: "ws", binding: projectworkspace.RoutineBinding{ID: "bind", ProjectID: "proj", ProjectRuntimeID: "runtime", ApplicationID: &appID, RoutineID: "routine", ActionKind: projectworkspace.ActionAppCommand, Status: "active", ActionSpecJSON: json.RawMessage(`{"command":["job"]}`)}, runtime: projectworkspace.ProjectRuntime{ID: "runtime", ProjectID:"proj", Status: projectworkspace.RuntimeDefined}, app: projectworkspace.Application{ID: "app", ProjectRuntimeID: "runtime", Status: projectworkspace.AppRunning}}
	_, err := New(p, &fakeGateway{}).Execute(context.Background(), ExecuteCommand{BindingID: "bind", TaskID: "task", PrincipalID: "worker", CapabilityLeaseID: "lease"})
	if err != ErrRuntimeNotReady {
		t.Fatalf("err=%v", err)
	}
}


func TestExecutorRejectsCrossProjectOrSubstitutedRoutineTargetsBeforeToolGateway(t *testing.T) {
 appID:="app"
 base:=func()*fakeProjects{
  return &fakeProjects{
   workspace:"tenant",
   binding:projectworkspace.RoutineBinding{
    ID:"binding",ProjectID:"project-a",ProjectRuntimeID:"runtime-a",
    ApplicationID:&appID,RoutineID:"routine",ActionKind:projectworkspace.ActionAppCommand,
    Status:"active",ActionSpecJSON:json.RawMessage(`{"command":["backup"]}`),
   },
   runtime:projectworkspace.ProjectRuntime{
    ID:"runtime-a",ProjectID:"project-a",Status:projectworkspace.RuntimeRunning,
   },
   app:projectworkspace.Application{
    ID:"app",ProjectRuntimeID:"runtime-a",Status:projectworkspace.AppRunning,
   },
  }
 }
 cmd:=ExecuteCommand{BindingID:"binding",TaskID:"task",PrincipalID:"worker",CapabilityLeaseID:"lease"}
 for _,tc:=range []struct{name string;change func(*fakeProjects)}{
  {"binding id substituted",func(f *fakeProjects){f.binding.ID="another-binding"}},
  {"binding project missing",func(f *fakeProjects){f.binding.ProjectID=""}},
  {"binding runtime missing",func(f *fakeProjects){f.binding.ProjectRuntimeID=""}},
  {"runtime id mismatched",func(f *fakeProjects){f.runtime.ID="runtime-b"}},
  {"runtime belongs to another Project",func(f *fakeProjects){f.runtime.ProjectID="project-b"}},
  {"runtime Project missing",func(f *fakeProjects){f.runtime.ProjectID=""}},
  {"application id substituted",func(f *fakeProjects){f.app.ID="another-app"}},
  {"app targets another runtime",func(f *fakeProjects){f.app.ProjectRuntimeID="runtime-b"}},
 }{
  t.Run(tc.name,func(t *testing.T){
   p:=base()
   tc.change(p)
   g:=&fakeGateway{}
   if _,err:=New(p,g).Execute(context.Background(),cmd);err!=ErrBindingScopeMismatch{
    t.Fatalf("mismatched Routine target allowed: %v",err)
   }
   if g.calls!=0{t.Fatal("mismatched Project/Runtime reached the execution gateway")}
  })
 }
 // A fully matched project/routine/application remains executable through
 // the ordinary governed Tool Gateway; the guard must not deny valid work.
 p:=base()
 g:=&fakeGateway{}
 if _,err:=New(p,g).Execute(context.Background(),cmd);err!=nil||g.calls!=1{
  t.Fatalf("matched routine unable to execute: calls=%d err=%v",g.calls,err)
 }
}
