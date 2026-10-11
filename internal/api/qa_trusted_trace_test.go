package api

import (
 "context"
 "encoding/json"
 "net/http/httptest"
 "strings"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/task"
)

type qaTraceTaskService struct{
 received task.CreateCommand
}
func (f *qaTraceTaskService)Create(_ context.Context,c task.CreateCommand)(task.Task,error){
 f.received=c
 return task.Task{ID:"created-task",WorkspaceID:c.WorkspaceID,State:task.StateCreated},nil
}
func (f *qaTraceTaskService)List(_ context.Context,_ string,_ int)([]task.Task,error){return nil,nil}

func TestQAServerMintsTrustedTraceForCapturedTaskNotCallerHeader(t *testing.T){
 s:=NewServer(nil,nil,qaCaptureTestAuth{})
 fake:=&qaTraceTaskService{}
 s.SetTasks(fake)
 s.SetFederation(&qaCaptureTestFederation{})
 h:=s.Handler()
 call:=func(method,path,body string,headers map[string]string)*httptest.ResponseRecorder{
  req:=httptest.NewRequest(method,path,strings.NewReader(body))
  req.Header.Set("Authorization","Bearer adminA")
  if body!=""{req.Header.Set("Content-Type","application/json")}
  for k,v:=range headers{req.Header.Set(k,v)}
  rw:=httptest.NewRecorder();h.ServeHTTP(rw,req);return rw
 }
 starter:=call("POST","/v1/qa/api-capture/start",`{"level":"verbose","seconds":60}`,nil)
 if starter.Code!=201{t.Fatalf("capture not started: %d %s",starter.Code,starter.Body)}
 var owned qaAPICaptureView
 if err:=json.Unmarshal(starter.Body.Bytes(),&owned);err!=nil{t.Fatal(err)}
 forged:="forged-by-browser-untrusted-trace-id"
 headers:=map[string]string{
  "Accept":"application/json",
  "X-OnePane-QA-Capture":owned.CaptureID,
  "X-Trace-ID":forged,
 }
 created:=call("POST","/v1/tasks",`{"workspace_id":"tenant","objective":"private objective"}`,headers)
 if created.Code!=201{t.Fatalf("task creation: %d %s",created.Code,created.Body)}
 if fake.received.TraceID==nil||*fake.received.TraceID==forged||len(*fake.received.TraceID)!=48{
  t.Fatalf("untrusted trace not replaced: %+v",fake.received.TraceID)
 }
 report:=call("GET","/v1/qa/api-capture","",nil)
 if report.Code!=200{t.Fatalf("capture read: %d",report.Code)}
 if err:=json.Unmarshal(report.Body.Bytes(),&owned);err!=nil{t.Fatal(err)}
 if len(owned.Observations)!=1||!qaSafeTraceRef(owned.Observations[0].TraceRef){
  t.Fatalf("missing minted trace observation: %+v",owned.Observations)
 }
 if owned.Observations[0].TraceRef!=qaOpaqueRef("trace",*fake.received.TraceID){
  t.Fatal("capture and persisted Task event command use different minted trace")
 }
 if strings.Contains(report.Body.String(),forged)||strings.Contains(report.Body.String(),*fake.received.TraceID){
  t.Fatal("raw or spoofed trace escaped response")
 }
 // Only tagged requests get a trusted trace. Without the capture tag, the
 // existing caller header is not promoted to trusted diagnostic evidence.
 ignored:=call("POST","/v1/tasks",`{"workspace_id":"tenant","objective":"another"}`,
  map[string]string{"Accept":"application/json","X-Trace-ID":forged})
 if ignored.Code!=201||fake.received.TraceID==nil||*fake.received.TraceID!=forged{
  t.Fatalf("noncaptured legacy Task headers unexpectedly changed: %d",ignored.Code)
 }
 fresh:=call("GET","/v1/qa/api-capture","",nil)
 if err:=json.Unmarshal(fresh.Body.Bytes(),&owned);err!=nil||len(owned.Observations)!=1{
  t.Fatal("untagged create was accidentally recorded")
 }
 // Accepted tags on other endpoint types must not mint a trusted Task trace.
 other:=call("GET","/v1/health","",map[string]string{
  "Accept":"application/json","X-OnePane-QA-Capture":owned.CaptureID,"X-Trace-ID":forged})
 if other.Code!=200{t.Fatalf("health failed: %d",other.Code)}
 fresh=call("GET","/v1/qa/api-capture","",nil)
 if err:=json.Unmarshal(fresh.Body.Bytes(),&owned);err!=nil||len(owned.Observations)!=2{
  t.Fatalf("health should only add a general metadata observation: %d",len(owned.Observations))
 }
 if owned.Observations[1].TraceRef!=""{t.Fatal("unrelated HTTP request incorrectly linked")}
}

func TestQATraceRefValidatorRejectsSpoofedText(t *testing.T){
 for _,bad:=range []string{"trace-ABC","trace-"+strings.Repeat("g",24),
  "trace-"+strings.Repeat("A",24),strings.Repeat("b",48),"trace-"+strings.Repeat("a",25)}{
  if qaSafeTraceRef(bad){t.Fatalf("accepted unsafe trace ref: %q",bad)}
 }
}
