package sandboxrunner

import (
 "encoding/json"
 "fmt"
 "sort"
 "strings"
)

// This is a general Workspace inventory, never a closed allowlist of what
// the user may build. Any executable installed in the approved image remains
// accessible through separately-authorized project.app.exec. Detection does
// not grant execution authority, host privileges or network egress.
type WorkspaceTool struct{
 Name string `json:"name"`
 Category string `json:"category"`
}
type WorkspaceToolInventory struct{
 Tools []WorkspaceTool `json:"tools"`
 Count int `json:"count"`
 Truncated bool `json:"truncated"`
 Source string `json:"source"`
 Note string `json:"note"`
}

const toolDiscoveryCap=256
const toolDiscoveryStdoutMax=32768

// Entire program text is compiled into OnePane. The caller cannot specify
// shell content, executable names, paths, flags or environment. Only names of
// executable files under *absolute directories* in the sandbox's PATH are
// emitted; no binary runs and no directory/env values leave the container.
// The fixed runner needs POSIX sh inside the approved OCI image. Distroless
// images are reported unsupported rather than secretly probing the host.
const workspaceToolDiscoveryScript = `set -eu
remaining="${PATH:-/usr/local/bin:/usr/bin:/bin}"
count=0
while :; do
 case "$remaining" in
  *:*) dir=${remaining%%:*}; remaining=${remaining#*:};;
  *) dir="$remaining"; remaining="";;
 esac
 case "$dir" in
  /*)
   if [ -d "$dir" ]; then
    for item in "$dir"/*; do
     [ -f "$item" ] && [ -x "$item" ] || continue
     name=${item##*/}
     case "$name" in
      ""|*[!A-Za-z0-9._+-]*) continue;;
     esac
     if [ "$count" -ge 256 ]; then
      printf '%s\n' '__ONEPANE_TRUNCATED__'
      exit 0
     fi
     printf '%s\n' "$name"
     count=$((count + 1))
    done
   fi
   ;;
 esac
 [ -n "$remaining" ] || break
done`

func workspaceToolDiscoveryCommand()[]string{
 return []string{"sh","-c",workspaceToolDiscoveryScript}
}

func validToolDiscoveryEnvelope(raw json.RawMessage)bool{
 var m map[string]json.RawMessage
 if err:=json.Unmarshal(raw,&m);err!=nil||len(m)!=2{return false}
 for k,v:=range m{
  if k!="runtime_id"&&k!="application_id"{return false}
  var str string
  if json.Unmarshal(v,&str)!=nil||!safeID.MatchString(str){return false}
 }
 return m["runtime_id"]!=nil&&m["application_id"]!=nil
}
func knownToolCategory(n string)string{
 switch n{
 case "python","python3","pypy3","pip","pip3","uv","poetry","conda",
  "node","npm","npx","pnpm","yarn","bun","deno","go","rustc","cargo",
  "gcc","g++","clang","clang++","cmake","make","ninja","meson","zig",
  "dotnet","csc","mcs","java","javac","mvn","gradle","ruby","gem",
  "php","composer","perl","lua","luajit","swift","kotlinc","dart",
  "flutter","R","Rscript","julia","jupyter","jupyter-lab":
  return "languages_and_build"
 case "godot","godot4","UnrealEditor","UnrealBuildTool","blender","openscad",
  "FreeCAD","ffmpeg","ffprobe","magick","convert","inkscape","gimp","aseprite":
  return "creative_and_engines"
 case "git","git-lfs","hg","svn","pytest","ruff","black","mypy","go-test",
  "ctest","valgrind","gdb","lldb","playwright","chromium","firefox":
  return "versioning_testing"
 case "sqlite3","duckdb","psql","mysql","redis-cli","jq","yq","csvkit":
  return "data_and_analysis"
 case "terraform","tofu","ansible","kubectl","helm","podman","docker":
  return "infrastructure_cli"
 case "bash","sh","zsh","fish","busybox","curl","wget","tar","zip","unzip":
  return "shell_and_utilities"
 default:return "other_installed_executable"
 }
}
func allowedToolName(n string)bool{
 if len(n)==0||len(n)>128||n=="__ONEPANE_TRUNCATED__"{return false}
 for _,r:=range n{
  if !((r>='A'&&r<='Z')||(r>='a'&&r<='z')||(r>='0'&&r<='9')||r=='_'||r=='-'||r=='.'||r=='+'){
   return false
  }
 }
 return true
}
func decodeWorkspaceToolDiscovery(stdout string)(WorkspaceToolInventory,error){
 if len(stdout)>toolDiscoveryStdoutMax{
  return WorkspaceToolInventory{},fmt.Errorf("%w: Workspace tool discovery output exceeds bound",ErrInvalidInput)
 }
 names:=map[string]bool{}
 truncated:=false
 if stdout==""{return WorkspaceToolInventory{Tools:[]WorkspaceTool{},Source:"fixed_in_container_path_scan",Note:"No executable names observed inside this Workspace image; no host fallback."},nil}
 for _,line:=range strings.Split(strings.TrimSuffix(stdout,"\n"),"\n"){
  if line=="__ONEPANE_TRUNCATED__"{truncated=true;continue}
  if !allowedToolName(line){
   return WorkspaceToolInventory{},fmt.Errorf("%w: invalid Workspace executable inventory",ErrInvalidInput)
  }
  names[line]=true
  if len(names)>toolDiscoveryCap{
   return WorkspaceToolInventory{},fmt.Errorf("%w: oversized Workspace executable inventory",ErrInvalidInput)
  }
 }
 tools:=make([]WorkspaceTool,0,len(names))
 for n:=range names{tools=append(tools,WorkspaceTool{Name:n,Category:knownToolCategory(n)})}
 sort.Slice(tools,func(i,j int)bool{return tools[i].Name<tools[j].Name})
 return WorkspaceToolInventory{Tools:tools,Count:len(tools),Truncated:truncated,
  Source:"fixed_in_container_path_scan",
  Note:"Executable names observed inside this sandbox image, not validated versions or granted permissions. Other installed executables can run through separately authorized project.app.exec; software installation is governed by the Workspace image and network policy.",
 },nil
}
