//go:build windows
package hostmetrics

import (
 "fmt"
 "os"
 "syscall"
 "unsafe"
 "strings"
 "path/filepath"
 "sync"
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
// MIB_IFROW is the native GetIfTable record. Counters are 32-bit, so we
// accumulate per-adapter deltas to provide monotonic 64-bit totals to Sampler.
// The sampler turns these totals into bytes/second at its own sampling rate.
type mibIfRow struct {
 Name [256]uint16
 Index, Type, MTU, Speed, PhysLength uint32
 PhysicalAddress [8]byte
 AdminStatus, OperStatus, LastChange uint32
 InOctets, InUcastPkts, InNUcastPkts, InDiscards, InErrors, InUnknownProtos uint32
 OutOctets, OutUcastPkts, OutNUcastPkts, OutDiscards, OutErrors, OutQLen, DescrLen uint32
 Description [256]byte
}
type ifOctets struct { rx,tx uint32 }
var winNet struct {
 sync.Mutex
 prior map[uint32]ifOctets
 rx,tx uint64
}

func readNetwork()(rx,tx uint64,err error){
 proc:=syscall.NewLazyDLL("iphlpapi.dll").NewProc("GetIfTable")
 var size uint32
 proc.Call(0,uintptr(unsafe.Pointer(&size)),0)
 if size<4||size>16<<20 {return 0,0,fmt.Errorf("GetIfTable invalid table length %d",size)}
 buf:=make([]byte,size)
 result,_,e:=proc.Call(uintptr(unsafe.Pointer(&buf[0])),uintptr(unsafe.Pointer(&size)),0)
 if result!=0{return 0,0,fmt.Errorf("GetIfTable failed (%d): %v",result,e)}
 count:=*(*uint32)(unsafe.Pointer(&buf[0]))
 rowSize:=int(unsafe.Sizeof(mibIfRow{}))
 if count>uint32((len(buf)-4)/rowSize){return 0,0,fmt.Errorf("GetIfTable returned malformed interface count")}
 current:=make(map[uint32]ifOctets,count)
 for i:=uint32(0);i<count;i++{
  row:=(*mibIfRow)(unsafe.Pointer(&buf[4+int(i)*rowSize]))
  // 5 = operational; 24 = loopback. Do not count disconnected interfaces.
  if row.OperStatus!=5||row.Type==24{continue}
  current[row.Index]=ifOctets{row.InOctets,row.OutOctets}
 }
 winNet.Lock()
 defer winNet.Unlock()
 if winNet.prior==nil{winNet.prior=current;return winNet.rx,winNet.tx,nil}
 for id,cur:=range current{
  if prev,ok:=winNet.prior[id];ok{
   // Unsigned 32-bit subtraction handles interface counter rollover.
   drx,dtx:=uint32(cur.rx-prev.rx),uint32(cur.tx-prev.tx)
   // Reject apparent resets/adapter replacements; a single five-second
   // sample this large is not a credible ordinary client transfer.
   if drx<2<<30 {winNet.rx+=uint64(drx)}
   if dtx<2<<30 {winNet.tx+=uint64(dtx)}
  }
 }
 winNet.prior=current
 return winNet.rx,winNet.tx,nil
}
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

func readDiskAt(path string)(diskStats,error){
 root:=filepath.VolumeName(filepath.Clean(path))
 if root=="" {return diskStats{},fmt.Errorf("no volume for %q",path)}
 root+=string(os.PathSeparator)
 u,e:=syscall.UTF16PtrFromString(root);if e!=nil{return diskStats{},e}
 var free,total,available uint64
 ok,_,err:=winKernel.NewProc("GetDiskFreeSpaceExW").Call(uintptr(unsafe.Pointer(u)),uintptr(unsafe.Pointer(&free)),uintptr(unsafe.Pointer(&total)),uintptr(unsafe.Pointer(&available)))
 if ok==0{return diskStats{},fmt.Errorf("GetDiskFreeSpaceExW %s: %v",root,err)}
 return diskStats{path:root,total:total,free:available},nil
}
