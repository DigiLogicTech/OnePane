package featurepolicy

import "testing"

func TestFeaturePrivilegeExpandsOnlyWhenAuthorityIncreases(t *testing.T){
 cases:=[]struct{name,before,after string;want bool}{
  {"no features","{}","{}",false},
  {"unrelated UI",`{"onepane_ui":{"workspaces":[]}}`,`{"onepane_ui":{"workspaces":[{"id":"one"}]}}`,false},
  {"disable file writes","{}",`{"feature_policy":{"FileWrite":"disabled"}}`,false},
  {"enable execute","{}",`{"feature_policy":{"ProcessExecute":"enabled"}}`,true},
  {"enable network","{}",`{"feature_policy":{"NetworkAccess":"enabled"}}`,true},
  {"enable secrets","{}",`{"feature_policy":{"SecretsAccess":"enabled"}}`,true},
  {"enable autonomy","{}",`{"feature_policy":{"AutonomousRoutines":"enabled"}}`,true},
  {"enable cloud","{}",`{"feature_policy":{"CloudRouting":"enabled"}}`,true},
  {"restore read after deny",`{"feature_policy":{"FileRead":"disabled"}}`,"{}",true},
  {"unchanged grant",`{"feature_policy":{"ProcessExecute":"enabled"}}`,`{"feature_policy":{"ProcessExecute":"enabled"}}`,false},
  {"narrow from execute",`{"feature_policy":{"ProcessExecute":"enabled"}}`,`{"feature_policy":{"ProcessExecute":"disabled"}}`,false},
  {"configuration authority escalation","{}",`{"feature_policy":{"ConfigAuthority":"auto"}}`,true},
  {"invalid proposed","{}",`{"feature_policy":{"NetworkAccess":"unlimited"}}`,false},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   expansion,err:=FeaturePrivilegeExpands([]byte(tc.before),[]byte(tc.after))
   if tc.name=="invalid proposed"{
    if err==nil{t.Fatal("invalid policy accepted")}
    return
   }
   if err!=nil||expansion!=tc.want{
    t.Fatalf("expansion=%v err=%v want=%v",expansion,err,tc.want)
   }
  })
 }
}
