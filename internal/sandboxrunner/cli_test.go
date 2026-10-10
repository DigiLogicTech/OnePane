package sandboxrunner

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSpecHashNeverDependsOnSecretPlaintext(t *testing.T) {
	base := ContainerSpec{RuntimeID: "r", ApplicationID: "a", Image: "example/app:1", WorkspacePath: "/tmp/ws", Environment: map[string]string{"API_KEY": "secret-one"}, EnvironmentIdentity: map[string]string{"API_KEY": "vault:secret:v2"}, Limits: ResourceLimits{CPUMillis: 1000, MemoryMB: 512, PIDs: 64}}
	h1 := specHash(base)
	base.Environment["API_KEY"] = "secret-two"
	h2 := specHash(base)
	if h1 != h2 {
		t.Fatalf("plaintext changed spec hash: %s != %s", h1, h2)
	}
	base.EnvironmentIdentity["API_KEY"] = "vault:secret:v3"
	if h3 := specHash(base); h3 == h2 {
		t.Fatal("secret version identity did not change spec hash")
	}
}

func TestEnvFileIsPrivateAndContainsNoCLIEncoding(t *testing.T) {
	workspace := tempEnvWorkspace(t)
	path, err := writeEnvFile(workspace, map[string]string{"API_KEY": "top-secret", "MODE": "prod"})
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm()&0o077 != 0 {
		t.Fatalf("env file permissions=%o", st.Mode().Perm())
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if !strings.Contains(text, "API_KEY=top-secret\n") || !strings.Contains(text, "MODE=prod\n") {
		t.Fatalf("unexpected env file: %q", text)
	}
}

func TestEnvFileRejectsMultilineValues(t *testing.T) {
	if _, err := writeEnvFile(tempEnvWorkspace(t), map[string]string{"PRIVATE_KEY": "line1\nline2"}); err == nil {
		t.Fatal("expected multiline environment secret rejection")
	}
}

func TestRuntimeNetworkNameIsDeterministicAndBounded(t *testing.T) {
	name := runtimeNetworkName(strings.Repeat("Runtime.With Spaces!", 10))
	if name == "" || len(name) > 63 || name != runtimeNetworkName(strings.Repeat("Runtime.With Spaces!", 10)) {
		t.Fatalf("network name=%q", name)
	}
}

func tempEnvWorkspace(t *testing.T) string {
 t.Helper()
 workspace:=filepath.Join(t.TempDir(),"workspace")
 if err:=os.Mkdir(workspace,0o700);err!=nil{t.Fatal(err)}
 return workspace
}

func TestEnvFileNeverUsesSystemTemporaryOrContainerMount(t *testing.T){
 workspace:=tempEnvWorkspace(t)
 path,err:=writeEnvFile(workspace,map[string]string{"TOKEN":"private"})
 if err!=nil{t.Fatal(err)}
 defer os.Remove(path)
 expectedDir:=filepath.Join(filepath.Dir(workspace),".onepane-private-env")
 if filepath.Dir(path)!=expectedDir{t.Fatalf("credential file escaped configured runtime: %s",path)}
 if strings.HasPrefix(path,workspace+string(os.PathSeparator)){
  t.Fatalf("credentials staged inside container writable root: %s",path)
 }
 info,err:=os.Stat(expectedDir)
 if err!=nil||info.Mode().Perm()&0o077!=0{
  t.Fatalf("staging folder permissions unsafe: %v, %v",info,err)
 }
}

func TestEnvFileRejectsSymlinkOrPermissivePrivateFolder(t *testing.T){
 workspace:=tempEnvWorkspace(t)
 staging:=filepath.Join(filepath.Dir(workspace),".onepane-private-env")
 if err:=os.Symlink(t.TempDir(),staging);err!=nil{t.Skipf("symlink not supported: %v",err)}
 if _,err:=writeEnvFile(workspace,map[string]string{"TOKEN":"private"});!errors.Is(err,ErrInvalidInput){
  t.Fatalf("followed symlinked secret staging folder: %v",err)
 }
 if err:=os.Remove(staging);err!=nil{t.Fatal(err)}
 if err:=os.Mkdir(staging,0o700);err!=nil{t.Fatal(err)}
 if err:=os.Chmod(staging,0o755);err!=nil{t.Fatal(err)}
 if _,err:=writeEnvFile(workspace,map[string]string{"TOKEN":"private"});!errors.Is(err,ErrInvalidInput){
  t.Fatalf("accepted permissive staging folder: %v",err)
 }
}

func TestStalePrivateEnvCleanupNeverTouchesRecentOrUnrelatedFiles(t *testing.T){
 workspace:=tempEnvWorkspace(t)
 dir:=filepath.Join(filepath.Dir(workspace),".onepane-private-env")
 if err:=os.Mkdir(dir,0o700);err!=nil{t.Fatal(err)}
 write:=func(name string)string{
  t.Helper()
  p:=filepath.Join(dir,name)
  if err:=os.WriteFile(p,[]byte("TOP_SECRET=old\n"),0o600);err!=nil{t.Fatal(err)}
  return p
 }
 stale:=write("onepane-sandbox-env-old")
 recent:=write("onepane-sandbox-env-active")
 userFile:=write("user-workspace-content")
 old:=time.Now().Add(-48*time.Hour)
 for _,p:=range []string{stale,userFile}{
  if err:=os.Chtimes(p,old,old);err!=nil{t.Fatal(err)}
 }
 link:=filepath.Join(dir,"onepane-sandbox-env-link")
 if err:=os.Symlink(stale,link);err!=nil{t.Skipf("symlink unsupported: %v",err)}
 path,err:=writeEnvFile(workspace,map[string]string{"NEW":"fresh"})
 if err!=nil{t.Fatal(err)}
 defer os.Remove(path)
 if _,err:=os.Lstat(stale);!os.IsNotExist(err){
  t.Fatalf("old credential file retained after renewed tool use: %v",err)
 }
 for _,p:=range []string{recent,userFile,link}{
  if _,err:=os.Lstat(p);err!=nil{t.Fatalf("cleanup touched active, unrelated, or symlink file %s: %v",p,err)}
 }
 if stringMustRead(t,path)!="NEW=fresh\n"{t.Fatal("new credentials corrupted by stale-file cleanup")}
}

func TestNoSecretsStillPrunesPreviousCrashCredential(t *testing.T){
 workspace:=tempEnvWorkspace(t)
 dir:=filepath.Join(filepath.Dir(workspace),".onepane-private-env")
 if err:=os.Mkdir(dir,0o700);err!=nil{t.Fatal(err)}
 old:=filepath.Join(dir,"onepane-sandbox-env-crashed")
 if err:=os.WriteFile(old,[]byte("TOKEN=old\n"),0o600);err!=nil{t.Fatal(err)}
 when:=time.Now().Add(-48*time.Hour)
 if err:=os.Chtimes(old,when,when);err!=nil{t.Fatal(err)}
 path,err:=writeEnvFile(workspace,nil)
 if err!=nil||path!=""{t.Fatalf("empty bindings should clean only: %q %v",path,err)}
 if _,err:=os.Lstat(old);!os.IsNotExist(err){t.Fatalf("abandoned credential not pruned: %v",err)}
}

func stringMustRead(t *testing.T,path string)string{
 t.Helper()
 raw,err:=os.ReadFile(path)
 if err!=nil{t.Fatal(err)}
 return string(raw)
}
