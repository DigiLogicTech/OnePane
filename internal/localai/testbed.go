package localai

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/inference"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type AdmissionStatus string

const (
	AdmissionPending    AdmissionStatus = "pending"
	AdmissionAccepted   AdmissionStatus = "accepted"
	AdmissionRestricted AdmissionStatus = "restricted"
	AdmissionRejected   AdmissionStatus = "rejected"
)

type ModelRestrictions struct {
	MaxContextTokens int64     `json:"max_context_tokens,omitempty"`
	DenyCapabilities []string  `json:"deny_capabilities,omitempty"`
	DenyRoles        []string  `json:"deny_roles,omitempty"`
	DenyUseCases     []UseCase `json:"deny_use_cases,omitempty"` // retained as descriptive metadata until task use-case is first-class in scheduler requests
	AllowToolUse     *bool     `json:"allow_tool_use,omitempty"`
	Notes            []string  `json:"notes,omitempty"`
}

type ModelSpecSheet struct {
	DeploymentID      string            `json:"deployment_id"`
	ModelID           string            `json:"model_id"`
	HardwareProfileID string            `json:"hardware_profile_id"`
	Model             ModelSpec         `json:"model"`
	Quantization      string            `json:"quantization,omitempty"`
	RuntimeName       string            `json:"runtime_name,omitempty"`
	RuntimeBackend    string            `json:"runtime_backend,omitempty"`
	RequestedContext  int64             `json:"requested_context_tokens,omitempty"`
	VerifiedContext   *int64            `json:"verified_context_tokens,omitempty"`
	MeasuredTPS       *float64          `json:"measured_tps,omitempty"`
	MeasuredTTFTMS    *float64          `json:"measured_ttft_ms,omitempty"`
	Placement         PlacementPlan     `json:"placement"`
	CatalogClaims     json.RawMessage   `json:"catalog_claims"`
	LLMFit            json.RawMessage   `json:"llmfit"`
	Qualification     json.RawMessage   `json:"qualification"`
	Restrictions      ModelRestrictions `json:"restrictions"`
	AdmissionStatus   AdmissionStatus   `json:"admission_status"`
	AdmittedBy        *string           `json:"admitted_by,omitempty"`
	AdmissionNotes    *string           `json:"admission_notes,omitempty"`
	AdmittedAt        *int64            `json:"admitted_at,omitempty"`
	Revision          int64             `json:"revision"`
	CreatedAt         int64             `json:"created_at"`
	UpdatedAt         int64             `json:"updated_at"`
}

type TestbedSession struct {
	ID                string        `json:"id"`
	DeploymentID      string        `json:"deployment_id"`
	HardwareProfileID string        `json:"hardware_profile_id"`
	Placement         PlacementPlan `json:"placement"`
	Status            string        `json:"status"`
	CreatedBy         *string       `json:"created_by,omitempty"`
	Notes             *string       `json:"notes,omitempty"`
	Revision          int64         `json:"revision"`
	StartedAt         int64         `json:"started_at"`
	CompletedAt       *int64        `json:"completed_at,omitempty"`
	CreatedAt         int64         `json:"created_at"`
	UpdatedAt         int64         `json:"updated_at"`
}

type TestbedTurn struct {
	ID                 string          `json:"id"`
	SessionID          string          `json:"session_id"`
	Sequence           int64           `json:"sequence"`
	RequestJSON        json.RawMessage `json:"request_json"`
	ResponseJSON       json.RawMessage `json:"response_json"`
	UsageJSON          json.RawMessage `json:"usage_json"`
	MetricsJSON        json.RawMessage `json:"metrics_json"`
	SyntheticToolProbe bool            `json:"synthetic_tool_probe"`
	CreatedAt          int64           `json:"created_at"`
}

type TestbedTurnCommand struct {
	Prompt             string          `json:"prompt,omitempty"`
	RequestJSON        json.RawMessage `json:"request_json,omitempty"`
	SyntheticToolProbe bool            `json:"synthetic_tool_probe,omitempty"`
	MaxTokens          int             `json:"max_tokens,omitempty"`
}

type AdmissionCommand struct {
	Status           AdmissionStatus   `json:"status"`
	Restrictions     ModelRestrictions `json:"restrictions,omitempty"`
	Notes            string            `json:"notes,omitempty"`
	ActorPrincipalID string            `json:"-"`
}

