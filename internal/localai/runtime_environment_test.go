package localai

import(
 "strings"
 "testing"
)

func TestManagedInferenceProcessCannotInheritServiceCloudCredentials(t *testing.T){
 host:=[]string{
  "PATH=/usr/local/bin:/usr/bin",
  "LANG=en_AU.UTF-8",
  "CUDA_VISIBLE_DEVICES=0",
  "VULKAN_SDK=/opt/vulkan",
  "HSA_OVERRIDE_GFX_VERSION=10.3.0",
  "OPENAI_API_KEY=PRIVATE_KEY_MUST_NOT_INHERIT",
  "AZURE_CLIENT_SECRET=PRIVATE_KEY_MUST_NOT_INHERIT",
  "AWS_SECRET_ACCESS_KEY=PRIVATE_KEY_MUST_NOT_INHERIT",
  "HF_TOKEN=PRIVATE_KEY_MUST_NOT_INHERIT",
  "HTTP_PROXY=http://user:PRIVATE_KEY_MUST_NOT_INHERIT@proxy",
  "HTTPS_PROXY=http://user:PRIVATE_KEY_MUST_NOT_INHERIT@proxy",
  "PYTHONPATH=/tmp/untrusted-injection",
  "LD_PRELOAD=/tmp/untrusted-injection.so",
  "HOME=/home/onepane-private",
  "USERPROFILE=C:\\Users\\service",
  "ONEPANE_MANAGED_RUNTIME=untrusted",
  "path=/another/path",
  "MALFORMED_NO_EQUALS",
 }
 result:=managedRuntimeHostEnvironment(host)
 got:=strings.Join(result,"\n")
 for _,required:=range []string{"PATH=/usr/local/bin", "LANG=en_AU.UTF-8",
  "CUDA_VISIBLE_DEVICES=0","VULKAN_SDK=/opt/vulkan",
  "HSA_OVERRIDE_GFX_VERSION=10.3.0"}{
  if !strings.Contains(got,required){t.Fatalf("required nonsecret driver variable lost: %s",required)}
 }
 for _,denied:=range []string{
  "PRIVATE_KEY_MUST_NOT_INHERIT","HTTP_PROXY","HTTPS_PROXY",
  "PYTHONPATH","LD_PRELOAD","ONEPANE_MANAGED_RUNTIME","USERPROFILE=",
  "HOME=","/another/path","MALFORMED_NO_EQUALS",
 }{
  if strings.Contains(got,denied){t.Fatalf("unapproved service environment escaped to managed runtime: %s",denied)}
 }
 }
func TestManagedRuntimeEnvironmentNeverCreatesNewUnapprovedNames(t *testing.T){
 if got:=managedRuntimeHostEnvironment([]string{"DATABASE_URL=secret","INVALID","Z_TOKEN=secret"});len(got)!=0{
  t.Fatalf("unapproved host variable survived: %v",got)
 }
}
