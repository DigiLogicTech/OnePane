package chatcommands

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type Service struct {
	Store     Store
	Authority Authority
	Now       func() time.Time
	NewID     func() string
}

func NewService(store Store) *Service {
	seq := int64(0)
	return &Service{Store: store, Now: func() time.Time { return time.Now().UTC() }, NewID: func() string { seq++; return fmt.Sprintf("q-%d", seq) }}
}

func (s *Service) Handle(ctx context.Context, sessionID, input string) (Result, error) {
	cmd, err := Parse(input)
	if err != nil {
		return Result{}, err
	}
	if s.Store == nil {
		return Result{}, errors.New("chatcommands: nil store")
	}
	if s.Authority != nil {
		if err := s.Authority.CanControlSession(sessionID, cmd.Name); err != nil {
			return Result{}, err
		}
	}
	r := Result{Handled: true, Action: ActionNone, Command: &cmd}
	switch cmd.Name {
	case "queue":
		return s.handleQueue(ctx, sessionID, cmd, r)
	case "busy":
		return s.handleBusy(ctx, sessionID, cmd, r)
	case "approvals":
		return s.handleApprovals(ctx, sessionID, cmd, r)
	case "yolo":
		return s.handleYolo(ctx, sessionID, cmd, r)
	case "model":
		return s.handleOverride(ctx, sessionID, cmd, r, true)
	case "agent":
		return s.handleOverride(ctx, sessionID, cmd, r, false)
	case "reasoning":
		return s.handleReasoning(ctx, sessionID, cmd, r)
	case "steer":
		r.Action = ActionSteer
		r.Message = strings.Join(cmd.Args, " ")
	case "stop":
		r.Action = ActionStop
	case "retry":
		r.Action = ActionRetry
	case "continue":
		r.Action = ActionContinue
	case "new":
		r.Action = ActionNewSession
	case "branch":
		r.Action = ActionBranch
		r.Message = strings.Join(cmd.Args, " ")
	case "background":
		r.Action = ActionBackground
		r.Message = strings.Join(cmd.Args, " ")
	case "goal":
		r.Action = ActionGoal
		r.Message = strings.Join(cmd.Args, " ")
	case "subgoal":
		r.Action = ActionSubgoal
		r.Message = strings.Join(cmd.Args, " ")
	case "verify":
		r.Action = ActionVerify
		r.Message = strings.Join(cmd.Args, " ")
	case "checkpoint":
		r.Action = ActionCheckpoint
		r.Message = strings.Join(cmd.Args, " ")
	case "approve", "deny":
		r.Action = ActionApproval
		r.Message = cmd.Name + " " + strings.Join(cmd.Args, " ")
	case "sandbox", "tools", "skills", "task", "plan":
		r.Action = ActionSessionChange
		r.Message = cmd.Name + " " + strings.Join(cmd.Args, " ")
	default:
		r.Action = ActionReadOnly
		r.Message = cmd.Name + " " + strings.Join(cmd.Args, " ")
	}
	return r, nil
}

