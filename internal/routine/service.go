package routine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/id"
	"github.com/DigiLogicTech/OnePane/internal/storage"
	"github.com/DigiLogicTech/OnePane/internal/task"
)

type Status string

const (
	StatusActive   Status = "active"
	StatusPaused   Status = "paused"
	StatusDisabled Status = "disabled"
	StatusArchived Status = "archived"
)

type OccurrenceState string

const (
	OccurrenceExpected     OccurrenceState = "expected"
	OccurrenceInstantiated OccurrenceState = "instantiated"
	OccurrenceRunning      OccurrenceState = "running"
	OccurrenceVerifying    OccurrenceState = "verifying"
	OccurrenceSucceeded    OccurrenceState = "succeeded"
	OccurrenceRetrying     OccurrenceState = "retrying"
	OccurrenceFailed       OccurrenceState = "failed"
	OccurrenceSkipped      OccurrenceState = "skipped"
	OccurrenceCancelled    OccurrenceState = "cancelled"
	OccurrenceMissed       OccurrenceState = "missed"
	OccurrenceSuperseded   OccurrenceState = "superseded"
)

type Routine struct {
	ID, WorkspaceID, Name, Timezone, CreatedBy string
	DefinitionVersion                          int64
	Status                                     Status
	TriggerJSON, PolicyJSON                    json.RawMessage
	CreatedAt, UpdatedAt                       int64
}

type Occurrence struct {
	ID, RoutineID, OccurrenceKey         string
	DefinitionVersion                    int64
	TaskID                               *string
	State                                OccurrenceState
	IntendedLocalTime                    *string
	TriggerTimeUTC, CreatedAt, UpdatedAt int64
}

type Trigger struct {
	Kind         string `json:"kind"`
	EverySeconds int64  `json:"every_seconds,omitempty"`
	StartAtUTC   *int64 `json:"start_at_utc,omitempty"`
	EndAtUTC     *int64 `json:"end_at_utc,omitempty"`
	LocalTime    string `json:"local_time,omitempty"`
	Weekdays     []int  `json:"weekdays,omitempty"`
}

type Policy struct {
	CatchUp        string          `json:"catch_up,omitempty"` // latest | all | skip
	MaxCatchUp     int             `json:"max_catch_up,omitempty"`
	MaxOccurrences    int             `json:"max_occurrences,omitempty"`
	ProjectID         *string         `json:"project_id,omitempty"`
	ProjectWorkspaceID *string        `json:"project_workspace_id,omitempty"`
	Objective          string          `json:"objective,omitempty"`
	Priority           int             `json:"priority,omitempty"`
	Completion         json.RawMessage `json:"completion,omitempty"`
}

type CreateCommand struct {
	WorkspaceID, Name, Timezone, CreatedBy string
	Trigger                                Trigger
	Policy                                 Policy
}

type Service struct {
	db     *sql.DB
	tx     storage.Transactor
	tasks  *task.Service
	events event.Store
	ids    id.Generator
	clock  clock.Clock
}

func NewService(db *sql.DB, tx storage.Transactor, clk clock.Clock, tasks *task.Service) *Service {
	return &Service{db: db, tx: tx, tasks: tasks, events: event.Store{}, ids: id.Generator{}, clock: clk}
}

func validateTrigger(t Trigger) error {
	if t.EndAtUTC != nil && *t.EndAtUTC <= 0 {
		return errors.New("end_at_utc must be a positive unix millisecond timestamp")
	}
	if t.StartAtUTC != nil && t.EndAtUTC != nil && *t.EndAtUTC < *t.StartAtUTC {
		return errors.New("end_at_utc must not be before start_at_utc")
	}
	switch t.Kind {
	case "interval":
		if t.EverySeconds < 60 || t.EverySeconds > 365*24*3600 {
			return errors.New("interval every_seconds must be between 60 seconds and 365 days")
		}
	case "daily":
		if _, err := time.Parse("15:04", t.LocalTime); err != nil {
			return errors.New("daily trigger requires local_time HH:MM")
		}
		seen := map[int]bool{}
		for _, d := range t.Weekdays {
			if d < 1 || d > 7 || seen[d] {
				return errors.New("weekdays must contain unique ISO weekday values 1..7")
			}
			seen[d] = true
		}
	default:
		return errors.New("unsupported routine trigger kind")
	}
	return nil
}
func normalizePolicy(p Policy) (Policy, error) {
	if p.CatchUp == "" {
		p.CatchUp = "latest"
	}
	switch p.CatchUp {
	case "latest", "all", "skip":
	default:
		return p, errors.New("invalid catch_up policy")
	}
	if p.MaxCatchUp == 0 {
		p.MaxCatchUp = 10
	}
	if p.MaxCatchUp < 1 || p.MaxCatchUp > 100 {
		return p, errors.New("max_catch_up must be 1..100")
	}
	if p.MaxOccurrences < 0 || p.MaxOccurrences > 100000 {
		return p, errors.New("max_occurrences must be 1..100000 when set")
	}
	if len(p.Completion) == 0 {
		p.Completion = json.RawMessage(`{}`)
	}
	if !json.Valid(p.Completion) {
		return p, errors.New("completion must be valid JSON")
	}
	return p, nil
}

