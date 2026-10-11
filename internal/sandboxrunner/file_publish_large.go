package sandboxrunner

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/base64"
 "encoding/hex"
 "encoding/json"
 "fmt"
 "io"
 "strconv"
 "strings"
)

// Publication of substantial build outputs uses an explicit, finite, audited
// transfer. It cannot rely on unbounded single-command stdout; OCI runCLI
// deliberately caps command output to 1 MiB. This is a bounded first step,
// not yet a multi-GiB streaming or archival upload solution.
const WorkspaceLargePublicationLimit=32<<20
const workspacePublicationChunkSize=512<<10
const workspacePublicationReceiptLimit=900<<10

// A fresh no-follow descriptor is acquired on EVERY chunk. Device, inode,
// size, mtime and ctime must match the manifest for every read. Full SHA-256
// is computed inside OCI both before and after the transfer, and again over
// the assembled host bytes. This detects changes between chunk reads rather
// than combining bytes from multiple build versions into one published asset.
// Only OCI-local relative paths can be named: never host paths or commands.
const workspaceLargePublicationRead = `import os,sys,stat,hashlib,json,base64
kind=sys.argv[1]
parts=sys.argv[2].split("/")
offset=int(sys.argv[3])
root=os.open("/workspace",os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW)
parent=root
try:
 for part in parts[:-1]:
  child=os.open(part,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW,dir_fd=parent)
  if parent!=root:os.close(parent)
  parent=child
 fd=os.open(parts[-1],os.O_RDONLY|os.O_NONBLOCK|os.O_NOFOLLOW,dir_fd=parent)
 try:
  st=os.fstat(fd)
  if not stat.S_ISREG(st.st_mode) or st.st_size<1 or st.st_size>33554432:
   raise ValueError("unsupported size or file type")
  fingerprint=[st.st_dev,st.st_ino,st.st_size,st.st_mtime_ns,st.st_ctime_ns]
  if kind in ("manifest","verify"):
   if offset!=0:raise ValueError("bad offset")
   digest=hashlib.sha256()
   while True:
    block=os.read(fd,524288)
    if not block:break
    digest.update(block)
   st2=os.fstat(fd)
   if [st2.st_dev,st2.st_ino,st2.st_size,st2.st_mtime_ns,st2.st_ctime_ns]!=fingerprint:
    raise ValueError("changed during hash")
   print(json.dumps({"fingerprint":fingerprint,"size_bytes":st.st_size,"sha256":digest.hexdigest()}))
  elif kind=="chunk":
   if offset<0 or offset>=st.st_size or offset%524288:raise ValueError("bad offset")
   expected=min(524288,st.st_size-offset)
   os.lseek(fd,offset,os.SEEK_SET)
   blob=os.read(fd,expected)
   st2=os.fstat(fd)
   if len(blob)!=expected or [st2.st_dev,st2.st_ino,st2.st_size,st2.st_mtime_ns,st2.st_ctime_ns]!=fingerprint:
    raise ValueError("changed during read")
   print(json.dumps({"fingerprint":fingerprint,"offset":offset,"size_bytes":len(blob),
    "sha256":hashlib.sha256(blob).hexdigest(),"content_base64":base64.b64encode(blob).decode("ascii")}))
  else:raise ValueError("invalid action")
 finally:os.close(fd)
except (OSError,ValueError):
 print('{"error":"Workspace publication source unavailable or changed"}',file=sys.stderr)
 sys.exit(2)
finally:
 if parent!=root:os.close(parent)
 os.close(root)
`

type largePublicationManifest struct {
 Fingerprint []int64 `json:"fingerprint"`
 Size int64 `json:"size_bytes"`
 SHA256 string `json:"sha256"`
}
type largePublicationChunk struct{
 Fingerprint []int64 `json:"fingerprint"`
 Offset int64 `json:"offset"`
 Size int `json:"size_bytes"`
 SHA256 string `json:"sha256"`
 Content string `json:"content_base64"`
}

func publicationLargeReadCommand(relative,action string,offset int64)([]string,error){
 if !validWorkspaceRelativePath(relative)||strings.HasPrefix(relative,".onepane-"){
  return nil,fmt.Errorf("%w: invalid large publication Workspace path",ErrInvalidInput)
 }
 if action!="manifest"&&action!="verify"&&action!="chunk"{
  return nil,fmt.Errorf("%w: invalid large publication command",ErrInvalidInput)
 }
 if offset<0||offset>WorkspaceLargePublicationLimit||
  (action!="chunk"&&offset!=0)||(action=="chunk"&&offset%workspacePublicationChunkSize!=0){
  return nil,fmt.Errorf("%w: invalid large publication offset",ErrInvalidInput)
 }
 return []string{"python3","-I","-S","-c",workspaceLargePublicationRead,
  action,relative,strconv.FormatInt(offset,10)},nil
}

