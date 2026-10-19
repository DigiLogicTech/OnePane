//go:build !windows
package localai
import "os"
// POSIX managed runtime logs use 0600 in a 0700 directory.
func protectManagedRuntimeLog(_ *os.File)error{return nil}
