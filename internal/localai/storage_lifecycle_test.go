package localai

import (
 "os"
 "path/filepath"
 "testing"
)
func TestStorageAreaCountsFilesWithoutFollowingSymlinks(t *testing.T){
 root:=t.TempDir()
 if err:=os.MkdirAll(filepath.Join(root,"owned"),0o700);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(root,"owned","model.gguf"),[]byte("verified"),0o600);err!=nil{t.Fatal(err)}
 // Directories are not counted as files.
 area:=storageArea("Managed test",root)
 if area.Bytes!=int64(len("verified"))||area.Files!=1{t.Fatalf("unexpected area: %+v",area)}
}
func TestStorageAreaMissingRootIsEmpty(t *testing.T){
 root:=filepath.Join(t.TempDir(),"missing")
 area:=storageArea("Empty",root)
 if area.Bytes!=0||area.Files!=0{t.Fatalf("missing root unexpectedly consumes bytes: %+v",area)}
}
