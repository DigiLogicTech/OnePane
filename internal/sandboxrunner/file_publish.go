package sandboxrunner

import (
 "context"
 "crypto/sha256"
 "encoding/base64"
 "encoding/hex"
 "encoding/json"
 "fmt"
 "strings"
)

const WorkspacePublicationLimit = 256 << 10

// workspacePublicationRead runs only inside a rootless OCI application whose
// mount and identity were independently verified. No parent/leaf symlink can
// be followed, even if the source file changes after the host-side precheck.
const workspacePublicationRead = `import base64,hashlib,json,os,stat,sys
parts=sys.argv[1].split("/")
root=os.open("/workspace",os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW)
parent=root
try:
    for segment in parts[:-1]:
        nxt=os.open(segment,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW,dir_fd=parent)
        if parent!=root:os.close(parent)
        parent=nxt
    fd=os.open(parts[-1],os.O_RDONLY|os.O_NONBLOCK|os.O_NOFOLLOW,dir_fd=parent)
    try:
        if not stat.S_ISREG(os.fstat(fd).st_mode):
            raise ValueError("publication requires regular file")
        content=os.read(fd,262145)
        if len(content)>262144:
            raise ValueError("publication exceeds the 256KiB single-file limit")
        print(json.dumps({"content_base64":base64.b64encode(content).decode("ascii"),
          "sha256":hashlib.sha256(content).hexdigest(),"size_bytes":len(content)}))
    finally:os.close(fd)
except (OSError,ValueError) as exc:
    print(json.dumps({"error":"Workspace publication source unavailable or unsafe"}),file=sys.stderr)
    sys.exit(2)
finally:
    if parent!=root:os.close(parent)
    os.close(root)
`

// WorkspacePublicationRequest is an internal trusted storage operation,
// populated after OCI content verification; models cannot choose a tenant,
// Project, provenance principal or destination via arbitrary request fields.
type WorkspacePublicationRequest struct{
 WorkspaceID string
 TaskID string
 AttemptID string
 RuntimeID string
 ApplicationID string
 Path string
 Name string
 MediaType string
 Content []byte
 ContentHash string
}
type WorkspacePublication struct{
 ArtifactID string `json:"artifact_id"`
 LibraryAssetID string `json:"library_asset_id"`
 Version int64 `json:"version"`
 ContentHash string `json:"content_hash"`
 SizeBytes int64 `json:"size_bytes"`
 SourceWorkspaceID string `json:"source_workspace_id"`
}
type WorkspacePublicationSink interface{
 PublishWorkspaceFile(context.Context,WorkspacePublicationRequest)(WorkspacePublication,error)
}

func publicationReadCommand(relative string)([]string,error){
 if !validWorkspaceRelativePath(relative)||strings.HasPrefix(relative,".onepane-"){
  return nil,fmt.Errorf("%w: invalid Workspace publication path",ErrInvalidInput)
 }
 return []string{"python3","-I","-S","-c",workspacePublicationRead,relative},nil
}

func decodePublicationRead(raw string)([]byte,string,error){
 var v struct{
  Contents string `json:"content_base64"`
  SHA256 string `json:"sha256"`
  Size int `json:"size_bytes"`
 }
 if json.Unmarshal([]byte(raw),&v)!=nil||v.Size<0||v.Size>WorkspacePublicationLimit||
  len(v.SHA256)!=64||len(v.Contents)>base64.StdEncoding.EncodedLen(WorkspacePublicationLimit){
  return nil,"",fmt.Errorf("%w: invalid OCI publication receipt",ErrInvalidInput)
 }
 b,err:=base64.StdEncoding.Strict().DecodeString(v.Contents)
 if err!=nil||base64.StdEncoding.EncodeToString(b)!=v.Contents||len(b)!=v.Size{
  return nil,"",fmt.Errorf("%w: OCI publication content is not canonical",ErrInvalidInput)
 }
 digest:=sha256.Sum256(b)
 if hex.EncodeToString(digest[:])!=v.SHA256{
  return nil,"",fmt.Errorf("%w: OCI publication SHA-256 mismatch",ErrInvalidInput)
 }
 return b,v.SHA256,nil
}
