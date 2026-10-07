package api

import (
 "net/http"
 "strings"
 "time"
)

type attentionDisposition struct {
 AlertID string `json:"alert_id"`
 Disposition string `json:"disposition"`
 UpdatedAt int64 `json:"updated_at"`
}

// These records are a user-specific presentation layer. Their source events
// and task/installation failures are never removed or rewritten.
func (s *Server) attentionDispositions(w http.ResponseWriter,r *http.Request) {
 identity,ok:=s.authenticate(w,r);if !ok{return}
 workspace:=strings.TrimSpace(r.URL.Query().Get("workspace_id"))
 if workspace==""{writeError(w,http.StatusBadRequest,"workspace_id is required");return}
 permission:="events.read"
 if r.Method==http.MethodPost{permission="task.write"}
 if !s.authorize(w,r,identity,workspace,permission){return}
 if s.attentionDB==nil{writeError(w,http.StatusServiceUnavailable,"attention storage unavailable");return}
 if r.Method==http.MethodGet{
  rows,err:=s.attentionDB.QueryContext(r.Context(),"SELECT alert_id,disposition,updated_at FROM ui_attention_dispositions WHERE workspace_id=? AND principal_id=? ORDER BY updated_at DESC LIMIT 1000",workspace,identity.PrincipalID)
  if err!=nil{writeError(w,http.StatusInternalServerError,"could not read attention dispositions");return}
  defer rows.Close()
  result:=make([]attentionDisposition,0)
  for rows.Next(){var v attentionDisposition;if err:=rows.Scan(&v.AlertID,&v.Disposition,&v.UpdatedAt);err!=nil{writeError(w,http.StatusInternalServerError,"could not read attention record");return};result=append(result,v)}
  if err:=rows.Err();err!=nil{writeError(w,http.StatusInternalServerError,"could not read attention records");return}
  writeJSON(w,http.StatusOK,map[string]any{"items":result});return
 }
 if r.Method!=http.MethodPost{writeError(w,http.StatusMethodNotAllowed,"method not allowed");return}
 var in struct{AlertID string `json:"alert_id"`;Disposition string `json:"disposition"`}
 if !decodeJSON(w,r,&in){return}
 in.AlertID=strings.TrimSpace(in.AlertID);in.Disposition=strings.TrimSpace(in.Disposition)
 if len(in.AlertID)<4||len(in.AlertID)>256||strings.ContainsAny(in.AlertID,"\n\r\x00"){writeError(w,http.StatusBadRequest,"invalid attention id");return}
 if in.Disposition!="active"&&in.Disposition!="acknowledged"&&in.Disposition!="archived"{writeError(w,http.StatusBadRequest,"invalid disposition");return}
 now:=time.Now().UnixMilli()
 tx,err:=s.attentionDB.BeginTx(r.Context(),nil)
 if err!=nil{writeError(w,http.StatusInternalServerError,"attention write unavailable");return}
 defer tx.Rollback()
 _,err=tx.ExecContext(r.Context(),`INSERT INTO ui_attention_dispositions(workspace_id,principal_id,alert_id,disposition,updated_at)
 VALUES(?,?,?,?,?) ON CONFLICT(workspace_id,principal_id,alert_id) DO UPDATE SET disposition=excluded.disposition,updated_at=excluded.updated_at`,workspace,identity.PrincipalID,in.AlertID,in.Disposition,now)
 if err==nil{_,err=tx.ExecContext(r.Context(),"INSERT INTO ui_attention_disposition_history(workspace_id,principal_id,alert_id,disposition,changed_at) VALUES(?,?,?,?,?)",workspace,identity.PrincipalID,in.AlertID,in.Disposition,now)}
 if err!=nil{writeError(w,http.StatusInternalServerError,"could not update attention disposition");return}
 if err=tx.Commit();err!=nil{writeError(w,http.StatusInternalServerError,"could not commit attention disposition");return}
 writeJSON(w,http.StatusOK,attentionDisposition{AlertID:in.AlertID,Disposition:in.Disposition,UpdatedAt:now})
}
