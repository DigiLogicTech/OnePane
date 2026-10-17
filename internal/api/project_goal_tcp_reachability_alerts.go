package api

import (
 "context"
 "net/http"
 "strings"

 "github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

type tcpReachabilityAlertService interface{
 ListTCPReachabilityAlerts(context.Context,string,string)([]projectworkspace.TCPReachabilityAlert,error)
 AcknowledgeTCPReachabilityAlert(context.Context,projectworkspace.AcknowledgeTCPReachabilityAlertCommand)(projectworkspace.TCPReachabilityAlert,error)
}

// A TCP-baseline drift alert is NOT a confirmed cyber incident.
// Acknowledgement is NOT authorisation for autonomous remediation.
func(s *Server)listProjectGoalTCPReachabilityAlerts(w http.ResponseWriter,r *http.Request){
 identity,ok:=s.authenticate(w,r)
 if !ok{return}
 if s.projects==nil{writeError(w,http.StatusServiceUnavailable,"Project service unavailable");return}
 project,err:=s.projects.Project(r.Context(),strings.TrimSpace(r.PathValue("projectID")))
 if err!=nil{respondDomain(w,nil,err,0);return}
 if !s.authorize(w,r,identity,project.WorkspaceID,"project.read"){return}
 svc,ok:=s.projects.(tcpReachabilityAlertService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"TCP drift alerts unavailable");return}
 out,err:=svc.ListTCPReachabilityAlerts(r.Context(),project.ID,strings.TrimSpace(r.PathValue("goalID")))
 respondDomain(w,out,err,http.StatusOK)
}

func(s *Server)acknowledgeProjectGoalTCPReachabilityAlert(w http.ResponseWriter,r *http.Request){
 identity,ok:=s.authenticate(w,r)
 if !ok{return}
 if s.projects==nil{writeError(w,http.StatusServiceUnavailable,"Project service unavailable");return}
 project,err:=s.projects.Project(r.Context(),strings.TrimSpace(r.PathValue("projectID")))
 if err!=nil{respondDomain(w,nil,err,0);return}
 if !s.authorize(w,r,identity,project.WorkspaceID,"project.write"){return}
 svc,ok:=s.projects.(tcpReachabilityAlertService)
 if !ok{writeError(w,http.StatusServiceUnavailable,"TCP drift alerts unavailable");return}
 var body struct {
  ExpectedRevision int64 `json:"expected_revision"`
 }
 if !decodeJSON(w,r,&body){return}
 out,err:=svc.AcknowledgeTCPReachabilityAlert(r.Context(),
  projectworkspace.AcknowledgeTCPReachabilityAlertCommand{
   ProjectID:project.ID,GoalID:strings.TrimSpace(r.PathValue("goalID")),
   AlertID:strings.TrimSpace(r.PathValue("alertID")),
   ExpectedRevision:body.ExpectedRevision,ActorPrincipalID:identity.PrincipalID,
  })
 respondDomain(w,out,err,http.StatusOK)
}