func (s *Service) ensurePendingSpecSheetTx(ctx context.Context, tx storage.Tx, deploymentID, modelID, hardwareID string, placement PlacementPlan, claims, llmfit, qualification json.RawMessage, now int64) error {
	if len(claims) == 0 || !json.Valid(claims) {
		claims = json.RawMessage(`{}`)
	}
	if len(llmfit) == 0 || !json.Valid(llmfit) {
		llmfit = json.RawMessage(`{}`)
	}
	if len(qualification) == 0 || !json.Valid(qualification) {
		qualification = json.RawMessage(`{}`)
	}
	place, _ := json.Marshal(placement)
	_, err := tx.ExecContext(ctx, `INSERT INTO model_spec_sheets(deployment_id,model_id,hardware_profile_id,placement_json,catalog_claims_json,llmfit_json,qualification_json,restrictions_json,admission_status,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?,'{}','pending',1,?,?)
		ON CONFLICT(deployment_id) DO UPDATE SET
			hardware_profile_id=excluded.hardware_profile_id,placement_json=excluded.placement_json,catalog_claims_json=excluded.catalog_claims_json,llmfit_json=excluded.llmfit_json,qualification_json=excluded.qualification_json,
			admission_status=CASE WHEN model_spec_sheets.hardware_profile_id<>excluded.hardware_profile_id OR model_spec_sheets.placement_json<>excluded.placement_json THEN 'pending' ELSE model_spec_sheets.admission_status END,
			restrictions_json=CASE WHEN model_spec_sheets.hardware_profile_id<>excluded.hardware_profile_id OR model_spec_sheets.placement_json<>excluded.placement_json THEN '{}' ELSE model_spec_sheets.restrictions_json END,
			admitted_by=CASE WHEN model_spec_sheets.hardware_profile_id<>excluded.hardware_profile_id OR model_spec_sheets.placement_json<>excluded.placement_json THEN NULL ELSE model_spec_sheets.admitted_by END,
			admission_notes=CASE WHEN model_spec_sheets.hardware_profile_id<>excluded.hardware_profile_id OR model_spec_sheets.placement_json<>excluded.placement_json THEN NULL ELSE model_spec_sheets.admission_notes END,
			admitted_at=CASE WHEN model_spec_sheets.hardware_profile_id<>excluded.hardware_profile_id OR model_spec_sheets.placement_json<>excluded.placement_json THEN NULL ELSE model_spec_sheets.admitted_at END,
			updated_at=excluded.updated_at,revision=model_spec_sheets.revision+1`, deploymentID, modelID, hardwareID, string(place), string(claims), string(llmfit), string(qualification), now, now)
	return err
}

func (s *Service) SpecSheet(ctx context.Context, deploymentID string) (ModelSpecSheet, error) {
	var x ModelSpecSheet
	var place, claims, llmfit, qual, restr string
	var by, notes sql.NullString
	var admitted sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT deployment_id,model_id,hardware_profile_id,placement_json,catalog_claims_json,llmfit_json,qualification_json,restrictions_json,admission_status,admitted_by,admission_notes,admitted_at,revision,created_at,updated_at FROM model_spec_sheets WHERE deployment_id=?`, strings.TrimSpace(deploymentID)).Scan(&x.DeploymentID, &x.ModelID, &x.HardwareProfileID, &place, &claims, &llmfit, &qual, &restr, &x.AdmissionStatus, &by, &notes, &admitted, &x.Revision, &x.CreatedAt, &x.UpdatedAt)
	if err != nil {
		return x, err
	}
	if json.Unmarshal([]byte(place), &x.Placement) != nil || json.Unmarshal([]byte(restr), &x.Restrictions) != nil {
		return x, errors.New("model spec sheet is corrupt")
	}
	x.CatalogClaims = json.RawMessage(claims)
	x.LLMFit = json.RawMessage(llmfit)
	x.Qualification = json.RawMessage(qual)
	if by.Valid {
		x.AdmittedBy = &by.String
	}
	if notes.Valid {
		x.AdmissionNotes = &notes.String
	}
	if admitted.Valid {
		v := admitted.Int64
		x.AdmittedAt = &v
	}
	var planJSON, runtimeConfig string
	var quant, runtimeName string
	if err := s.db.QueryRowContext(ctx, `SELECT p.plan_json,p.quantization,p.runtime_name,d.runtime_config_json FROM managed_local_models mm JOIN local_model_install_plans p ON p.id=mm.plan_id JOIN model_deployments d ON d.id=mm.deployment_id WHERE mm.deployment_id=?`, deploymentID).Scan(&planJSON, &quant, &runtimeName, &runtimeConfig); err == nil {
		var rec Recommendation
		if json.Unmarshal([]byte(planJSON), &rec) == nil {
			x.Model = rec.Model
			x.RequestedContext = rec.ContextTokens
		}
		x.Quantization = quant
		x.RuntimeName = runtimeName
		var cfg struct {
			RuntimeBackend string `json:"runtime_backend"`
		}
		if json.Unmarshal([]byte(runtimeConfig), &cfg) == nil {
			x.RuntimeBackend = cfg.RuntimeBackend
		}
	}
	var verified sql.NullInt64
	var measuredTPS, ttft sql.NullFloat64
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(COALESCE(context_max_verified,0)) FROM compatibility_profiles WHERE deployment_id=?`, deploymentID).Scan(&verified); err == nil && verified.Valid && verified.Int64 > 0 {
		v := verified.Int64
		x.VerifiedContext = &v
	}
	if err := s.db.QueryRowContext(ctx, `SELECT tokens_per_second,ttft_ms FROM local_model_qualification_runs WHERE deployment_id=? AND status='succeeded' ORDER BY completed_at DESC LIMIT 1`, deploymentID).Scan(&measuredTPS, &ttft); err == nil {
		if measuredTPS.Valid {
			v := measuredTPS.Float64
			x.MeasuredTPS = &v
		}
		if ttft.Valid {
			v := ttft.Float64
			x.MeasuredTTFTMS = &v
		}
	}
	return x, nil
}