func (s *Service) Create(ctx context.Context, cmd CreateCommand) (Routine, error) {
	if s == nil || s.tasks == nil || strings.TrimSpace(cmd.WorkspaceID) == "" || strings.TrimSpace(cmd.Name) == "" || strings.TrimSpace(cmd.CreatedBy) == "" {
		return Routine{}, errors.New("workspace, name and creator required")
	}
	if _, err := time.LoadLocation(cmd.Timezone); err != nil {
		return Routine{}, fmt.Errorf("invalid timezone: %w", err)
	}
	if err := validateTrigger(cmd.Trigger); err != nil {
		return Routine{}, err
	}
	pol, err := normalizePolicy(cmd.Policy)
	if err != nil {
		return Routine{}, err
	}
	now := s.clock.UnixMilli()
	if cmd.Trigger.EndAtUTC != nil && *cmd.Trigger.EndAtUTC <= now {
		return Routine{}, errors.New("end_at_utc must be in the future")
	}
	triggerRaw, _ := json.Marshal(cmd.Trigger)
	policyRaw, _ := json.Marshal(pol)
	idv, _ := s.ids.New("routine")
	r := Routine{ID: idv, WorkspaceID: cmd.WorkspaceID, Name: strings.TrimSpace(cmd.Name), DefinitionVersion: 1, Status: StatusActive, TriggerJSON: triggerRaw, PolicyJSON: policyRaw, Timezone: cmd.Timezone, CreatedBy: cmd.CreatedBy, CreatedAt: now, UpdatedAt: now}
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		var ok int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM workspace_memberships wm JOIN principals p ON p.id=wm.principal_id WHERE wm.workspace_id=? AND wm.principal_id=? AND wm.status='active' AND p.status='active'`, r.WorkspaceID, r.CreatedBy).Scan(&ok); err != nil {
			return err
		}
		if ok != 1 {
			return errors.New("routine creator is not an active workspace member")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO routines(id,workspace_id,name,definition_version,status,trigger_json,policy_json,timezone,created_by,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, r.ID, r.WorkspaceID, r.Name, r.DefinitionVersion, r.Status, string(r.TriggerJSON), string(r.PolicyJSON), r.Timezone, r.CreatedBy, now, now); err != nil {
			return err
		}
		eid, _ := s.ids.New("evt")
		payload, _ := json.Marshal(map[string]any{"routine_id": r.ID, "name": r.Name, "definition_version": 1, "status": r.Status})
		actor := r.CreatedBy
		return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: &r.WorkspaceID, Type: "routine.created", AggregateType: "routine", AggregateID: r.ID, ActorPrincipalID: &actor, Payload: payload, OccurredAt: now})
	})
	return r, err
}

func (s *Service) SetStatus(ctx context.Context, idv string, expectedDefinition int64, status Status, actor *string) (Routine, error) {
	if expectedDefinition < 1 || (status != StatusActive && status != StatusPaused && status != StatusDisabled && status != StatusArchived) {
		return Routine{}, errors.New("invalid routine status command")
	}
	now := s.clock.UnixMilli()
	err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		r, err := routineTx(ctx, tx, idv)
		if err != nil {
			return err
		}
		if r.DefinitionVersion != expectedDefinition {
			return errors.New("routine definition conflict")
		}
		if r.Status == StatusArchived {
			return errors.New("archived routine is terminal")
		}
		res, err := tx.ExecContext(ctx, `UPDATE routines SET status=?,updated_at=? WHERE id=? AND definition_version=?`, status, now, idv, expectedDefinition)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("routine definition conflict")
		}
		eid, _ := s.ids.New("evt")
		payload, _ := json.Marshal(map[string]any{"routine_id": idv, "from": r.Status, "to": status})
		return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: &r.WorkspaceID, Type: "routine.status_changed", AggregateType: "routine", AggregateID: idv, ActorPrincipalID: actor, Payload: payload, OccurredAt: now})
	})
	if err != nil {
		return Routine{}, err
	}
	return s.Get(ctx, idv)
}

func (s *Service) Get(ctx context.Context, idv string) (Routine, error) {
	return scanRoutine(s.db.QueryRowContext(ctx, `SELECT id,workspace_id,name,definition_version,status,trigger_json,policy_json,timezone,created_by,created_at,updated_at FROM routines WHERE id=?`, idv))
}

func (s *Service) List(ctx context.Context, workspaceID string) ([]Routine, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, errors.New("workspace required")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,workspace_id,name,definition_version,status,trigger_json,policy_json,timezone,created_by,created_at,updated_at FROM routines WHERE workspace_id=? ORDER BY updated_at DESC`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Routine, 0)
	for rows.Next() {
		r, err := scanRoutine(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type rowScanner interface{ Scan(...any) error }

func scanRoutine(row rowScanner) (Routine, error) {
	var r Routine
	var trig, pol string
	err := row.Scan(&r.ID, &r.WorkspaceID, &r.Name, &r.DefinitionVersion, &r.Status, &trig, &pol, &r.Timezone, &r.CreatedBy, &r.CreatedAt, &r.UpdatedAt)
	r.TriggerJSON = json.RawMessage(trig)
	r.PolicyJSON = json.RawMessage(pol)
	return r, err
}
func routineTx(ctx context.Context, tx storage.Tx, idv string) (Routine, error) {
	return scanRoutine(tx.QueryRowContext(ctx, `SELECT id,workspace_id,name,definition_version,status,trigger_json,policy_json,timezone,created_by,created_at,updated_at FROM routines WHERE id=?`, idv))
}

// Tick deterministically materializes all due occurrences up to now. It is safe
// to call repeatedly: (routine_id, occurrence_key) is unique and each accepted
// occurrence creates its Task in the same database transaction.
func (s *Service) Tick(ctx context.Context, now time.Time) ([]Occurrence, error) {
	now = now.UTC()
	rows, err := s.db.QueryContext(ctx, `SELECT id,workspace_id,name,definition_version,status,trigger_json,policy_json,timezone,created_by,created_at,updated_at FROM routines WHERE status='active' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rs []Routine
	for rows.Next() {
		r, err := scanRoutine(rows)
		if err != nil {
			return nil, err
		}
		rs = append(rs, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var created []Occurrence
	for _, r := range rs {
		xs, err := s.due(ctx, r, now)
		if err != nil {
			return created, fmt.Errorf("routine %s: %w", r.ID, err)
		}
		for _, d := range xs {
			o, err := s.materialize(ctx, r, d)
			if err != nil {
				if isOccurrenceConflict(err) {
					continue
				}
				return created, err
			}
			created = append(created, o)
		}
		if err := s.disableIfExhausted(ctx, r, now); err != nil {
			return created, fmt.Errorf("routine %s exhaustion: %w", r.ID, err)
		}
	}
	return created, nil
}

type dueOccurrence struct {
	TriggerUTC    time.Time
	IntendedLocal string
	State         OccurrenceState
}

func (s *Service) due(ctx context.Context, r Routine, now time.Time) ([]dueOccurrence, error) {
	var trig Trigger
	var pol Policy
	if json.Unmarshal(r.TriggerJSON, &trig) != nil || json.Unmarshal(r.PolicyJSON, &pol) != nil {
		return nil, errors.New("stored routine definition is corrupt")
	}
	if err := validateTrigger(trig); err != nil {
		return nil, err
	}
	pol, err := normalizePolicy(pol)
	if err != nil {
		return nil, err
	}
	completedRuns, err := s.materializedOccurrenceCount(ctx, r)
	if err != nil {
		return nil, err
	}
	if pol.MaxOccurrences > 0 && completedRuns >= pol.MaxOccurrences {
		return nil, nil
	}
	var last sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(trigger_time_utc) FROM routine_occurrences WHERE routine_id=? AND definition_version=?`, r.ID, r.DefinitionVersion).Scan(&last); err != nil {
		return nil, err
	}
	after := time.UnixMilli(r.CreatedAt)
	if last.Valid {
		after = time.UnixMilli(last.Int64)
	}
	effectiveNow := now
	if trig.EndAtUTC != nil {
		end := time.UnixMilli(*trig.EndAtUTC).UTC()
		if after.After(end) || after.Equal(end) {
			return nil, nil
		}
		if effectiveNow.After(end) {
			effectiveNow = end
		}
	}
	if !after.Before(effectiveNow) {
		return nil, nil
	}
	if pol.CatchUp == "latest" || pol.CatchUp == "skip" {
		latest, intended, ok, err := latestDueTrigger(trig, r.Timezone, time.UnixMilli(r.CreatedAt), after, effectiveNow)
		if err != nil || !ok {
			return nil, err
		}
		state := OccurrenceExpected
		if pol.CatchUp == "skip" {
			state = OccurrenceMissed
		}
		return []dueOccurrence{{TriggerUTC: latest, IntendedLocal: intended, State: state}}, nil
	}
	// `all` is intentionally bounded. If more work remains, the next scheduler
	// tick continues from the newest materialized occurrence rather than trying
	// to synthesize an unbounded backlog in one transaction burst.
	remaining := pol.MaxCatchUp
	if pol.MaxOccurrences > 0 && pol.MaxOccurrences-completedRuns < remaining {
		remaining = pol.MaxOccurrences - completedRuns
	}
	due := make([]dueOccurrence, 0, remaining)
	cursor := after
	for len(due) < remaining {
		next, intended, ok, err := nextTrigger(trig, r.Timezone, time.UnixMilli(r.CreatedAt), cursor)
		if err != nil {
			return nil, err
		}
		if !ok || next.After(effectiveNow) {
			break
		}
		due = append(due, dueOccurrence{TriggerUTC: next, IntendedLocal: intended, State: OccurrenceExpected})
		cursor = next
	}
	return due, nil
}


func (s *Service) materializedOccurrenceCount(ctx context.Context, r Routine) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM routine_occurrences WHERE routine_id=? AND definition_version=? AND task_id IS NOT NULL`, r.ID, r.DefinitionVersion).Scan(&n)
	return n, err
}

func (s *Service) disableIfExhausted(ctx context.Context, r Routine, now time.Time) error {
	var trig Trigger
	var pol Policy
	if err := json.Unmarshal(r.TriggerJSON, &trig); err != nil {
		return err
	}
	if err := json.Unmarshal(r.PolicyJSON, &pol); err != nil {
		return err
	}
	pol, err := normalizePolicy(pol)
	if err != nil {
		return err
	}
	exhausted := false
	reason := ""
	if pol.MaxOccurrences > 0 {
		n, err := s.materializedOccurrenceCount(ctx, r)
		if err != nil {
			return err
		}
		if n >= pol.MaxOccurrences {
			exhausted = true
			reason = "max_occurrences"
		}
	}
	if !exhausted && trig.EndAtUTC != nil && !now.Before(time.UnixMilli(*trig.EndAtUTC).UTC()) {
		exhausted = true
		reason = "end_at_utc"
	}
	if !exhausted {
		return nil
	}
	at := s.clock.UnixMilli()
	return s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE routines SET status='disabled',updated_at=? WHERE id=? AND status='active'`, at, r.ID)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil || n == 0 {
			return err
		}
		eid, _ := s.ids.New("evt")
		payload, _ := json.Marshal(map[string]any{"routine_id": r.ID, "reason": reason})
		return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: &r.WorkspaceID, Type: "routine.exhausted", AggregateType: "routine", AggregateID: r.ID, Payload: payload, OccurredAt: at})
	})
}

