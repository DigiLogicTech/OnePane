package task

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
)

type taskScanFixture struct {
	completion any
	result     any
	metadata   any
}

func (f taskScanFixture) Scan(dest ...any) error {
	if len(dest) == 19 {
		*dest[0].(*string) = "task-1"
		*dest[1].(*string) = "ws-1"
		*dest[2].(*sql.NullString) = sql.NullString{String: "project-1", Valid: true}
		*dest[3].(*sql.NullString) = sql.NullString{String: "pws-1", Valid: true}
		*dest[4].(*sql.NullString) = sql.NullString{}
		*dest[5].(*sql.NullString) = sql.NullString{}
		*dest[6].(*sql.NullString) = sql.NullString{}
		*dest[7].(*string) = "hello"
		*dest[8].(*State) = StateCreated
		*dest[9].(*SchedulingClass) = ClassUserInteractive
		*dest[10].(*int) = 1
		switch v := f.completion.(type) {
		case string:
			*dest[11].(*sql.NullString) = sql.NullString{String: v, Valid: true}
		case nil:
		default:
			return fmt.Errorf("unexpected completion fixture %T", v)
		}
		switch v := f.result.(type) {
		case string:
			*dest[12].(*sql.NullString) = sql.NullString{String: v, Valid: true}
		case nil:
		default:
			return fmt.Errorf("unexpected result fixture %T", v)
		}
		*dest[13].(*int64) = 1
		*dest[14].(*sql.NullInt64) = sql.NullInt64{}
		*dest[15].(*sql.NullInt64) = sql.NullInt64{}
		*dest[16].(*sql.NullInt64) = sql.NullInt64{}
		*dest[17].(*int64) = 1
		*dest[18].(*int64) = 1
		return nil
	}
	if len(dest) == 9 {
		*dest[0].(*string) = "attempt-1"
		*dest[1].(*string) = "task-1"
		*dest[2].(*int64) = 1
		*dest[3].(*sql.NullString) = sql.NullString{}
		*dest[4].(*AttemptState) = AttemptCreated
		*dest[5].(*sql.NullString) = sql.NullString{}
		*dest[6].(*sql.NullInt64) = sql.NullInt64{}
		*dest[7].(*sql.NullInt64) = sql.NullInt64{}
		switch v := f.metadata.(type) {
		case string:
			*dest[8].(*sql.NullString) = sql.NullString{String: v, Valid: true}
		case nil:
		default:
			return fmt.Errorf("unexpected metadata fixture %T", v)
		}
		return nil
	}
	return fmt.Errorf("unexpected destination count %d", len(dest))
}

func TestScanTaskAcceptsSQLiteTextJSON(t *testing.T) {
	got, err := scanTask(taskScanFixture{completion: `{"state":"observed"}`, result: `{"ok":true}`})
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(got.Completion) || string(got.Completion) != `{"state":"observed"}` {
		t.Fatalf("completion=%q", got.Completion)
	}
	if !json.Valid(got.Result) || string(got.Result) != `{"ok":true}` {
		t.Fatalf("result=%q", got.Result)
	}
	if got.ProjectID == nil || *got.ProjectID != "project-1" || got.ProjectWorkspaceID == nil || *got.ProjectWorkspaceID != "pws-1" {
		t.Fatalf("scope project=%v workspace=%v", got.ProjectID, got.ProjectWorkspaceID)
	}
}

func TestScanTaskRejectsMalformedPersistedJSON(t *testing.T) {
	if _, err := scanTask(taskScanFixture{completion: `{broken`}); err == nil {
		t.Fatal("expected malformed completion_json to fail")
	}
}

func TestScanAttemptAcceptsSQLiteTextJSON(t *testing.T) {
	got, err := scanAttempt(taskScanFixture{metadata: `{"worker":"local"}`})
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(got.Metadata) || string(got.Metadata) != `{"worker":"local"}` {
		t.Fatalf("metadata=%q", got.Metadata)
	}
}
