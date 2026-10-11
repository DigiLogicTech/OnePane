package localai

import (
 "context"
 "errors"
 "testing"
)

type fakeQualificationLease struct {
 calls []string
 activeRequests int
 releaseErr error
 stopErr error
 stopped bool
}

func (f *fakeQualificationLease) Release(_ context.Context, id string)error{
 f.calls=append(f.calls,"release:"+id)
 if f.releaseErr!=nil{return f.releaseErr}
 if f.activeRequests>0{f.activeRequests--}
 return nil
}
func (f *fakeQualificationLease) StopIfIdle(_ context.Context,id string)(bool,error){
 f.calls=append(f.calls,"stop_if_idle:"+id)
 if f.stopErr!=nil{return false,f.stopErr}
 if f.activeRequests>0{return false,nil}
 f.stopped=true
 return true,nil
}

func TestAgentCheckAlwaysReleasesBeforeDraining(t *testing.T){
 f:=&fakeQualificationLease{activeRequests:1}
 drained,err:=cleanupQualificationResidency(context.Background(),f,"model-world")
 if err!=nil||!drained||!f.stopped||f.activeRequests!=0{
  t.Fatalf("Agent Check failed to unload unused model: drained=%v state=%+v err=%v",drained,f,err)
 }
 if len(f.calls)!=2||f.calls[0]!="release:model-world"||f.calls[1]!="stop_if_idle:model-world"{
  t.Fatalf("unexpected drain order: %+v",f.calls)
 }
}

func TestAgentCheckDoesNotEvictConcurrentInference(t *testing.T){
 // Agent Check and inference each hold one lease. The check releases only
 // its own reference; the model must remain resident for real inference.
 f:=&fakeQualificationLease{activeRequests:2}
 drained,err:=cleanupQualificationResidency(context.Background(),f,"model-world")
 if err!=nil||drained||f.stopped||f.activeRequests!=1{
  t.Fatalf("Agent Check evicted in-use runtime: drained=%v state=%+v err=%v",drained,f,err)
 }
}

func TestAgentCheckCleanupErrorsAreNotSilenced(t *testing.T){
 badRelease:=errors.New("cannot persist activity lease")
 f:=&fakeQualificationLease{activeRequests:1,releaseErr:badRelease}
 stopped,err:=cleanupQualificationResidency(context.Background(),f,"model-world")
 if stopped||!errors.Is(err,badRelease)||len(f.calls)!=1{
  t.Fatalf("released incorrectly after failed lease update: stopped=%v err=%v calls=%v",stopped,err,f.calls)
 }
 badStop:=errors.New("cannot verify stopped process")
 g:=&fakeQualificationLease{activeRequests:1,stopErr:badStop}
 stopped,err=cleanupQualificationResidency(context.Background(),g,"model-world")
 if stopped||!errors.Is(err,badStop)||len(g.calls)!=2{
  t.Fatalf("unload failure was hidden: stopped=%v err=%v calls=%v",stopped,err,g.calls)
 }
}
