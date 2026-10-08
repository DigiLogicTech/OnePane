//go:build windows

package main

import (
 "fmt"
 "os"
 "syscall"
 "unsafe"
)

// A Windows Job object closes the gap left by a crashed harnessd: all
// registered descendants are terminated when the service closes its job.
// The normal path still uses authenticated graceful shutdown first.
const jobObjectExtendedLimitInformation=9
const jobObjectLimitKillOnJobClose=0x00002000
type jobBasicLimit struct {
 PerProcessUserTimeLimit int64
 PerJobUserTimeLimit int64
 LimitFlags uint32
 MinimumWorkingSetSize uintptr
 MaximumWorkingSetSize uintptr
 ActiveProcessLimit uint32
 Affinity uintptr
 PriorityClass uint32
 SchedulingClass uint32
}
type jobIOCounters struct{ReadOperationCount,WriteOperationCount,OtherOperationCount,ReadTransferCount,WriteTransferCount,OtherTransferCount uint64}
type jobExtendedLimit struct{
 BasicLimitInformation jobBasicLimit
 IoInfo jobIOCounters
 ProcessMemoryLimit uintptr
 JobMemoryLimit uintptr
 PeakProcessMemoryUsed uintptr
 PeakJobMemoryUsed uintptr
}
var jobKernel32=syscall.NewLazyDLL("kernel32.dll")
var createJobObjectW=jobKernel32.NewProc("CreateJobObjectW")
var setInformationJobObject=jobKernel32.NewProc("SetInformationJobObject")
var assignProcessToJobObject=jobKernel32.NewProc("AssignProcessToJobObject")

type ownedBackendJob struct{handle syscall.Handle}
func newOwnedBackendJob()(*ownedBackendJob,error){
 h,_,callErr:=createJobObjectW.Call(0,0)
 if h==0{return nil,fmt.Errorf("CreateJobObjectW: %w",callErr)}
 job:=&ownedBackendJob{handle:syscall.Handle(h)}
 limits:=jobExtendedLimit{}
 limits.BasicLimitInformation.LimitFlags=jobObjectLimitKillOnJobClose
 ok,_,err:=setInformationJobObject.Call(h,jobObjectExtendedLimitInformation,
  uintptr(unsafe.Pointer(&limits)),unsafe.Sizeof(limits))
 if ok==0{_ = job.Close();return nil,fmt.Errorf("SetInformationJobObject: %w",err)}
 return job,nil
}
func (job *ownedBackendJob) Assign(pid int)error{
 if pid<=1{return fmt.Errorf("invalid managed process PID")}
 h,err:=syscall.OpenProcess(syscall.PROCESS_SET_QUOTA|syscall.PROCESS_TERMINATE|syscall.PROCESS_QUERY_INFORMATION,false,uint32(pid))
 if err!=nil{return err}
 defer syscall.CloseHandle(h)
 ok,_,callErr:=assignProcessToJobObject.Call(uintptr(job.handle),uintptr(h))
 if ok==0{return fmt.Errorf("AssignProcessToJobObject: %w",callErr)}
 return nil
}
func (job *ownedBackendJob) Close()error{
 if job==nil||job.handle==0{return nil}
 err:=syscall.CloseHandle(job.handle);job.handle=0
 return err
}
var _ = os.ErrClosed
