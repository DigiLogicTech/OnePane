//go:build windows

package main

import (
 "os"
 "path/filepath"
 "sync"
)

// The service and its child share a bounded append-only log writer. Rotation
// happens under the same lock so a long-running harness cannot exhaust C:.
const onePaneLogMaxBytes=int64(16<<20)
type boundedLogWriter struct {
 mu sync.Mutex
 path string
 file *os.File
 size int64
}
func openBoundedLogWriter(path string)(*boundedLogWriter,error){
 if err:=os.MkdirAll(filepath.Dir(path),0o755);err!=nil{return nil,err}
 f,err:=os.OpenFile(path,os.O_CREATE|os.O_WRONLY|os.O_APPEND,0o644)
 if err!=nil{return nil,err}
 st,err:=f.Stat();if err!=nil{_ = f.Close();return nil,err}
 return &boundedLogWriter{path:path,file:f,size:st.Size()},nil
}
func (w *boundedLogWriter) rotate()error{
 if err:=w.file.Close();err!=nil{return err}
 previous:=w.path+".1"
 if err:=os.Remove(previous);err!=nil&&!os.IsNotExist(err){return err}
 if err:=os.Rename(w.path,previous);err!=nil{return err}
 f,err:=os.OpenFile(w.path,os.O_CREATE|os.O_WRONLY|os.O_APPEND,0o644)
 if err!=nil{return err}
 w.file=f;w.size=0
 return nil
}
func (w *boundedLogWriter) Write(p []byte)(int,error){
 w.mu.Lock();defer w.mu.Unlock()
 if w.file==nil{return 0,os.ErrClosed}
 if w.size>0&&w.size+int64(len(p))>onePaneLogMaxBytes{
  if err:=w.rotate();err!=nil{return 0,err}
 }
 n,err:=w.file.Write(p);w.size+=int64(n);return n,err
}
func (w *boundedLogWriter) Close()error{
 w.mu.Lock();defer w.mu.Unlock()
 if w.file==nil{return nil}
 err:=w.file.Close();w.file=nil;return err
}
