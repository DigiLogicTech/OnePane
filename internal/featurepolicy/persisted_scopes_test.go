package featurepolicy

import (
	"encoding/json"
	"testing"
)

func TestFeatureDocumentDefaultsAndExplicitRestrictions(t *testing.T) {
	base:=AssistedDefaults()
	legacy,configured,err:=parseFeatureDocument([]byte(`{"onepane_ui":{"legacy":true}}`),base)
	if err!=nil||configured||legacy.ModelRouting!=Enabled{
		t.Fatalf("legacy policy changed: configured=%v err=%v %+v",configured,err,legacy)
	}
	restricted,configured,err:=parseFeatureDocument([]byte(`{"feature_policy":{"Files":"disabled","CloudRouting":"disabled"}}`),Policy{})
	if err!=nil||!configured||restricted.Files!=Disabled||restricted.CloudRouting!=Disabled{
		t.Fatalf("explicit restriction lost: configured=%v err=%v %+v",configured,err,restricted)
	}
	project:=AssistedDefaults()
	project.CloudRouting=Enabled
	effective:=ResolvePolicyChain(project,restricted,Policy{Files:Enabled,CloudRouting:Enabled})
	if effective.Files!=Disabled||effective.CanUseCloudModel(){
		t.Fatalf("Task expanded stored Workspace denial: %+v",effective)
	}
}

func TestFeatureDocumentFailsClosedForCorruptAndUnknownValues(t *testing.T) {
	tests:=[]string{
		`null`,`[]`,`{broken`,`{"feature_policy":null}`,
		`{"feature_policy":42}`,`{"feature_policy":[]}`,
		`{"feature_policy":{"Files":"allow everything"}}`,
		`{"feature_policy":{"CloudRouting":"enabled","Mystery":"enabled"}}`,
		`{"feature_policy":{"ConfigAuthority":"super-admin"}}`,
	}
	for _,raw:=range tests {
		_,_,err:=parseFeatureDocument([]byte(raw),Policy{})
		if err==nil {t.Errorf("malformed document accepted: %s",raw)}
	}
}
func TestInvalidFeaturePolicyActionNeverGrantsExecution(t *testing.T) {
	base:=AssistedDefaults()
	base.CloudRouting=Enabled
	for _,action:=range []string{"secrets.export","network.open","shell.exec",""} {
		if base.ValidateExecution(action){t.Fatalf("granted unknown action %q",action)}
	}
}
func TestScopedPolicyJSONRoundTrip(t *testing.T) {
	v:=AssistedDefaults()
	b,err:=json.Marshal(map[string]any{"feature_policy":v})
	if err!=nil{t.Fatal(err)}
	decoded,configured,err:=parseFeatureDocument(b,Policy{})
	if err!=nil||!configured||decoded.Files!=Enabled{
		t.Fatalf("round trip lost fields: %+v configured=%t err=%v",decoded,configured,err)
	}
}
