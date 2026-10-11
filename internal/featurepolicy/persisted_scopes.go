package featurepolicy

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// StoredScopes reads trusted, versioned scope data from the persisted Project,
// canonical Workspace and Task. Task-supplied overrides can only restrict a
// Project/Workspace grant, never expand it.
type StoredScopes struct{ DB *sql.DB }

type ScopedDecision struct {
	Effective Policy
	Configured bool
}

// feature_policy is opt-in for existing installations. Existing authority,
// sandbox and data-flow gates remain mandatory even when no feature policy
// was configured; the absence of a feature policy is NOT an execution grant.
func parseFeatureDocument(raw []byte, base Policy) (Policy, bool, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return base, false, nil
	}
	var outer map[string]json.RawMessage
	if err := json.Unmarshal(raw, &outer); err != nil || outer == nil {
		return Policy{}, false, fmt.Errorf("invalid scope document")
	}
	value, exists := outer["feature_policy"]
	if !exists {
		return base, false, nil
	}
	if len(value) == 0 || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return Policy{}, false, fmt.Errorf("null feature policy is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	p := base
	if err := decoder.Decode(&p); err != nil {
		return Policy{}, false, fmt.Errorf("invalid feature policy: %w", err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return Policy{}, false, fmt.Errorf("trailing feature policy data")
	}
	if err := validatePolicyValues(p); err != nil {
		return Policy{}, false, err
	}
	return p, true, nil
}

func validatePolicyValues(p Policy) error {
	for _, state := range []State{
		p.Assistant,p.ProjectAI,p.ProjectMemory,p.TelemetryAccess,
		p.TelemetryAutonomy,p.ModelRouting,p.AutomaticFallback,
		p.CapabilityRouting,p.CloudRouting,p.Images,p.Files,
		p.AudioInput,p.AudioOutput,p.Camera,p.Video,p.Team,p.Council,
		p.CrossProjectShare,p.AutonomousRoutines,
	} {
		switch state {
		case "",Inherited,Enabled,Disabled,Unavailable:
		default:
			return fmt.Errorf("invalid feature policy state")
		}
	}
	switch p.ConfigAuthority {
	case "",AuthorityNever,AuthorityRead,AuthorityPropose,AuthorityAuto:
		return nil
	default:
		return fmt.Errorf("invalid feature policy authority")
	}
}

// ForTask loads the real source scopes, not permissions claimed by a Task or
// model response. The tenant and canonical ownership checks are intentional:
// a Task cannot name another Project's Workspace to obtain additional grants.
func (s StoredScopes) ForTask(ctx context.Context, tenantID, taskID string) (ScopedDecision, error) {
	if s.DB == nil || strings.TrimSpace(tenantID) == "" || strings.TrimSpace(taskID) == "" {
		return ScopedDecision{}, fmt.Errorf("scope resolver unavailable")
	}
	var topStatus string
	if err := s.DB.QueryRowContext(ctx,"SELECT status FROM workspaces WHERE id=?",tenantID).Scan(&topStatus); err != nil {
		return ScopedDecision{}, fmt.Errorf("execution Workspace unavailable")
	}
	if topStatus != "active" {
		return ScopedDecision{}, fmt.Errorf("Workspace %s is not active (state=%s)",tenantID,topStatus)
	}
	var projectID,workspaceID sql.NullString
	var completion string
	err := s.DB.QueryRowContext(ctx,
		"SELECT project_id,project_workspace_id,COALESCE(completion_json,'{}') FROM tasks WHERE id=? AND workspace_id=?",
		taskID,tenantID).Scan(&projectID,&workspaceID,&completion)
	if err != nil { return ScopedDecision{}, fmt.Errorf("resolve canonical Task scope: %w",err) }
	if !projectID.Valid {
		if workspaceID.Valid {
			return ScopedDecision{}, fmt.Errorf("canonical Workspace requires a Project owner")
		}
		return ScopedDecision{Configured:false},nil // legacy Task: existing authority still applies
	}
	var projectJSON,projectStatus string
	if err := s.DB.QueryRowContext(ctx,
		"SELECT project_policy_json,status FROM projects WHERE id=? AND workspace_id=?",
		projectID.String,tenantID).Scan(&projectJSON,&projectStatus);err!=nil||projectStatus!="active"{
		return ScopedDecision{},fmt.Errorf("Project scope unavailable or inactive")
	}
	project,fromProject,err:=parseFeatureDocument([]byte(projectJSON),AssistedDefaults())
	if err!=nil{return ScopedDecision{},err}
	workspace:=Policy{}
	fromWorkspace:=false
	if workspaceID.Valid {
		var settings,status string
		if err:=s.DB.QueryRowContext(ctx,
			"SELECT ai_settings_json,status FROM project_workspaces WHERE id=? AND project_id=?",
			workspaceID.String,projectID.String).Scan(&settings,&status);err!=nil||status!="active"{
			return ScopedDecision{},fmt.Errorf("canonical Workspace scope unavailable or inactive")
		}
		workspace,fromWorkspace,err=parseFeatureDocument([]byte(settings),Policy{})
		if err!=nil{return ScopedDecision{},err}
	}
	var taskDoc map[string]json.RawMessage
	if err:=json.Unmarshal([]byte(completion),&taskDoc);err!=nil{
		return ScopedDecision{},fmt.Errorf("invalid Task execution scope")
	}
	taskOverride:=Policy{}
	fromTask:=false
	if routing,ok:=taskDoc["onepane_routing"];ok {
		taskOverride,fromTask,err=parseFeatureDocument(routing,Policy{})
		if err!=nil{return ScopedDecision{},err}
	}
	return ScopedDecision{
		Effective:ResolvePolicyChain(project,workspace,taskOverride),
		Configured:fromProject||fromWorkspace||fromTask,
	},nil
}

// AuthorizeTask determines whether a configured feature policy also permits
// the action. The existing gateway authorization remains mandatory.
func (s StoredScopes) AuthorizeTask(ctx context.Context,tenantID,taskID,action string)(bool,error){
	decision,err:=s.ForTask(ctx,tenantID,taskID)
	if err!=nil{return false,err}
	if !decision.Configured{return true,nil}
	return decision.Effective.ValidateExecution(action),nil
}
