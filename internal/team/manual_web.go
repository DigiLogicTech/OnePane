package team

import (
    "context"
    "crypto/sha256"
    "database/sql"
    "encoding/hex"
    "encoding/json"
    "strings"

    "github.com/DigiLogicTech/OnePane/internal/storage"
)

// Manual Web Council seats carry operator-attested output from an external
// provider website. OnePane does not automate websites or grant tool authority.
type ManualWebTurn struct {
    TurnID string `json:"turn_id"`
    SessionID string `json:"session_id"`
    WorkspaceID string `json:"workspace_id"`
    MemberID string `json:"member_id"`
    ProviderID string `json:"provider_id"`
    ModelLabel string `json:"model_label"`
    PromptText string `json:"prompt_text"`
    PromptSHA256 string `json:"prompt_sha256"`
    ConversationGeneration int64 `json:"conversation_generation"`
    Status string `json:"status"`
    ResponseSHA256 *string `json:"response_sha256,omitempty"`
    SubmittedBy *string `json:"submitted_by,omitempty"`
    CreatedAt int64 `json:"created_at"`
    SubmittedAt *int64 `json:"submitted_at,omitempty"`
}
type QueueManualWebTurnCommand struct {
    TurnID, SessionID, WorkspaceID, MemberID, ProviderID, ModelLabel, Prompt string
}
type SubmitManualWebResponseCommand struct {
    TurnID, SubmittedBy, ResponseText string
    ConversationGeneration int64
}
func manualWebDigest(content string) string {
    h:=sha256.Sum256([]byte(content))
    return hex.EncodeToString(h[:])
}
func ValidateManualWebProvider(provider, model string) (string,string,error) {
    provider=strings.ToLower(strings.TrimSpace(provider))
    model=strings.TrimSpace(model)
    if provider==""||len(provider)>48||model==""||len(model)>128||strings.ContainsAny(model,"\r\n") {
        return "","",ErrInvalid
    }
    for _,c:=range provider {
        if !(c>='a'&&c<='z'||c>='0'&&c<='9'||c=='-'||c=='_') {return "","",ErrInvalid}
    }
    return provider,model,nil
}
func scanManualWebTurn(row interface{Scan(...any)error}) (ManualWebTurn,error) {
    var t ManualWebTurn
    var response,principal sql.NullString
    var submitted sql.NullInt64
    err:=row.Scan(&t.TurnID,&t.SessionID,&t.WorkspaceID,&t.MemberID,&t.ProviderID,&t.ModelLabel,&t.PromptText,&t.PromptSHA256,
        &t.ConversationGeneration,&t.Status,&response,&principal,&t.CreatedAt,&submitted)
    if response.Valid{t.ResponseSHA256=&response.String}
    if principal.Valid{t.SubmittedBy=&principal.String}
    if submitted.Valid{t.SubmittedAt=&submitted.Int64}
    return t,err
}
const manualWebTurnSelect = `SELECT turn_id,session_id,workspace_id,member_id,provider_id,model_label,prompt_text,prompt_sha256,
    conversation_generation,status,response_sha256,submitted_by,created_at,submitted_at FROM manual_web_council_turns`

func (s *Service) ManualWebTurn(ctx context.Context, turnID string) (ManualWebTurn,error) {
    return scanManualWebTurn(s.db.QueryRowContext(ctx,manualWebTurnSelect+" WHERE turn_id=?",turnID))
}
func (s *Service) ManualWebTurns(ctx context.Context, workspaceID string) ([]ManualWebTurn,error) {
    if strings.TrimSpace(workspaceID)=="" {return nil,ErrInvalid}
    rows,err:=s.db.QueryContext(ctx,manualWebTurnSelect+`
        WHERE workspace_id=? AND EXISTS (
          SELECT 1 FROM team_sessions ss WHERE ss.id=manual_web_council_turns.session_id
          AND ss.status NOT IN ('cancelled'))
        ORDER BY CASE WHEN status='awaiting_input' THEN 0 ELSE 1 END,
        created_at DESC,turn_id LIMIT 100`,workspaceID)
    if err!=nil{return nil,err}
    defer rows.Close()
    out:=[]ManualWebTurn{}
    for rows.Next(){
        m,err:=scanManualWebTurn(rows)
        if err!=nil{return nil,err}
        out=append(out,m)
    }
    return out,rows.Err()
}

