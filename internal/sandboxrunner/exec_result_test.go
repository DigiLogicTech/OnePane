package sandboxrunner

import (
 "context"
 "errors"
 "fmt"
 "os"
 "os/exec"
 "strconv"
 "strings"
 "testing"
)

// Exercise exit status parsing with this test executable; there is no host
// shell dependency, registry access, OCI daemon or elevated privilege.
func TestSandboxExecExitFixture(t *testing.T) {
 value:=os.Getenv("ONEPANE_EXEC_EXIT_FIXTURE")
 if value==""{return}
 code,err:=strconv.Atoi(value)
 if err!=nil{os.Exit(99)}
 _,_=fmt.Fprintln(os.Stdout,"build output")
 _,_=fmt.Fprintln(os.Stderr,"compiler diagnostics")
 os.Exit(code)
}

func TestContainerExecProcessExitIsObservableBuildResult(t *testing.T) {
 for _,tc:=range []struct{
  code int
  wantError bool
 }{{0,false},{1,false},{2,false},{126,false},{127,false},{125,true}} {
  t.Run(fmt.Sprintf("exit_%d",tc.code),func(t *testing.T){
   cmd:=exec.Command(os.Args[0],"-test.run=^TestSandboxExecExitFixture$")
   cmd.Env=append(os.Environ(),fmt.Sprintf("ONEPANE_EXEC_EXIT_FIXTURE=%d",tc.code))
   var out,errOut strings.Builder
   cmd.Stdout=&out
   cmd.Stderr=&errOut
   procErr:=cmd.Run()
   result,err:=classifyContainerExecResult(strings.TrimSpace(out.String()),strings.TrimSpace(errOut.String()),procErr)
   if tc.wantError {
    if err==nil{t.Fatalf("engine failure %d reported as command result: %+v",tc.code,result)}
    return
   }
   if err!=nil{
    t.Fatalf("exit %d incorrectly treated as OCI infrastructure failure: %v",tc.code,err)
   }
   if result.ExitCode!=tc.code||result.Stdout!="build output"||
    result.Stderr!="compiler diagnostics"{
    t.Fatalf("command diagnostics lost for exit %d: %+v",tc.code,result)
   }
  })
 }
}

func TestContainerExecContextAndInvocationErrorsRemainFailures(t *testing.T) {
 for _,err:=range []error{context.Canceled,context.DeadlineExceeded,exec.ErrNotFound} {
  if _,got:=classifyContainerExecResult("stdout","stderr",err);!errors.Is(got,err){
   t.Fatalf("invocation failure incorrectly turned into command result: %v",got)
  }
 }
}
