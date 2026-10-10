package sandboxrunner

import (
 "encoding/json"
 "fmt"
 "strings"
 "io"
)

// The Unreal 5.8 Experimental ModelContextProtocol plugin is unauthenticated
// and normally loopback-only. OnePane must NEVER forward its HTTP endpoint to
// a Node, a remote agent, another Workspace or a shared proxy. The probe runs
// only from inside the independently inspected, approved OCI application;
// 127.0.0.1 is therefore that application's own network namespace.
//
// This fixed script performs a read-only MCP initialize handshake. It never
// invokes tools/call, list_toolsets, call_tool or any editor mutation, and
// does not accept URLs, headers, Python code or port numbers from an agent.
// No dynamic MCP Tool is exposed to the OnePane Tool Gateway by this probe.
const unrealMCPHandshakeScript = `import json,sys,urllib.request,urllib.error
url="http://127.0.0.1:8000/mcp"
payload={"jsonrpc":"2.0","id":1,"method":"initialize",
 "params":{"protocolVersion":"2025-06-18","capabilities":{},
 "clientInfo":{"name":"OnePane","version":"RC11"}}}
class NoRedirect(urllib.request.HTTPRedirectHandler):
 def redirect_request(self,request,fp,code,msg,headers,newurl):return None
opener=urllib.request.build_opener(urllib.request.ProxyHandler({}),NoRedirect)
request=urllib.request.Request(url,json.dumps(payload,separators=(",",":")).encode("utf-8"),
 {"Content-Type":"application/json","Accept":"application/json, text/event-stream",
 "MCP-Protocol-Version":"2025-06-18"},method="POST")
try:
 with opener.open(request,timeout=4) as response:
  if response.status!=200:raise ValueError("unexpected status")
  content_type=response.headers.get("Content-Type","").split(";",1)[0].strip().lower()
  if content_type=="application/json":
   raw=response.read(16385)
   if len(raw)>16384:raise ValueError("response too large")
   data=json.loads(raw)
  elif content_type=="text/event-stream":
   data=None
   budget=16384
   while budget>0:
    line=response.readline(min(budget,4096))
    if not line:break
    budget-=len(line)
    if line.startswith(b"data:"):
     data=json.loads(line[5:].strip())
     break
   if data is None:raise ValueError("missing MCP result")
  else:raise ValueError("unsupported content type")
  if not isinstance(data,dict) or data.get("jsonrpc")!="2.0" or data.get("id")!=1:raise ValueError("invalid MCP envelope")
  result=data.get("result")
  if not isinstance(result,dict) or not isinstance(result.get("serverInfo"),dict):raise ValueError("missing server info")
  if result["serverInfo"].get("name")!="unreal-mcp":raise ValueError("unexpected editor identity")
  print('{"available":true,"server":"unreal-mcp"}')
except (OSError,ValueError,KeyError,TypeError,UnicodeDecodeError,OverflowError):
 print('{"available":false,"server":"unavailable"}')
 sys.exit(2)
`

func unrealMCPProbeCommand()[]string{
 return []string{"python3","-I","-S","-c",unrealMCPHandshakeScript}
}

func validUnrealMCPEnvelope(raw json.RawMessage)bool{
 var fields map[string]json.RawMessage
 if err:=json.Unmarshal(raw,&fields);err!=nil||len(fields)<2||len(fields)>3{return false}
 for k:=range fields{
  switch k{case "runtime_id","application_id","timeout_seconds":default:return false}
 }
 for _,key:=range []string{"runtime_id","application_id"}{
  v,ok:=fields[key];if !ok{return false}
  var value string
  if json.Unmarshal(v,&value)!=nil||!safeID.MatchString(value){return false}
 }
 if v,ok:=fields["timeout_seconds"];ok{
  var seconds int
  if json.Unmarshal(v,&seconds)!=nil||seconds<1||seconds>30{return false}
 }
 return true
}
func parseUnrealMCPProbe(stdout string)(bool,error){
 if len(stdout)>256{return false,fmt.Errorf("%w: oversized Unreal MCP probe output",ErrInvalidInput)}
 var out struct{
  Available bool `json:"available"`
  Server string `json:"server"`
 }
 decoder:=json.NewDecoder(strings.NewReader(stdout))
 decoder.DisallowUnknownFields()
 if err:=decoder.Decode(&out);err!=nil{return false,fmt.Errorf("%w: invalid Unreal MCP handshake",ErrInvalidInput)}
 var trailing any
 if err:=decoder.Decode(&trailing);err!=io.EOF{return false,fmt.Errorf("%w: malformed or duplicate Unreal MCP output",ErrInvalidInput)}
 if out.Available&&out.Server=="unreal-mcp"{return true,nil}
 if !out.Available&&out.Server=="unavailable"{return false,nil}
 return false,fmt.Errorf("%w: unexpected Unreal MCP server identity",ErrInvalidInput)
}
