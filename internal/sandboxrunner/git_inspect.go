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
