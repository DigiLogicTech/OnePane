package projectworkspace

import (
 "context"
 "errors"
 "net"
 "os"
 "testing"
 "time"
)

// Both manual and recurring TCP workers use tcpDialOutcome. No network
// traffic, real host or granted LAN target is needed for these regressions.
func TestTCPDialOutcomeNeverMisstatesCancelledOrMissingConnections(t *testing.T) {
 client,server:=net.Pipe()
 defer client.Close()
 defer server.Close()

 cancelledCtx,cancel:=context.WithCancel(context.Background())
 cancel()
 deadlineCtx,deadlineCancel:=context.WithDeadline(context.Background(),time.Now().Add(-time.Second))
 defer deadlineCancel()

 cases:=[]struct{
  name string
  parent context.Context
  dial context.Context
  connection net.Conn
  dialErr error
  want string
 }{
  {"real connection",context.Background(),context.Background(),client,nil,"connected"},
  {"nil connection no error",context.Background(),context.Background(),nil,nil,"not_connected"},
  {"ordinary failure",context.Background(),context.Background(),nil,errors.New("refused"),"not_connected"},
  {"cancelled dial",context.Background(),context.Background(),nil,context.Canceled,"cancelled"},
  {"cancelled parent after connect",cancelledCtx,context.Background(),client,nil,"cancelled"},
  {"cancelled dial context after connect",context.Background(),cancelledCtx,client,nil,"cancelled"},
  {"deadline result",context.Background(),context.Background(),nil,context.DeadlineExceeded,"timed_out"},
  {"os deadline result",context.Background(),context.Background(),nil,os.ErrDeadlineExceeded,"timed_out"},
  {"expired dial context",context.Background(),deadlineCtx,nil,nil,"timed_out"},
  {"expired parent context",deadlineCtx,context.Background(),nil,nil,"timed_out"},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   got:=tcpDialOutcome(tc.parent,tc.dial,tc.connection,tc.dialErr)
   if got!=tc.want{t.Fatalf("got %q, want %q",got,tc.want)}
  })
 }
}
