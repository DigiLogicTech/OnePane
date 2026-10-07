package localai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// LLMFitAdvisory is intentionally advisory. OnePane never treats these values as
// empirical qualification. Provenance is kept so the UI can distinguish model
// catalogue claims, llmfit estimates/benchmarks, and OnePane measurements.
type LLMFitAdvisory struct {
	Source                  string          `json:"source"`
	ModelRef                string          `json:"model_ref,omitempty"`
	FitLevel                string          `json:"fit_level,omitempty"`
	RunMode                 string          `json:"run_mode,omitempty"`
	BestQuant               string          `json:"best_quant,omitempty"`
	Runtime                 string          `json:"runtime,omitempty"`
	ParamsB                 *float64        `json:"params_b,omitempty"`
	NativeContext           *int64          `json:"native_context,omitempty"`
	UsableContext           *int64          `json:"usable_context,omitempty"`
	EffectiveContext        *int64          `json:"effective_context,omitempty"`
	MemoryRequiredGB        *float64        `json:"memory_required_gb,omitempty"`
	MemoryAvailableGB       *float64        `json:"memory_available_gb,omitempty"`
	DiskSizeGB              *float64        `json:"disk_size_gb,omitempty"`
	EstimatedTPS            *float64        `json:"estimated_tps,omitempty"`
	MeasuredTPS             *float64        `json:"measured_tps,omitempty"`
	PrefillTPS              *float64        `json:"prefill_tps,omitempty"`
	TTFTMS                  *float64        `json:"ttft_ms,omitempty"`
	EstimateConfidence      string          `json:"estimate_confidence,omitempty"`
	EstimateConfidenceLabel string          `json:"estimate_confidence_label,omitempty"`
	Capabilities            []string        `json:"capabilities,omitempty"`
	SupportsTP              []int           `json:"supports_tp,omitempty"`
	TensorParallelSupported *bool           `json:"tensor_parallel_supported,omitempty"`
	Installed               *bool           `json:"installed,omitempty"`
	License                 string          `json:"license,omitempty"`
	VerifyCommand           string          `json:"verify_command,omitempty"`
	EstimateBasis           json.RawMessage `json:"estimate_basis,omitempty"`
	Raw                     json.RawMessage `json:"raw,omitempty"`
}

type LLMFitClient struct {
	base   *url.URL
	client *http.Client
}

