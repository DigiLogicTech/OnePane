package assurance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func checkJSON(raw json.RawMessage, checks []JSONCheck) error {
	if len(checks) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var root any
	if err := dec.Decode(&root); err != nil {
		return err
	}
	for i, c := range checks {
		value, found, err := jsonPointer(root, c.Pointer)
		if err != nil {
			return fmt.Errorf("check %d: %w", i, err)
		}
		switch strings.ToLower(strings.TrimSpace(c.Operator)) {
		case "exists":
			if !found {
				return fmt.Errorf("check %d: pointer %q does not exist", i, c.Pointer)
			}
		case "not_empty":
			if !found || emptyJSONValue(value) {
				return fmt.Errorf("check %d: pointer %q is empty", i, c.Pointer)
			}
		case "equals":
			if !found || len(c.Value) == 0 || !json.Valid(c.Value) {
				return fmt.Errorf("check %d: equality value invalid", i)
			}
			var want any
			d := json.NewDecoder(bytes.NewReader(c.Value))
			d.UseNumber()
			if err := d.Decode(&want); err != nil {
				return err
			}
			a, _ := json.Marshal(value)
			b, _ := json.Marshal(want)
			if !bytes.Equal(a, b) {
				return fmt.Errorf("check %d: pointer %q differs", i, c.Pointer)
			}
		case "type":
			if !found || !jsonType(value, c.Type) {
				return fmt.Errorf("check %d: pointer %q is not %s", i, c.Pointer, c.Type)
			}
		default:
			return fmt.Errorf("check %d: unsupported operator %q", i, c.Operator)
		}
	}
	return nil
}

func jsonPointer(root any, pointer string) (any, bool, error) {
	if pointer == "" {
		return root, true, nil
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, false, fmt.Errorf("JSON pointer must start with /")
	}
	cur := root
	for _, raw := range strings.Split(pointer[1:], "/") {
		token := strings.ReplaceAll(strings.ReplaceAll(raw, "~1", "/"), "~0", "~")
		switch x := cur.(type) {
		case map[string]any:
			v, ok := x[token]
			if !ok {
				return nil, false, nil
			}
			cur = v
		case []any:
			n, err := strconv.Atoi(token)
			if err != nil || n < 0 || n >= len(x) {
				return nil, false, nil
			}
			cur = x[n]
		default:
			return nil, false, nil
		}
	}
	return cur, true, nil
}

func emptyJSONValue(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(x) == ""
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	default:
		return false
	}
}
func jsonType(v any, want string) bool {
	switch strings.ToLower(strings.TrimSpace(want)) {
	case "object":
		_, ok := v.(map[string]any)
		return ok
	case "array":
		_, ok := v.([]any)
		return ok
	case "string":
		_, ok := v.(string)
		return ok
	case "number":
		_, ok := v.(json.Number)
		return ok
	case "boolean", "bool":
		_, ok := v.(bool)
		return ok
	case "null":
		return v == nil
	default:
		return false
	}
}
