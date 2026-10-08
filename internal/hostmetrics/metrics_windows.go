//go:build windows
package hostmetrics

import (
 "fmt"
 "os"
 "syscall"
 "unsafe"
 "strings"
)
var winKernel=syscall.NewLazyDLL("kernel32.dll")
var winPSAPI=syscall.NewLazyDLL("psapi.dll")
type winFiletime struct{LowDateTime,HighDateTime uint32}
func (f winFiletime) value()uint64{return uint64(f.HighDateTime)<<32|uint64(f.LowDateTime)}
func readCPU()(idle,total uint64,err error){
 var i,k,u winFiletime
 ok,_,e:=winKernel.NewProc("GetSystemTimes").Call(uintptr(unsafe.Pointer(&i)),uintptr(unsafe.Pointer(&k)),uintptr(unsafe.Pointer(&u)))
 if ok==0{return 0,0,fmt.Errorf("GetSystemTimes: %v",e)}
 return i.value(),k.value()+u.value(),nil
}
type winMemoryStatus struct{
 Length uint32
 MemoryLoad uint32
 TotalPhys,AvailPhys,TotalPageFile,AvailPageFile,TotalVirtual,AvailVirtual,AvailExtendedVirtual uint64
}
func readMemory()(memStats,error){
 var s winMemoryStatus;s.Length=uint32(unsafe.Sizeof(s))
 ok,_,e:=winKernel.NewProc("GlobalMemoryStatusEx").Call(uintptr(unsafe.Pointer(&s)))
 if ok==0{return memStats{},fmt.Errorf("GlobalMemoryStatusEx: %v",e)}
 return memStats{total:s.TotalPhys,available:s.AvailPhys},nil
}
func readDisk()(diskStats,error){
 drive:=strings.TrimSpace(os.Getenv("SystemDrive"));if drive==""{drive="C:"}
 root:=drive+"\\"
 u,e:=syscall.UTF16PtrFromString(root);if e!=nil{return diskStats{},e}
 var free,total,available uint64
 ok,_,err:=winKernel.NewProc("GetDiskFreeSpaceExW").Call(uintptr(unsafe.Pointer(u)),uintptr(unsafe.Pointer(&free)),uintptr(unsafe.Pointer(&total)),uintptr(unsafe.Pointer(&available)))
 if ok==0{return diskStats{},fmt.Errorf("GetDiskFreeSpaceExW: %v",err)}
 return diskStats{path:root,total:total,free:available},nil
}
func readNetwork()(rx,tx uint64,err error){return 0,0,fmt.Errorf("Windows network throughput collector not available")}
type winProcessMemoryCounters struct{
 CB,PageFaultCount uint32
 PeakWorkingSetSize,WorkingSetSize,QuotaPeakPagedPoolUsage,QuotaPagedPoolUsage,QuotaPeakNonPagedPoolUsage,QuotaNonPagedPoolUsage,PagefileUsage,PeakPagefileUsage uintptr
}
func readProcessRSS()(uint64,error){
 var s winProcessMemoryCounters;s.CB=uint32(unsafe.Sizeof(s))
 proc,_,_:=winKernel.NewProc("GetCurrentProcess").Call()
 ok,_,e:=winPSAPI.NewProc("GetProcessMemoryInfo").Call(proc,uintptr(unsafe.Pointer(&s)),uintptr(s.CB))
 if ok==0{return 0,fmt.Errorf("GetProcessMemoryInfo: %v",e)}
 return uint64(s.WorkingSetSize),nil
}
