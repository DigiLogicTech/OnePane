package localai

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/id"
	"github.com/DigiLogicTech/OnePane/internal/inference"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type QualificationStatus string

const (
	QualificationCreated   QualificationStatus = "created"
	QualificationRunning   QualificationStatus = "running"
	QualificationPassed    QualificationStatus = "passed"
	QualificationLimited   QualificationStatus = "limited"
	QualificationFailed    QualificationStatus = "failed"
	QualificationCancelled QualificationStatus = "cancelled"
)

type QualificationRun struct {
	ID, DeploymentID, HardwareProfileID, RuntimeInstanceID string
	Status                                                 QualificationStatus
	RequestedContext                                       int64
	VerifiedContext                                        *int64
	ProtocolLevel                                          *string
	PromptTokensTested, CompletionTokens                   int64
	TTFTMS, TokensPerSecond                                *float64
	PeakMemoryBytes                                        *int64
	MetricsJSON, EvidenceJSON                              json.RawMessage
	FailureReason                                          *string
	StartedAt, CompletedAt                                 *int64
	Revision, CreatedAt, UpdatedAt                         int64
}

type ProbeResult struct {
	StatusCode       int
	Body             json.RawMessage
	Duration         time.Duration
	PromptTokens     int64
	CompletionTokens int64
}

type ModelProbe interface {
	Chat(context.Context, int, map[string]any) (ProbeResult, error)
}

type HTTPModelProbe struct{ Client *http.Client }

