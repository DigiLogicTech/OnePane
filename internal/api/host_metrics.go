package api
import (
 "net/http"
 "strings"
 "github.com/DigiLogicTech/OnePane/internal/hostmetrics"
)
func (s *Server) hostMetricsRequest(w http.ResponseWriter,r *http.Request){
 actor,ok:=s.authenticate(w,r);if !ok{return}
 workspace:=strings.TrimSpace(r.URL.Query().Get("workspace_id"))
 if !s.authorize(w,r,actor,workspace,"model.read"){return}
 writeJSON(w,http.StatusOK,hostmetrics.Collect())
}
