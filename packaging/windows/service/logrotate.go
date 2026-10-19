//go:build windows

package main

import (
 "errors"
 "fmt"
 "io"
 "os"
 "path/filepath"
 "sync"
)

// The service and backend share a bounded append-only log writer.
const onePaneLogMaxBytes=int64(16<<20)
type boundedLogWriter struct {
 mu sync.Mutex
 path string
 file *os.File
 size int64
}

func openPrivateServiceLogFile(path string)(*os.File,error){
 // Follow no existing log symlinks/reparse files. A service log can contain
 // secrets, startup errors or local-model paths and must stay in its data root.
 if err:=inspectExistingServiceLog(path);err!=nil{return nil,err}
 f,err:=os.OpenFile(path,os.O_CREATE|os.O_WRONLY|os.O_APPEND,0o600)
 if err!=nil{return nil,err}
 closeError:=func(err error)(*os.File,error){_ = f.Close();return nil,err}
 // Apply explicit DACL before appending anything, including error details.
 if err:=securePrivateServiceFile(f);err!=nil{return closeError(err)}
 held,err:=f.Stat()
 if err!=nil{return closeError(err)}
 named,err:=os.Lstat(path)
 if err!=nil||!named.Mode().IsRegular()||!os.SameFile(held,named){
  return closeError(fmt.Errorf("service log pathname changed during protected open: %v",err))
 }
 return f,nil
}
func inspectExistingServiceLog(path string)error{
 st,err:=os.Lstat(path)
 if os.IsNotExist(err){return nil}
 if err!=nil{return err}
 if !st.Mode().IsRegular(){return fmt.Errorf("service log path is not a regular file")}
 return nil
}
// Keep an older rotated service log private as well. It may contain
// diagnostic secrets from an earlier version using inherited NTFS ACLs.
func securePreviousServiceLog(path string)error{
 previous:=path+".1"
 if err:=inspectExistingServiceLog(previous);err!=nil{return err}
 if _,err:=os.Lstat(previous);os.IsNotExist(err){return nil}else if err!=nil{return err}
 f,err:=openPrivateServiceLogFile(previous)
 if err!=nil{return err}
 return f.Close()
}
func openBoundedLogWriter(path string)(*boundedLogWriter,error){
 if err:=os.MkdirAll(filepath.Dir(path),0o755);err!=nil{return nil,err}
 if err:=securePreviousServiceLog(path);err!=nil{return nil,err}
 f,err:=openPrivateServiceLogFile(path)
 if err!=nil{return nil,err}
 st,err:=f.Stat();if err!=nil{_ = f.Close();return nil,err}
 return &boundedLogWriter{path:path,file:f,size:st.Size()},nil
}
func (w *boundedLogWriter) rotate()error{
 return w.rotateWith(os.Remove,os.Rename)
}

// A transient Windows rename/share-denial must not permanently disable
// logging. An existing rotated log is checked/deleted BEFORE closing the
// active handle; a failed rename reopens and revalidates the same file.
func (w *boundedLogWriter) rotateWith(remove func(string)error,rename func(string,string)error)error{
 return w.rotateWithOpen(remove,rename,openPrivateServiceLogFile)
}

// Internal operation injection exercises the Windows case where rename
// succeeds but creating a protected replacement log is denied. Rollback is
// allowed ONLY while no other object occupies the original pathname.
func (w *boundedLogWriter) rotateWithOpen(remove func(string)error,
 rename func(string,string)error,open func(string)(*os.File,error))error{
 if w.file==nil{return os.ErrClosed}
 previous:=w.path+".1"
 if err:=remove(previous);err!=nil&&!os.IsNotExist(err){return err}
 original,err:=w.file.Stat()
 if err!=nil{return err}
 if err:=w.file.Close();err!=nil{w.file=nil;return err}
 w.file=nil
 if err:=rename(w.path,previous);err!=nil{
  return w.reopenAfterFailedRename(original,err)
 }
 f,err:=open(w.path)
 if err!=nil{
  // Do not overwrite a pathname newly created by another process. If no
  // replacement was created, put the exact original file back and reopen it
  // using the protected ACL path; never silently drop diagnostic history.
  _,pathErr:=os.Lstat(w.path)
  if pathErr==nil{
   return errors.Join(err,fmt.Errorf("replacement service log path already exists; refusing destructive rollback"))
  }
  if !os.IsNotExist(pathErr){return errors.Join(err,pathErr)}
  if restoreErr:=rename(previous,w.path);restoreErr!=nil{
   return errors.Join(err,fmt.Errorf("restore original service log: %w",restoreErr))
  }
  return w.reopenAfterFailedRename(original,err)
 }
 w.file=f
 w.size=0
 return nil
}

func (w *boundedLogWriter) reopenAfterFailedRename(original os.FileInfo,cause error)error{
 f,err:=openPrivateServiceLogFile(w.path)
 if err!=nil{return errors.Join(cause,err)}
 info,err:=f.Stat()
 if err!=nil||!os.SameFile(original,info){
  _=f.Close()
  return errors.Join(cause,fmt.Errorf("service log pathname changed during rotation recovery"))
 }
 w.file=f
 w.size=info.Size()
 return cause
}
// Write bounds the disk footprint even if a single stdout/stderr chunk is
// larger than the log limit. Splitting the input retains the most recent
// history across the current and one rotated file without dropping bytes.
func (w *boundedLogWriter) Write(p []byte)(int,error){
 w.mu.Lock()
 defer w.mu.Unlock()
 if w.file==nil{return 0,os.ErrClosed}
 written:=0
 for len(p)>0{
  if w.size>=onePaneLogMaxBytes{
   if err:=w.rotate();err!=nil{return written,err}
  }
  available:=onePaneLogMaxBytes-w.size
  chunk:=len(p)
  if int64(chunk)>available{chunk=int(available)}
  n,err:=w.file.Write(p[:chunk])
  w.size+=int64(n)
  written+=n
  p=p[n:]
  if err!=nil{return written,err}
  if n!=chunk{return written,io.ErrShortWrite}
 }
 return written,nil
}
func (w *boundedLogWriter) Close()error{
 w.mu.Lock();defer w.mu.Unlock()
 if w.file==nil{return nil}
 err:=w.file.Close();w.file=nil;return err
}