// ensurePendingManualTestbedSpec repairs only a missing pending sheet
// for a registered managed model. It never invents qualification evidence,
// changes admission, or revives an uninstalled deployment.
func (s *Service) ensurePendingManualTestbedSpec(ctx context.Context, deploymentID string) error {
	var modelID, hardwareID, planJSON string
	var claims sql.NullString
	err := s.db.QueryRowContext(ctx,`SELECT mm.model_id,p.hardware_profile_id,p.plan_json,m.static_metadata_json
FROM managed_local_models mm
JOIN local_model_install_plans p ON p.id=mm.plan_id
JOIN model_deployments d ON d.id=mm.deployment_id
JOIN models m ON m.id=mm.model_id
WHERE mm.deployment_id=? AND mm.status NOT IN ('removed','failed')`,
		deploymentID).Scan(&modelID,&hardwareID,&planJSON,&claims)
	if errors.Is(err,sql.ErrNoRows) {
		return errors.New("model deployment no longer exists; rescan installed models before retrying")
	}
	if err!=nil{return err}
	var rec Recommendation
	if err=json.Unmarshal([]byte(planJSON),&rec);err!=nil{return fmt.Errorf("invalid model installation plan: %w",err)}
	if rec.Placement.Mode==""{return errors.New("model placement is not configured; choose Compute before Agent Check")}
	placement,err:=json.Marshal(rec.Placement);if err!=nil{return err}
	source:="{}"
	if claims.Valid&&json.Valid([]byte(claims.String)){source=claims.String}
	now:=s.clock.UnixMilli()
	_,err=s.db.ExecContext(ctx,`INSERT INTO model_spec_sheets(
 deployment_id,model_id,hardware_profile_id,placement_json,catalog_claims_json,
 llmfit_json,qualification_json,restrictions_json,admission_status,revision,created_at,updated_at)
 VALUES(?,?,?,?,?,'{}','{"status":"pending_manual_agent_check"}','{}','pending',1,?,?)
 ON CONFLICT(deployment_id) DO NOTHING`,
 deploymentID,modelID,hardwareID,string(placement),source,now,now)
	return err
}

