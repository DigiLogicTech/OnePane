package localai

import (
 "context"
 "net/http"
 "net/http/httptest"
 "net/url"
 "os"
 "strconv"
 "strings"
 "testing"
)

func TestRuntimeHealthRequiresTrustedLoopbackReadinessEvidence(t *testing.T){
 body:=`{"status":"ok"}`
 code:=http.StatusOK
 redirect:=""
 hits:=0
 source:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  hits++
  if r.URL.Path!="/health"{t.Errorf("wrong managed health path: %s",r.URL.Path)}
  if redirect!=""{w.Header().Set("Location",redirect)}
  w.WriteHeader(code)
  _,_=w.Write([]byte(body))
 }))
 defer source.Close()
 parsed,err:=url.Parse(source.URL)
 if err!=nil{t.Fatal(err)}
 port,err:=strconv.Atoi(parsed.Port())
 if err!=nil{t.Fatal(err)}
 routed:=0
 intercept:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  routed++
  t.Error("local inference health travelled to untrusted configured HTTP client")
 }))
 defer intercept.Close()
 proxy,err:=url.Parse(intercept.URL)
 if err!=nil{t.Fatal(err)}
 supervisor:=&RuntimeSupervisor{
  http:&http.Client{Transport:&http.Transport{Proxy:http.ProxyURL(proxy)}},
 }
 inst:=RuntimeInstance{Port:port}
 for _,tc:=range []struct{
  name,raw string
  code int
  ready bool
 }{
  {"ready",`{"status":"ok","slots_idle":1}`,200,true},
  {"healthy",`{"status":"healthy"}`,200,true},
  {"response contains secret",`{"status":"ok","token":"SECRET_HEALTH_CANARY","path":"/private/secrets","slots_idle":2}`,200,true},
  {"unbounded count",`{"status":"ok","slots_idle":99999999,"slots_processing":-1}`,200,true},
  {"llama busy ready",`{"status":"no slot available"}`,200,true},
  {"empty","",200,false},
  {"malformed","SECRET_HEALTH_CANARY",200,false},
  {"non object",`["ok"]`,200,false},
  {"no status",`{"data":"SECRET_HEALTH_CANARY"}`,200,false},
  {"loading",`{"status":"loading model"}`,200,false},
  {"error status",`{"status":"error"}`,200,false},
  {"invalid type",`{"status":123}`,200,false},
  {"upstream 503",`{"status":"ok"}`,503,false},
  {"oversized",`{"status":"ok","raw":"`+strings.Repeat("z",1<<20)+`"}`,200,false},
 }{
  t.Run(tc.name,func(t *testing.T){
   body,code,redirect=tc.raw,tc.code,""
   got,e:=supervisor.Health(context.Background(),inst)
   if tc.ready{
    if e!=nil||got==nil{t.Fatalf("legitimate health unavailable: %v",e)}
    if _,bad:=got["token"];bad{t.Fatal("private engine token persisted")}
    if _,bad:=got["path"];bad{t.Fatal("private engine path persisted")}
    if count,ok:=got["slots_idle"];ok{
     n,valid:=count.(int64)
     if !valid||n<0||n>4096{t.Fatalf("unbounded slots persisted: %#v",count)}
    }
   }else{
    if e==nil||got!=nil{t.Fatalf("untrusted health accepted: %+v %v",got,e)}
    if strings.Contains(e.Error(),"SECRET_HEALTH_CANARY"){
     t.Fatalf("private health response escaped to error: %v",e)
    }
   }
  })
 }
 redirect=intercept.URL
 body="redirect"
 code=http.StatusFound
 if _,err:=supervisor.Health(context.Background(),inst);err==nil{
  t.Fatal("runtime health followed redirected local request")
 }
 if routed!=0{t.Fatalf("untrusted proxy/redirect called %d times",routed)}
 if hits==0{t.Fatal("health probe did not read from actual local server")}
 for _,p:=range []int{0,1,1023,65536}{
  if _,err:=supervisor.Health(context.Background(),RuntimeInstance{Port:p});err==nil{
   t.Fatalf("invalid process health port %d accepted",p)
  }
 }
}


// Guard against a future refactor probing a recycled/unverified PID's
// localhost HTTP listener before the supervisor checks process identity.
// This is a safety-oriented source-order invariant, not physical attestation.
func TestStartupHealthProbesVerifyManagedProcessBeforeSocket(t *testing.T) {
 raw,err:=os.ReadFile("supervisor.go")
 if err!=nil{t.Fatal(err)}
 source:=string(raw)
 begin:=strings.Index(source,"func (s *RuntimeSupervisor) waitHealthy(")
 if begin<0{t.Fatal("start health path missing")}
 scope:=source[begin:]
 if end:=strings.Index(scope,"\nfunc ");end>0{scope=scope[:end]}
 verified:=strings.Index(scope,"s.VerifyIdentity(ctx,inst)")
 health:=strings.Index(scope,"s.Health(ctx, inst)")
 if verified<0||health<0||verified>=health{
  t.Fatal("managed localhost health request is not guarded by process identity")
 }
}
