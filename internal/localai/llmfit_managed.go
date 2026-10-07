package localai

import (
 "context"
 "errors"
 "fmt"
 "net/http"
 "os"
 "os/exec"
 "path/filepath"
 "runtime"
 "strings"
 "sync"
 "time"
)

// Managed llmfit is an optional, pinned, local-only advisory service. It does
// not execute downloaded models or take over OnePane scheduling.
const llmfitManagedVersion="1.1.16"
const llmfitManagedURL="https://github.com/AlexsJones/llmfit/releases/download/v1.1.16/llmfit-v1.1.16-x86_64-pc-windows-msvc.zip"
const llmfitManagedSHA="bd95bc78e65a15f4d7b62431c082e0d55c9505739f63f6fdf6f9d69605c270ba"
const llmfitManagedEndpoint="http://127.0.0.1:8787"

var llmfitManagedLock sync.Mutex

type ManagedLLMFitStatus struct{
 Version string `json:"version"`
 Installed bool `json:"installed"`
 Running bool `json:"running"`
 Endpoint string `json:"endpoint"`
 Supported bool `json:"supported"`
 Message string `json:"message,omitempty"`
}
func (s *Service) managedLLMFitRoot()string{return filepath.Join(s.dataDir,"runtimes","llmfit",llmfitManagedVersion)}
func (s *Service) managedLLMFitExe()string{return filepath.Join(s.managedLLMFitRoot(),"llmfit.exe")}
func llmfitHealth(ctx context.Context)bool{
 c:=&http.Client{Timeout:1400*time.Millisecond}
 req,err:=http.NewRequestWithContext(ctx,"GET",llmfitManagedEndpoint+"/health",nil)
 if err!=nil{return false}
 resp,err:=c.Do(req);if err!=nil{return false}
 defer resp.Body.Close()
 return resp.StatusCode==200
}
func (s *Service) ManagedLLMFit(ctx context.Context)(ManagedLLMFitStatus,error){
 st:=ManagedLLMFitStatus{Version:llmfitManagedVersion,Endpoint:llmfitManagedEndpoint,Supported:runtime.GOOS=="windows"&&runtime.GOARCH=="amd64"}
 if stat,err:=os.Stat(s.managedLLMFitExe());err==nil&&!stat.IsDir(){st.Installed=true}
 st.Running=llmfitHealth(ctx)
 if !st.Supported {st.Message="The managed llmfit runtime is currently Windows x64 only. Configure an external llmfit service on other nodes."} else if !st.Installed {st.Message="Install the verified llmfit advisory runtime to enable hardware-fit discovery."} else if !st.Running {st.Message="Installed; start llmfit to load its model catalogue."} else {st.Message="llmfit REST API is healthy."}
 return st,nil
}
func (s *Service) InstallManagedLLMFit(ctx context.Context)error{
 llmfitManagedLock.Lock();defer llmfitManagedLock.Unlock()
 if runtime.GOOS!="windows"||runtime.GOARCH!="amd64"{return errors.New("managed llmfit is available on Windows x64 only")}
 if llmfitHealth(ctx){return errors.New("stop llmfit before replacing the runtime")}
 if stat,err:=os.Stat(s.managedLLMFitExe());err==nil&&!stat.IsDir(){return nil}
 root:=s.managedLLMFitRoot()
 staging:=root+".installing"
 _=os.RemoveAll(staging)
 defer os.RemoveAll(staging)
 dl:=filepath.Join(s.dataDir,"components","downloads","llmfit-"+llmfitManagedVersion+".zip")
 if _,err:=s.fetcher.Fetch(ctx,llmfitManagedURL,dl,llmfitManagedSHA);err!=nil{return fmt.Errorf("download verified llmfit: %w",err)}
 if err:=ExtractRuntimeArchive(dl,"zip",staging);err!=nil{return err}
 if err:=normalizeLLMFitExecutable(staging);err!=nil{return err}
 if err:=os.MkdirAll(filepath.Dir(root),0o700);err!=nil{return err}
 if err:=os.Rename(staging,root);err!=nil{return err}
 _=os.Remove(dl)
 // Installation and configuration must agree even after a service restart.
 s.llmfit,_=NewLLMFitClient(llmfitManagedEndpoint)
 return nil
}

