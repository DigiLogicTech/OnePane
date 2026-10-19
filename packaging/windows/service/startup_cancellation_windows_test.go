//go:build windows

package main

import (
 "context"
 "errors"
 "testing"
)

// A stop during slow first-run configuration must fence any subsequent
// managed child creation. This test intentionally has no HKLM, SCM, files or
// real subprocess; canceled bootstrap exits before any of those are accessed.
func TestCanceledServiceStartupNeverReachesBackendLaunch(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background())
 cancel()
 if err:=runBackendWithContext(ctx);!errors.Is(err,context.Canceled){
  t.Fatalf("canceled Windows bootstrap continued: %v",err)
 }
}

func TestNilServiceStartupContextFailsClosed(t *testing.T){
 if err:=runBackendWithContext(nil);err==nil{
  t.Fatal("nil service startup context was accepted")
 }
}