// Invoked by TeamWorker on the exact running Council turn. A failed write
// rolls back the turn state and the handoff atomically.
func (s *Service) QueueManualWebTurn(ctx context.Context,c QueueManualWebTurnCommand) (ManualWebTurn,error) {
    provider,model,err:=ValidateManualWebProvider(c.ProviderID,c.ModelLabel)
    if err!=nil{return ManualWebTurn{},err}
    if strings.TrimSpace(c.Prompt)==""||len(c.Prompt)>160<<10{return ManualWebTurn{},ErrInvalid}
    now:=s.clock.UnixMilli()
    sha:=manualWebDigest(c.Prompt)
    err=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
        changed,err:=tx.ExecContext(ctx,`UPDATE team_turn_requests SET status='blocked',
            error_text='Awaiting manual Web Chat response',updated_at=?
            WHERE id=? AND session_id=? AND workspace_id=? AND member_id=? AND status='running'
            AND response_message_id IS NULL AND EXISTS
            (SELECT 1 FROM team_sessions ss WHERE ss.id=team_turn_requests.session_id
             AND ss.round_number=team_turn_requests.round_number AND ss.status='deliberating')`,
            now,c.TurnID,c.SessionID,c.WorkspaceID,c.MemberID)
        if err!=nil{return err}
        n,_:=changed.RowsAffected()
        if n!=1{return ErrSessionState}
        if _,err=tx.ExecContext(ctx,`INSERT INTO manual_web_council_turns
            (turn_id,session_id,workspace_id,member_id,provider_id,model_label,prompt_text,prompt_sha256,
             conversation_generation,status,created_at) VALUES(?,?,?,?,?,?,?,?,1,'awaiting_input',?)`,
             c.TurnID,c.SessionID,c.WorkspaceID,c.MemberID,provider,model,c.Prompt,sha,now);err!=nil{return err}
        return s.emit(ctx,tx,c.WorkspaceID,"team.manual_web_awaiting_input","team_session",c.SessionID,nil,
            map[string]any{"turn_id":c.TurnID,"member_id":c.MemberID,"provider_id":provider,"model_label":model,"prompt_sha256":sha})
    })
    if err!=nil{return ManualWebTurn{},err}
    return s.ManualWebTurn(ctx,c.TurnID)
}

// New Conversation means restart the external conversation manually and send
// the same immutable prompt packet. The Council seat/round does not change.
func (s *Service) RestartManualWebConversation(ctx context.Context,turnID,actor string) (ManualWebTurn,error) {
    if !s.isHuman(ctx,actor){return ManualWebTurn{},ErrHumanRequired}
    now:=s.clock.UnixMilli()
    err:=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
        updated,err:=tx.ExecContext(ctx,`UPDATE manual_web_council_turns
            SET conversation_generation=conversation_generation+1
            WHERE turn_id=? AND status='awaiting_input'
              AND EXISTS(SELECT 1 FROM team_turn_requests tr JOIN team_sessions ss ON ss.id=tr.session_id
                WHERE tr.id=manual_web_council_turns.turn_id AND tr.status='blocked'
                AND ss.status='deliberating')`,turnID)
        if err!=nil{return err}
        n,_:=updated.RowsAffected()
        if n!=1{return ErrSessionState}
        t,err:=scanManualWebTurn(tx.QueryRowContext(ctx,manualWebTurnSelect+" WHERE turn_id=?",turnID))
        if err!=nil{return err}
        if !s.workspaceMember(ctx,t.WorkspaceID,actor){return ErrInvalid}
        return s.emit(ctx,tx,t.WorkspaceID,"team.manual_web_new_conversation","team_session",t.SessionID,&actor,
            map[string]any{"turn_id":t.TurnID,"conversation_generation":t.ConversationGeneration})
    })
    if err!=nil{return ManualWebTurn{},err}
    return s.ManualWebTurn(ctx,turnID)
}

