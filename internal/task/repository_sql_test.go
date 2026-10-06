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
	if len(dest) == 17 {
		*dest[0].(*string) = "task-1"
		*dest[1].(*string) = "ws-1"
		*dest[2].(*sql.NullString) = sql.NullString{}
		*dest[3].(*sql.NullString) = sql.NullString{}
		*dest[4].(*sql.NullString) = sql.NullString{}
		*dest[5].(*sql.NullString) = sql.NullString{}
		*dest[6].(*string) = "hello"
		*dest[7].(*State) = StateCreated
		*dest[8].(*SchedulingClass) = ClassUserInteractive
		*dest[9].(*int) = 1
		switch v := f.completion.(type) {
		case string:
			*dest[10].(*sql.NullString) = sql.NullString{String: v, Valid: true}
		case nil:
		default:
			return fmt.Errorf("unexpected completion fixture %T", v)
		}
		switch v := f.result.(type) {
		case string:
			*dest[11].(*sql.NullString) = sql.NullString{String: v, Valid: true}
		case nil:
		default:
			return fmt.Errorf("unexpected result fixture %T", v)
		}
		*dest[12].(*int64) = 1
		*dest[13].(*sql.NullInt64) = sql.NullInt64{}
		*dest[14].(*sql.NullInt64) = sql.NullInt64{}
		*dest[15].(*int64) = 1
		*dest[16].(*int64) = 1
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