func (s *Service) handleQueue(ctx context.Context, id string, cmd ParsedCommand, r Result) (Result, error) {
	controls, err := s.Store.Controls(ctx, id)
	if err != nil {
		return Result{}, err
	}
	if len(cmd.Args) == 0 || cmd.Args[0] == "list" {
		q, e := s.Store.Queue(ctx, id)
		r.Action = ActionQueueChanged
		r.Queue = q
		r.Controls = &controls
		return r, e
	}
	sub := strings.ToLower(cmd.Args[0])
	switch sub {
	case "pause":
		controls.QueuePaused = true
		if err := s.Store.SaveControls(ctx, controls); err != nil {
			return Result{}, err
		}
		r.Message = "Queue paused"
	case "resume":
		controls.QueuePaused = false
		if err := s.Store.SaveControls(ctx, controls); err != nil {
			return Result{}, err
		}
		r.Message = "Queue resumed"
	case "clear":
		if err := s.Store.ReplaceQueue(ctx, id, nil); err != nil {
			return Result{}, err
		}
		r.Message = "Queue cleared"
	case "remove", "rm":
		if len(cmd.Args) < 2 {
			return Result{}, fmt.Errorf("usage: /queue remove <n>")
		}
		n, err := strconv.Atoi(cmd.Args[1])
		if err != nil || n < 1 {
			return Result{}, fmt.Errorf("invalid queue position")
		}
		items, err := s.Store.Queue(ctx, id)
		if err != nil {
			return Result{}, err
		}
		if n > len(items) {
			return Result{}, ErrQueueItemNotFound
		}
		items = append(items[:n-1], items[n:]...)
		if err := s.Store.ReplaceQueue(ctx, id, items); err != nil {
			return Result{}, err
		}
		r.Message = fmt.Sprintf("Removed queue item %d", n)
	case "move":
		if len(cmd.Args) < 3 {
			return Result{}, fmt.Errorf("usage: /queue move <from> <to>")
		}
		from, e1 := strconv.Atoi(cmd.Args[1])
		to, e2 := strconv.Atoi(cmd.Args[2])
		items, err := s.Store.Queue(ctx, id)
		if e1 != nil || e2 != nil || from < 1 || to < 1 || from > len(items) || to > len(items) {
			return Result{}, fmt.Errorf("invalid queue position")
		}
		if err != nil {
			return Result{}, err
		}
		item := items[from-1]
		items = append(items[:from-1], items[from:]...)
		to--
		items = append(items[:to], append([]QueueItem{item}, items[to:]...)...)
		if err := s.Store.ReplaceQueue(ctx, id, items); err != nil {
			return Result{}, err
		}
		r.Message = fmt.Sprintf("Moved queue item %d to %d", from, to+1)
	default:
		prompt := strings.Join(cmd.Args, " ")
		if strings.TrimSpace(prompt) == "" {
			return Result{}, fmt.Errorf("queue prompt is empty")
		}
		q := QueueItem{ID: s.NewID(), SessionID: id, Prompt: prompt, Status: QueuePending, CreatedAt: s.Now(), UpdatedAt: s.Now()}
		if err := s.Store.Enqueue(ctx, q); err != nil {
			return Result{}, err
		}
		r.Message = "Prompt queued"
	}
	q, err := s.Store.Queue(ctx, id)
	r.Action = ActionQueueChanged
	r.Queue = q
	r.Controls = &controls
	return r, err
}

func (s *Service) handleBusy(ctx context.Context, id string, cmd ParsedCommand, r Result) (Result, error) {
	c, err := s.Store.Controls(ctx, id)
	if err != nil {
		return Result{}, err
	}
	if len(cmd.Args) == 0 {
		r.Action = ActionSessionChange
		r.Controls = &c
		r.Message = string(c.BusyMode)
		return r, nil
	}
	m := BusyMode(strings.ToLower(cmd.Args[0]))
	if m != BusyQueue && m != BusySteer && m != BusyInterrupt {
		return Result{}, fmt.Errorf("usage: /busy queue|steer|interrupt")
	}
	c.BusyMode = m
	if err := s.Store.SaveControls(ctx, c); err != nil {
		return Result{}, err
	}
	r.Action = ActionSessionChange
	r.Controls = &c
	r.Message = "Busy mode set to " + string(m)
	return r, nil
}

