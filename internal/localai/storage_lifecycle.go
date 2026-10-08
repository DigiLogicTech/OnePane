package localai

import (
 "context"
 "database/sql"
 "fmt"
 "io/fs"
 "os"
 "path/filepath"
 "strings"
 "time"
)

type StorageArea struct {
 Name string `json:"name"`
 Path string `json:"path"`
 Bytes int64 `json:"bytes"`
 Files int64 `json:"files"`
}
type StorageReport struct{
 Areas []StorageArea `json:"areas"`
 ReclaimedBytes int64 `json:"reclaimed_bytes,omitempty"`
 RemovedPaths []string `json:"removed_paths,omitempty"`
 Warning string `json:"warning,omitempty"`
}
func storageArea(name,path string) StorageArea {
 area:=StorageArea{Name:name,Path:path}
 _=filepath.WalkDir(path,func(p string,d fs.DirEntry,err error)error{
  if err!=nil{return filepath.SkipDir}
  if d.IsDir(){return nil}
  if d.Type()&os.ModeSymlink!=0{return nil}
  info,e:=d.Info();if e==nil&&info.Mode().IsRegular(){area.Bytes+=info.Size();area.Files++}
  return nil
 })
 return area
}
func (s *Service) ManagedStorageReport(ctx context.Context)(StorageReport,error){
 areas:=[]StorageArea{
  storageArea("Managed runtimes",filepath.Join(s.dataDir,"runtimes")),
  storageArea("Runtime downloads",filepath.Join(s.dataDir,"downloads")),
  storageArea("Component downloads",filepath.Join(s.dataDir,"components","downloads")),
  storageArea("Install caches",filepath.Join(s.dataDir,"components","install-cache")),
  storageArea("Component logs",filepath.Join(s.dataDir,"components","logs")),
  storageArea("Managed model pool",s.modelRoot),
 }
 return StorageReport{Areas:areas,Warning:"External Windows Temp, the legacy SYSTEM npm cache and user projects are excluded from automatic cleanup."},nil
}
func (s *Service) CleanupOwnedStorage(ctx context.Context)(StorageReport,error){
 var jobs int
 // Never remove staging or downloads while a component or model install is
 // active. Interrupted/resumable model jobs retain their .partial artifacts.
 if err:=s.db.QueryRowContext(ctx,"SELECT COUNT(*) FROM managed_component_jobs WHERE status IN ('queued','running')").Scan(&jobs);err!=nil{return StorageReport{},err}
 if jobs>0{return StorageReport{},fmt.Errorf("%d component lifecycle job(s) are active; cleanup is blocked",jobs)}
 if err:=s.db.QueryRowContext(ctx,"SELECT COUNT(*) FROM local_ai_install_jobs WHERE status IN ('queued','resolving','provisioning','starting','qualifying','interrupted')").Scan(&jobs);err!=nil&&err!=sql.ErrNoRows{return StorageReport{},err}
 if jobs>0{return StorageReport{},fmt.Errorf("%d model install job(s) are active or resumable; cleanup is blocked",jobs)}
 roots:=[]string{
  filepath.Join(s.dataDir,"components","install-cache","omniroute"),
 }
 // Node ZIP is only disposable after a managed Node runtime exists.
 if _,err:=os.Stat(filepath.Join(s.dataDir,"runtimes","omniroute","3.8.51","node-v22.22.2-win-x64","node.exe"));err==nil{
  roots=append(roots,filepath.Join(s.dataDir,"components","downloads","node-v22.22.2-win-x64.zip"))
 }
 // Disabled llama.cpp backends may retain Windows-locked DLL files after
 // uninstall. Reclaim their OnePane-owned runtime directories only after all
 // active process records have drained, never deleting the shared model pool.
 llamaRoot:=filepath.Clean(filepath.Join(s.dataDir,"runtimes","llamacpp"))
 disabled,err:=s.db.QueryContext(ctx,`SELECT r.install_root FROM managed_local_runtimes r
 WHERE r.runtime_name LIKE 'llamacpp@%' AND r.status='disabled'
 AND NOT EXISTS (
   SELECT 1 FROM local_runtime_instances i WHERE i.runtime_id=r.id
   AND i.status IN ('starting','healthy','busy','draining')
 )`)
 if err!=nil{return StorageReport{},err}
 var disabledPaths []string
 for disabled.Next(){
  var root string
  if err:=disabled.Scan(&root);err!=nil{disabled.Close();return StorageReport{},err}
  clean:=filepath.Clean(root)
  if strings.HasPrefix(strings.ToLower(clean),strings.ToLower(llamaRoot)+string(os.PathSeparator)){
   disabledPaths=append(disabledPaths,clean)
  }
 }
 if err:=disabled.Err();err!=nil{disabled.Close();return StorageReport{},err}
 disabled.Close()
 for _,root:=range disabledPaths{
  var ready int
  if err:=s.db.QueryRowContext(ctx,"SELECT COUNT(*) FROM managed_local_runtimes WHERE install_root=? AND status='ready'",root).Scan(&ready);err!=nil{return StorageReport{},err}
  if ready==0{roots=append(roots,root)}
 }
 // Clean abandoned installation staging older than 24h under owned runtimes.
 runtimeRoot:=filepath.Join(s.dataDir,"runtimes")
 _=filepath.WalkDir(runtimeRoot,func(p string,d fs.DirEntry,err error)error{
  if err!=nil{return filepath.SkipDir}
  if !d.IsDir(){return nil}
  if p==runtimeRoot{return nil}
  name:=strings.ToLower(d.Name())
  if strings.HasSuffix(name,".installing")||strings.HasSuffix(name,".previous"){
   if info,e:=d.Info();e==nil&&time.Since(info.ModTime())>24*time.Hour{
    roots=append(roots,p)
   }
   return filepath.SkipDir
  }
  return nil
 })
 report:=StorageReport{RemovedPaths:[]string{},Warning:"User projects, model weights, resumable downloads and external system caches were preserved."}
 rootBoundary:=strings.ToLower(filepath.Clean(s.dataDir))+string(os.PathSeparator)
 for _,path:=range roots{
  clean:=filepath.Clean(path)
  if !strings.HasPrefix(strings.ToLower(clean),rootBoundary){continue}
  before:=storageArea("cleanup",clean)
  if _,err:=os.Lstat(clean);os.IsNotExist(err){continue}else if err!=nil{return report,err}
  if err:=os.RemoveAll(clean);err!=nil{return report,err}
  report.ReclaimedBytes+=before.Bytes
  report.RemovedPaths=append(report.RemovedPaths,clean)
 }
 after,err:=s.ManagedStorageReport(ctx)
 if err!=nil{return report,err}
 report.Areas=after.Areas
 return report,nil
}
