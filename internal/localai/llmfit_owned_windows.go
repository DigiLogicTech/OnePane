//go:build windows

package localai

import (
 "encoding/json"
 "errors"
 "fmt"
 "os"
 "path/filepath"
 "strings"

 "golang.org/x/sys/windows"
)

type llmfitProcessIdentity struct {
 PID uint32 `json:"pid"`
 CreationTicks uint64 `json:"creation_ticks"`
 Exe string `json:"executable"`
}
func llmfitProcessRecord(root string)string{return filepath.Join(root,"managed-process.json")}
func llmfitProcessInfo(pid uint32)(llmfitProcessIdentity,windows.Handle,error){
 h,err:=windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE,false,pid)
 if err!=nil{return llmfitProcessIdentity{},0,err}
 buf:=make([]uint16,4096);length:=uint32(len(buf))
 if err:=windows.QueryFullProcessImageName(h,0,&buf[0],&length);err!=nil{windows.CloseHandle(h);return llmfitProcessIdentity{},0,err}
 var c,e,k,u windows.Filetime
 if err:=windows.GetProcessTimes(h,&c,&e,&k,&u);err!=nil{windows.CloseHandle(h);return llmfitProcessIdentity{},0,err}
 return llmfitProcessIdentity{PID:pid,CreationTicks:(uint64(c.HighDateTime)<<32)|uint64(c.LowDateTime),Exe:filepath.Clean(windows.UTF16ToString(buf[:length]))},h,nil
}
func recordOwnedLLMFit(root,exe string,pid int)error{
 actual,h,err:=llmfitProcessInfo(uint32(pid));if err!=nil{return err};defer windows.CloseHandle(h)
 if !strings.EqualFold(actual.Exe,filepath.Clean(exe)){return fmt.Errorf("llmfit startup process identity mismatch: %s",actual.Exe)}
 payload,err:=json.Marshal(actual);if err!=nil{return err}
 dest:=llmfitProcessRecord(root)
 if err:=os.MkdirAll(root,0o700);err!=nil{return err}
 temp:=dest+".partial"
 if err:=os.WriteFile(temp,payload,0o600);err!=nil{return err}
 if err:=os.Rename(temp,dest);err!=nil{os.Remove(temp);return err}
 return nil
}
func terminateOwnedLLMFit(root,exe string)(bool,error){
 path:=llmfitProcessRecord(root)
 b,err:=os.ReadFile(path)
 if errors.Is(err,os.ErrNotExist){return false,nil}
 if err!=nil{return false,err}
 var expected llmfitProcessIdentity
 if err:=json.Unmarshal(b,&expected);err!=nil{return false,err}
 if expected.PID==0||expected.CreationTicks==0||!strings.EqualFold(filepath.Clean(expected.Exe),filepath.Clean(exe)){return false,errors.New("managed llmfit process record is invalid")}
 actual,h,err:=llmfitProcessInfo(expected.PID)
 if err!=nil {
  if errors.Is(err,windows.ERROR_INVALID_PARAMETER){_ = os.Remove(path);return false,nil}
  return false,fmt.Errorf("unable to verify previous llmfit process: %w",err)
 }
 defer windows.CloseHandle(h)
 if actual.CreationTicks!=expected.CreationTicks||!strings.EqualFold(actual.Exe,expected.Exe){return false,errors.New("llmfit process identity changed; refusing to terminate unrelated process")}
 if err:=windows.TerminateProcess(h,1);err!=nil{return false,err}
 _=os.Remove(path)
 return true,nil
}
