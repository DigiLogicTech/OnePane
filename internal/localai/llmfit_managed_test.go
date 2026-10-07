package localai

import(
 "os"
 "path/filepath"
 "testing"
)
func TestNormalizeLLMFitVerifiedNestedWindowsArchive(t *testing.T){
 root:=t.TempDir()
 nested:=filepath.Join(root,"llmfit-v1.1.16-x86_64-pc-windows-msvc")
 if err:=os.MkdirAll(nested,0o700);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(nested,"llmfit.exe"),[]byte("fixture"),0o700);err!=nil{t.Fatal(err)}
 if err:=normalizeLLMFitExecutable(root);err!=nil{t.Fatal(err)}
 b,err:=os.ReadFile(filepath.Join(root,"llmfit.exe"))
 if err!=nil||string(b)!="fixture"{t.Fatalf("expected relocated verified binary: %v",err)}
}
func TestNormalizeLLMFitRejectsMissingOrAmbiguousBinary(t *testing.T){
 root:=t.TempDir()
 if err:=normalizeLLMFitExecutable(root);err==nil{t.Fatal("accepted missing executable")}
 if err:=os.WriteFile(filepath.Join(root,"llmfit.exe"),[]byte("one"),0o700);err!=nil{t.Fatal(err)}
 sub:=filepath.Join(root,"other")
 if err:=os.MkdirAll(sub,0o700);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(sub,"llmfit.exe"),[]byte("two"),0o700);err!=nil{t.Fatal(err)}
 if err:=normalizeLLMFitExecutable(root);err==nil{t.Fatal("accepted two executables")}
}
