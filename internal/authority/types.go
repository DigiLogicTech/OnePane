package authority

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type Status string

const (
	StatusActive      Status = "active"
	StatusExpired     Status = "expired"
	StatusRevoked     Status = "revoked"
	StatusExhausted   Status = "exhausted"
	StatusInvalidated Status = "invalidated"
)

type ActionMode string

const (
	ActionObserve          ActionMode = "observe"
	ActionRead             ActionMode = "read"
	ActionExecuteSandboxed ActionMode = "execute_sandboxed"
	ActionMutate           ActionMode = "mutate"
	ActionExternalSend     ActionMode = "external_send"
)

type Scope struct {
	AnyResource      bool         `json:"any_resource,omitempty"`
	ResourceRefs     []string     `json:"resource_refs,omitempty"`
	ResourcePrefixes []string     `json:"resource_prefixes,omitempty"`
	AnyAction        bool         `json:"any_action,omitempty"`
	Actions          []ActionMode `json:"actions,omitempty"`
}

type Lease struct {
	ID           string
	WorkspaceID  string
	PrincipalID  string
	TaskID       *string
	CapabilityID string
	Scope        Scope
	Status       Status
	IssuedBy     string
	IssuedAt     int64
	ExpiresAt    int64
	UsageLimit   *int64
	UsageCount   int64
	Revision     int64
}

type IssueCommand struct {
	WorkspaceID      string
	PrincipalID      string
	TaskID           *string
	CapabilityID     string
	Scope            Scope
	IssuedBy         string
	ExpiresAt        int64
	UsageLimit       *int64
	ActorPrincipalID *string
	RequestID        *string
	TraceID          *string
}

type TransitionCommand struct {
	LeaseID          string
	ExpectedRevision int64
	ActorPrincipalID *string
	RequestID        *string
	TraceID          *string
	Reason           string
}

type ConsumeCommand struct {
	LeaseID          string
	ExpectedRevision int64
	Uses             int64
	ActorPrincipalID *string
	RequestID        *string
	TraceID          *string
}

type Subject struct {
	PrincipalType    string
	PrincipalStatus  string
	WorkspaceStatus  string
	MembershipStatus *string
}

var (
	ErrInvalidCommand     = errors.New("invalid authority command")
	ErrInvalidScope       = errors.New("invalid capability scope")
	ErrInvalidTransition  = errors.New("invalid capability lease transition")
	ErrRevisionConflict   = errors.New("revision conflict")
	ErrExpired            = errors.New("capability lease expired")
	ErrRevoked            = errors.New("capability lease revoked")
	ErrExhausted          = errors.New("capability lease exhausted")
	ErrInvalidated        = errors.New("capability lease invalidated")
	ErrWorkspaceInactive  = errors.New("workspace is not active")
	ErrPrincipalInactive  = errors.New("principal is not active")
	ErrMembershipInactive = errors.New("workspace membership is not active")
	ErrTaskWorkspace      = errors.New("task belongs to a different workspace")
	ErrSelfGrant          = errors.New("agent principal cannot grant authority to itself")
)

func ValidStatus(s Status) bool {
	switch s {
	case StatusActive, StatusExpired, StatusRevoked, StatusExhausted, StatusInvalidated:
		return true
	default:
		return false
	}
}

func CanTransition(from, to Status) bool {
	if from == to {
		return false
	}
	return from == StatusActive && (to == StatusExpired || to == StatusRevoked || to == StatusExhausted || to == StatusInvalidated)
}

func ValidActionMode(m ActionMode) bool {
	switch m {
	case ActionObserve, ActionRead, ActionExecuteSandboxed, ActionMutate, ActionExternalSend:
		return true
	default:
		return false
	}
}

func (s Scope) Validate() error {
	if s.AnyResource && (len(s.ResourceRefs) > 0 || len(s.ResourcePrefixes) > 0) {
		return fmt.Errorf("%w: any_resource cannot be combined with explicit resource constraints", ErrInvalidScope)
	}
	if !s.AnyResource && len(s.ResourceRefs) == 0 && len(s.ResourcePrefixes) == 0 {
		return fmt.Errorf("%w: an explicit resource boundary is required", ErrInvalidScope)
	}
	if s.AnyAction && len(s.Actions) > 0 {
		return fmt.Errorf("%w: any_action cannot be combined with explicit actions", ErrInvalidScope)
	}
	if !s.AnyAction && len(s.Actions) == 0 {
		return fmt.Errorf("%w: an explicit action boundary is required", ErrInvalidScope)
	}

	seen := map[string]struct{}{}
	for _, ref := range s.ResourceRefs {
		if strings.TrimSpace(ref) == "" {
			return fmt.Errorf("%w: blank resource_ref", ErrInvalidScope)
		}
		key := "ref:" + ref
		if _, ok := seen[key]; ok {
			return fmt.Errorf("%w: duplicate resource_ref %q", ErrInvalidScope, ref)
		}
		seen[key] = struct{}{}
	}
	for _, prefix := range s.ResourcePrefixes {
		if strings.TrimSpace(prefix) == "" {
			return fmt.Errorf("%w: blank resource_prefix", ErrInvalidScope)
		}
		key := "prefix:" + prefix
		if _, ok := seen[key]; ok {
			return fmt.Errorf("%w: duplicate resource_prefix %q", ErrInvalidScope, prefix)
		}
		seen[key] = struct{}{}
	}
	seenActions := map[ActionMode]struct{}{}
	for _, action := range s.Actions {
		if !ValidActionMode(action) {
			return fmt.Errorf("%w: unknown action %q", ErrInvalidScope, action)
		}
		if _, ok := seenActions[action]; ok {
			return fmt.Errorf("%w: duplicate action %q", ErrInvalidScope, action)
		}
		seenActions[action] = struct{}{}
	}
	return nil
}

func (s Scope) AllowsResource(resourceRef string) bool {
	if s.AnyResource {
		return true
	}
	for _, ref := range s.ResourceRefs {
		if resourceRef == ref {
			return true
		}
	}
	for _, prefix := range s.ResourcePrefixes {
		if strings.HasPrefix(resourceRef, prefix) {
			return true
		}
	}
	return false
}

func (s Scope) AllowsAction(action ActionMode) bool {
	if s.AnyAction {
		return true
	}
	for _, allowed := range s.Actions {
		if action == allowed {
			return true
		}
	}
	return false
}

func (s Scope) JSON() (json.RawMessage, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	b, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("marshal capability scope: %w", err)
	}
	return b, nil
}