func (s *Service) handleApprovals(ctx context.Context, id string, cmd ParsedCommand, r Result) (Result, error) {
	c, err := s.Store.Controls(ctx, id)
	if err != nil {
		return Result{}, err
	}
	if c.ApprovalLevel == "" {
		c.ApprovalLevel = ApprovalMedium
	}
	arg := "status"
	if len(cmd.Args) > 0 {
		arg = strings.ToLower(cmd.Args[0])
	}
	switch ApprovalLevel(arg) {
	case ApprovalHigh, ApprovalMedium, ApprovalLow:
		c.ApprovalLevel = ApprovalLevel(arg)
		// Choosing an explicit profile also exits YOLO, making the action
		// predictable and giving the user a quick way back to normal mode.
		c.ApprovalMode = ApprovalManual
		if err := s.Store.SaveControls(ctx, c); err != nil {
			return Result{}, err
		}
		r.Message = "Approval strictness set to " + arg
	case "status":
		r.Message = "Approval strictness is " + string(c.ApprovalLevel)
	default:
		return Result{}, fmt.Errorf("usage: /approvals high|medium|low|status")
	}
	r.Action = ActionSessionChange
	r.Controls = &c
	return r, nil
}

func (s *Service) handleYolo(ctx context.Context, id string, cmd ParsedCommand, r Result) (Result, error) {
	c, err := s.Store.Controls(ctx, id)
	if err != nil {
		return Result{}, err
	}
	arg := "status"
	if len(cmd.Args) > 0 {
		arg = strings.ToLower(cmd.Args[0])
	}
	switch arg {
	case "status":
	case "on":
		if s.Authority != nil {
			if err := s.Authority.CanEnableAutoApproval(id); err != nil {
				return Result{}, err
			}
		}
		c.ApprovalMode = ApprovalAutoAuthorized
		if err := s.Store.SaveControls(ctx, c); err != nil {
			return Result{}, err
		}
		r.Message = "YOLO enabled for this session: authorized approvals will be auto-approved"
	case "off":
		c.ApprovalMode = ApprovalManual
		if err := s.Store.SaveControls(ctx, c); err != nil {
			return Result{}, err
		}
		r.Message = "YOLO disabled; approvals are manual"
	default:
		return Result{}, fmt.Errorf("usage: /yolo on|off|status")
	}
	r.Action = ActionSessionChange
	r.Controls = &c
	if r.Message == "" {
		r.Message = "YOLO is " + map[bool]string{true: "on", false: "off"}[c.ApprovalMode == ApprovalAutoAuthorized]
	}
	return r, nil
}

func (s *Service) handleOverride(ctx context.Context, id string, cmd ParsedCommand, r Result, model bool) (Result, error) {
	c, err := s.Store.Controls(ctx, id)
	if err != nil {
		return Result{}, err
	}
	if len(cmd.Args) == 0 {
		r.Action = ActionSessionChange
		r.Controls = &c
		return r, nil
	}
	v := strings.Join(cmd.Args, " ")
	if v == "auto" {
		v = ""
	}
	if model {
		c.ModelOverride = v
		r.Action = ActionModel
	} else {
		c.AgentOverride = v
		r.Action = ActionAgent
	}
	if err := s.Store.SaveControls(ctx, c); err != nil {
		return Result{}, err
	}
	r.Controls = &c
	r.Message = "Session override updated"
	return r, nil
}
func (s *Service) handleReasoning(ctx context.Context, id string, cmd ParsedCommand, r Result) (Result, error) {
	c, err := s.Store.Controls(ctx, id)
	if err != nil {
		return Result{}, err
	}
	if len(cmd.Args) == 0 {
		r.Action = ActionSessionChange
		r.Controls = &c
		return r, nil
	}
	v := strings.ToLower(cmd.Args[0])
	valid := map[string]bool{"off": true, "low": true, "medium": true, "high": true, "extra-high": true, "auto": true}
	if !valid[v] {
		return Result{}, fmt.Errorf("invalid reasoning effort")
	}
	if v == "auto" {
		v = ""
	}
	c.ReasoningEffort = v
	if err := s.Store.SaveControls(ctx, c); err != nil {
		return Result{}, err
	}
	r.Action = ActionReasoning
	r.Controls = &c
	r.Message = "Reasoning preference updated"
	return r, nil
}
