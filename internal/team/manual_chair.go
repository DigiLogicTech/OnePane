package team

import (
    "context"
    "database/sql"
    "encoding/json"
    "strings"

    "github.com/DigiLogicTech/OnePane/internal/storage"
)

// Chair proposals are separate from participant responses. The Chair may
// design the agenda and critique questions but can never advance a round;
// the deterministic worker and (optionally) the operator gate that decision.
type ManualWebChairTurn struct {
    ID string `json:"id"`
    SessionID string `json:"session_id"`
    WorkspaceID string `json:"workspace_id"`
    MemberID string `json:"member_id"`
    ProviderID string `json:"provider_id"`
    ModelLabel string `json:"model_label"`
    Stage string `json:"stage"`
    AfterRound int64 `json:"after_round"`
    PromptText string `json:"prompt_text"`
    PromptSHA256 string `json:"prompt_sha256"`
    Status string `json:"status"`
    ResponseText *string `json:"response_text,omitempty"`
    ResponseSHA256 *string `json:"response_sha256,omitempty"`
    ApprovedText *string `json:"approved_text,omitempty"`
    ApprovedSHA256 *string `json:"approved_sha256,omitempty"`
    SubmittedBy *string `json:"submitted_by,omitempty"`
    ApprovedBy *string `json:"approved_by,omitempty"`
    CreatedAt int64 `json:"created_at"`
    SubmittedAt *int64 `json:"submitted_at,omitempty"`
    ApprovedAt *int64 `json:"approved_at,omitempty"`
}
type QueueManualWebChairCommand struct{
    SessionID, WorkspaceID, MemberID, ProviderID, ModelLabel, Stage, Prompt string
    AfterRound int64
}
type SubmitManualWebChairCommand struct{ ID, Actor, Response string }
type ApproveManualWebChairCommand struct{ ID, Actor, ApprovedText string }

const manualChairSelect=`SELECT id,session_id,workspace_id,member_id,provider_id,model_label,
 stage,after_round,prompt_text,prompt_sha256,status,response_text,response_sha256,
 approved_text,approved_sha256,submitted_by,approved_by,created_at,submitted_at,approved_at
 FROM manual_web_council_chair_turns`
