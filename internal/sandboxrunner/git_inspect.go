package sandboxrunner

import (
 "fmt"
 "strings"
)

// gitInspectCommand exposes a deliberately closed set of nonmutating Git
// observations. No model-controlled argv, shell, Git alias, pathspec,
// subcommand, commit hooks or host executable can be injected here.
// Git runs in the same verified rootless OCI sandbox as app.exec.
func gitInspectCommand(action string) ([]string,error) {
 if strings.TrimSpace(action)!=action {
  return nil,fmt.Errorf("%w: unsupported Git inspection action",ErrInvalidInput)
 }
 base:=[]string{"git","--no-pager","-c","core.fsmonitor=false",
  "-c","core.hooksPath=/dev/null","-c","diff.external=",
  "-C","/workspace"}
 switch action {
 case "status":
  return append(base,"status","--short","--untracked-files=normal"),nil
 case "diff":
  return append(base,"diff","--no-ext-diff","--no-textconv","--"),nil
 case "log":
  return append(base,"log","--max-count=12","--oneline","--no-decorate"),nil
 case "tracked_files":
  return append(base,"ls-files"),nil
 default:
  return nil,fmt.Errorf("%w: unsupported Git inspection action",ErrInvalidInput)
 }
}

// gitMutateCommand makes local-only repository changes with a fixed identity.
// No caller-supplied Git flags, remote URL, repository path, force action,
// hook execution or shell is accepted. Task authority and OCI inspection are
// separately enforced by the Agent Worker and Adapter before execution.
func gitMutateCommand(action, message string) ([]string,error) {
 base:=[]string{"git","--no-pager",
  "-c","core.fsmonitor=false",
  "-c","core.hooksPath=/dev/null",
  "-c","commit.gpgsign=false",
  "-c","user.name=OnePane Agent",
  "-c","user.email=agent@onepane.local",
  "-C","/workspace"}
 switch action {
 case "init":
  if message!=""{return nil,fmt.Errorf("%w: Git init does not accept a message",ErrInvalidInput)}
  return append(base,"init","--initial-branch=main"),nil
 case "stage_all":
  if message!=""{return nil,fmt.Errorf("%w: Git staging does not accept a message",ErrInvalidInput)}
  return append(base,"add","--all","--","."),nil
 case "commit":
  if strings.TrimSpace(message)==""||len(message)>512||
   strings.ContainsRune(message,'\x00')||
   strings.ContainsAny(message,"\r\n"){
   return nil,fmt.Errorf("%w: Git commit message must be one printable line of 1..512 bytes",ErrInvalidInput)
  }
  return append(base,"commit","--no-verify","-m",message),nil
 default:
  return nil,fmt.Errorf("%w: Git mutation action is not approved",ErrInvalidInput)
 }
}
