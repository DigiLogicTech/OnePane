package localai

import (
    "context"
    "database/sql"
    "encoding/json"
    "fmt"
    "io/fs"
    "os"
    "path/filepath"
    "strings"
)

// ColibriPoolModel is an eligible local directory, not a downloadable GGUF.
// The browser never receives arbitrary filesystem listings outside the model pool.
type ColibriPoolModel struct {
    ModelPath string `json:"model_path"`
    ModelRef string `json:"model_ref"`
    DisplayName string `json:"display_name"`
    ReportedContextTokens int64 `json:"reported_context_tokens"`
    InitialContextTokens int64 `json:"initial_context_tokens"`
    Registered bool `json:"registered"`
}

const colibriInitialContextCap int64 = 131072

// Colibri's initial requested context is a declared ceiling, NOT a tested limit.
// Invalid or giant tokenizer sentinels must never become runtime allocations.
func colibriDeclaredContext(data []byte) int64 {
    var raw map[string]any
    if json.Unmarshal(data, &raw) != nil { return 0 }
    read := func(node map[string]any) int64 {
        for _, key := range []string{"max_position_embeddings", "n_positions", "max_seq_len", "seq_length", "model_max_length"} {
            if val, ok := node[key].(float64); ok && val >= 512 && val <= 1048576 && val == float64(int64(val)) {
                return int64(val)
            }
        }
        return 0
    }
    if text, ok := raw["text_config"].(map[string]any); ok {
        if n := read(text); n > 0 { return n }
    }
    return read(raw)
}

func initialColibriContext(declared int64) int64 {
    if declared <= 0 { return 8192 }
    if declared > colibriInitialContextCap { return colibriInitialContextCap }
    return declared
}

// ColibriPoolModels performs a bounded, read-only discovery in the configured
// pool; WalkDir does not follow symlinked directories.
func (s *Service) ColibriPoolModels(ctx context.Context) ([]ColibriPoolModel, error) {
    root := filepath.Clean(s.modelRoot)
    if !filepath.IsAbs(root) { return nil, fmt.Errorf("configured model pool is not absolute") }
    registered := map[string]bool{}
    rows, err := s.db.QueryContext(ctx, "SELECT runtime_config_json FROM model_deployments WHERE LOWER(COALESCE(runtime_name,''))='colibri'")
    if err != nil { return nil, err }
    for rows.Next() {
        var config string
        if err := rows.Scan(&config); err != nil { _ = rows.Close(); return nil, err }
        var value struct{ ModelPath string `json:"model_path"` }
        if json.Unmarshal([]byte(config), &value) == nil && value.ModelPath != "" {
            registered[strings.ToLower(filepath.Clean(value.ModelPath))] = true
        }
    }
    if err := rows.Err(); err != nil { _ = rows.Close(); return nil, err }
    _ = rows.Close()

    results := make([]ColibriPoolModel, 0)
    seen := 0
    err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
        if ctx.Err() != nil { return ctx.Err() }
        if walkErr != nil { return nil } // An inaccessible subfolder is not a pool-wide error.
        if !d.IsDir() { return nil }
        seen++
        if seen > 4000 { return fs.SkipAll }
        rel, err := filepath.Rel(root, path)
        if err != nil { return fs.SkipDir }
        if rel != "." && len(strings.Split(rel, string(os.PathSeparator))) > 5 { return fs.SkipDir }
        name := strings.ToLower(d.Name())
        if rel != "." && (name == ".git" || name == "__pycache__" || name == "node_modules" || name == ".cache") { return fs.SkipDir }
        if rel == "." { return nil } // The pool itself is never a selectable model.
        conf := filepath.Join(path, "config.json")
        info, err := os.Lstat(conf)
        if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 { return nil }
        b, err := os.ReadFile(conf)
        if err != nil { return nil }
        var obj map[string]any
        if json.Unmarshal(b, &obj) != nil { return nil }
        declared := colibriDeclaredContext(b)
        results = append(results, ColibriPoolModel{
            ModelPath:path, ModelRef:d.Name(), DisplayName:d.Name(),
            ReportedContextTokens:declared, InitialContextTokens:initialColibriContext(declared),
            Registered:registered[strings.ToLower(filepath.Clean(path))],
        })
        if len(results) >= 150 { return fs.SkipAll }
        return fs.SkipDir
    })
    if err != nil { return nil, err }
    return results, nil
}

// An omitted context means automatic model-config selection, capped to prevent
// pathological allocations. Actual capability is never promoted without checks.
func colibriAutoContext(path string) int64 {
    data, err := os.ReadFile(filepath.Join(path, "config.json"))
    if err != nil || len(data) > 1<<20 { return 8192 }
    return initialColibriContext(colibriDeclaredContext(data))
}

// Compile-time witness for the SQL state reader.
var _ = sql.ErrNoRows
