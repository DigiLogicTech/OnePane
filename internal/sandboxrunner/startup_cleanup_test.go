package sandboxrunner

import (
 "errors"
 "os"
 "path/filepath"
 "testing"
 "time"
)

func TestStartupCredentialSweepOnlyTouchesExpiredManagedPrivateFiles(t *testing.T){
 root:=t.TempDir()
 managed:=filepath.Join(root,"projects","runtime-world")
 workspace:=filepath.Join(managed,"workspace")
 private:=filepath.Join(managed,".onepane-private-env")
 if err:=os.MkdirAll(workspace,0o700);err!=nil{t.Fatal(err)}
 if err:=os.Mkdir(private,0o700);err!=nil{t.Fatal(err)}
 makeFile:=func(path string,older bool){
  t.Helper()
  if err:=os.WriteFile(path,[]byte("not-to-log"),0o600);err!=nil{t.Fatal(err)}
  if older{
   old:=time.Now().Add(-48*time.Hour)
   if err:=os.Chtimes(path,old,old);err!=nil{t.Fatal(err)}
  }
 }
 stale:=filepath.Join(private,"onepane-sandbox-env-crashed")
 recent:=filepath.Join(private,"onepane-sandbox-env-active")
 unrelated:=filepath.Join(private,"my-own-backup")
 workspaceFile:=filepath.Join(workspace,"onepane-sandbox-env-source")
 for _,tc:=range []struct{p string;old bool}{
  {stale,true},{recent,false},{unrelated,true},{workspaceFile,true},
 }{makeFile(tc.p,tc.old)}
 if err:=CleanupStaleRuntimeCredentialFiles(root);err!=nil{t.Fatal(err)}
 if _,err:=os.Lstat(stale);!errors.Is(err,os.ErrNotExist){
  t.Fatalf("expired managed secret not removed: %v",err)
 }
 for _,p:=range []string{recent,unrelated,workspaceFile}{
  if _,err:=os.Lstat(p);err!=nil{t.Fatalf("startup touched protected file %s: %v",p,err)}
 }
}

func TestStartupSweepNeverFollowsSymlinkedRuntimeOrCredentialFolder(t *testing.T){
 root:=t.TempDir()
 outside:=t.TempDir()
 projects:=filepath.Join(root,"projects")
 if err:=os.Mkdir(projects,0o700);err!=nil{t.Fatal(err)}
 old:=filepath.Join(outside,"onepane-sandbox-env-foreign")
 if err:=os.WriteFile(old,[]byte("private"),0o600);err!=nil{t.Fatal(err)}
 before:=time.Now().Add(-48*time.Hour)
 if err:=os.Chtimes(old,before,before);err!=nil{t.Fatal(err)}
 if err:=os.Symlink(outside,filepath.Join(projects,"runtime-foreign"));err!=nil{
  t.Skipf("symlinks unavailable: %v",err)
 }
 runtimeRoot:=filepath.Join(projects,"runtime-world")
 if err:=os.Mkdir(runtimeRoot,0o700);err!=nil{t.Fatal(err)}
 if err:=os.Symlink(outside,filepath.Join(runtimeRoot,".onepane-private-env"));err!=nil{t.Fatal(err)}
 if err:=CleanupStaleRuntimeCredentialFiles(root);!errors.Is(err,ErrInvalidInput){
  t.Fatalf("symlinked secret folder accepted: %v",err)
 }
 if _,err:=os.Lstat(old);err!=nil{t.Fatalf("symlink startup sweep escaped configured root: %v",err)}
}

func TestStartupSweepDoesNotCreateOrModifyMissingRoots(t *testing.T){
 root:=t.TempDir()
 if err:=CleanupStaleRuntimeCredentialFiles(root);err!=nil{t.Fatal(err)}
 if _,err:=os.Lstat(filepath.Join(root,"projects"));!errors.Is(err,os.ErrNotExist){
  t.Fatalf("cleanup created workspace root: %v",err)
 }
 if err:=CleanupStaleRuntimeCredentialFiles("");!errors.Is(err,ErrInvalidInput){
  t.Fatalf("empty root was accepted: %v",err)
 }
}
