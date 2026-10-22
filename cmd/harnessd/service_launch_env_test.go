package main

import (
 "os"
 "testing"
)

func TestSCMControlSecretRemovedFromBackendSubprocessEnvironment(t *testing.T) {
 const token="abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
 t.Setenv("ONEPANE_SERVICE_SHUTDOWN_TOKEN",token)
 got:=consumeServiceLaunchToken()
 if got!=token{t.Fatal("service control credential changed during capture")}
 if _,present:=os.LookupEnv("ONEPANE_SERVICE_SHUTDOWN_TOKEN");present{
  t.Fatal("service control secret remains inheritable by worker processes")
 }
 if second:=consumeServiceLaunchToken();second!=""{
  t.Fatalf("secret was not consumed once: %q",second)
 }
}