func NewLLMFitClient(raw string) (*LLMFitClient, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("llmfit URL must be a clean http(s) base URL")
	}
	if u.Scheme == "http" {
		host := u.Hostname()
		if host != "127.0.0.1" && host != "localhost" && host != "::1" {
			return nil, errors.New("plaintext llmfit is permitted only on loopback")
		}
	}
	return &LLMFitClient{base: u, client: &http.Client{Timeout: 4 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (c *LLMFitClient) ModelAdvisories(ctx context.Context, req RecommendRequest) (map[string]LLMFitAdvisory, error) {
	if c == nil || c.base == nil {
		return nil, nil
	}
	u := *c.base
	u.Path = strings.TrimRight(u.Path, "/") + "/api/v1/models"
	q := u.Query()
	q.Set("limit", strconv.Itoa(max(50, req.Limit)))
	q.Set("use_case", string(req.UseCase))
	if req.MinimumFit != "" {
		q.Set("min_fit", string(req.MinimumFit))
	}
	q.Set("sort", "score")
	if req.ContextTokens > 0 {
		q.Set("max_context", strconv.FormatInt(req.ContextTokens, 10))
	}
	u.RawQuery = q.Encode()
	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("llmfit returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	var root any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, err
	}
	rows := extractLLMFitRows(root)
	out := make(map[string]LLMFitAdvisory, len(rows))
	for _, row := range rows {
		raw, _ := json.Marshal(row)
		a := advisoryFromMap(row)
		a.Source = "llmfit"
		a.Raw = raw
		if a.ModelRef != "" {
			out[normalizeModelKey(a.ModelRef)] = a
		}
	}
	return out, nil
}

func (c *LLMFitClient) SearchModels(ctx context.Context, query string, limit int) ([]LLMFitAdvisory, error) {
	if c == nil || c.base == nil { return nil, nil }
	if limit <= 0 { limit = 30 }
	if limit > 100 { limit = 100 }
	u := *c.base
	basePath := strings.TrimRight(u.Path, "/") + "/api/v1/models"
	query = strings.TrimSpace(query)
	if query != "" { basePath += "/" + url.PathEscape(query) }
	u.Path = basePath
	q := u.Query()
	q.Set("limit", strconv.Itoa(limit))
	q.Set("runtime", "llamacpp")
	q.Set("sort", "score")
	u.RawQuery = q.Encode()
	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	resp, err := c.client.Do(httpReq)
	if err != nil { return nil, err }
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK { return nil, fmt.Errorf("llmfit returned HTTP %d", resp.StatusCode) }
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil { return nil, err }
	var root any
	if err := json.Unmarshal(body, &root); err != nil { return nil, err }
	rows := extractLLMFitRows(root)
	out := make([]LLMFitAdvisory, 0, len(rows))
	for _, row := range rows {
		raw, _ := json.Marshal(row)
		a := advisoryFromMap(row)
		a.Source = "llmfit"
		a.Raw = raw
		if a.ModelRef != "" { out = append(out, a) }
	}
	return out, nil
}

func (s *Service) DiscoverLLMFit(ctx context.Context, query string, limit int) ([]LLMFitAdvisory, error) {
	if s == nil || s.llmfit == nil { return nil, errors.New("llmfit service is not configured; start llmfit serve on the configured loopback endpoint") }
	return s.llmfit.SearchModels(ctx, query, limit)
}

func extractLLMFitRows(v any) []map[string]any {
	switch x := v.(type) {
	case []any:
		var out []map[string]any
		for _, v := range x {
			if m, ok := v.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	case map[string]any:
		for _, key := range []string{"models", "results", "fits", "data"} {
			if rows := extractLLMFitRows(x[key]); len(rows) > 0 {
				return rows
			}
		}
	}
	return nil
}

func advisoryFromMap(m map[string]any) LLMFitAdvisory {
	a := LLMFitAdvisory{}
	a.ModelRef = firstString(m, "model_ref", "model", "id", "name")
	if mm, ok := m["model"].(map[string]any); ok {
		if s := firstString(mm, "model_ref", "id", "name"); s != "" {
			a.ModelRef = s
		}
	}
	a.FitLevel = firstString(m, "fit_level", "fit")
	a.RunMode = firstString(m, "run_mode")
	a.BestQuant = firstString(m, "best_quant", "quantization")
	a.Runtime = firstString(m, "runtime")
	a.EstimateConfidence = firstString(m, "estimate_confidence")
	a.EstimateConfidenceLabel = firstString(m, "estimate_confidence_label")
	a.ParamsB = firstFloatPtr(m, "params_b")
	a.NativeContext = firstInt64Ptr(m, "context_length")
	a.UsableContext = firstInt64Ptr(m, "usable_context")
	a.EffectiveContext = firstInt64Ptr(m, "effective_context_length")
	a.MemoryRequiredGB = firstFloatPtr(m, "memory_required_gb")
	a.MemoryAvailableGB = firstFloatPtr(m, "memory_available_gb")
	a.DiskSizeGB = firstFloatPtr(m, "disk_size_gb")
	a.EstimatedTPS = firstFloatPtr(m, "estimated_tps")
	a.PrefillTPS = firstFloatPtr(m, "prefill_tps")
	a.TTFTMS = firstFloatPtr(m, "ttft_ms")
	a.Capabilities = firstStringSlice(m, "capability_ids", "capabilities")
	a.SupportsTP = firstIntSlice(m, "supports_tp")
	if len(a.SupportsTP) > 0 {
		v := false
		for _, n := range a.SupportsTP {
			if n > 1 {
				v = true
				break
			}
		}
		a.TensorParallelSupported = &v
	}
	if v, ok := m["installed"].(bool); ok {
		a.Installed = &v
	}
	a.License = firstString(m, "license")
	a.VerifyCommand = firstString(m, "verify_command")
	if basis, ok := m["estimate_basis"]; ok {
		if raw, err := json.Marshal(basis); err == nil {
			a.EstimateBasis = raw
		}
	}
	if mt, ok := m["measured_tps"].(map[string]any); ok {
		a.MeasuredTPS = firstFloatPtr(mt, "tokens_per_second", "tps", "value")
	} else {
		a.MeasuredTPS = firstFloatPtr(m, "measured_tps")
	}
	if b, ok := m["tensor_parallel_supported"].(bool); ok {
		a.TensorParallelSupported = &b
	}
	return a
}

func normalizeModelKey(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
func firstStringSlice(m map[string]any, keys ...string) []string {
	for _, k := range keys {
		v, ok := m[k].([]any)
		if !ok {
			continue
		}
		out := make([]string, 0, len(v))
		for _, raw := range v {
			if s, ok := raw.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	}
	return nil
}
func firstIntSlice(m map[string]any, keys ...string) []int {
	for _, k := range keys {
		v, ok := m[k].([]any)
		if !ok {
			continue
		}
		out := make([]int, 0, len(v))
		for _, raw := range v {
			switch n := raw.(type) {
			case float64:
				out = append(out, int(n))
			case json.Number:
				if x, err := n.Int64(); err == nil {
					out = append(out, int(x))
				}
			}
		}
		return out
	}
	return nil
}

func firstFloatPtr(m map[string]any, keys ...string) *float64 {
	for _, k := range keys {
		switch v := m[k].(type) {
		case float64:
			x := v
			return &x
		case json.Number:
			if x, e := v.Float64(); e == nil {
				return &x
			}
		}
	}
	return nil
}
func firstInt64Ptr(m map[string]any, keys ...string) *int64 {
	for _, k := range keys {
		switch v := m[k].(type) {
		case float64:
			x := int64(v)
			return &x
		case json.Number:
			if x, e := v.Int64(); e == nil {
				return &x
			}
		}
	}
	return nil
}