func (s *Service) StartTestbed(ctx context.Context, deploymentID string, actor *string, notes string) (TestbedSession, error) {
	var out TestbedSession
	if actor != nil && *actor == FederatedModelManagerPrincipal {
		if err := s.ensureFederatedModelManager(ctx); err != nil {
			return out, err
		}
	}
	sheet, err := s.SpecSheet(ctx, deploymentID)
	if errors.Is(err, sql.ErrNoRows) {
		// An interrupted qualification can leave an otherwise valid managed
		// model without its first spec sheet. Manual Agent Check must be able
		// to qualify a still-registered model with pending admission.
		if repairErr := s.ensurePendingManualTestbedSpec(ctx, deploymentID); repairErr != nil {
			return out, fmt.Errorf("cannot prepare Agent Check for deployment %s: %w", deploymentID, repairErr)
		}
		sheet, err = s.SpecSheet(ctx, deploymentID)
	}
	if err != nil {
		return out, fmt.Errorf("cannot load deployment %s for Agent Check: %w", deploymentID, err)
	}
    // Agent Check must not create an active trial for an absent model artifact.
    if s.supervisor!=nil {
      if _,_,_,e:=s.supervisor.resolve(ctx,deploymentID);e!=nil {
        return out,fmt.Errorf("Agent Check preflight: %w",e)
      }
    }
	idv, err := s.ids.New("tb")
	if err != nil {
		return out, err
	}
	now := s.clock.UnixMilli()
	place, _ := json.Marshal(sheet.Placement)
	var notePtr *string
	if strings.TrimSpace(notes) != "" {
		v := strings.TrimSpace(notes)
		notePtr = &v
	}
	out = TestbedSession{ID: idv, DeploymentID: deploymentID, HardwareProfileID: sheet.HardwareProfileID, Placement: sheet.Placement, Status: "active", CreatedBy: actor, Notes: notePtr, Revision: 1, StartedAt: now, CreatedAt: now, UpdatedAt: now}
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO model_testbed_sessions(id,deployment_id,hardware_profile_id,placement_json,status,created_by,notes,revision,started_at,created_at,updated_at) VALUES(?,?,?,?, 'active',?,?,1,?,?,?)`, idv, deploymentID, sheet.HardwareProfileID, string(place), actor, notePtr, now, now, now); err != nil {
			return err
		}
		eid, _ := s.ids.New("evt")
		payload, _ := json.Marshal(map[string]any{"testbed_session_id": idv, "deployment_id": deploymentID})
		return s.events.Append(ctx, tx, event.Event{ID: eid, Type: "local_ai.testbed_started", AggregateType: "model_testbed_session", AggregateID: idv, ActorPrincipalID: actor, Payload: payload, OccurredAt: now})
	})
	return out, err
}

func (s *Service) TestbedSession(ctx context.Context, idv string) (TestbedSession, error) {
	var x TestbedSession
	var place string
	var actor, note sql.NullString
	var done sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT id,deployment_id,hardware_profile_id,placement_json,status,created_by,notes,revision,started_at,completed_at,created_at,updated_at FROM model_testbed_sessions WHERE id=?`, idv).Scan(&x.ID, &x.DeploymentID, &x.HardwareProfileID, &place, &x.Status, &actor, &note, &x.Revision, &x.StartedAt, &done, &x.CreatedAt, &x.UpdatedAt)
	if err != nil {
		return x, err
	}
	_ = json.Unmarshal([]byte(place), &x.Placement)
	if actor.Valid {
		x.CreatedBy = &actor.String
	}
	if note.Valid {
		x.Notes = &note.String
	}
	if done.Valid {
		v := done.Int64
		x.CompletedAt = &v
	}
	return x, nil
}

