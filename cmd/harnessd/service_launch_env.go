package main

import "os"

// Consume the Windows service launch secret before any spawned inference,
// sandbox, CLI, browser or other child process can inherit it via os.Environ.
// Keep the secret only in the service-control handler's private Go closure.
func consumeServiceLaunchToken() string {
 token:=os.Getenv("ONEPANE_SERVICE_SHUTDOWN_TOKEN")
 _=os.Unsetenv("ONEPANE_SERVICE_SHUTDOWN_TOKEN")
 return token
}
