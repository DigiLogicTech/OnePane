package agentworker

import (
 "errors"
 "fmt"
)

// Keep recovery errors proportional to a small, fixed memory budget even if
// a database contains thousands of damaged Workers. Every candidate is still
// inspected: dropping surplus *diagnostic copies* never skips recovery work.
const maxWorkerRecoveryErrorDetails = 32

type boundedWorkerRecoveryErrors struct {
 details []error
 omitted int
 category string
}

func (b *boundedWorkerRecoveryErrors) add(err error) {
 if err == nil { return }
 if len(b.details) < maxWorkerRecoveryErrorDetails {
  b.details = append(b.details, err)
 } else {
  b.omitted++
 }
}

// result also retains fatal cursor/DB errors even if the per-Worker diagnostic
// budget was exhausted. It does not echo an unbounded list into API/log output.
func (b *boundedWorkerRecoveryErrors) result(terminal ...error) error {
 all := make([]error, 0, len(b.details)+len(terminal)+1)
 all = append(all, b.details...)
 for _,err := range terminal {
  if err != nil { all = append(all,err) }
 }
 if b.omitted != 0 {
  all = append(all, fmt.Errorf("%d additional %s require operator review",
   b.omitted,b.category))
 }
 return errors.Join(all...)
}
