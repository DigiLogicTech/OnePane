package api

import (
    "context"
    "net/http"
    "strings"

    "github.com/DigiLogicTech/OnePane/internal/team"
)

// This is an optional extension of teamService to preserve mock compatibility.
type manualWebCouncilService interface {
    ManualWebTurn(context.Context,string) (team.ManualWebTurn,error)
    ManualWebTurns(context.Context,string) ([]team.ManualWebTurn,error)
    RestartManualWebConversation(context.Context,string,string) (team.ManualWebTurn,error)
    SubmitManualWebResponse(context.Context,team.SubmitManualWebResponseCommand) (team.ManualWebTurn,error)
}
func (s *Server) manualWebService(w http.ResponseWriter) (manualWebCouncilService,bool) {
    if s.team==nil {
        writeError(w,http.StatusServiceUnavailable,"Council service unavailable")
        return nil,false
    }
    extension,ok:=s.team.(manualWebCouncilService)
    if !ok {
        writeError(w,http.StatusServiceUnavailable,"Manual Web Council unavailable")
        return nil,false
    }
    return extension,true
}
func (s *Server) listManualWebTurns(w http.ResponseWriter,r *http.Request) {
    i,ok:=s.authenticate(w,r);if !ok{return}
    ws:=strings.TrimSpace(r.URL.Query().Get("workspace_id"))
    if ws=="" {writeError(w,http.StatusBadRequest,"workspace_id is required");return}
    if !s.authorize(w,r,i,ws,"team.read"){return}
    manual,ready:=s.manualWebService(w);if !ready{return}
    rows,err:=manual.ManualWebTurns(r.Context(),ws)
    respondDomain(w,rows,err,http.StatusOK)
}
func (s *Server) manualWebTurnFor(w http.ResponseWriter,r *http.Request,cap string) (manualWebCouncilService,team.ManualWebTurn,string,bool) {
    i,ok:=s.authenticate(w,r);if !ok{return nil,team.ManualWebTurn{},"",false}
    manual,ready:=s.manualWebService(w);if !ready{return nil,team.ManualWebTurn{},"",false}
    turn,err:=manual.ManualWebTurn(r.Context(),strings.TrimSpace(r.PathValue("turnID")))
    if err!=nil {respondDomain(w,nil,err,0);return nil,team.ManualWebTurn{},"",false}
    if !s.authorize(w,r,i,turn.WorkspaceID,cap){return nil,team.ManualWebTurn{},"",false}
    return manual,turn,i.PrincipalID,true
}
func (s *Server) restartManualWebTurn(w http.ResponseWriter,r *http.Request) {
    manual,turn,actor,ok:=s.manualWebTurnFor(w,r,"team.write");if !ok{return}
    var in struct{ ExpectedGeneration int64 `json:"conversation_generation"` }
    if !decodeJSON(w,r,&in){return}
    if in.ExpectedGeneration!=turn.ConversationGeneration {
        writeError(w,http.StatusConflict,"stale conversation handoff");return
    }
    updated,err:=manual.RestartManualWebConversation(r.Context(),turn.TurnID,actor)
    if err!=nil {respondDomain(w,nil,err,0);return}
    writeJSON(w,http.StatusOK,updated)
}
func (s *Server) submitManualWebTurn(w http.ResponseWriter,r *http.Request) {
    manual,turn,actor,ok:=s.manualWebTurnFor(w,r,"team.write");if !ok{return}
    var in struct{
        ResponseText string `json:"response_text"`
        ConversationGeneration int64 `json:"conversation_generation"`
    }
    if !decodeJSON(w,r,&in){return}
    if in.ConversationGeneration!=turn.ConversationGeneration{
        writeError(w,http.StatusConflict,"stale conversation handoff");return
    }
    out,err:=manual.SubmitManualWebResponse(r.Context(),team.SubmitManualWebResponseCommand{
        TurnID:turn.TurnID,SubmittedBy:actor,ResponseText:in.ResponseText,ConversationGeneration:in.ConversationGeneration,
    })
    if err!=nil{respondDomain(w,nil,err,0);return}
    writeJSON(w,http.StatusOK,out)
}
