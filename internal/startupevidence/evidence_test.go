package startupevidence

import (
 "encoding/json"
 "os"
 "path/filepath"
 "runtime"
 "strings"
 "testing"
)

func TestTypedEventsAndOfflineRead(t *testing.T) {
 root:=t.TempDir()
 if err:=Record(root,Backend,Config,Begin);err!=nil{t.Fatal(err)}
 if err:=Record(root,Backend,Config,Failed);err!=nil{t.Fatal(err)}
 if err:=Record(root,WindowsService,ServiceReady,OK);err!=nil{t.Fatal(err)}
 events,err:=Read(root);if err!=nil{t.Fatal(err)}
 if len(events)!=3{t.Fatalf("got %d events, want 3",len(events))}
 for _,e:=range events{
  if e.Schema!=1||e.AtUTC==""{t.Fatalf("invalid event: %+v",e)}
  raw,err:=json.Marshal(e);if err!=nil{t.Fatal(err)}
  for _,private:=range []string{root,"password","token","username","command_line"}{
   if strings.Contains(string(raw),private){t.Fatalf("private field leaked")}
  }
 }
 raw,err:=os.ReadFile(filepath.Join(root,"diagnostics","backend.jsonl"));if err!=nil{t.Fatal(err)}
 if strings.Contains(string(raw),root){t.Fatal("absolute path leaked")}
}
func TestRejectUnknownValuesAndNonPrivatePaths(t *testing.T) {
 root:=t.TempDir()
 if err:=Record(root,Backend,Stage("configuration /Users/private --token=x"),Failed);err==nil{
  t.Fatal("freeform stage accepted")
 }
 if err:=Record(root,Component("attacker"),Config,Failed);err==nil{t.Fatal("unknown component accepted")}
 if err:=Record(root,Backend,Config,Outcome("token"));err==nil{t.Fatal("unknown outcome accepted")}
 if err:=Record("relative",Backend,Config,Begin);err==nil{t.Fatal("relative path accepted")}
 dir:=filepath.Join(root,"diagnostics")
 if err:=os.Symlink(t.TempDir(),dir);err==nil{
  if err:=Record(root,Backend,Config,Begin);err==nil{t.Fatal("symlinked evidence directory accepted")}
 }
 if runtime.GOOS!="windows" {
  root2:=t.TempDir()
  if err:=os.Mkdir(filepath.Join(root2,"diagnostics"),0755);err!=nil{t.Fatal(err)}
  if err:=Record(root2,Backend,Config,Begin);err==nil{t.Fatal("public existing diagnostics directory accepted")}
 }
}
func TestRotationBoundsAndSanitisedReader(t *testing.T) {
 root:=t.TempDir()
 for i:=0;i<800;i++ {
  if err:=Record(root,Backend,Bootstrap,Begin);err!=nil{t.Fatal(err)}
 }
 dir:=filepath.Join(root,"diagnostics")
 for _,suffix:=range []string{"",".1"} {
  st,err:=os.Stat(filepath.Join(dir,"backend.jsonl"+suffix));if err!=nil{t.Fatal(err)}
  if st.Size()>MaxFileBytes{t.Fatalf("oversized evidence segment: %d",st.Size())}
 }
 // Keep injected-test data separate from the nearly full rotating backend file.
 f,err:=os.OpenFile(filepath.Join(dir,"windows_service.jsonl"),os.O_CREATE|os.O_APPEND|os.O_WRONLY,0600)
 if err!=nil{t.Fatal(err)}
 _,err=f.WriteString("{\"schema\":1,\"at_utc\":\"2026-10-10T00:00:00Z\",\"component\":\"windows_service\",\"stage\":\"configuration\",\"outcome\":\"ok\",\"secret\":\"api-key\"}\n")
 if err!=nil{t.Fatal(err)}
 if err:=f.Close();err!=nil{t.Fatal(err)}
 got,err:=Read(root);if err!=nil{t.Fatal(err)}
 data,err:=json.Marshal(got);if err!=nil{t.Fatal(err)}
 if strings.Contains(string(data),"api-key"){t.Fatal("reader leaked unrecognised content")}
}
