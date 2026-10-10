package sandboxrunner

import (
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "fmt"
 "sort"
 "strings"
)

// This preflight checks *declared requirements* against a specific running,
// independently inspected rootless Workspace application. It never installs
// packages, runs a discovered program, accepts a host path, changes the
// image or grants permission to execute any listed tool.
const maxToolchainRequirements=32
const maxToolchainPreflightStdout=8192

type ToolchainRequirements struct {
 RuntimeID string
 ApplicationID string
 Required []string
}

type ToolchainPreflightResult struct {
 Status string `json:"status"`
 RequirementsRef string `json:"requirements_sha256"`
 RuntimeSpecRef string `json:"runtime_spec_sha256"`
 Required []string `json:"required"`
 Available []string `json:"available"`
 Missing []string `json:"missing"`
 Observed bool `json:"observed"`
 ExecutionGranted bool `json:"execution_granted"`
 InstallationGranted bool `json:"installation_granted"`
 Note string `json:"note"`
}

// The fixed shell performs only POSIX [ -f ] / [ -x ] checks in absolute
// directories from the isolated OCI application's PATH. Tool names are
// positional parameters; no eval, command substitution, executable call,
// user-supplied shell source, auto-download or network access.
const workspaceToolchainPreflightScript = `set -eu
for tool do
 present=0
 remaining="${PATH:-/usr/local/bin:/usr/bin:/bin}"
 while :; do
  case "$remaining" in
   *:*) dir=${remaining%%:*}; remaining=${remaining#*:};;
   *) dir="$remaining"; remaining="";;
  esac
  case "$dir" in
   /*)
    if [ -f "$dir/$tool" ] && [ -x "$dir/$tool" ]; then
     present=1
     break
    fi
    ;;
  esac
  [ -n "$remaining" ] || break
 done
 printf '%s\\t%s\\n' "$tool" "$present"
done`

func decodeToolchainRequirements(raw json.RawMessage)(ToolchainRequirements,error){
 var m map[string]json.RawMessage
 if err:=json.Unmarshal(raw,&m);err!=nil||len(m)!=3{
  return ToolchainRequirements{},fmt.Errorf("%w: toolchain preflight requires runtime_id, application_id and required",ErrInvalidInput)
 }
 for k:=range m{
  if k!="runtime_id"&&k!="application_id"&&k!="required"{
   return ToolchainRequirements{},fmt.Errorf("%w: unknown preflight parameter",ErrInvalidInput)
  }
 }
 var r ToolchainRequirements
 if json.Unmarshal(m["runtime_id"],&r.RuntimeID)!=nil||
  json.Unmarshal(m["application_id"],&r.ApplicationID)!=nil||
  json.Unmarshal(m["required"],&r.Required)!=nil||
  !safeID.MatchString(r.RuntimeID)||!safeID.MatchString(r.ApplicationID)||
  len(r.Required)==0||len(r.Required)>maxToolchainRequirements{
  return ToolchainRequirements{},fmt.Errorf("%w: invalid declared toolchain prerequisites",ErrInvalidInput)
 }
 unique:=make(map[string]bool,len(r.Required))
 for _,n:=range r.Required{
  if !allowedToolName(n)||n=="."||n==".."||unique[n]{
   return ToolchainRequirements{},fmt.Errorf("%w: invalid or duplicate toolchain executable",ErrInvalidInput)
  }
  unique[n]=true
 }
 sort.Strings(r.Required)
 return r,nil
}

func workspaceToolchainPreflightCommand(r ToolchainRequirements)[]string{
 argv:=[]string{"sh","-c",workspaceToolchainPreflightScript,"onepane-preflight"}
 return append(argv,r.Required...)
}

func fingerprintToolchainRequirements(names []string)string{
 digest:=sha256.Sum256([]byte(strings.Join(names,"\x00")))
 return hex.EncodeToString(digest[:])
}

func parseToolchainPreflight(raw string,r ToolchainRequirements,specHash string)(ToolchainPreflightResult,error){
 result:=ToolchainPreflightResult{
  Status:"unknown",
  RequirementsRef:fingerprintToolchainRequirements(r.Required),
  RuntimeSpecRef:specHash,
  Required:append([]string(nil),r.Required...),
  Available:[]string{},Missing:[]string{},
  ExecutionGranted:false,InstallationGranted:false,
  Note:"Presence was checked without invoking listed executables. Versions and dependencies are unverified; execution, package installation and network access require separate Workspace authority.",
 }
 if len(raw)>maxToolchainPreflightStdout||raw==""||!strings.HasSuffix(raw,"\n"){
  return ToolchainPreflightResult{},fmt.Errorf("%w: incomplete toolchain preflight result",ErrInvalidInput)
 }
 lines:=strings.Split(strings.TrimSuffix(raw,"\n"),"\n")
 if len(lines)!=len(r.Required){
  return ToolchainPreflightResult{},fmt.Errorf("%w: unexpected number of toolchain results",ErrInvalidInput)
 }
 for i,line:=range lines{
  expected:=r.Required[i]
  if line==expected+"\t1"{
   result.Available=append(result.Available,expected)
  }else if line==expected+"\t0"{
   result.Missing=append(result.Missing,expected)
  }else{
   return ToolchainPreflightResult{},fmt.Errorf("%w: noncanonical toolchain preflight result",ErrInvalidInput)
  }
 }
 result.Observed=true
 result.Status="ready"
 if len(result.Missing)>0{result.Status="missing"}
 return result,nil
}

func toolchainPreflightUnavailable(r ToolchainRequirements,specHash string)ToolchainPreflightResult{
 return ToolchainPreflightResult{
  Status:"unavailable",RequirementsRef:fingerprintToolchainRequirements(r.Required),
  RuntimeSpecRef:specHash,Required:append([]string(nil),r.Required...),
  Available:[]string{},Missing:[]string{},Observed:false,
  ExecutionGranted:false,InstallationGranted:false,
  Note:"The approved OCI image could not execute OnePane's fixed POSIX preflight. Required tools are unknown, not proven absent. No host fallback or package installation attempted.",
 }
}
