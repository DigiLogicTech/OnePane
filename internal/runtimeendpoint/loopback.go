package runtimeendpoint

import (
 "encoding/json"
 "net"
 "net/url"
 "strings"
)

// LiteralLoopbackHTTP confirms ONLY that an HTTP transport targets a
// literal loopback IP on the current host. It does not establish that the
// bridged runtime performs local-only compute or uses no cloud services.
// DNS names (even localhost), URL credentials, invalid/ambiguous URLs and
// remote private addresses do not qualify as independent local proof.
func LiteralLoopbackHTTP(raw json.RawMessage) bool {
 var endpoint struct{ BaseURL string `json:"base_url"` }
 if err:=json.Unmarshal(raw,&endpoint);err!=nil{return false}
 base:=strings.TrimSpace(endpoint.BaseURL)
 if base==""||base!=endpoint.BaseURL{return false}
 u,err:=url.Parse(base)
 if err!=nil||u==nil||u.Opaque!=""||u.User!=nil||
  u.Fragment!=""||u.RawQuery!=""||u.Host==""||
  (u.Scheme!="http"&&u.Scheme!="https"){return false}
 host:=u.Hostname()
 ip:=net.ParseIP(host)
 return ip!=nil&&ip.IsLoopback()
}