// Keep the latest diagnostic on the model Spec Sheet when no inference can run.
// An unsuccessful run never changes admission or invents a benchmark.
func (s *Service) markManualAgentCheckFailed(ctx context.Context, sess TestbedSession, failure error) {
 if failure==nil{return}
 // The former v2 qualification stored failure.Error() verbatim, potentially
 // exposing local model paths, prompts, bearer tokens or process output to
 // any Spec Sheet reader. The v3 record includes only fixed labels; the
 // separately authorized QA evidence endpoint provides observed stages.
 raw,_:=json.Marshal(map[string]any{
  "status":"blocked","profile_version":"onepane.manual-agent-check/v3",
  "evidence":map[string]any{
   "infrastructure_error":true,
   "failure_details":"inspect_structured_agentcheck_evidence",
  },
  "session_id":sess.ID,
 })
 _,_=s.db.ExecContext(ctx,`UPDATE model_spec_sheets SET qualification_json=?,updated_at=?,revision=revision+1 WHERE deployment_id=? AND hardware_profile_id=?`,string(raw),s.clock.UnixMilli(),sess.DeploymentID,sess.HardwareProfileID)
}
func (s *Service) RunTestbedTurn(ctx context.Context, sessionID string, cmd TestbedTurnCommand) (completed TestbedTurn, resultErr error) {
	var out TestbedTurn
	sess, err := s.TestbedSession(ctx, sessionID)
	if err != nil {
		return out, err
	}
	if sess.Status != "active" {
		return out, errors.New("testbed session is not active")
	}
	var req json.RawMessage
	if len(cmd.RequestJSON) > 0 {
		if !json.Valid(cmd.RequestJSON) || len(cmd.RequestJSON) > 1<<20 {
			return out, errors.New("invalid testbed request JSON")
		}
		req = append(json.RawMessage(nil), cmd.RequestJSON...)
	} else {
		if strings.TrimSpace(cmd.Prompt) == "" {
			return out, errors.New("testbed prompt required")
		}
		max := cmd.MaxTokens
		if max <= 0 || max > 8192 {
			max = 1024
		}
		body := map[string]any{"messages": []map[string]string{{"role": "system", "content": "OnePane model testbed. Do not execute external actions. Tool calls are synthetic probes only."}, {"role": "user", "content": cmd.Prompt}}, "max_tokens": max, "temperature": 0.2}
		if cmd.SyntheticToolProbe {
			body["tools"] = []any{map[string]any{"type": "function", "function": map[string]any{"name": "onepane_test_probe", "description": "Synthetic no-side-effect tool used only to test tool-call formatting", "parameters": map[string]any{"type": "object", "properties": map[string]any{"value": map[string]string{"type": "string"}}, "required": []string{"value"}}}}}
			body["tool_choice"] = "auto"
		}
		req, _ = json.Marshal(body)
	}
	// Each manual Agent Check turn holds an activity lease. A long-running
	// CPU-only probe must not be evicted by the idle reaper or a concurrent
	// Agent Check, even while deployment admission remains pending.
	if s.supervisor != nil {
		if _,err:=s.supervisor.Acquire(ctx,sess.DeploymentID);err!=nil{
			s.markManualAgentCheckFailed(ctx,sess,err)
			s.recordAgentCheckFailure(ctx,sess,"runtime_acquire",err)
			return out,fmt.Errorf("acquire managed runtime for testbed: %w",err)
		}
		defer func(){
			cleanupCtx,cancel:=context.WithTimeout(context.Background(),15*time.Second)
			defer cancel()
			if err:=s.supervisor.Release(cleanupCtx,sess.DeploymentID);err!=nil{
				s.recordAgentCheckFailure(cleanupCtx,sess,"runtime_release",err)
				resultErr=errors.Join(resultErr,fmt.Errorf("release Agent Check probe runtime: %w",err))
			}
		}()
	}
	dep, err := s.inference.Deployment(ctx, sess.DeploymentID)
	if err != nil {
		s.recordAgentCheckFailure(ctx,sess,"deployment_read",err)
		return out, err
	}
	model, err := s.inference.Model(ctx, dep.ModelID)
	if err != nil {
		s.recordAgentCheckFailure(ctx,sess,"model_read",err)
		return out, err
	}
	rid, _ := s.ids.New("tbreq")
	start := time.Now()
	transport := inference.LocalOpenAITransport{Resolver: s}
	result, err := transport.Dispatch(ctx, inference.DispatchRequest{RequestID: rid, Model: model, Deployment: dep, RequestJSON: req}, nil)
 if err!=nil{s.markManualAgentCheckFailed(ctx,sess,err);s.recordAgentCheckFailure(ctx,sess,"inference_dispatch",err);return out,err}
	elapsed := time.Since(start)
	metrics, _ := json.Marshal(map[string]any{"elapsed_ms": elapsed.Milliseconds(), "placement": sess.Placement, "synthetic_tool_probe": cmd.SyntheticToolProbe})
	usage := result.UsageJSON
	if len(usage) == 0 || !json.Valid(usage) {
		usage = json.RawMessage(`{}`)
	}
	resp := result.ResponseJSON
	if len(resp) == 0 || !json.Valid(resp) {
		err=errors.New("testbed model returned invalid JSON")
		s.recordAgentCheckFailure(ctx,sess,"response_validation",err)
		return out, err
	}
	var seq int64
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence_no),0)+1 FROM model_testbed_turns WHERE session_id=?`, sessionID).Scan(&seq); err != nil {
		s.recordAgentCheckFailure(ctx,sess,"turn_store",err)
		return out, err
	}
	tid, _ := s.ids.New("tbturn")
	now := s.clock.UnixMilli()
	out = TestbedTurn{ID: tid, SessionID: sessionID, Sequence: seq, RequestJSON: req, ResponseJSON: resp, UsageJSON: usage, MetricsJSON: metrics, SyntheticToolProbe: cmd.SyntheticToolProbe, CreatedAt: now}
	probe := 0
	if cmd.SyntheticToolProbe {
		probe = 1
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO model_testbed_turns(id,session_id,sequence_no,request_json,response_json,usage_json,metrics_json,synthetic_tool_probe,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, tid, sessionID, seq, string(req), string(resp), string(usage), string(metrics), probe, now)
	if err!=nil{s.recordAgentCheckFailure(ctx,sess,"turn_store",err)}
	return out, err
}

func (s *Service) ListTestbedTurns(ctx context.Context, sessionID string) ([]TestbedTurn, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,session_id,sequence_no,request_json,response_json,usage_json,metrics_json,synthetic_tool_probe,created_at FROM model_testbed_turns WHERE session_id=? ORDER BY sequence_no`, strings.TrimSpace(sessionID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TestbedTurn
	for rows.Next() {
		var x TestbedTurn
		var req, resp, usage, metrics string
		var probe int
		if err := rows.Scan(&x.ID, &x.SessionID, &x.Sequence, &req, &resp, &usage, &metrics, &probe, &x.CreatedAt); err != nil {
			return nil, err
		}
		x.RequestJSON = json.RawMessage(req)
		x.ResponseJSON = json.RawMessage(resp)
		x.UsageJSON = json.RawMessage(usage)
		x.MetricsJSON = json.RawMessage(metrics)
		x.SyntheticToolProbe = probe == 1
		out = append(out, x)
	}
	return out, rows.Err()
}

