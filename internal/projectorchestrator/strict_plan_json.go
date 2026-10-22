package projectorchestrator

import (
    "bytes"
    "encoding/json"
    "fmt"
    "io"
)

// rejectAmbiguousPlannerJSON rejects duplicate object members anywhere in
// model-authored directives. encoding/json otherwise silently uses the last
// value, which could change action, Workspace choice or executable DAG steps.
// This is a parser safeguard only; canonical scope and standing-grant checks
// remain mandatory at Task admission and the final side-effect boundary.
func rejectAmbiguousPlannerJSON(raw []byte) error {
    if len(raw) == 0 || len(raw) > 64*1024 {
        return fmt.Errorf("planner JSON exceeds the bounded directive size")
    }
    decoder := json.NewDecoder(bytes.NewReader(raw))
    tokens := 0
    var scan func(int) error
    scan = func(depth int) error {
        tokens++
        if depth > 32 || tokens > 4096 {
            return fmt.Errorf("planner JSON nesting or tokens exceed limit")
        }
        token, err := decoder.Token()
        if err != nil {
            return err
        }
        delim, ok := token.(json.Delim)
        if !ok {
            return nil
        }
        switch delim {
        case '{':
            seen := make(map[string]struct{})
            for decoder.More() {
                member, err := decoder.Token()
                if err != nil {
                    return err
                }
                key, ok := member.(string)
                if !ok {
                    return fmt.Errorf("planner JSON object member is not a string")
                }
                if _, duplicate := seen[key]; duplicate {
                    return fmt.Errorf("duplicate planner JSON member %q", key)
                }
                seen[key] = struct{}{}
                if err := scan(depth + 1); err != nil {
                    return err
                }
            }
            end, err := decoder.Token()
            if err != nil {
                return err
            }
            if end != json.Delim('}') {
                return fmt.Errorf("invalid planner JSON object terminator")
            }
        case '[':
            for decoder.More() {
                if err := scan(depth + 1); err != nil {
                    return err
                }
            }
            end, err := decoder.Token()
            if err != nil {
                return err
            }
            if end != json.Delim(']') {
                return fmt.Errorf("invalid planner JSON array terminator")
            }
        default:
            return fmt.Errorf("unexpected closing planner JSON delimiter")
        }
        return nil
    }
    if err := scan(0); err != nil {
        return err
    }
    if _, err := decoder.Token(); err != io.EOF {
        if err != nil {
            return err
        }
        return fmt.Errorf("multiple planner JSON values")
    }
    return nil
}
