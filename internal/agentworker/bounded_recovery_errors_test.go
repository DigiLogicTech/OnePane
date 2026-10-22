package agentworker

import (
 "errors"
 "fmt"
 "strings"
 "testing"
)

func TestBoundedWorkerRecoveryErrorsKeepsFirstIdentitiesAndFatalCause(t *testing.T) {
 marker := errors.New("first damaged worker")
 fatal := errors.New("SQLite cursor failure")
 p := boundedWorkerRecoveryErrors{category:"waiting Worker reconciliation failures"}
 p.add(nil)
 p.add(marker)
 for n:=1;n<100;n++ {p.add(fmt.Errorf("worker-%03d",n))}
 got:=p.result(nil,fatal)
 if got==nil||!errors.Is(got,marker)||!errors.Is(got,fatal) {
  t.Fatalf("saved first cause or fatal cursor error lost: %v",got)
 }
 message:=got.Error()
 for _,must:=range []string{
  "worker-001","worker-031",
  "68 additional waiting Worker reconciliation failures require operator review",
  "SQLite cursor failure",
 } {
  if !strings.Contains(message,must) {t.Fatalf("missing %q: %s",must,message)}
 }
 for _,omit:=range []string{"worker-032","worker-099"} {
  if strings.Contains(message,omit) {t.Fatalf("error budget leaked omitted %q: %s",omit,message)}
 }
 if len(message)>3000{t.Fatalf("error report grew without bound: %d bytes",len(message))}
}

func TestBoundedWorkerRecoveryErrorsEmptyAndSmall(t *testing.T){
 p:=boundedWorkerRecoveryErrors{category:"orphan Agent Attempt recovery failures"}
 if got:=p.result();got!=nil{t.Fatalf("no failures must not report error: %v",got)}
 one:=errors.New("one uncertain attempt")
 p.add(one)
 if got:=p.result();!errors.Is(got,one){t.Fatalf("single error was lost: %v",got)}
}