// Manual Agent Check only records observed evidence. Testbed completion without
// a single successful inference is never eligible for production admission.
func testbedResponseEvidence(raw json.RawMessage) (string, bool) {
 var result struct { Choices []struct { Message struct {
  Content string `json:"content"`
  ToolCalls []struct { Function struct { Name string `json:"name"`; Arguments string `json:"arguments"` } `json:"function"` } `json:"tool_calls"`
 } `json:"message"` } `json:"choices"` }
 if json.Unmarshal(raw,&result)!=nil||len(result.Choices)==0{return "",false}
 message:=result.Choices[0].Message
 toolCalled:=false
 for _,call:=range message.ToolCalls{if call.Function.Name=="onepane_test_probe" {
   var args struct{Value string `json:"value"`}
   if json.Unmarshal([]byte(call.Function.Arguments),&args)==nil&&args.Value=="agent-check"{toolCalled=true}
 }}
 return strings.TrimSpace(message.Content),toolCalled
}
func (s *Service) CompleteTestbed(ctx context.Context, sessionID string) error {
 sess,err:=s.TestbedSession(ctx,sessionID);if err!=nil{return err}
 if sess.Status!="active"{return errors.New("testbed session is not active")}
 turns,err:=s.ListTestbedTurns(ctx,sessionID);if err!=nil{return err}
 if len(turns)==0{err=errors.New("cannot complete Agent Check: no successful model inference turns were recorded");s.recordAgentCheckFailure(ctx,sess,"completion_validation",err);return err}
 evidence:=map[string]any{"plain_ok":false,"json_ok":false,"schema_ok":false,"tools_ok":false,"context_probes":[]any{},"testbed_session_id":sessionID}
 plainChecked,jsonChecked,toolsChecked:=false,false,false
 var totalTokens int64
 var totalElapsed int64
 for _,turn:=range turns{
  content,toolCalled:=testbedResponseEvidence(turn.ResponseJSON)
  if turn.SyntheticToolProbe{toolsChecked=true;evidence["tools_ok"]=toolCalled
  }else if !plainChecked{plainChecked=true;evidence["plain_ok"]=content=="ONEPANE_OK"
  }else if !jsonChecked{
   jsonChecked=true
   var payload map[string]any
   valid:=json.Unmarshal([]byte(content),&payload)==nil
   evidence["json_ok"]=valid
   if valid{status,_:=payload["status"].(string);number,ok:=payload["number"].(float64);evidence["schema_ok"]=status=="ok"&&ok&&number==7}
  }
  var usage struct {CompletionTokens int64 `json:"completion_tokens"`}
  var timing struct {ElapsedMS int64 `json:"elapsed_ms"`}
  if json.Unmarshal(turn.UsageJSON,&usage)==nil&&json.Unmarshal(turn.MetricsJSON,&timing)==nil&&usage.CompletionTokens>0&&timing.ElapsedMS>0{
   totalTokens+=usage.CompletionTokens;totalElapsed+=timing.ElapsedMS
  }
 }
 evidence["plain_tested"]=plainChecked;evidence["json_tested"]=jsonChecked;evidence["tools_tested"]=toolsChecked
 // Read the separate typed, immutable failure journal rather than copying
 // legacy qualification errors (which could contain arbitrary runtime text).
 // An eventually-successful probe after an earlier infrastructure failure
 // remains limited; it must not be silently promoted to fully passed.
 var observedFailures int64
 if err:=s.db.QueryRowContext(ctx,`SELECT COUNT(*) FROM model_agentcheck_failure_observations
   WHERE session_id=? AND deployment_id=?`,sessionID,sess.DeploymentID).Scan(&observedFailures);err!=nil{return err}
 evidence["structured_failure_observations"]=observedFailures
 status:="limited"
 if observedFailures==0&&evidence["plain_ok"]==true&&evidence["json_ok"]==true&&evidence["schema_ok"]==true&&evidence["tools_ok"]==true{status="passed"}
 metrics:=map[string]any{"successful_turns":len(turns)}
 if totalTokens>0&&totalElapsed>0{metrics["completion_tokens_per_second"]=float64(totalTokens)*1000/float64(totalElapsed)}
 quality:=map[string]any{"status":status,"profile_version":"onepane.manual-agent-check/v2","evidence":evidence,"metrics":metrics}
 raw,err:=json.Marshal(quality);if err!=nil{return err}
 now:=s.clock.UnixMilli()
 if err:=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
  res,err:=tx.ExecContext(ctx,`UPDATE model_testbed_sessions SET status='completed',completed_at=?,updated_at=?,revision=revision+1 WHERE id=? AND status='active'`,now,now,sessionID)
  if err!=nil{return err};n,_:=res.RowsAffected();if n!=1{return errors.New("testbed session state conflict")}
  // Never promote admission or claim context verification from a manual probe.
  _,err=tx.ExecContext(ctx,`UPDATE model_spec_sheets SET qualification_json=?,updated_at=?,revision=revision+1 WHERE deployment_id=? AND hardware_profile_id=?`,string(raw),now,sess.DeploymentID,sess.HardwareProfileID)
  return err
 });err!=nil{s.recordAgentCheckFailure(ctx,sess,"completion_persist",err);return err}
 // All probes and evidence are persisted. Release the model's CPU/GPU memory.
 // Do not evict concurrent inference, which is protected by StopIfIdle.
 if s.supervisor!=nil{
  if _,err:=s.supervisor.StopIfIdle(ctx,sess.DeploymentID);err!=nil{
   s.recordAgentCheckFailure(ctx,sess,"runtime_unload",err)
   return fmt.Errorf("Agent Check evidence saved but runtime could not be unloaded: %w",err)
  }
 }
 return nil
}

