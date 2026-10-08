package api
import (
 "net/http"
 "strings"
 "github.com/DigiLogicTech/OnePane/internal/hostmetrics"
 "github.com/DigiLogicTech/OnePane/internal/config"
)
func (s *Server) hostMetricsRequest(w http.ResponseWriter,r *http.Request){
 actor,ok:=s.authenticate(w,r);if !ok{return}
 workspace:=strings.TrimSpace(r.URL.Query().Get("workspace_id"))
 if !s.authorize(w,r,actor,workspace,"model.read"){return}
 paths:=map[string]string{"Models":s.modelPoolPath}
 if s.configPath!="" {
  if cfg,err:=config.Load(s.configPath);err==nil{
   if cfg.LocalAI.ModelPoolPath!=""{paths["Models"]=cfg.LocalAI.ModelPoolPath}
   paths["Projects"]=cfg.Storage.ProjectRoot
  }
 }
 writeJSON(w,http.StatusOK,hostmetrics.CollectWithPaths(paths))
}
