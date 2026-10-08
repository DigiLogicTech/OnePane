//go:build windows

package localai

import (
 "fmt"
 "os"
 "path/filepath"
 "syscall"
 "unsafe"
)

var diskSpaceKernel32=syscall.NewLazyDLL("kernel32.dll")
var getDiskFreeSpaceExW=diskSpaceKernel32.NewProc("GetDiskFreeSpaceExW")

// The on-disk runtime can temporarily coexist with its old generation, its
// verified compressed archive and staging files. Refuse the update rather
// than exhausting the system volume and leaving a half-installed runtime.
func checkRuntimeDiskBudget(destination string,reserveBytes int64) error {
 directory:=filepath.Clean(destination)
 for {
  info,err:=os.Stat(directory)
  if err==nil&&info.IsDir(){break}
  parent:=filepath.Dir(directory)
  if parent==directory{return fmt.Errorf("cannot resolve runtime storage volume for %s",destination)}
  directory=parent
 }
 wide,err:=syscall.UTF16PtrFromString(directory)
 if err!=nil{return err}
 var available,total,free uint64
 result,_,callErr:=getDiskFreeSpaceExW.Call(
  uintptr(unsafe.Pointer(wide)),
  uintptr(unsafe.Pointer(&available)),
  uintptr(unsafe.Pointer(&total)),
  uintptr(unsafe.Pointer(&free)),
 )
 if result==0{return fmt.Errorf("check runtime disk space: %w",callErr)}
 if available<uint64(reserveBytes){
  return fmt.Errorf("not enough free space for managed runtime update: %.1f GiB available, %.1f GiB required (includes rollback space). Move OnePane runtime storage to a larger volume before retrying",float64(available)/float64(1<<30),float64(reserveBytes)/float64(1<<30))
 }
 return nil
}