func scanManualChair(row interface{Scan(...any)error})(ManualWebChairTurn,error){
 var t ManualWebChairTurn
 var response,responseSum,approved,approvedSum,submittedBy,approvedBy sql.NullString
 var submitted,approvedAt sql.NullInt64
 err:=row.Scan(&t.ID,&t.SessionID,&t.WorkspaceID,&t.MemberID,&t.ProviderID,&t.ModelLabel,
  &t.Stage,&t.AfterRound,&t.PromptText,&t.PromptSHA256,&t.Status,&response,&responseSum,
  &approved,&approvedSum,&submittedBy,&approvedBy,&t.CreatedAt,&submitted,&approvedAt)
 if response.Valid{t.ResponseText=&response.String}
 if responseSum.Valid{t.ResponseSHA256=&responseSum.String}
 if approved.Valid{t.ApprovedText=&approved.String}
 if approvedSum.Valid{t.ApprovedSHA256=&approvedSum.String}
 if submittedBy.Valid{t.SubmittedBy=&submittedBy.String}
 if approvedBy.Valid{t.ApprovedBy=&approvedBy.String}
 if submitted.Valid{t.SubmittedAt=&submitted.Int64}
 if approvedAt.Valid{t.ApprovedAt=&approvedAt.Int64}
 return t,err
}
func (s *Service) ManualWebChairTurn(ctx context.Context,id string)(ManualWebChairTurn,error){
 return scanManualChair(s.db.QueryRowContext(ctx,manualChairSelect+" WHERE id=?",id))
}
func (s *Service) ManualWebChairTurns(ctx context.Context,workspace string)([]ManualWebChairTurn,error){
 if strings.TrimSpace(workspace)==""{return nil,ErrInvalid}
 rows,err:=s.db.QueryContext(ctx,manualChairSelect+`
 WHERE workspace_id=? AND EXISTS
 (SELECT 1 FROM team_sessions ss WHERE ss.id=manual_web_council_chair_turns.session_id AND ss.status!='cancelled')
 ORDER BY CASE status WHEN 'awaiting_input' THEN 0 WHEN 'awaiting_approval' THEN 1 ELSE 2 END,
 created_at DESC,id DESC LIMIT 100`,workspace)
 if err!=nil{return nil,err}
 defer rows.Close()
 out:=[]ManualWebChairTurn{}
 for rows.Next(){t,e:=scanManualChair(rows);if e!=nil{return nil,e};out=append(out,t)}
 return out,rows.Err()
}
// Idempotent under Tick retries: UNIQUE(session,stage,after_round).
func (s *Service) QueueManualWebChair(ctx context.Context,c QueueManualWebChairCommand)(ManualWebChairTurn,error){
 if c.Stage!="agenda"&&c.Stage!="review"{return ManualWebChairTurn{},ErrInvalid}
 if c.Stage=="agenda"&&c.AfterRound!=0 || c.Stage=="review"&&c.AfterRound<1{return ManualWebChairTurn{},ErrInvalid}
 provider,model,err:=ValidateManualWebProvider(c.ProviderID,c.ModelLabel)
 if err!=nil||strings.TrimSpace(c.Prompt)==""||len(c.Prompt)>160<<10{return ManualWebChairTurn{},ErrInvalid}
 var existingID string
 err=s.db.QueryRowContext(ctx,`SELECT id FROM manual_web_council_chair_turns WHERE session_id=? AND stage=? AND after_round=?`,c.SessionID,c.Stage,c.AfterRound).Scan(&existingID)
 if err==nil{return s.ManualWebChairTurn(ctx,existingID)}
 if err!=sql.ErrNoRows{return ManualWebChairTurn{},err}
 idv,err:=s.ids.New("tchair");if err!=nil{return ManualWebChairTurn{},err}
 now:=s.clock.UnixMilli()
 err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
   var status string
   var round int64
   if err:=tx.QueryRowContext(ctx,`SELECT status,round_number FROM team_sessions WHERE id=? AND workspace_id=?`,c.SessionID,c.WorkspaceID).Scan(&status,&round);err!=nil{return err}
   if status!="deliberating"||round!=c.AfterRound{return ErrSessionState}
   var snapshotRaw string
   if err:=tx.QueryRowContext(ctx,`SELECT snapshot_json FROM team_session_manifests WHERE session_id=? AND research_mode=1 AND execution_mode='council'`,c.SessionID).Scan(&snapshotRaw);err!=nil{return err}
   var snap SessionSnapshot
   if json.Unmarshal([]byte(snapshotRaw),&snap)!=nil||snap.Research.ChairMode!="manual"||snap.Research.ChairMemberID!=c.MemberID{return ErrInvalid}
   _,err:=tx.ExecContext(ctx,`INSERT INTO manual_web_council_chair_turns
    (id,session_id,workspace_id,member_id,provider_id,model_label,stage,after_round,prompt_text,prompt_sha256,status,created_at)
    VALUES(?,?,?,?,?,?,?,?,?,?,'awaiting_input',?)`,
     idv,c.SessionID,c.WorkspaceID,c.MemberID,provider,model,c.Stage,c.AfterRound,c.Prompt,manualWebDigest(c.Prompt),now)
   if err!=nil{return err}
   return s.emit(ctx,tx,c.WorkspaceID,"team.chair_turn_queued","team_session",c.SessionID,nil,
     map[string]any{"chair_turn_id":idv,"stage":c.Stage,"after_round":c.AfterRound,"prompt_sha256":manualWebDigest(c.Prompt)})
 })
 if err!=nil{
  // Another worker may have raced the insert; the unique key preserves exactly one turn.
  if e:=s.db.QueryRowContext(ctx,`SELECT id FROM manual_web_council_chair_turns WHERE session_id=? AND stage=? AND after_round=?`,c.SessionID,c.Stage,c.AfterRound).Scan(&existingID);e==nil{return s.ManualWebChairTurn(ctx,existingID)}
  return ManualWebChairTurn{},err
 }
 return s.ManualWebChairTurn(ctx,idv)
}
func(s *Service) chairActor(ctx context.Context,tx storage.Tx,ws,actor string)bool{
 if !s.isHuman(ctx,actor){return false}
 var status string
 return tx.QueryRowContext(ctx,`SELECT status FROM workspace_memberships WHERE workspace_id=? AND principal_id=?`,ws,actor).Scan(&status)==nil&&status=="active"
}
func(s *Service) SubmitManualWebChair(ctx context.Context,c SubmitManualWebChairCommand)(ManualWebChairTurn,error){
 response:=strings.TrimSpace(c.Response)
 if response==""||len(response)>256<<10{return ManualWebChairTurn{},ErrInvalid}
 now:=s.clock.UnixMilli()
 err:=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
   t,err:=scanManualChair(tx.QueryRowContext(ctx,manualChairSelect+" WHERE id=?",c.ID));if err!=nil{return err}
   if !s.chairActor(ctx,tx,t.WorkspaceID,c.Actor){return ErrHumanRequired}
   if t.Status!="awaiting_input"{return ErrSessionState}
   var round int64
   var status,snapshotRaw string
   if err:=tx.QueryRowContext(ctx,`SELECT ss.round_number,ss.status,m.snapshot_json FROM team_sessions ss
     JOIN team_session_manifests m ON m.session_id=ss.id WHERE ss.id=?`,t.SessionID).Scan(&round,&status,&snapshotRaw);err!=nil{return err}
   if status!="deliberating"||round!=t.AfterRound{return ErrSessionState}
   var snapshot SessionSnapshot
   if json.Unmarshal([]byte(snapshotRaw),&snapshot)!=nil||snapshot.Research.ChairMemberID!=t.MemberID{return ErrSessionState}
   requiresApproval:=snapshot.Research.ChairRequireApproval
   next:="awaiting_approval"
   if !requiresApproval{next="approved"}
   var approvedText,approvedSHA,approvedBy any
   var approvedAt any
   if !requiresApproval{approvedText=response;approvedSHA=manualWebDigest(response);approvedBy=c.Actor;approvedAt=now}
   changed,err:=tx.ExecContext(ctx,`UPDATE manual_web_council_chair_turns
    SET status=?,response_text=?,response_sha256=?,submitted_by=?,submitted_at=?,
     approved_text=?,approved_sha256=?,approved_by=?,approved_at=?
    WHERE id=? AND status='awaiting_input'`,
      next,response,manualWebDigest(response),c.Actor,now,approvedText,approvedSHA,approvedBy,approvedAt,t.ID)
   if err!=nil{return err}
   n,_:=changed.RowsAffected();if n!=1{return ErrSessionState}
   return s.emit(ctx,tx,t.WorkspaceID,"team.chair_response_submitted","team_session",t.SessionID,&c.Actor,
      map[string]any{"chair_turn_id":t.ID,"stage":t.Stage,"response_sha256":manualWebDigest(response),"approval_required":requiresApproval})
 })
 if err!=nil{return ManualWebChairTurn{},err}
 return s.ManualWebChairTurn(ctx,c.ID)
}
func(s *Service) ApproveManualWebChair(ctx context.Context,c ApproveManualWebChairCommand)(ManualWebChairTurn,error){
 text:=strings.TrimSpace(c.ApprovedText)
 if text==""||len(text)>256<<10{return ManualWebChairTurn{},ErrInvalid}
 now:=s.clock.UnixMilli()
 err:=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
   t,err:=scanManualChair(tx.QueryRowContext(ctx,manualChairSelect+" WHERE id=?",c.ID));if err!=nil{return err}
   if !s.chairActor(ctx,tx,t.WorkspaceID,c.Actor){return ErrHumanRequired}
   if t.Status!="awaiting_approval"{return ErrSessionState}
   var round int64
   var status string
   if err:=tx.QueryRowContext(ctx,`SELECT round_number,status FROM team_sessions WHERE id=?`,t.SessionID).Scan(&round,&status);err!=nil{return err}
   if status!="deliberating"||round!=t.AfterRound{return ErrSessionState}
   changed,err:=tx.ExecContext(ctx,`UPDATE manual_web_council_chair_turns SET
     status='approved',approved_text=?,approved_sha256=?,approved_by=?,approved_at=?
     WHERE id=? AND status='awaiting_approval'`,text,manualWebDigest(text),c.Actor,now,t.ID)
   if err!=nil{return err}
   n,_:=changed.RowsAffected();if n!=1{return ErrSessionState}
   return s.emit(ctx,tx,t.WorkspaceID,"team.chair_proposal_approved","team_session",t.SessionID,&c.Actor,
       map[string]any{"chair_turn_id":t.ID,"stage":t.Stage,"approved_sha256":manualWebDigest(text),"edited":t.ResponseText!=nil&&*t.ResponseText!=text})
 })
 if err!=nil{return ManualWebChairTurn{},err}
 return s.ManualWebChairTurn(ctx,c.ID)
}