// AbortTestbed closes a failed manual Agent Check and frees model residency.
// Sessions are not allowed to manufacture a successful qualification on abort.
// The DB permits active/completed/cancelled: a failed infrastructure probe is
// captured in qualification evidence while the testbed lifecycle is cancelled.
func (s *Service) AbortTestbed(ctx context.Context, sessionID string, cause string) error {
 sess,err:=s.TestbedSession(ctx,strings.TrimSpace(sessionID));if err!=nil{return err}
 if sess.Status=="completed" {
  // A late error in the UI must not turn completed evidence into failure.
  if s.supervisor!=nil {_,err=s.supervisor.StopIfIdle(ctx,sess.DeploymentID)}
  return err
 }
 if sess.Status!="active" {return nil}
 cause=strings.TrimSpace(cause)
 if len(cause)>500{cause=cause[:500]}
 if cause==""{cause="Agent Check interrupted"}
 now:=s.clock.UnixMilli()
 res,err:=s.db.ExecContext(ctx,`UPDATE model_testbed_sessions SET status='cancelled',completed_at=?,updated_at=?,revision=revision+1 WHERE id=? AND status='active'`,now,now,sessionID)
 if err!=nil{return err}
 n,err:=res.RowsAffected();if err!=nil{return err}
 if n!=1{return errors.New("Agent Check session was already finalized")}
 s.markManualAgentCheckFailed(ctx,sess,errors.New(cause))
 s.recordAgentCheckFailure(ctx,sess,"session_abort",errors.New("Agent Check abort requested"))
 if s.supervisor!=nil{
  if _,err:=s.supervisor.StopIfIdle(ctx,sess.DeploymentID);err!=nil{s.recordAgentCheckFailure(ctx,sess,"runtime_unload",err);return fmt.Errorf("Agent Check stopped, but model unload failed: %w",err)}
 }
 return nil
}

func validAdmission(v AdmissionStatus) bool {
	return v == AdmissionPending || v == AdmissionAccepted || v == AdmissionRestricted || v == AdmissionRejected
}

