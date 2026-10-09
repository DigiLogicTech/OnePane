package sandboxrunner

import (
 "fmt"
 "os"
 "path"
 "path/filepath"
 "strings"
)

// workspaceTextPreview opens every path element with O_NOFOLLOW inside the
// actual container, eliminating any symlink race between a host observation
// and the subsequent OCI read. Python3 is an explicit toolchain requirement.
const workspaceTextPreview = `import os, stat, sys
relative = sys.argv[1]
parts = relative.split("/")
root = os.open("/workspace", os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
directory = root
try:
    for piece in parts[:-1]:
        nextfd = os.open(piece, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=directory)
        if directory != root: os.close(directory)
        directory = nextfd
    fd = os.open(parts[-1], os.O_RDONLY | os.O_NONBLOCK | os.O_NOFOLLOW, dir_fd=directory)
    try:
        if not stat.S_ISREG(os.fstat(fd).st_mode):
            raise ValueError("preview requires regular file")
        content = os.read(fd, 65537)
        if b'\\x00' in content:
            raise ValueError("binary file cannot be text-previewed")
        content[:65536].decode("utf-8", errors="strict")
        sys.stdout.buffer.write(content[:65536])
    finally:
        os.close(fd)
except (OSError, ValueError, UnicodeError) as exc:
    print("Workspace text preview denied: " + str(exc), file=sys.stderr)
    sys.exit(2)
finally:
    if directory != root: os.close(directory)
    os.close(root)
`

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
  return []string{"python3","-I","-S","-c",workspaceTextPreview,relative},nil
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
