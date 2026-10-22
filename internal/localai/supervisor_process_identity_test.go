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
   name:"second model overrides allowed model",backend:"cuda",exe:"/models/runtime/llama-server",model:"/models/weights/a.gguf",port:11001,
   ident:ProcessIdentity{Executable:"/models/runtime/llama-server",Args:[]string{
    "llama-server","-m","/models/weights/a.gguf","-m","/models/weights/other.gguf","--host","127.0.0.1","--port","11001",
   }},
  },
  {
   name:"duplicate same model is ambiguous",backend:"cuda",exe:"/models/runtime/llama-server",model:"/models/weights/a.gguf",port:11001,
   ident:ProcessIdentity{Executable:"/models/runtime/llama-server",Args:[]string{
    "llama-server","-m","/models/weights/a.gguf","-m","/models/weights/a.gguf","--host","127.0.0.1","--port","11001",
   }},
  },
  {
   name:"conflicting host override",backend:"colibri",exe:"/usr/bin/python3",model:"/models/cache/b",port:11003,
   allowed:[]string{"/colibri/coli"},
   ident:ProcessIdentity{Executable:"/usr/bin/python3",Args:[]string{
    "/usr/bin/python3","/colibri/coli","--model","/models/cache/b","--host","127.0.0.1","--host","0.0.0.0","--port","11003",
   }},
  },
  {
   name:"duplicate port override",backend:"cuda",exe:"/models/runtime/llama-server",model:"/models/weights/a.gguf",port:11001,
   ident:ProcessIdentity{Executable:"/models/runtime/llama-server",Args:[]string{
    "llama-server","-m","/models/weights/a.gguf","--host","127.0.0.1","--port","11001","--port","11002",
   }},
  },
  {
   name:"alias model option override",backend:"cuda",exe:"/models/runtime/llama-server",model:"/models/weights/a.gguf",port:11001,
   ident:ProcessIdentity{Executable:"/models/runtime/llama-server",Args:[]string{
    "llama-server","-m","/models/weights/a.gguf","--model","/models/weights/other.gguf","--host","127.0.0.1","--port","11001",
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

func TestManagedProcessIdentityRejectsInlineOrFusedOverrides(t *testing.T) {
 base:=[]string{"llama-server","-m","/models/weights/a.gguf","--host","127.0.0.1","--port","11001"}
 for _,tc:=range []struct{
  name string
  override []string
 }{
  {"inline network exposure",[]string{"--host=0.0.0.0"}},
  {"inline port redirection",[]string{"--port=11002"}},
  {"inline model alias",[]string{"--model=/models/weights/other.gguf"}},
  {"inline short model",[]string{"-m=/models/weights/other.gguf"}},
  {"fused short model",[]string{"-m/models/weights/other.gguf"}},
 }{
  t.Run(tc.name,func(t *testing.T){
   args:=append(append([]string(nil),base...),tc.override...)
   ident:=ProcessIdentity{Executable:"/models/runtime/llama-server",Args:args}
   if managedProcessCLIIdentified("cuda",ident,"/models/runtime/llama-server",nil,"/models/weights/a.gguf",11001){
    t.Fatalf("unapproved override accepted as managed model ownership: %v",args)
   }
  })
 }
 // Path normalization must not turn an invalid TCP port or host into
 // apparently valid CLI network configuration.
 for _,tc:=range []struct{name string; args []string}{
  {"noncanonical port",[]string{"llama-server","-m","/models/weights/a.gguf","--host","127.0.0.1","--port","./11001"}},
  {"noncanonical host",[]string{"llama-server","-m","/models/weights/a.gguf","--host","./127.0.0.1","--port","11001"}},
 }{
  t.Run(tc.name,func(t *testing.T){
   ident:=ProcessIdentity{Executable:"/models/runtime/llama-server",Args:tc.args}
   if managedProcessCLIIdentified("cuda",ident,"/models/runtime/llama-server",nil,"/models/weights/a.gguf",11001){
    t.Fatalf("nonliteral network argument accepted: %v",tc.args)
   }
  })
 }
 colibri:=ProcessIdentity{Executable:"/usr/bin/python3",Args:[]string{
  "/usr/bin/python3","/colibri/coli","serve","--model","/models/cache/b","--host","127.0.0.1","--port","11003","--model=/models/cache/other",
 }}
 if managedProcessCLIIdentified("colibri",colibri,"/usr/bin/python3",[]string{"/colibri/coli"},"/models/cache/b",11003){
  t.Fatal("Colibri inline model override accepted as managed ownership")
 }
}

func TestManagedProcessIdentityRejectsInertOptionsAndPythonCodeImpersonation(t *testing.T){
 model:="/models/cache/b"
 script:="/colibri/coli"
 for _,tc:=range []struct{
  name string
  args []string
 }{
  {"Python code before approved script",[]string{"/usr/bin/python3","-c","print('untrusted')",script,"--model",model,"--host","127.0.0.1","--port","11003"}},
  {"Python module before approved script",[]string{"/usr/bin/python3","-m","untrusted",script,"--model",model,"--host","127.0.0.1","--port","11003"}},
  {"approved script after unrelated program",[]string{"/usr/bin/python3","/tmp/unapproved.py",script,"--model",model,"--host","127.0.0.1","--port","11003"}},
  {"option terminator after script",[]string{"/usr/bin/python3",script,"--","--model",model,"--host","127.0.0.1","--port","11003"}},
 }{
  t.Run(tc.name,func(t *testing.T){
   ident:=ProcessIdentity{Executable:"/usr/bin/python3",Args:tc.args}
   if managedProcessCLIIdentified("colibri",ident,"/usr/bin/python3",[]string{script},model,11003){
    t.Fatalf("unapproved Python execution accepted as Colibri residency: %v",tc.args)
   }
  })
 }
 native:=ProcessIdentity{Executable:"/models/runtime/llama-server",Args:[]string{
  "llama-server","--","-m","/models/weights/a.gguf","--host","127.0.0.1","--port","11001",
 }}
 if managedProcessCLIIdentified("cuda",native,"/models/runtime/llama-server",nil,"/models/weights/a.gguf",11001){
  t.Fatal("CLI arguments after option terminator accepted as managed process")
 }
 // py.exe -3 script is OnePane's explicitly constructed Windows argv.
 win:=ProcessIdentity{Executable:"/bin/py.exe",Args:[]string{
  "/bin/py.exe","-3",script,"serve","--model",model,"--host","127.0.0.1","--port","11003",
 }}
 if !managedProcessCLIIdentified("colibri",win,"/bin/py.exe",[]string{script},model,11003){
  t.Fatal("approved py.exe -3 Colibri script launcher was rejected")
 }
}
