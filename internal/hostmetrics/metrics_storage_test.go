package hostmetrics

import (
 "os"
 "path/filepath"
 "testing"
)

func TestCollectStorageVolumesDeduplicatesSharedDrive(t *testing.T) {
 root:=t.TempDir()
 model:=filepath.Join(root,"Models")
 projects:=filepath.Join(root,"Projects")
 if err:=os.MkdirAll(model,0700);err!=nil{t.Fatal(err)}
 if err:=os.MkdirAll(projects,0700);err!=nil{t.Fatal(err)}
 volumes:=collectStorageVolumes(map[string]string{"Models":model,"Projects":projects})
 if len(volumes)!=1 {t.Fatalf("expected one physical volume, got %#v",volumes)}
 if len(volumes[0].Roles)!=2 {t.Fatalf("expected both roles, got %#v",volumes[0].Roles)}
 if volumes[0].TotalBytes==0 {t.Fatal("total volume capacity unavailable")}
}