func decodePublicationJSON(raw string,dest any)error{
 if len(raw)==0||len(raw)>workspacePublicationReceiptLimit{
  return fmt.Errorf("%w: invalid bounded OCI publication receipt",ErrInvalidInput)
 }
 d:=json.NewDecoder(strings.NewReader(raw))
 d.DisallowUnknownFields()
 if err:=d.Decode(dest);err!=nil{return fmt.Errorf("%w: invalid OCI publication response",ErrInvalidInput)}
 var trailing any
 if err:=d.Decode(&trailing);err!=io.EOF{
  return fmt.Errorf("%w: trailing OCI publication response",ErrInvalidInput)
 }
 return nil
}
func validPublicationDigest(s string)bool{
 if len(s)!=64{return false}
 for _,c:=range s{if !((c>='0'&&c<='9')||(c>='a'&&c<='f')){return false}}
 return true
}
func validLargeFingerprint(m []int64)bool{
 if len(m)!=5{return false}
 return m[0]>=0&&m[1]>=0&&m[2]>0&&m[2]<=WorkspaceLargePublicationLimit&&m[3]>=0&&m[4]>=0
}
func sameLargeFingerprint(a,b []int64)bool{
 if !validLargeFingerprint(a)||!validLargeFingerprint(b){return false}
 for i:=range a{if a[i]!=b[i]{return false}}
 return true
}
func decodeLargeManifest(raw string)(largePublicationManifest,error){
 var m largePublicationManifest
 if err:=decodePublicationJSON(raw,&m);err!=nil{return m,err}
 if !validLargeFingerprint(m.Fingerprint)||m.Fingerprint[2]!=m.Size||!validPublicationDigest(m.SHA256){
  return largePublicationManifest{},fmt.Errorf("%w: invalid OCI large publication manifest",ErrInvalidInput)
 }
 return m,nil
}
func decodeLargeChunk(raw string,m largePublicationManifest,offset int64)([]byte,error){
 var c largePublicationChunk
 if err:=decodePublicationJSON(raw,&c);err!=nil{return nil,err}
 expected:=m.Size-offset
 if expected>workspacePublicationChunkSize{expected=workspacePublicationChunkSize}
 if !sameLargeFingerprint(c.Fingerprint,m.Fingerprint)||c.Offset!=offset||
  c.Size!=int(expected)||!validPublicationDigest(c.SHA256)||
  len(c.Content)>base64.StdEncoding.EncodedLen(workspacePublicationChunkSize){
  return nil,fmt.Errorf("%w: invalid OCI large publication chunk",ErrInvalidInput)
 }
 rawChunk,err:=base64.StdEncoding.Strict().DecodeString(c.Content)
 if err!=nil||len(rawChunk)!=int(expected)||
  base64.StdEncoding.EncodeToString(rawChunk)!=c.Content{
  return nil,fmt.Errorf("%w: noncanonical OCI large publication chunk",ErrInvalidInput)
 }
 digest:=sha256.Sum256(rawChunk)
 if hex.EncodeToString(digest[:])!=c.SHA256{
  clear(rawChunk)
  return nil,fmt.Errorf("%w: corrupted OCI large publication chunk",ErrInvalidInput)
 }
 return rawChunk,nil
}

type largePublicationExecutor func(context.Context,[]string)(ExecResult,error)

// readWorkspaceLargePublication has no host path access. It operates only
// through the already independently verified OCI engine connection supplied
// by the sandbox Adapter. The public Tool result never contains file bytes.
func readWorkspaceLargePublication(ctx context.Context,relative string,execute largePublicationExecutor)(out []byte,hash string,err error){
 if execute==nil{return nil,"",fmt.Errorf("%w: missing sandbox execution",ErrInvalidInput)}
 executeStage:=func(kind string,offset int64)(string,error){
  cmd,commandErr:=publicationLargeReadCommand(relative,kind,offset)
  if commandErr!=nil{return "",commandErr}
  observed,execErr:=execute(ctx,cmd)
  if ctx.Err()!=nil{return "",ctx.Err()}
  if execErr!=nil{return "",execErr}
  if observed.ExitCode!=0{
   return "",fmt.Errorf("%w: source unavailable or changed while reading",ErrInvalidInput)
  }
  return observed.Stdout,nil
 }
 first,err:=executeStage("manifest",0)
 if err!=nil{return nil,"",err}
 manifest,err:=decodeLargeManifest(first)
 if err!=nil{return nil,"",err}
 payload:=make([]byte,int(manifest.Size))
 valid:=false
 defer func(){if !valid{clear(payload)}}()
 for offset:=int64(0);offset<manifest.Size;offset+=workspacePublicationChunkSize{
  raw,stageErr:=executeStage("chunk",offset)
  if stageErr!=nil{return nil,"",stageErr}
  piece,decodeErr:=decodeLargeChunk(raw,manifest,offset)
  if decodeErr!=nil{return nil,"",decodeErr}
  copy(payload[int(offset):],piece)
  clear(piece)
 }
 local:=sha256.Sum256(payload)
 if hex.EncodeToString(local[:])!=manifest.SHA256{
  return nil,"",fmt.Errorf("%w: assembled OCI publication hash mismatch",ErrInvalidInput)
 }
 finalRaw,err:=executeStage("verify",0)
 if err!=nil{return nil,"",err}
 final,err:=decodeLargeManifest(finalRaw)
 if err!=nil{return nil,"",err}
 if !sameLargeFingerprint(manifest.Fingerprint,final.Fingerprint)||
  final.Size!=manifest.Size||final.SHA256!=manifest.SHA256{
  return nil,"",fmt.Errorf("%w: Workspace source changed during publication",ErrInvalidInput)
 }
 // Keep the bytes exclusively in memory until Publisher independently checks
 // live Task/Attempt/Project/Workspace ownership and reserves the idempotent
 // content hash. No chunk is written as an untrusted host-side scratch file.
 valid=true
 return payload,manifest.SHA256,nil
}

func safeLargePublicationResponse(raw []byte)bool{
 return len(bytes.TrimSpace(raw))<=workspacePublicationReceiptLimit
}