func latestDueTrigger(t Trigger, tz string, created, after, now time.Time) (time.Time, string, bool, error) {
	switch t.Kind {
	case "interval":
		anchor := created.UTC()
		if t.StartAtUTC != nil {
			anchor = time.UnixMilli(*t.StartAtUTC).UTC()
		}
		step := time.Duration(t.EverySeconds) * time.Second
		first := anchor
		if !first.After(after) {
			first = first.Add((after.Sub(first)/step + 1) * step)
		}
		if first.After(now) {
			return time.Time{}, "", false, nil
		}
		latest := first.Add((now.Sub(first) / step) * step)
		return latest, latest.Format(time.RFC3339), true, nil
	case "daily":
		loc, err := time.LoadLocation(tz)
		if err != nil {
			return time.Time{}, "", false, err
		}
		hm, _ := time.Parse("15:04", t.LocalTime)
		allowed := func(w time.Weekday) bool {
			if len(t.Weekdays) == 0 {
				return true
			}
			iso := int(w)
			if iso == 0 {
				iso = 7
			}
			for _, d := range t.Weekdays {
				if d == iso {
					return true
				}
			}
			return false
		}
		localNow := now.In(loc)
		for back := 0; back < 8; back++ {
			date := localNow.AddDate(0, 0, -back)
			candidate := time.Date(date.Year(), date.Month(), date.Day(), hm.Hour(), hm.Minute(), 0, 0, loc)
			utc := candidate.UTC()
			if !allowed(candidate.Weekday()) || utc.After(now) || !utc.After(after) || utc.Before(created.UTC()) {
				continue
			}
			return utc, candidate.Format("2006-01-02T15:04:05-07:00"), true, nil
		}
		return time.Time{}, "", false, nil
	default:
		return time.Time{}, "", false, errors.New("unsupported trigger")
	}
}

