package sandboxrunner

import (
 "crypto/sha256"
 "encoding/base64"
 "encoding/hex"
 "fmt"
 "strings"
)

// WorkspaceFileEditLimit prevents oversized tool proposals and unbounded
// command line arguments. Larger build artifacts use a separate verified
// publication workflow, never a model-supplied shell command.
const WorkspaceFileEditLimit = 64 << 10

// workspaceFileEditor is source owned by OnePane, not an agent-supplied
// program. Executed only inside an already verified, rootless OCI application.
// Directory and file descriptors are opened relative to /workspace using
// O_NOFOLLOW; updates use a same-directory fsynced atomic rename. Python 3
// must be provided by the digest-pinned Workspace toolchain image.
const workspaceFileEditor = `import base64, errno, hashlib, json, os, secrets, stat, sys
action, relative, data64, expected = sys.argv[1:]
data = base64.b64decode(data64, validate=True)
parts = relative.split("/")
root = os.open("/workspace", os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
directory = root
temporary = None
try:
    for piece in parts[:-1]:
        nextfd = os.open(piece, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=directory)
        if directory != root: os.close(directory)
        directory = nextfd
    leaf = parts[-1]
    if action == "mkdir":
        os.mkdir(leaf, 0o700, dir_fd=directory)
        os.fsync(directory)
        print(json.dumps({"action":action,"path":relative,"bytes":0,"sha256":hashlib.sha256(b"").hexdigest(),"written":True}))
    else:
        try:
            existing = os.open(leaf, os.O_RDONLY | os.O_NOFOLLOW, dir_fd=directory)
        except FileNotFoundError:
            existing = None
        if action == "create":
            if existing is not None:
                os.close(existing)
                raise FileExistsError("Workspace file already exists")
        else:
            if existing is None: raise FileNotFoundError("Workspace file does not exist")
            try:
                info = os.fstat(existing)
                if not stat.S_ISREG(info.st_mode): raise ValueError("not a regular file")
                if info.st_size > 16 * 1024 * 1024: raise ValueError("existing file too large for a safe compare-and-swap")
                digest = hashlib.sha256()
                while True:
                    chunk = os.read(existing, 65536)
                    if not chunk: break
                    digest.update(chunk)
                if digest.hexdigest() != expected:
                    raise ValueError("file changed since the expected SHA-256 revision")
            finally:
                os.close(existing)
        temporary = ".onepane-write-" + secrets.token_hex(16)
        fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600, dir_fd=directory)
        try:
            view = memoryview(data)
            while view:
                view = view[os.write(fd, view):]
            os.fsync(fd)
        finally:
            os.close(fd)
        os.replace(temporary, leaf, src_dir_fd=directory, dst_dir_fd=directory)
        temporary = None
        os.fsync(directory)
        print(json.dumps({"action":action,"path":relative,"bytes":len(data),"sha256":hashlib.sha256(data).hexdigest(),"written":True}))
except (OSError, ValueError) as exc:
    print(json.dumps({"written":False,"reason":str(exc)}),file=sys.stderr)
    sys.exit(2)
finally:
    if temporary is not None:
        try: os.unlink(temporary,dir_fd=directory)
        except OSError: pass
    if directory != root: os.close(directory)
    os.close(root)
`

func fileEditCommand(action, relative, contentsB64, expectedSHA string) ([]string,string,int,error) {
 if action!="create"&&action!="replace"&&action!="mkdir" {
  return nil,"",0,fmt.Errorf("%w: only mkdir, create or compare-and-swap replace is supported",ErrInvalidInput)
 }
 if !validWorkspaceRelativePath(relative) || strings.HasPrefix(relative,".onepane-"){
  return nil,"",0,fmt.Errorf("%w: invalid Workspace file path",ErrInvalidInput)
 }
 if action=="mkdir"{
  if contentsB64!=""||expectedSHA!=""{
   return nil,"",0,fmt.Errorf("%w: directory creation cannot contain file contents or a revision",ErrInvalidInput)
  }
  digest:=sha256.Sum256(nil)
  return []string{"python3","-I","-S","-c",workspaceFileEditor,action,relative,"",""},hex.EncodeToString(digest[:]),0,nil
 }
 if len(contentsB64)==0 || len(contentsB64)>base64.StdEncoding.EncodedLen(WorkspaceFileEditLimit) {
  return nil,"",0,fmt.Errorf("%w: file contents must be 1..65536 bytes encoded as base64",ErrInvalidInput)
 }
 data,err:=base64.StdEncoding.Strict().DecodeString(contentsB64)
 if err!=nil||len(data)==0||len(data)>WorkspaceFileEditLimit||
  base64.StdEncoding.EncodeToString(data)!=contentsB64 {
  return nil,"",0,fmt.Errorf("%w: invalid canonical base64 contents",ErrInvalidInput)
 }
 if action=="create"&&expectedSHA!=""{
  return nil,"",0,fmt.Errorf("%w: create cannot overwrite a revision",ErrInvalidInput)
 }
 if action=="replace"{
  if len(expectedSHA)!=64{return nil,"",0,fmt.Errorf("%w: expected SHA-256 required for replacement",ErrInvalidInput)}
  for _,c:=range expectedSHA{if !strings.ContainsRune("0123456789abcdef",c){
   return nil,"",0,fmt.Errorf("%w: invalid SHA-256 revision",ErrInvalidInput)
  }}
 }
 digest:=sha256.Sum256(data)
 return []string{"python3","-I","-S","-c",workspaceFileEditor,action,relative,contentsB64,expectedSHA},hex.EncodeToString(digest[:]),len(data),nil
}

func boundedEditorDiagnostic(reason string) string {
 if len(reason)>384{reason=reason[:384]}
 return strings.Map(func(r rune)rune {
  if r < ' ' || r == 127{return -1}
  return r
 },reason)
}
