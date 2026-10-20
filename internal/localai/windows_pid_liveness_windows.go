//go:build windows

package localai

import (
    "errors"
    "math"

    "golang.org/x/sys/windows"
)

// windowsUntrackedPIDAlive is intentionally conservative: an inaccessible
// process cannot be declared dead merely because the service identity cannot
// inspect it. This is occupancy evidence ONLY, never authority to terminate
// or reuse the PID: VerifyIdentity independently rejects untracked processes.
func windowsUntrackedPIDAlive(pid int) bool {
    if pid <= 1 {
        return false
    }
    if uint64(pid) > math.MaxUint32 {
        return true
    }
    h,err:=windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION,
        false,uint32(pid))
    if err!=nil{
        return windowsUntrackedProcessErrorIsOccupied(err)
    }
    defer windows.CloseHandle(h)
    var exitCode uint32
    if err:=windows.GetExitCodeProcess(h,&exitCode);err!=nil{
        return true
    }
    // STILL_ACTIVE (259) is also a legal exit code; a false positive is
    // safer than accidental GPU overcommit or signalling an unrelated PID.
    return exitCode==259
}

func windowsUntrackedProcessErrorIsOccupied(err error) bool {
    // ERROR_INVALID_PARAMETER for OpenProcess means no such process.
    // ACCESS_DENIED or other uncertainty must block a new model instead.
    return !errors.Is(err,windows.ERROR_INVALID_PARAMETER)
}
