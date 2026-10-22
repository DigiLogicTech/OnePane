package nodepreflight

import (
 "os"
 "fmt"
 "path/filepath"
 "strings"
 "testing"

 "gopkg.in/yaml.v3"
)

// This is a SOURCE contract test only. It must never dispatch a workflow,
// provision a runner, pull an image, launch Podman, or report physical QA.
func TestRC11PhysicalWorkflowIsExplicitReviewedManualDispatchOnly(t *testing.T){
 raw,err:=os.ReadFile(filepath.Join("..","..",".github","workflows","rc11-rootless-physical.yml"))
 if err!=nil{t.Fatal(err)}
 var spec struct{
  On map[string]any `yaml:"on"`
  Jobs map[string]struct{
   If string `yaml:"if"`
   Environment string `yaml:"environment"`
   RunsOn any `yaml:"runs-on"`
   Needs []string `yaml:"needs"`
   Steps []struct{
    Uses string `yaml:"uses"`
    With map[string]any `yaml:"with"`
    Run string `yaml:"run"`
   } `yaml:"steps"`
  } `yaml:"jobs"`
 }
 if err:=yaml.Unmarshal(raw,&spec);err!=nil{t.Fatal(err)}
 if len(spec.On)!=1||spec.On["workflow_dispatch"]==nil{
  t.Fatalf("physical workflow is not manual-only: %+v",spec.On)
 }
 if len(spec.Jobs)!=6{t.Fatalf("review all physical jobs before enabling: %d",len(spec.Jobs))}
 for name,job:=range spec.Jobs{
  if name=="physical-acceptance-summary"{
   if job.If!="${{ always() }}"||
    strings.Join(job.Needs,",")!="physical-rootless-node,physical-rootless-godot,physical-rootless-large-artifact,physical-rootless-library-e2e,physical-rootless-workspace-service"||
    fmt.Sprint(job.RunsOn)!="ubuntu-latest"{
    t.Fatal("the independent hosted physical acceptance summary has lost its dependencies")
   }
   if len(job.Steps)!=1||
    !strings.Contains(job.Steps[0].Run,`"$NODE_RESULT" != "success"`)||
    !strings.Contains(job.Steps[0].Run,"exit 1")||
    !strings.Contains(job.Steps[0].Run,"UNVERIFIED"){
    t.Fatal("skipped or failed physical jobs could be misreported as accepted")
   }
   continue
  }
  condition:=job.If
  for _,term:=range []string{
   "github.event_name == 'workflow_dispatch'",
   "github.ref == 'refs/heads/feature/rc11-policy-worker-diagnostics-integration-20261011'",
   "inputs.expected_commit_sha == github.sha",
   "inputs.operator_confirmation == 'AUTHORIZE_RC11_ROOTLESS_PHYSICAL_ACCEPTANCE'",
   "vars.ONEPANE_PHYSICAL_ACCEPTANCE_ENABLED == 'approved'",
  }{
   if !strings.Contains(condition,term){
    t.Errorf("%s missing physical gate %q",name,term)
   }
  }
  imageGate:="vars.ONEPANE_LARGE_ARTIFACT_SMOKE_IMAGE != ''"
  if strings.Contains(name,"godot"){imageGate="vars.ONEPANE_GODOT_SMOKE_IMAGE != ''"}
  if name=="physical-rootless-node"{imageGate="vars.ONEPANE_ROOTLESS_SMOKE_IMAGE != ''"}
  if !strings.Contains(condition,imageGate){
   t.Errorf("%s missing approved digest opt-in %q",name,imageGate)
  }
  if job.Environment!="rc11-physical-node-approval"{
   t.Errorf("%s lacks protected operator environment",name)
  }
  if fmt.Sprint(job.RunsOn)!="[self-hosted linux x64]"{
   t.Errorf("%s unexpected runner: %v",name,job.RunsOn)
  }
  checkout:=0
  for _,step:=range job.Steps{
   if strings.HasPrefix(step.Uses,"actions/checkout@"){
    checkout++
    if step.Uses!="actions/checkout@v4"||
     step.With["ref"]!="${{ github.sha }}"||
     step.With["persist-credentials"]!=false{
     t.Errorf("%s checkout not source-SHA pinned without credentials: %+v",name,step)
    }
   }
   if strings.Contains(step.Run,"podman pull")||
    strings.Contains(step.Run,"--pull=always")||
    strings.Contains(step.Run,"curl |")||
    strings.Contains(step.Run,"sudo "){
    t.Errorf("%s contains forbidden automatic pull/privilege elevation",name)
   }
  }
  if checkout!=1{t.Errorf("%s expected one immutable source checkout, got %d",name,checkout)}
 }
}
