package localai

import (
 "context"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"
)

// Launched as a managed runtime by the parent test. The helper writes ONLY
// whether forbidden parent variables reached it, not their actual values.
func TestManagedRuntimeEmptyEnvHelper(t *testing.T){
 var out string
 for _,arg:=range os.Args{
  if strings.HasPrefix(arg,"onepane-child-env-check="){
   out=strings.TrimPrefix(arg,"onepane-child-env-check=")
  }
 }
 if out==""{return}
 exposed:=os.Getenv("ONEPANE_SERVICE_SHUTDOWN_TOKEN")!=""||
  os.Getenv("OPENAI_API_KEY")!=""||
  os.Getenv("AZURE_CLIENT_SECRET")!=""
 verdict:="isolated"
 if exposed{verdict="leaked"}
 if err:=os.WriteFile(out,[]byte(verdict),0600);err!=nil{t.Fatal(err)}
}

// Verify the actual OS child environment. Prior behaviour assigned a nil
// Cmd.Env for empty LaunchSpec.Env; os/exec interpreted that as INHERIT ALL.
func TestManagedRuntimeLaunchWithNilOrEmptyEnvDoesNotInheritSecrets(t *testing.T){
 t.Setenv("ONEPANE_SERVICE_SHUTDOWN_TOKEN","parent-secret-do-not-inherit")
 t.Setenv("OPENAI_API_KEY","parent-cloud-secret-do-not-inherit")
 t.Setenv("AZURE_CLIENT_SECRET","parent-provider-secret-do-not-inherit")
 for _,env:=range [][]string{nil,{},{"LANG=C"}}{
  name:="nil"
  if env!=nil {name="empty"}
  if len(env)>0{name="allowlisted"}
  t.Run(name,func(t *testing.T){
   out:=filepath.Join(t.TempDir(),"verdict.txt")
   ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second)
   defer cancel()
   backend:=NewOSProcessBackend()
   _,err:=backend.Start(ctx,LaunchSpec{
    Executable:os.Args[0],
    Args:[]string{"-test.run=^TestManagedRuntimeEmptyEnvHelper$","--","onepane-child-env-check="+out},
    Env:env,
   })
   if err!=nil{t.Fatalf("managed child launch: %v",err)}
   ticker:=time.NewTicker(20*time.Millisecond)
   defer ticker.Stop()
   for {
    data,readErr:=os.ReadFile(out)
    if readErr==nil{
     if string(data)!="isolated"{t.Fatalf("managed child inherited service/provider secrets: %s",data)}
     return
    }
    select{
    case <-ctx.Done():t.Fatalf("child did not complete environment check: %v",ctx.Err())
    case <-ticker.C:
    }
   }
  })
 }
}
