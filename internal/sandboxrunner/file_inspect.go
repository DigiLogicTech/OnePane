package sandboxrunner

import (
 "fmt"
 "os"
 "path"
 "path/filepath"
 "strings"
)

// fileInspectCommand only allows listing or bounded text preview. There is
// no model-controlled executable or shell. The read path is independently
// checked against the managed Workspace from the service's own data root.
func fileInspectCommand(action,relative string)([]string,error){
 switch action{
 case "list":
  if relative!=""{return nil,fmt.Errorf("%w: file list cannot select host path",ErrInvalidInput)}
  return []string{"find","/workspace","-maxdepth","4",
   "-name",".git","-prune","-o","-type","f","-print"},nil
 case "preview_text":
  if !validWorkspaceRelativePath(relative){
   return nil,fmt.Errorf("%w: invalid relative Workspace path",ErrInvalidInput)
  }
  return []string{"head","-c","65536","--","/workspace/"+relative},nil
 default:
  return nil,fmt.Errorf("%w: unsupported Workspace file inspection",ErrInvalidInput)
 }
}

func validWorkspaceRelativePath(rel string) bool {
 if rel==""||len(rel)>512||path.IsAbs(rel)||strings.ContainsAny(rel,"\\:\x00"){
  return false
 }
 if strings.TrimSpace(rel)!=rel||path.Clean(rel)!=rel{return false}
 for _,c:=range rel{
  if c<32||c==127{return false}
 }
 for _,p:=range strings.Split(rel,"/"){
  if p==""||p=="."||p==".."||p==".git"{return false}
 }
 return true
}

// Verify every existing path segment via Lstat, never following symlinks into
// another runtime, a host path, .git internals, or an OCI secret. Revalidation
// at container execution remains mandatory; this is defence in depth for a
// text preview, NOT a host filesystem access or a mutation capability.
func verifyWorkspacePreviewPath(workspace,relative string) error{
 if !validWorkspaceRelativePath(relative){return ErrInvalidInput}
 base,err:=filepath.Abs(workspace)
 if err!=nil{return err}
 current:=base
 parts:=strings.Split(relative,"/")
 for i,p:=range parts {
  current=filepath.Join(current,p)
  info,err:=os.Lstat(current)
  if err!=nil{return fmt.Errorf("%w: Workspace preview target unavailable",ErrInvalidInput)}
  if info.Mode()&os.ModeSymlink!=0{
   return fmt.Errorf("%w: Workspace symlink preview is denied",ErrInvalidInput)
  }
  if i<len(parts)-1 {
   if !info.IsDir(){return ErrInvalidInput}
  }else if !info.Mode().IsRegular(){
   return fmt.Errorf("%w: Workspace preview requires a regular file",ErrInvalidInput)
  }
 }
 return nil
}
