package localai

import (
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestColibriAutoContextRejectsOversizedAndSymlinkedMetadata(t *testing.T){
 dir:=t.TempDir()
 conf:=filepath.Join(dir,"config.json")
 if err:=os.WriteFile(conf,[]byte(`{"max_position_embeddings":32768}`),0600);err!=nil{t.Fatal(err)}
 if n:=colibriAutoContext(dir);n!=32768{t.Fatalf("legitimate context misread: %d",n)}
 if err:=os.WriteFile(conf,[]byte(strings.Repeat("A",1<<20+1)),0600);err!=nil{t.Fatal(err)}
 if n:=colibriAutoContext(dir);n!=8192{t.Fatalf("unbounded config accepted: %d",n)}
 if err:=os.Remove(conf);err!=nil{t.Fatal(err)}
 if err:=os.Symlink(filepath.Join(t.TempDir(),"missing"),conf);err==nil{
  if n:=colibriAutoContext(dir);n!=8192{t.Fatalf("symlinked config treated as trusted: %d",n)}
 }
}