func nextTrigger(t Trigger, tz string, created, after time.Time) (time.Time, string, bool, error) {
	switch t.Kind {
	case "interval":
		anchor := created.UTC()
		if t.StartAtUTC != nil {
			anchor = time.UnixMilli(*t.StartAtUTC).UTC()
		}
		step := time.Duration(t.EverySeconds) * time.Second
		if !anchor.After(after) {
			delta := after.Sub(anchor)
			n := delta/step + 1
			anchor = anchor.Add(n * step)
		}
		return anchor, anchor.Format(time.RFC3339), true, nil
	case "daily":
		loc, err := time.LoadLocation(tz)
		if err != nil {
			return time.Time{}, "", false, err
		}
		hm, _ := time.Parse("15:04", t.LocalTime)
		cursor := after.In(loc)
		allowed := func(w time.Weekday) bool {
			if len(t.Weekdays) == 0 {
				return true
			}
			iso := int(w)
			if iso == 0 {
				iso = 7
			}
			for _, d := range t.Weekdays {
				if d == iso {
					return true
				}
			}
			return false
		}
		for days := 0; days < 370; days++ {
			date := cursor.AddDate(0, 0, days)
			candidate := time.Date(date.Year(), date.Month(), date.Day(), hm.Hour(), hm.Minute(), 0, 0, loc)
			if !candidate.After(cursor) || !allowed(candidate.Weekday()) {
				continue
			}
			if candidate.UTC().Before(created.UTC()) {
				continue
			}
			return candidate.UTC(), candidate.Format("2006-01-02T15:04:05-07:00"), true, nil
		}
		return time.Time{}, "", false, errors.New("unable to calculate daily occurrence")
	default:
		return time.Time{}, "", false, errors.New("unsupported trigger")
	}
}