func (s *Service) AdmitModel(ctx context.Context, deploymentID string, cmd AdmissionCommand) (ModelSpecSheet, error) {
	if cmd.ActorPrincipalID == FederatedModelManagerPrincipal {
		if err := s.ensureFederatedModelManager(ctx); err != nil {
			return ModelSpecSheet{}, err
		}
	}
	if !validAdmission(cmd.Status) || strings.TrimSpace(cmd.ActorPrincipalID) == "" {
		return ModelSpecSheet{}, errors.New("valid admission status and actor are required")
	}
	if cmd.Status == AdmissionRestricted && cmd.Restrictions.MaxContextTokens < 0 {
		return ModelSpecSheet{}, errors.New("invalid restriction")
	}
	sheet, err := s.SpecSheet(ctx, deploymentID)
	if err != nil {
		return ModelSpecSheet{}, err
	}
	if cmd.Status == AdmissionAccepted || cmd.Status == AdmissionRestricted {
		placementJSON, _ := json.Marshal(sheet.Placement)
		var completed int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM model_testbed_sessions WHERE deployment_id=? AND hardware_profile_id=? AND placement_json=? AND status='completed'`, deploymentID, sheet.HardwareProfileID, string(placementJSON)).Scan(&completed); err != nil {
			return ModelSpecSheet{}, err
		}
		if completed < 1 {
			return ModelSpecSheet{}, errors.New("production admission requires a completed manual testbed session for the current hardware profile and placement")
		}
	}
	if cmd.Status == AdmissionPending {
		cmd.Restrictions = ModelRestrictions{}
	}
	raw, _ := json.Marshal(cmd.Restrictions)
	now := s.clock.UnixMilli()
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		var admittedBy any = cmd.ActorPrincipalID
		var admittedAt any = now
		var admissionNotes any = nullIfEmpty(cmd.Notes)
		if cmd.Status == AdmissionPending {
			admittedBy, admittedAt, admissionNotes = nil, nil, nil
		}
		res, err := tx.ExecContext(ctx, `UPDATE model_spec_sheets SET admission_status=?,restrictions_json=?,admitted_by=?,admission_notes=?,admitted_at=?,updated_at=?,revision=revision+1 WHERE deployment_id=? AND revision=?`, cmd.Status, string(raw), admittedBy, admissionNotes, admittedAt, now, deploymentID, sheet.Revision)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return errors.New("model admission revision conflict")
		}
		// Alpha 3 separates immutable model-function investigation from mutable
		// deployment performance. Once an exact model UID has passed admission,
		// preserve that fact across deployment removal/recreation and hardware swaps.
		if cmd.Status == AdmissionAccepted || cmd.Status == AdmissionRestricted {
			specSnapshot, _ := json.Marshal(map[string]any{
				"model": sheet.Model, "quantization": sheet.Quantization,
				"catalog_claims": json.RawMessage(sheet.CatalogClaims),
			})
			qualificationSnapshot, _ := json.Marshal(map[string]any{
				"completed": true, "profile_version": "onepane.agent-check/v1",
				"deployment_id": deploymentID, "hardware_profile_id": sheet.HardwareProfileID,
				"qualification": json.RawMessage(sheet.Qualification), "restrictions": cmd.Restrictions,
				"qualified_at": now,
			})
			if _, err := tx.ExecContext(ctx, `INSERT INTO model_identity_specs(model_uid,investigation_state,spec_json,qualification_json,archived,created_at,updated_at) VALUES(?,'qualified',?,?,0,?,?)
				ON CONFLICT(model_uid) DO UPDATE SET investigation_state='qualified',spec_json=excluded.spec_json,qualification_json=excluded.qualification_json,archived=0,updated_at=excluded.updated_at`, sheet.ModelID, string(specSnapshot), string(qualificationSnapshot), now, now); err != nil {
				return err
			}
		}
		eid, _ := s.ids.New("evt")
		payload, _ := json.Marshal(map[string]any{"deployment_id": deploymentID, "admission_status": cmd.Status, "restrictions": cmd.Restrictions, "hardware_profile_id": sheet.HardwareProfileID, "placement": sheet.Placement})
		return s.events.Append(ctx, tx, event.Event{ID: eid, Type: "local_ai.model_admission_changed", AggregateType: "model_deployment", AggregateID: deploymentID, ActorPrincipalID: &cmd.ActorPrincipalID, Payload: payload, OccurredAt: now})
	})
	if err != nil {
		return ModelSpecSheet{}, err
	}
	return s.SpecSheet(ctx, deploymentID)
}

// FederatedInferenceAllowed protects the pending-admission boundary. Non-managed
// local deployments keep their historical behavior; managed deployments must be
// explicitly accepted or restricted before ordinary peer inference/export.
func (s *Service) FederatedInferenceAllowed(ctx context.Context, deploymentID string) bool {
	var managed int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM managed_local_models WHERE deployment_id=?`, deploymentID).Scan(&managed)
	if err != nil {
		return false
	}
	if managed == 0 {
		return true
	}
	var status string
	if s.db.QueryRowContext(ctx, `SELECT admission_status FROM model_spec_sheets WHERE deployment_id=?`, deploymentID).Scan(&status) != nil {
		return false
	}
	return status == string(AdmissionAccepted) || status == string(AdmissionRestricted)
}

func (s *Service) ManagedDeploymentWorkspace(ctx context.Context, deploymentID string) (*string, error) {
	var ws sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT p.workspace_id FROM managed_local_models mm JOIN local_model_install_plans p ON p.id=mm.plan_id WHERE mm.deployment_id=?`, deploymentID).Scan(&ws)
	if err != nil {
		return nil, err
	}
	if !ws.Valid {
		return nil, nil
	}
	v := ws.String
	return &v, nil
}
