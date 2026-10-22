//go:build windows

package main

import (
 "testing"
 "time"
)

func TestSCMStartupCleanupReapsGracefullyWithoutForce(t *testing.T){
 done:=make(chan error,1)
 gracefulCalls,forceCalls:=0,0
 ok:=drainBackend(done,func(){
  gracefulCalls++
  done<-nil
 },func(){forceCalls++},10*time.Millisecond,10*time.Millisecond)
 if !ok||gracefulCalls!=1||forceCalls!=0{
  t.Fatalf("expected prompt graceful child reap: done=%v graceful=%d forced=%d",ok,gracefulCalls,forceCalls)
 }
}

func TestSCMStartupCleanupForcesUnresponsiveBackendAndWaitsForReap(t *testing.T){
 done:=make(chan error,1)
 gracefulCalls,forceCalls:=0,0
 ok:=drainBackend(done,func(){gracefulCalls++},func(){
  forceCalls++
  done<-nil
 },time.Millisecond,10*time.Millisecond)
 if !ok||gracefulCalls!=1||forceCalls!=1{
  t.Fatalf("did not force and reap timed out child: done=%v graceful=%d forced=%d",ok,gracefulCalls,forceCalls)
 }
}

func TestSCMStartupCleanupReportsStillUnreapedChild(t *testing.T){
 done:=make(chan error)
 forced:=false
 ok:=drainBackend(done,func(){},func(){forced=true},time.Millisecond,time.Millisecond)
 if ok||!forced{
  t.Fatal("unresponsive child was silently treated as clean SCM stop")
 }
}

func TestSCMUnexpectedCleanBackendExitIsFailure(t *testing.T){
 stopping:=make(chan struct{})
 if actual:=classifyBackendExit(stopping);actual!=1{
  t.Fatalf("unexpected clean exit classified as success: %d",actual)
 }
 close(stopping)
 if actual:=classifyBackendExit(stopping);actual!=0{
  t.Fatalf("explicit SCM stop classified as crash: %d",actual)
 }
}