func (s *Service) materialize(ctx context.Context, r Routine, d dueOccurrence) (Occurrence, error) {
	key := "v" + strconv.FormatInt(r.DefinitionVersion, 10) + ":" + strconv.FormatInt(d.TriggerUTC.UnixMilli(), 10)
	oid, _ := s.ids.New("occ")
	local := d.IntendedLocal
	now := s.clock.UnixMilli()
	o := Occurrence{ID: oid, RoutineID: r.ID, DefinitionVersion: r.DefinitionVersion, OccurrenceKey: key, State: d.State, IntendedLocalTime: &local, TriggerTimeUTC: d.TriggerUTC.UnixMilli(), CreatedAt: now, UpdatedAt: now}
	err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO routine_occurrences(id,routine_id,definition_version,occurrence_key,state,intended_local_time,trigger_time_utc,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, o.ID, o.RoutineID, o.DefinitionVersion, o.OccurrenceKey, o.State, local, o.TriggerTimeUTC, now, now); err != nil {
			return err
		}
		if o.State == OccurrenceMissed {
			eid, _ := s.ids.New("evt")
			payload, _ := json.Marshal(map[string]any{"routine_id": r.ID, "occurrence_id": o.ID, "occurrence_key": key, "state": o.State})
			return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: &r.WorkspaceID, Type: "routine.occurrence_missed", AggregateType: "routine_occurrence", AggregateID: o.ID, Payload: payload, OccurredAt: now})
		}
		var pol Policy
		_ = json.Unmarshal(r.PolicyJSON, &pol)
		pol, _ = normalizePolicy(pol)
		projectID := pol.ProjectID
		if projectID == nil {
			var err error
			projectID, err = resolveSingleProjectBinding(ctx, tx, r.ID)
			if err != nil {
				return err
			}
		}
		objective := strings.TrimSpace(pol.Objective)
		if objective == "" {
			objective = "Routine: " + r.Name
		}
		completion := pol.Completion
		if len(completion) == 0 {
			completion = json.RawMessage(`{}`)
		}
		t, err := s.tasks.CreateInTransaction(ctx, tx, task.CreateCommand{WorkspaceID: r.WorkspaceID, ProjectID: projectID, ProjectWorkspaceID: pol.ProjectWorkspaceID, Objective: objective, SchedulingClass: task.ClassBackgroundRoutine, Priority: pol.Priority, Completion: completion})
		if err != nil {
			return err
		}
		o.TaskID = &t.ID
		o.State = OccurrenceInstantiated
		res, err := tx.ExecContext(ctx, `UPDATE routine_occurrences SET task_id=?,state='instantiated',updated_at=? WHERE id=? AND state='expected'`, t.ID, now, o.ID)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("routine occurrence state conflict")
		}
		eid, _ := s.ids.New("evt")
		payload, _ := json.Marshal(map[string]any{"routine_id": r.ID, "occurrence_id": o.ID, "occurrence_key": key, "task_id": t.ID, "trigger_time_utc": o.TriggerTimeUTC})
		return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: &r.WorkspaceID, Type: "routine.occurrence_instantiated", AggregateType: "routine_occurrence", AggregateID: o.ID, Payload: payload, OccurredAt: now})
	})
	return o, err
}

