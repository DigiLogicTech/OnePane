package sandboxrunner

import (
 "errors"
 "fmt"
 "os"
 "path/filepath"
 "time"
)

// CleanupStaleRuntimeCredentialFiles is a bounded startup sweep for private
// OCI env-file staging left by an earlier crash. It scans only
// <configured-data-root>/projects/<managed-runtime>/.onepane-private-env,
// never user Workspaces, models, global temp, project libraries or recovery
// snapshots. Symlinks and nonmanaged runtime folders are never followed.
func CleanupStaleRuntimeCredentialFiles(dataDir string)(retErr error){
 if dataDir==""{return fmt.Errorf("%w: data directory required",ErrInvalidInput)}
 root,err:=filepath.Abs(dataDir)
 if err!=nil{return err}
 root,err=filepath.EvalSymlinks(root)
 if errors.Is(err,os.ErrNotExist){return nil}
 if err!=nil{return err}
 projectRoot:=filepath.Join(root,"projects")
 st,err:=os.Lstat(projectRoot)
 if errors.Is(err,os.ErrNotExist){return nil}
 if err!=nil{return err}
 if !st.IsDir()||st.Mode()&os.ModeSymlink!=0{
  return fmt.Errorf("%w: projects root is not a managed directory",ErrInvalidInput)
 }
 runtimes,err:=os.ReadDir(projectRoot)
 if err!=nil{return err}
 now:=time.Now()
 for _,entry:=range runtimes{
  if !entry.IsDir()||!safeID.MatchString(entry.Name()){continue}
  runtimeRoot:=filepath.Join(projectRoot,entry.Name())
  runtimeInfo,err:=os.Lstat(runtimeRoot)
  if err!=nil{
   retErr=errors.Join(retErr,fmt.Errorf("inspect runtime directory %s: %w",entry.Name(),err))
   continue
  }
  if !runtimeInfo.IsDir()||runtimeInfo.Mode()&os.ModeSymlink!=0{continue}
  staging:=filepath.Join(runtimeRoot,".onepane-private-env")
  info,err:=os.Lstat(staging)
  if errors.Is(err,os.ErrNotExist){continue}
  if err!=nil{
   retErr=errors.Join(retErr,fmt.Errorf("inspect private credential staging: %w",err))
   continue
  }
  if !info.IsDir()||info.Mode()&os.ModeSymlink!=0||info.Mode().Perm()&0o077!=0{
   retErr=errors.Join(retErr,fmt.Errorf("%w: insecure credential staging in runtime %s",ErrInvalidInput,entry.Name()))
   continue
  }
  if err:=cleanupStaleEnvFiles(staging,now);err!=nil{
   retErr=errors.Join(retErr,fmt.Errorf("cleanup credential staging in runtime %s: %w",entry.Name(),err))
  }
 }
 return retErr
}
