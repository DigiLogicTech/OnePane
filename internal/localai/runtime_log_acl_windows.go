//go:build windows

package localai

import (
 "fmt"
 "os"
 "strings"
 "golang.org/x/sys/windows"
)

// Unix 0600 is not a Windows NTFS DACL. Managed model logs can contain
// private paths, prompts or provider credentials. Protect before append,
// verify via named-object API, then recheck same-file identity in caller.
func protectManagedRuntimeLog(f *os.File)error{
 if f==nil{return fmt.Errorf("nil managed runtime log")}
 user,err:=windows.GetCurrentProcessToken().GetTokenUser()
 if err!=nil{return fmt.Errorf("resolve local model service SID: %w",err)}
 sid:=user.User.Sid.String()
 if sid==""{return fmt.Errorf("empty local model service SID")}
 sd,err:=windows.SecurityDescriptorFromString(
  "D:P(A;;FA;;;SY)(A;;FA;;;BA)(A;;FA;;;"+sid+")")
 if err!=nil{return fmt.Errorf("prepare private runtime-log DACL: %w",err)}
 dacl,_,err:=sd.DACL()
 if err!=nil||dacl==nil{return fmt.Errorf("read runtime-log DACL: %v",err)}
 if err:=windows.SetNamedSecurityInfo(f.Name(),windows.SE_FILE_OBJECT,
  windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
  nil,nil,dacl,nil);err!=nil{return fmt.Errorf("protect runtime log ACL: %w",err)}
 actual,err:=windows.GetNamedSecurityInfo(f.Name(),windows.SE_FILE_OBJECT,
  windows.DACL_SECURITY_INFORMATION)
 if err!=nil{return fmt.Errorf("inspect runtime log ACL: %w",err)}
 flags,_,err:=actual.Control()
 if err!=nil{return err}
 applied,_,err:=actual.DACL()
 if err!=nil||applied==nil||flags&windows.SE_DACL_PROTECTED==0{
  return fmt.Errorf("runtime log ACL absent or inherited")
 }
 sddl:=strings.ToUpper(actual.String())
 for _,broad:=range []string{";;;WD)",";;;AU)",";;;BU)",
  ";;;S-1-1-0)",";;;S-1-5-11)",";;;S-1-5-32-545)"}{
  if strings.Contains(sddl,broad){return fmt.Errorf("runtime log ACL grants broad principal")}
 }
 return nil
}
