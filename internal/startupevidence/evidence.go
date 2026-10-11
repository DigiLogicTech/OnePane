// Package startupevidence records strict, typed, offline startup breadcrumbs.
// The API does not need to be running. Never pass user text, an error, a path,
// environment variables, process args, or secrets to the evidence format.
package startupevidence

import (
 "bufio"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "os"
 "path/filepath"
 "runtime"
 "sort"
 "sync"
 "time"
)

const MaxFileBytes int64 = 32 << 10

type Component string
const (
 Backend Component = "backend"
 WindowsService Component = "windows_service"
 LegacySetup Component = "legacy_setup"
)
type Stage string
const (
 Config Stage = "configuration"
 Bootstrap Stage = "bootstrap"
 State Stage = "state"
 API Stage = "api_listeners"
 ServiceLaunch Stage = "service_launch"
 ServiceReady Stage = "service_readiness"
 ServiceStop Stage = "service_stop"
 Installer Stage = "installer"
 Payload Stage = "payload"
 Rollback Stage = "rollback"
 ServiceRegister Stage = "service_registration"
)
type Outcome string
const (
 Begin Outcome = "begin"
 OK Outcome = "ok"
 Failed Outcome = "failed"
 Stopped Outcome = "stopped"
)
type Event struct {
 Schema int `json:"schema"`
 AtUTC string `json:"at_utc"`
 Component Component `json:"component"`
 Stage Stage `json:"stage"`
 Outcome Outcome `json:"outcome"`
}
var mu sync.Mutex

func allowedComponent(v Component) bool {switch v {case Backend,WindowsService,LegacySetup:return true};return false}
func allowedStage(v Stage) bool {switch v {case Config,Bootstrap,State,API,ServiceLaunch,ServiceReady,ServiceStop,Installer,Payload,Rollback,ServiceRegister:return true};return false}
func allowedOutcome(v Outcome) bool {switch v {case Begin,OK,Failed,Stopped:return true};return false}

// evidenceDir refuses symlinked diagnostics directories and nonprivate
// existing directories; it never creates or edits a missing data root.
func evidenceDir(root string, create bool) (string,error) {
 if !filepath.IsAbs(root) {return "", errors.New("absolute data root required")}
 st,err:=os.Lstat(root)
 if err!=nil{return "",err}
 if !st.IsDir()||st.Mode()&os.ModeSymlink!=0{return "",errors.New("data root is not a real directory")}
 dir:=filepath.Join(filepath.Clean(root),"diagnostics")
 if create {
  if err:=os.Mkdir(dir,0700);err!=nil&&!os.IsExist(err){return "",err}
 }
 st,err=os.Lstat(dir)
 if err!=nil{return "",err}
 if !st.IsDir()||st.Mode()&os.ModeSymlink!=0{return "",errors.New("diagnostics path is not a real directory")}
 if runtime.GOOS!="windows"&&st.Mode().Perm()&0077!=0{return "",errors.New("diagnostics directory is not private")}
 return dir,nil
}
func eventFile(dir string,component Component) string {
 return filepath.Join(dir,string(component)+".jsonl")
}
func regularOrMissing(path string) error {
 st,err:=os.Lstat(path)
 if os.IsNotExist(err){return nil}
 if err!=nil{return err}
 if !st.Mode().IsRegular()||st.Mode()&os.ModeSymlink!=0{return errors.New("invalid evidence file type")}
 return nil
}

// Record is best effort from a caller perspective; callers must not allow a
// diagnostic filesystem failure to interrupt startup or installer rollback.
// Each component owns a separate bounded file plus one bounded prior segment.
func Record(root string,component Component,stage Stage,outcome Outcome) error {
 if !allowedComponent(component)||!allowedStage(stage)||!allowedOutcome(outcome){
  return errors.New("unsupported startup evidence value")
 }
 mu.Lock();defer mu.Unlock()
 dir,err:=evidenceDir(root,true);if err!=nil{return err}
 path:=eventFile(dir,component)
 if err:=regularOrMissing(path);err!=nil{return err}
 if err:=regularOrMissing(path+".1");err!=nil{return err}
 row,err:=json.Marshal(Event{Schema:1,AtUTC:time.Now().UTC().Format(time.RFC3339Nano),
  Component:component,Stage:stage,Outcome:outcome})
 if err!=nil{return err}
 row=append(row,'\n')
 if int64(len(row))>MaxFileBytes{return errors.New("event exceeds maximum size")}
 st,err:=os.Stat(path)
 if err!=nil&&!os.IsNotExist(err){return err}
 if err==nil&&st.Size()+int64(len(row))>MaxFileBytes {
  if err:=os.Remove(path+".1");err!=nil&&!os.IsNotExist(err){return err}
  if err:=os.Rename(path,path+".1");err!=nil{return err}
 }
 f,err:=os.OpenFile(path,os.O_CREATE|os.O_WRONLY|os.O_APPEND,0600)
 if err!=nil{return err}
 defer f.Close()
 if runtime.GOOS!="windows" {
  if err:=f.Chmod(0600);err!=nil{return err}
 }
 n,err:=f.Write(row)
 if err!=nil{return err}
 if n!=len(row){return io.ErrShortWrite}
 return f.Sync()
}

// Read returns only validated, fixed-schema fields from on-disk evidence.
// This is intended for a local, operator-controlled offline export. File
// contents are never returned verbatim and unknown/corrupt rows are omitted.
func Read(root string) ([]Event,error) {
 mu.Lock();defer mu.Unlock()
 dir,err:=evidenceDir(root,false);if err!=nil{return nil,err}
 out:=make([]Event,0)
 for _,component:=range []Component{Backend,WindowsService,LegacySetup} {
  path:=eventFile(dir,component)
  for _,p:=range []string{path+".1",path} {
   if err:=regularOrMissing(p);err!=nil{return nil,err}
   f,err:=os.Open(p);if os.IsNotExist(err){continue};if err!=nil{return nil,err}
   st,err:=f.Stat();if err!=nil{_ = f.Close();return nil,err}
   if st.Size()>MaxFileBytes{_ = f.Close();return nil,errors.New("oversized evidence segment")}
   scanner:=bufio.NewScanner(io.LimitReader(f,MaxFileBytes+1))
   scanner.Buffer(make([]byte,1024),2048)
   for scanner.Scan(){
    var e Event
    if json.Unmarshal(scanner.Bytes(),&e)==nil&&e.Schema==1&&e.Component==component&&
     allowedStage(e.Stage)&&allowedOutcome(e.Outcome){
     if t,err:=time.Parse(time.RFC3339Nano,e.AtUTC);err==nil&&t.Location()!=nil{
      out=append(out,e)
     }
    }
   }
   scanErr:=scanner.Err()
   _=f.Close()
   if scanErr!=nil{return nil,fmt.Errorf("invalid evidence segment: %w",scanErr)}
  }
 }
 sort.SliceStable(out,func(i,j int)bool{return out[i].AtUTC<out[j].AtUTC})
 return out,nil
}