func NewHTTPModelProbe() *HTTPModelProbe {
	return &HTTPModelProbe{Client: &http.Client{Timeout: 90 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (p *HTTPModelProbe) Chat(ctx context.Context, port int, body map[string]any) (ProbeResult, error) {
	if port < 1024 || port > 65535 {
		return ProbeResult{}, errors.New("invalid loopback port")
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return ProbeResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/v1/chat/completions", port), bytes.NewReader(raw))
	if err != nil {
		return ProbeResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	start := time.Now()
	resp, err := p.Client.Do(req)
	dur := time.Since(start)
	if err != nil {
		return ProbeResult{}, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return ProbeResult{}, err
	}
	r := ProbeResult{StatusCode: resp.StatusCode, Body: b, Duration: dur}
	var decoded struct {
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
		} `json:"usage"`
	}
	_ = json.Unmarshal(b, &decoded)
	r.PromptTokens = decoded.Usage.PromptTokens
	r.CompletionTokens = decoded.Usage.CompletionTokens
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return r, fmt.Errorf("chat probe HTTP %d", resp.StatusCode)
	}
	return r, nil
}

type Qualifier struct {
	db         *sql.DB
	tx         storage.Transactor
	events     event.Store
	ids        id.Generator
	clock      interface{ UnixMilli() int64 }
	supervisor *RuntimeSupervisor
	inference  *inference.Service
	probe      ModelProbe
}

func NewQualifier(db *sql.DB, tx storage.Transactor, clk interface{ UnixMilli() int64 }, supervisor *RuntimeSupervisor, inf *inference.Service, probe ModelProbe) *Qualifier {
	if probe == nil {
		probe = NewHTTPModelProbe()
	}
	return &Qualifier{db: db, tx: tx, events: event.Store{}, ids: id.Generator{}, clock: clk, supervisor: supervisor, inference: inf, probe: probe}
}

type QualificationRequest struct {
	DeploymentID, HardwareProfileID, RoleName, CapabilityID string
	RequestedContext                                        int64
	ActorPrincipalID                                        *string
}

type qualificationEvidence struct {
	PlainOK       bool     `json:"plain_ok"`
	JSONOK        bool     `json:"json_ok"`
	SchemaOK      bool     `json:"schema_ok"`
	ToolsOK       bool     `json:"tools_ok"`
	ContextProbes []int64  `json:"context_probes"`
	PromptTokens  []int64  `json:"prompt_tokens"`
	Errors        []string `json:"errors,omitempty"`
}

func (q *Qualifier) Qualify(ctx context.Context, req QualificationRequest) (QualificationRun, error) {
	if q == nil || q.supervisor == nil || q.inference == nil || strings.TrimSpace(req.DeploymentID) == "" || strings.TrimSpace(req.HardwareProfileID) == "" || strings.TrimSpace(req.CapabilityID) == "" || req.RequestedContext <= 0 {
		return QualificationRun{}, errors.New("invalid qualification request")
	}
	dep, err := q.inference.Deployment(ctx, req.DeploymentID)
	if err != nil {
		return QualificationRun{}, err
	}
	if dep.Status != inference.DeploymentQualifying && dep.Status != inference.DeploymentDegraded {
		return QualificationRun{}, fmt.Errorf("deployment must be qualifying or degraded")
	}
	inst, err := q.supervisor.Start(ctx, req.DeploymentID)
	if err != nil {
		return QualificationRun{}, err
	}
	runID, _ := q.ids.New("qual")
	now := q.clock.UnixMilli()
	run := QualificationRun{ID: runID, DeploymentID: req.DeploymentID, HardwareProfileID: req.HardwareProfileID, RuntimeInstanceID: inst.ID, Status: QualificationRunning, RequestedContext: req.RequestedContext, MetricsJSON: json.RawMessage(`{}`), EvidenceJSON: json.RawMessage(`{}`), Revision: 1, CreatedAt: now, UpdatedAt: now}
	run.StartedAt = &now
	if _, err := q.db.ExecContext(ctx, `INSERT INTO local_model_qualification_runs(id,deployment_id,hardware_profile_id,runtime_instance_id,status,requested_context,metrics_json,evidence_json,started_at,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, run.ID, run.DeploymentID, run.HardwareProfileID, run.RuntimeInstanceID, run.Status, run.RequestedContext, string(run.MetricsJSON), string(run.EvidenceJSON), now, 1, now, now); err != nil {
		return QualificationRun{}, err
	}
	ev := qualificationEvidence{}
	model, err := q.inference.Model(ctx, dep.ModelID)
	if err != nil {
		return QualificationRun{}, q.fail(ctx, run, err)
	}
	base := map[string]any{"model": model.ModelRef, "stream": false, "temperature": 0, "max_tokens": 32}
	plain := cloneMap(base)
	plain["messages"] = []map[string]string{{"role": "user", "content": "Reply with exactly OK."}}
	pr, err := q.probe.Chat(ctx, inst.Port, plain)
	if err == nil && hasAssistantContent(pr.Body) {
		ev.PlainOK = true
	} else {
		ev.Errors = append(ev.Errors, "L0 plain completion failed: "+errText(err))
	}
	var completion int64
	if ev.PlainOK {
		completion += pr.CompletionTokens
	}
	js := cloneMap(base)
	js["messages"] = []map[string]string{{"role": "user", "content": "Return only this JSON object: {\"ok\":true}"}}
	js["response_format"] = map[string]any{"type": "json_object"}
	jr, jerr := q.probe.Chat(ctx, inst.Port, js)
	if jerr == nil && assistantJSONHasOK(jr.Body) {
		ev.JSONOK = true
		completion += jr.CompletionTokens
	} else {
		ev.Errors = append(ev.Errors, "L1 JSON failed: "+errText(jerr))
	}
	schema := cloneMap(base)
	schema["messages"] = []map[string]string{{"role": "user", "content": "Return an object with ok=true."}}
	schema["response_format"] = map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "qualification", "strict": true, "schema": map[string]any{"type": "object", "properties": map[string]any{"ok": map[string]any{"type": "boolean"}}, "required": []string{"ok"}, "additionalProperties": false}}}
	sr, serr := q.probe.Chat(ctx, inst.Port, schema)
	if serr == nil && assistantJSONHasOK(sr.Body) {
		ev.SchemaOK = true
		completion += sr.CompletionTokens
	} else {
		ev.Errors = append(ev.Errors, "L2 schema failed: "+errText(serr))
	}
	tools := cloneMap(base)
	tools["messages"] = []map[string]string{{"role": "user", "content": "Call the qualification_ping tool with value ping."}}
	tools["tools"] = []map[string]any{{"type": "function", "function": map[string]any{"name": "qualification_ping", "description": "qualification probe", "parameters": map[string]any{"type": "object", "properties": map[string]any{"value": map[string]any{"type": "string"}}, "required": []string{"value"}}}}}
	tools["tool_choice"] = "required"
	tr, terr := q.probe.Chat(ctx, inst.Port, tools)
	if terr == nil && hasToolCall(tr.Body, "qualification_ping") {
		ev.ToolsOK = true
		completion += tr.CompletionTokens
	} else {
		ev.Errors = append(ev.Errors, "L3 tool call failed: "+errText(terr))
	}
	level := "L0"
	if ev.JSONOK {
		level = "L1"
	}
	if ev.SchemaOK {
		level = "L2"
	}
	if ev.ToolsOK {
		level = "L3"
	}
	verified := int64(0)
	promptMax := int64(0)
	if ev.PlainOK {
		for _, target := range contextSteps(req.RequestedContext) {
			body := cloneMap(base)
			body["max_tokens"] = 1
			body["messages"] = []map[string]string{{"role": "user", "content": contextProbeText(target)}}
			cr, cerr := q.probe.Chat(ctx, inst.Port, body)
			if cerr != nil {
				ev.Errors = append(ev.Errors, fmt.Sprintf("context probe %d failed: %s", target, errText(cerr)))
				break
			}
			if cr.PromptTokens <= 0 {
				ev.Errors = append(ev.Errors, "context probe returned no usage.prompt_tokens")
				break
			}
			ev.ContextProbes = append(ev.ContextProbes, target)
			ev.PromptTokens = append(ev.PromptTokens, cr.PromptTokens)
			if cr.PromptTokens > verified {
				verified = cr.PromptTokens
			}
			if cr.PromptTokens > promptMax {
				promptMax = cr.PromptTokens
			}
			completion += cr.CompletionTokens
		}
	}
	if verified == 0 && ev.PlainOK && pr.PromptTokens > 0 {
		verified = pr.PromptTokens
		promptMax = pr.PromptTokens
	}
	status := QualificationFailed
	if ev.PlainOK {
		status = QualificationLimited
		if verified >= min64(req.RequestedContext*9/10, req.RequestedContext) && level != "L0" {
			status = QualificationPassed
		}
	}
	elapsed := pr.Duration.Seconds()
	var tps *float64
	if pr.CompletionTokens > 0 && elapsed > 0 {
		v := float64(pr.CompletionTokens) / elapsed
		tps = &v
	}
	// TTFT is intentionally left unset here. These probes are non-streaming,
	// so total request latency must not be mislabeled as time-to-first-token.
	var ttft *float64
	evidenceRaw, _ := json.Marshal(ev)
	metricsRaw, _ := json.Marshal(map[string]any{"protocol_level": level, "verified_context": verified, "requested_context": req.RequestedContext, "prompt_tokens_max": promptMax, "completion_tokens": completion, "baseline_latency_ms": pr.Duration.Milliseconds(), "ttft_measured": false})
	completed := q.clock.UnixMilli()
	run.Status = status
	run.CompletedAt = &completed
	run.ProtocolLevel = &level
	run.PromptTokensTested = promptMax
	run.CompletionTokens = completion
	run.TTFTMS = ttft
	run.TokensPerSecond = tps
	run.EvidenceJSON = evidenceRaw
	run.MetricsJSON = metricsRaw
	if verified > 0 {
		run.VerifiedContext = &verified
	}
	if status == QualificationFailed {
		return run, q.failWithEvidence(ctx, run, "baseline completion probe failed")
	}
	if err := q.publish(ctx, run, req, dep, model); err != nil {
		return run, q.failWithEvidence(ctx, run, err.Error())
	}
	return q.Run(ctx, run.ID)
}

func (q *Qualifier) publish(ctx context.Context, run QualificationRun, req QualificationRequest, dep inference.ModelDeployment, model inference.Model) error {
	// Successful empirical qualification may promote a user-approved managed model
	// out of quarantine. It never self-promotes into the reserved system `trusted` class.
	if model.TrustState == inference.ModelQuarantined {
		if _, err := q.inference.SetModelTrust(ctx, inference.SetModelTrustCommand{ModelID: model.ID, ExpectedTrustState: inference.ModelQuarantined, TrustState: inference.ModelUserTrusted, ActorPrincipalID: req.ActorPrincipalID, Reason: "managed model passed empirical local qualification"}); err != nil {
			return err
		}
	}
	now := q.clock.UnixMilli()
	qual := "limited"
	if run.Status == QualificationPassed {
		qual = "verified"
	}
	level := "L0"
	if run.ProtocolLevel != nil {
		level = *run.ProtocolLevel
	}
	verified := int64(0)
	if run.VerifiedContext != nil {
		verified = *run.VerifiedContext
	}
	profileID, _ := q.ids.New("compat")
	fingerprint := qualificationFingerprint(dep.ID, req.RoleName, req.CapabilityID, level, verified, run.EvidenceJSON)
	mediation := "{}"
	return q.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		var rev int64
		var current string
		if err := tx.QueryRowContext(ctx, `SELECT revision,status FROM model_deployments WHERE id=?`, dep.ID).Scan(&rev, &current); err != nil {
			return err
		}
		if rev != dep.Revision {
			return inference.ErrRevisionConflict
		}
		res, err := tx.ExecContext(ctx, `UPDATE model_deployments SET status='ready',residency_state='resident',context_max_verified=?,revision=revision+1,updated_at=? WHERE id=? AND revision=? AND status IN ('qualifying','degraded')`, nullablePositive(verified), now, dep.ID, dep.Revision)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return inference.ErrRevisionConflict
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO compatibility_profiles(id,deployment_id,role_name,capability_id,protocol_level,context_min,context_max_verified,context_max_supported,risk_class,qualification,mediation_json,evidence_json,profile_fingerprint,verified_at,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, profileID, dep.ID, nullString(req.RoleName), req.CapabilityID, level, 0, nullablePositive(verified), req.RequestedContext, "low", qual, mediation, string(run.EvidenceJSON), fingerprint, now, 1, now, now); err != nil {
			return err
		}
		res, err = tx.ExecContext(ctx, `UPDATE local_model_qualification_runs SET status=?,verified_context=?,protocol_level=?,prompt_tokens_tested=?,completion_tokens=?,ttft_ms=?,tokens_per_second=?,metrics_json=?,evidence_json=?,completed_at=?,updated_at=?,revision=revision+1 WHERE id=? AND status='running'`, run.Status, run.VerifiedContext, run.ProtocolLevel, run.PromptTokensTested, run.CompletionTokens, run.TTFTMS, run.TokensPerSecond, string(run.MetricsJSON), string(run.EvidenceJSON), now, now, run.ID)
		if err != nil {
			return err
		}
		n, err = res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("qualification run is no longer active")
		}
		if _, err := tx.ExecContext(ctx, `UPDATE managed_local_models SET status='ready',installed_at=COALESCE(installed_at,?),updated_at=?,revision=revision+1 WHERE deployment_id=? AND status='qualifying'`, now, now, dep.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE local_model_install_plans SET status='ready',updated_at=?,revision=revision+1 WHERE id=(SELECT plan_id FROM managed_local_models WHERE deployment_id=?) AND status='qualifying'`, now, dep.ID); err != nil {
			return err
		}
		var planJSON string
		if err := tx.QueryRowContext(ctx, `SELECT p.plan_json FROM managed_local_models mm JOIN local_model_install_plans p ON p.id=mm.plan_id WHERE mm.deployment_id=?`, dep.ID).Scan(&planJSON); err != nil {
			return err
		}
		var recommendation Recommendation
		_ = json.Unmarshal([]byte(planJSON), &recommendation)
		placementJSON, _ := json.Marshal(recommendation.Placement)
		llmfitJSON := json.RawMessage(`{}`)
		if recommendation.LLMFit != nil {
			if b, e := json.Marshal(recommendation.LLMFit); e == nil {
				llmfitJSON = b
			}
		}
		claims := model.StaticMetadataJSON
		if len(claims) == 0 || !json.Valid(claims) {
			claims = json.RawMessage(`{}`)
		}
		qualificationJSON, _ := json.Marshal(map[string]any{"status": run.Status, "verified_context": run.VerifiedContext, "protocol_level": run.ProtocolLevel, "tokens_per_second": run.TokensPerSecond, "ttft_ms": run.TTFTMS, "metrics": json.RawMessage(run.MetricsJSON), "evidence": json.RawMessage(run.EvidenceJSON)})
		if _, err := tx.ExecContext(ctx, `INSERT INTO model_spec_sheets(deployment_id,model_id,hardware_profile_id,placement_json,catalog_claims_json,llmfit_json,qualification_json,restrictions_json,admission_status,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?,'{}','pending',1,?,?) ON CONFLICT(deployment_id) DO UPDATE SET hardware_profile_id=excluded.hardware_profile_id,placement_json=excluded.placement_json,catalog_claims_json=excluded.catalog_claims_json,llmfit_json=excluded.llmfit_json,qualification_json=excluded.qualification_json,admission_status=CASE WHEN model_spec_sheets.hardware_profile_id<>excluded.hardware_profile_id OR model_spec_sheets.placement_json<>excluded.placement_json THEN 'pending' ELSE model_spec_sheets.admission_status END,restrictions_json=CASE WHEN model_spec_sheets.hardware_profile_id<>excluded.hardware_profile_id OR model_spec_sheets.placement_json<>excluded.placement_json THEN '{}' ELSE model_spec_sheets.restrictions_json END,admitted_by=CASE WHEN model_spec_sheets.hardware_profile_id<>excluded.hardware_profile_id OR model_spec_sheets.placement_json<>excluded.placement_json THEN NULL ELSE model_spec_sheets.admitted_by END,admission_notes=CASE WHEN model_spec_sheets.hardware_profile_id<>excluded.hardware_profile_id OR model_spec_sheets.placement_json<>excluded.placement_json THEN NULL ELSE model_spec_sheets.admission_notes END,admitted_at=CASE WHEN model_spec_sheets.hardware_profile_id<>excluded.hardware_profile_id OR model_spec_sheets.placement_json<>excluded.placement_json THEN NULL ELSE model_spec_sheets.admitted_at END,updated_at=excluded.updated_at,revision=model_spec_sheets.revision+1`, dep.ID, model.ID, req.HardwareProfileID, string(placementJSON), string(claims), string(llmfitJSON), string(qualificationJSON), now, now); err != nil {
			return err
		}
		eid, _ := q.ids.New("evt")
		payload, _ := json.Marshal(map[string]any{"qualification_run_id": run.ID, "deployment_id": dep.ID, "qualification": qual, "protocol_level": level, "context_max_verified": verified, "compatibility_profile_id": profileID})
		return q.events.Append(ctx, tx, event.Event{ID: eid, Type: "local_ai.qualification_completed", AggregateType: "model_deployment", AggregateID: dep.ID, ActorPrincipalID: req.ActorPrincipalID, Payload: payload, OccurredAt: now})
	})
}

func (q *Qualifier) Run(ctx context.Context, idv string) (QualificationRun, error) {
	var r QualificationRun
	var verified, prompt, completion sql.NullInt64
	var level, fail sql.NullString
	var ttft, tps sql.NullFloat64
	var peak, start, done sql.NullInt64
	var metrics, evidence string
	err := q.db.QueryRowContext(ctx, `SELECT id,deployment_id,hardware_profile_id,runtime_instance_id,status,requested_context,verified_context,protocol_level,prompt_tokens_tested,completion_tokens,ttft_ms,tokens_per_second,peak_memory_bytes,metrics_json,evidence_json,failure_reason,started_at,completed_at,revision,created_at,updated_at FROM local_model_qualification_runs WHERE id=?`, idv).Scan(&r.ID, &r.DeploymentID, &r.HardwareProfileID, &r.RuntimeInstanceID, &r.Status, &r.RequestedContext, &verified, &level, &prompt, &completion, &ttft, &tps, &peak, &metrics, &evidence, &fail, &start, &done, &r.Revision, &r.CreatedAt, &r.UpdatedAt)
	if verified.Valid {
		v := verified.Int64
		r.VerifiedContext = &v
	}
	if level.Valid {
		r.ProtocolLevel = &level.String
	}
	if prompt.Valid {
		r.PromptTokensTested = prompt.Int64
	}
	if completion.Valid {
		r.CompletionTokens = completion.Int64
	}
	if ttft.Valid {
		v := ttft.Float64
		r.TTFTMS = &v
	}
	if tps.Valid {
		v := tps.Float64
		r.TokensPerSecond = &v
	}
	if peak.Valid {
		v := peak.Int64
		r.PeakMemoryBytes = &v
	}
	if fail.Valid {
		r.FailureReason = &fail.String
	}
	if start.Valid {
		v := start.Int64
		r.StartedAt = &v
	}
	if done.Valid {
		v := done.Int64
		r.CompletedAt = &v
	}
	r.MetricsJSON = json.RawMessage(metrics)
	r.EvidenceJSON = json.RawMessage(evidence)
	return r, err
}
func (q *Qualifier) fail(ctx context.Context, run QualificationRun, cause error) error {
	return q.failWithEvidence(ctx, run, cause.Error())
}
func (q *Qualifier) failWithEvidence(ctx context.Context, run QualificationRun, reason string) error {
	now := q.clock.UnixMilli()
	res, err := q.db.ExecContext(ctx, `UPDATE local_model_qualification_runs SET status='failed',failure_reason=?,metrics_json=?,evidence_json=?,completed_at=?,updated_at=?,revision=revision+1 WHERE id=? AND revision=? AND status IN ('created','running')`, reason, string(run.MetricsJSON), string(run.EvidenceJSON), now, now, run.ID, run.Revision)
	if err != nil {
		return fmt.Errorf("qualification failure persistence: %w (original: %s)", err, reason)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("qualification failure persistence: %w (original: %s)", err, reason)
	}
	if n != 1 {
		return fmt.Errorf("qualification failure state conflict (original: %s)", reason)
	}
	return errors.New(reason)
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
func errText(err error) string {
	if err == nil {
		return "invalid response"
	}
	return err.Error()
}
func hasAssistantContent(raw []byte) bool {
	var x struct {
		Choices []struct {
			Message struct {
				Content any `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(raw, &x) != nil || len(x.Choices) == 0 {
		return false
	}
	return x.Choices[0].Message.Content != nil
}
func assistantJSONHasOK(raw []byte) bool {
	var x struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(raw, &x) != nil || len(x.Choices) == 0 {
		return false
	}
	var v map[string]any
	if json.Unmarshal([]byte(x.Choices[0].Message.Content), &v) != nil {
		return false
	}
	b, ok := v["ok"].(bool)
	return ok && b
}
func hasToolCall(raw []byte, name string) bool {
	var x struct {
		Choices []struct {
			Message struct {
				ToolCalls []struct {
					Function struct {
						Name string `json:"name"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(raw, &x) != nil || len(x.Choices) == 0 {
		return false
	}
	for _, c := range x.Choices[0].Message.ToolCalls {
		if c.Function.Name == name {
			return true
		}
	}
	return false
}
func contextSteps(max int64) []int64 {
	base := []int64{2048, 4096, 8192, 16384, 32768, 65536, 131072, 262144}
	var out []int64
	for _, v := range base {
		if v <= max {
			out = append(out, v)
		}
	}
	if len(out) == 0 || out[len(out)-1] != max {
		out = append(out, max)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
func contextProbeText(target int64) string { // approximate request; actual usage.prompt_tokens is authoritative.
	n := int(target)
	if n > 262144 {
		n = 262144
	}
	var b strings.Builder
	b.Grow(n * 3)
	b.WriteString("Read the following tokens and reply OK: ")
	for i := 0; i < n; i++ {
		b.WriteString("x ")
	}
	return b.String()
}
func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
func nullablePositive(v int64) any {
	if v > 0 {
		return v
	}
	return nil
}
func nullString(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return strings.TrimSpace(v)
}
func qualificationFingerprint(dep, role, cap, level string, context int64, evidence []byte) string {
	raw, _ := json.Marshal(map[string]any{"deployment_id": dep, "role": role, "capability": cap, "level": level, "context": context, "evidence": json.RawMessage(evidence)})
	sum := sha256Bytes(raw)
	return sum
}
func sha256Bytes(b []byte) string { h := sha256.Sum256(b); return fmt.Sprintf("%x", h[:]) }