// A process launched by the current harness instance is owned and can be
// stopped safely. Never kill an unrelated process simply because it listens
// on the standard llmfit loopback port.
var managedLLMFitProcesses=struct{sync.Mutex;process *os.Process}{}
func (s *Service) StartManagedLLMFit(ctx context.Context)error{
 llmfitManagedLock.Lock();defer llmfitManagedLock.Unlock()
 st,err:=s.ManagedLLMFit(ctx);if err!=nil{return err}
 if !st.Installed{return errors.New("install llmfit before starting it")}
 if st.Running{
  if s.llmfit==nil{s.llmfit,_=NewLLMFitClient(llmfitManagedEndpoint)}
  return nil
 }
 cmd:=exec.Command(s.managedLLMFitExe(),"serve","--host","127.0.0.1","--port","8787")
 cmd.Dir=s.managedLLMFitRoot()
 logDir:=filepath.Join(s.dataDir,"components","logs")
 if err:=os.MkdirAll(logDir,0o700);err!=nil{return err}
 log,err:=os.OpenFile(filepath.Join(logDir,"llmfit.log"),os.O_CREATE|os.O_APPEND|os.O_WRONLY,0o600)
 if err!=nil{return err}
 cmd.Stdout=log;cmd.Stderr=log
 if err:=cmd.Start();err!=nil{_ = log.Close();return err}
 _=log.Close()
 managedLLMFitProcesses.Lock();managedLLMFitProcesses.process=cmd.Process;managedLLMFitProcesses.Unlock()
 go func(){_ = cmd.Wait();managedLLMFitProcesses.Lock();if managedLLMFitProcesses.process==cmd.Process{managedLLMFitProcesses.process=nil};managedLLMFitProcesses.Unlock()}()
 for i:=0;i<30;i++{
  if llmfitHealth(ctx){s.llmfit,_=NewLLMFitClient(llmfitManagedEndpoint);return nil}
  select{case <-ctx.Done():return ctx.Err();case <-time.After(300*time.Millisecond):}
 }
 _=cmd.Process.Kill()
 return errors.New("llmfit did not become healthy on 127.0.0.1:8787; inspect components/logs/llmfit.log")
}
func (s *Service) StopManagedLLMFit(ctx context.Context)error{
 llmfitManagedLock.Lock();defer llmfitManagedLock.Unlock()
 managedLLMFitProcesses.Lock()
 p:=managedLLMFitProcesses.process
 managedLLMFitProcesses.process=nil
 managedLLMFitProcesses.Unlock()
 if p==nil&&llmfitHealth(ctx){return errors.New("llmfit is running outside the current OnePane service; stop the external process before uninstall")}
 if p!=nil{if err:=p.Kill();err!=nil{return err}}
 s.llmfit=nil
 return nil
}
func (s *Service) RemoveManagedLLMFit(ctx context.Context)error{
 llmfitManagedLock.Lock();defer llmfitManagedLock.Unlock()
 if llmfitHealth(ctx){return errors.New("stop llmfit before uninstalling")}
 root:=s.managedLLMFitRoot()
 if !strings.HasPrefix(filepath.Clean(root),filepath.Clean(s.dataDir)+string(os.PathSeparator)){return errors.New("managed llmfit path is outside OnePane storage")}
 if err:=os.RemoveAll(root);err!=nil{return err}
 s.llmfit=nil
 return nil
}

func normalizeLLMFitExecutable(staging string)error{
 // Upstream ZIP nests llmfit.exe in llmfit-vX.Y.Z-windows-target/.
 // Never execute by untrusted path: relocate a single regular executable
 // from the already digest-verified and safely extracted archive.
 var candidate string
 err=filepath.WalkDir(staging,func(path string,d os.DirEntry,e error)error{
  if e!=nil{return e}
  if d.IsDir(){return nil}
  if strings.EqualFold(d.Name(),"llmfit.exe"){
   if d.Type()&os.ModeSymlink!=0{return errors.New("llmfit executable must not be a symlink")}
   if candidate!=""{return errors.New("verified archive contains multiple llmfit executables")}
   candidate=path
  }
  return nil
 })
 if err!=nil{return err}
 if candidate==""{return errors.New("llmfit.exe missing from verified archive")}
 if stat,err:=os.Stat(candidate);err!=nil||!stat.Mode().IsRegular(){return errors.New("invalid llmfit executable in verified archive")}
 if candidate!=filepath.Join(staging,"llmfit.exe"){
  if err:=os.Rename(candidate,filepath.Join(staging,"llmfit.exe"));err!=nil{return err}
 }
 return nil
}
