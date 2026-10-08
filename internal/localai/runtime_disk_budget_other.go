//go:build !windows

package localai

// Windows is the immediate target of the managed runtime storage regression.
func checkRuntimeDiskBudget(_ string,_ int64)error{return nil}