func resolveSingleProjectBinding(ctx context.Context, tx storage.Tx, routineID string) (*string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT project_id FROM project_routine_bindings WHERE routine_id=? AND status='active' ORDER BY id LIMIT 2`, routineID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var xs []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		xs = append(xs, v)
	}
	if len(xs) > 1 {
		return nil, errors.New("routine has multiple active project bindings; v0.1 requires at most one")
	}
	if len(xs) == 1 {
		return &xs[0], nil
	}
	return nil, nil
}
func isOccurrenceConflict(err error) bool {
	return err != nil && (strings.Contains(strings.ToLower(err.Error()), "unique") || strings.Contains(strings.ToLower(err.Error()), "constraint"))
}

// SyncOccurrenceState projects Task state into RoutineOccurrence state without
// letting the Routine layer override Task truth.
func (s *Service) SyncOccurrenceState(ctx context.Context, occurrenceID string) (Occurrence, error) {
	o, err := s.Occurrence(ctx, occurrenceID)
	if err != nil {
		return o, err
	}
	if o.TaskID == nil {
		return o, nil
	}
	var st string
	if err := s.db.QueryRowContext(ctx, `SELECT state FROM tasks WHERE id=?`, *o.TaskID).Scan(&st); err != nil {
		return o, err
	}
	next := o.State
	switch task.State(st) {
	case task.StateRunning:
		next = OccurrenceRunning
	case task.StateCompletionRequested, task.StateVerifying:
		next = OccurrenceVerifying
	case task.StateComplete:
		next = OccurrenceSucceeded
	case task.StateFailed:
		next = OccurrenceFailed
	case task.StateCancelled:
		next = OccurrenceCancelled
	case task.StateBlocked:
		next = OccurrenceRetrying
	}
	if next != o.State {
		now := s.clock.UnixMilli()
		_, err = s.db.ExecContext(ctx, `UPDATE routine_occurrences SET state=?,updated_at=? WHERE id=?`, next, now, o.ID)
		if err != nil {
			return o, err
		}
	}
	return s.Occurrence(ctx, o.ID)
}
func (s *Service) Occurrence(ctx context.Context, idv string) (Occurrence, error) {
	var o Occurrence
	var taskID, local sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id,routine_id,definition_version,occurrence_key,task_id,state,intended_local_time,trigger_time_utc,created_at,updated_at FROM routine_occurrences WHERE id=?`, idv).Scan(&o.ID, &o.RoutineID, &o.DefinitionVersion, &o.OccurrenceKey, &taskID, &o.State, &local, &o.TriggerTimeUTC, &o.CreatedAt, &o.UpdatedAt)
	if taskID.Valid {
		o.TaskID = &taskID.String
	}
	if local.Valid {
		o.IntendedLocalTime = &local.String
	}
	return o, err
}