func (s *Service) SubmitManualWebResponse(ctx context.Context,c SubmitManualWebResponseCommand) (ManualWebTurn,error) {
    if !s.isHuman(ctx,c.SubmittedBy){return ManualWebTurn{},ErrHumanRequired}
    response:=strings.TrimSpace(c.ResponseText)
    if response==""||len(response)>256<<10||c.ConversationGeneration<1{return ManualWebTurn{},ErrInvalid}
    now:=s.clock.UnixMilli()
    digest:=manualWebDigest(response)
    err:=s.tx.Within(ctx,func(ctx context.Context,tx storage.Tx)error{
        t,err:=scanManualWebTurn(tx.QueryRowContext(ctx,manualWebTurnSelect+" WHERE turn_id=?",c.TurnID))
        if err!=nil{return err}
        if !s.workspaceMember(ctx,t.WorkspaceID,c.SubmittedBy){return ErrInvalid}
        if t.Status!="awaiting_input"||t.ConversationGeneration!=c.ConversationGeneration{return ErrSessionState}
        var round int64
        if err=tx.QueryRowContext(ctx,`SELECT tr.round_number FROM team_turn_requests tr
            JOIN team_sessions ss ON ss.id=tr.session_id
            WHERE tr.id=? AND tr.status='blocked' AND tr.response_message_id IS NULL
              AND ss.status='deliberating' AND ss.round_number=tr.round_number`,t.TurnID).Scan(&round);err!=nil{return ErrSessionState}
        mid,err:=s.ids.New("tmsg")
        if err!=nil{return err}
        content,_:=json.Marshal(map[string]any{
            "text":response, "source":"manual_web","provider_id":t.ProviderID,"model_label":t.ModelLabel,
            "model_identity":"operator_attested", "submitted_by":c.SubmittedBy,
            "prompt_sha256":t.PromptSHA256,"response_sha256":digest,
            "conversation_generation":t.ConversationGeneration,"round":round,
            "deliberation_only":true,"no_tools":true,
        })
        if _,err=tx.ExecContext(ctx,`INSERT INTO team_messages(id,workspace_id,session_id,author_member_id,
          author_principal_id,message_kind,content_json,round_number,created_at)
          VALUES(?,?,?,?,?,'agent',?,?,?)`,
            mid,t.WorkspaceID,t.SessionID,t.MemberID,c.SubmittedBy,string(content),round,now);err!=nil{return err}
        changed,err:=tx.ExecContext(ctx,`UPDATE team_turn_requests SET status='succeeded',response_message_id=?,
          error_text=NULL,updated_at=?,completed_at=? WHERE id=? AND status='blocked'
          AND response_message_id IS NULL`,mid,now,now,t.TurnID)
        if err!=nil{return err}
        n,_:=changed.RowsAffected()
        if n!=1{return ErrSessionState}
        changed,err=tx.ExecContext(ctx,`UPDATE manual_web_council_turns
          SET status='submitted',response_text=?,response_sha256=?,submitted_by=?,submitted_at=?
          WHERE turn_id=? AND status='awaiting_input' AND conversation_generation=?`,
          response,digest,c.SubmittedBy,now,t.TurnID,t.ConversationGeneration)
        if err!=nil{return err}
        n,_=changed.RowsAffected()
        if n!=1{return ErrSessionState}
        return s.emit(ctx,tx,t.WorkspaceID,"team.manual_web_submitted","team_session",t.SessionID,&c.SubmittedBy,
            map[string]any{"turn_id":t.TurnID,"member_id":t.MemberID,"provider_id":t.ProviderID,
              "model_label":t.ModelLabel,"prompt_sha256":t.PromptSHA256,"response_sha256":digest,
              "conversation_generation":t.ConversationGeneration})
    })
    if err!=nil{return ManualWebTurn{},err}
    return s.ManualWebTurn(ctx,c.TurnID)
}
