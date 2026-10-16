package localai

import "testing"

func TestManagedProcessIdentityWorksForColibriAndLlamaBackends(t *testing.T){
 for _,tc:=range []struct{
  name,backend,exe,model string
  allowed []string
  ident ProcessIdentity
  port int
  want bool
 }{
  {
   name:"native llama",backend:"cuda",exe:"/models/runtime/llama-server",model:"/models/weights/a.gguf",port:11001,want:true,
   ident:ProcessIdentity{Executable:"/models/runtime/llama-server",Args:[]string{
    "llama-server","-m","/models/weights/a.gguf","--host","127.0.0.1","--port","11001",
   }},
  },
  {
   name:"legacy Colibri Python",backend:"colibri",exe:"/usr/bin/python3",model:"/models/cache/a",port:11002,want:true,
   allowed:[]string{"/colibri/openai_server.py"},
   ident:ProcessIdentity{Executable:"/usr/bin/python3",Args:[]string{
    "/usr/bin/python3","/colibri/openai_server.py","--model","/models/cache/a","--host","127.0.0.1","--port","11002",
   }},
  },
  {
   name:"tiered Colibri Python",backend:"colibri",exe:"/usr/bin/python3",model:"/models/cache/b",port:11003,want:true,
   allowed:[]string{"/colibri/openai_server.py","/colibri/coli"},
   ident:ProcessIdentity{Executable:"/usr/bin/python3",Args:[]string{
    "/usr/bin/python3","/colibri/coli","serve","--model","/models/cache/b","--host","127.0.0.1","--port","11003",
   }},
  },
  {
   name:"wrong Python interpreter",backend:"colibri",exe:"/usr/bin/python3",model:"/models/cache/b",port:11003,
   allowed:[]string{"/colibri/coli"},
   ident:ProcessIdentity{Executable:"/tmp/python3",Args:[]string{
    "/tmp/python3","/colibri/coli","serve","--model","/models/cache/b","--host","127.0.0.1","--port","11003",
   }},
  },
  {
   name:"wrong model",backend:"colibri",exe:"/usr/bin/python3",model:"/models/cache/b",port:11003,
   allowed:[]string{"/colibri/coli"},
   ident:ProcessIdentity{Executable:"/usr/bin/python3",Args:[]string{
    "/usr/bin/python3","/colibri/coli","--model","/models/private","--host","127.0.0.1","--port","11003",
   }},
  },
  {
   name:"wrong host",backend:"cuda",exe:"/models/runtime/llama-server",model:"/models/weights/a.gguf",port:11001,
   ident:ProcessIdentity{Executable:"/models/runtime/llama-server",Args:[]string{
    "llama-server","-m","/models/weights/a.gguf","--host","0.0.0.0","--port","11001",
   }},
  },
  {
   name:"wrong port",backend:"colibri",exe:"/usr/bin/python3",model:"/models/cache/b",port:11003,
   allowed:[]string{"/colibri/coli"},
   ident:ProcessIdentity{Executable:"/usr/bin/python3",Args:[]string{
    "/usr/bin/python3","/colibri/coli","--model","/models/cache/b","--host","127.0.0.1","--port","11004",
   }},
  },
  {
   name:"script not registered",backend:"colibri",exe:"/usr/bin/python3",model:"/models/cache/b",port:11003,
   allowed:[]string{"/colibri/coli"},
   ident:ProcessIdentity{Executable:"/usr/bin/python3",Args:[]string{
    "/usr/bin/python3","/tmp/other.py","--model","/models/cache/b","--host","127.0.0.1","--port","11003",
   }},
  },
  {
   name:"invalid port",backend:"cpu",exe:"/models/runtime/llama-server",model:"/models/weights/a.gguf",port:1,
   ident:ProcessIdentity{Executable:"/models/runtime/llama-server",Args:[]string{
    "llama-server","-m","/models/weights/a.gguf","--host","127.0.0.1","--port","1",
   }},
  },
 }{
  t.Run(tc.name,func(t *testing.T){
   got:=managedProcessCLIIdentified(tc.backend,tc.ident,tc.exe,tc.allowed,tc.model,tc.port)
   if got!=tc.want{t.Fatalf("backend=%s identity=%+v got=%v want=%v",tc.backend,tc.ident,got,tc.want)}
  })
 }
}
