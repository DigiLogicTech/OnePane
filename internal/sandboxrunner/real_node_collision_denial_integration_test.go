//go:build integration

package sandboxrunner

import (
 "context"
 "errors"
 "fmt"
 "os"
 "regexp"
 "strings"
 "testing"
 "time"
)

// This adversarial physical smoke creates *only* a generated test-owned
// container with a predictable OnePane name and forged labels. The absence
// of verified mount/network/process isolation must prevent every action.
// It never targets a user container or uses host-wide cleanup.
func TestRealRootlessForeignNameCollisionCannotExecuteOrStop(t *testing.T){
 image:=strings.TrimSpace(os.Getenv("ONEPANE_SANDBOX_SMOKE_IMAGE"))
 if image==""{t.Skip("opt-in physical collision QA needs approved immutable image")}
 if ok,_:=regexp.MatchString(`^[^[:space:]]+@sha256:[a-fA-F0-9]{64}$`,image);!ok{
  t.Fatal("unapproved mutable physical QA image")
 }
 ctx,cancel:=context.WithTimeout(context.Background(),3*time.Minute)
 defer cancel()
 engine:=NewCLIEngine()
 p,err:=engine.Probe(ctx)
 if err!=nil||!p.Rootless{t.Fatal("trusted rootless Node required")}
 if _,err:=engine.InspectImage(ctx,image);err!=nil{t.Fatal("pre-pulled approved image required")}
 runtimeID:=fmt.Sprintf("opqa-collision-%d",time.Now().UnixNano())
 appID:="unowned"
 name:=containerName(runtimeID,appID)
 t.Cleanup(func(){
  cleanupCtx,stop:=context.WithTimeout(context.Background(),30*time.Second)
  defer stop()
  _,_,_=runCLI(cleanupCtx,p.Executable,"rm","-f",name)
 })
 // Deliberately spoof the labels and canonical name, but omit the
 // independently verified runtime network, mount and sandbox restrictions.
 _,_,err=runCLI(ctx,p.Executable,"run","-d","--pull=never",
  "--name",name,"--network=none",
  "--label","io.harness.runtime-id="+runtimeID,
  "--label","io.harness.application-id="+appID,
  "--label","io.harness.spec-hash=forged",
  image,"sh","-c","sleep 210")
 if err!=nil{t.Fatal("unable to create isolated test-owned name collision")}
 initial,err:=engine.InspectContainer(ctx,runtimeID,appID)
 if err!=nil||initial.Status!="running"||initial.ID==""{
  t.Fatalf("collision fixture did not start: %v",err)
 }
 if initial.IsolationVerified{
  t.Fatal("unowned external container incorrectly qualified as a OnePane sandbox")
 }
 if _,err:=engine.ExecContainer(ctx,runtimeID,appID,[]string{
  "sh","-c","echo escaped",
 });!errors.Is(err,ErrInvalidInput){
  t.Fatalf("foreign OCI container executed through guessed name: %v",err)
 }
 if _,err:=engine.StopContainer(ctx,runtimeID,appID);!errors.Is(err,ErrInvalidInput){
  t.Fatalf("foreign OCI container stopped through guessed name: %v",err)
 }
 if _,err:=engine.StopRuntime(ctx,runtimeID);!errors.Is(err,ErrInvalidInput){
  t.Fatalf("forged runtime label caused destructive stop: %v",err)
 }
 // Verify no unintended process stop, deletion or replacement occurred.
 after,err:=engine.InspectContainer(ctx,runtimeID,appID)
 if err!=nil||after.Status!="running"||after.ID!=initial.ID{
  t.Fatalf("foreign OCI container was modified despite denial: %v",err)
 }
 t.Log("Verified: an intentionally colliding rootless OCI name with forged runtime labels cannot be executed or stopped; exact foreign container remains running")
}
