package projectroutine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/projectworkspace"
	"github.com/DigiLogicTech/OnePane/internal/sandboxrunner"
	"github.com/DigiLogicTech/OnePane/internal/tool"
)

var (
	ErrInvalidCommand    = errors.New("invalid project routine execution command")
	ErrBindingInactive   = errors.New("project routine binding is not active")
	ErrUnsupportedAction = errors.New("project routine action is not implemented by the sandbox executor")
	ErrRuntimeNotReady   = errors.New("project runtime/application is not in verified running state")
)

type projectService interface {
	RoutineBinding(context.Context, string) (projectworkspace.RoutineBinding, error)
	Runtime(context.Context, string) (projectworkspace.ProjectRuntime, error)
	Application(context.Context, string) (projectworkspace.Application, error)
	ValidateTaskProject(context.Context, string, string) error
	TaskWorkspace(context.Context, string) (string, error)
}

type gateway interface {
	Invoke(context.Context, tool.InvokeCommand) (tool.Invocation, error)
}

type Executor struct {
	projects projectService
	tools    gateway
}

func New(projects projectService, tools gateway) *Executor {
	return &Executor{projects: projects, tools: tools}
}

type ExecuteCommand struct {
	BindingID         string
	TaskID            string
	AttemptID         *string
	PrincipalID       string
	CapabilityLeaseID string
	ActorPrincipalID  *string
	RequestID         *string
	TraceID           *string
}

type appCommandSpec struct {
	Command []string `json:"command"`
}

// Execute runs one already-materialized Routine occurrence through a normal
// Task/CapabilityLease. It never schedules itself and never completes the Task;
// Task verification/completion remains owned by the normal Assurance path.
func (e *Executor) Execute(ctx context.Context, cmd ExecuteCommand) (tool.Invocation, error) {
	if e == nil || e.projects == nil || e.tools == nil || strings.TrimSpace(cmd.BindingID) == "" || strings.TrimSpace(cmd.TaskID) == "" || strings.TrimSpace(cmd.PrincipalID) == "" || strings.TrimSpace(cmd.CapabilityLeaseID) == "" {
		return tool.Invocation{}, ErrInvalidCommand
	}
	binding, err := e.projects.RoutineBinding(ctx, cmd.BindingID)
	if err != nil {
		return tool.Invocation{}, err
	}
	if binding.Status != "active" {
		return tool.Invocation{}, ErrBindingInactive
	}
	if err := e.projects.ValidateTaskProject(ctx, cmd.TaskID, binding.ProjectID); err != nil {
		return tool.Invocation{}, err
	}
	if binding.ActionKind != projectworkspace.ActionAppCommand || binding.ApplicationID == nil {
		return tool.Invocation{}, ErrUnsupportedAction
	}
	runtime, err := e.projects.Runtime(ctx, binding.ProjectRuntimeID)
	if err != nil {
		return tool.Invocation{}, err
	}
	app, err := e.projects.Application(ctx, *binding.ApplicationID)
	if err != nil {
		return tool.Invocation{}, err
	}
	if app.ProjectRuntimeID != runtime.ID || runtime.Status != projectworkspace.RuntimeRunning || app.Status != projectworkspace.AppRunning {
		return tool.Invocation{}, ErrRuntimeNotReady
	}
	var spec appCommandSpec
	if err := json.Unmarshal(binding.ActionSpecJSON, &spec); err != nil || len(spec.Command) == 0 || len(spec.Command) > 128 {
		return tool.Invocation{}, ErrInvalidCommand
	}
	for _, arg := range spec.Command {
		if strings.ContainsRune(arg, '\x00') {
			return tool.Invocation{}, ErrInvalidCommand
		}
	}
	input, _ := json.Marshal(map[string]any{"runtime_id": runtime.ID, "application_id": app.ID, "command": spec.Command, "routine_binding_id": binding.ID, "routine_id": binding.RoutineID})
	workspaceID, err := e.projects.TaskWorkspace(ctx, cmd.TaskID)
	if err != nil {
		return tool.Invocation{}, err
	}
	inv, err := e.tools.Invoke(ctx, tool.InvokeCommand{WorkspaceID: workspaceID, TaskID: &cmd.TaskID, AttemptID: cmd.AttemptID, PrincipalID: cmd.PrincipalID, LeaseID: cmd.CapabilityLeaseID, ToolID: sandboxrunner.ToolAppExec, ToolVersion: "1", ResourceRef: "project_runtime:" + runtime.ID, Input: input, ActorPrincipalID: cmd.ActorPrincipalID, RequestID: cmd.RequestID, TraceID: cmd.TraceID})
	if err != nil {
		return inv, err
	}
	if inv.Status != tool.StatusSucceeded {
		return inv, fmt.Errorf("sandbox routine invocation ended in %s", inv.Status)
	}
	return inv, nil
}
