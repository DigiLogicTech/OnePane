//go:build !windows

package localai

import "errors"

func recordOwnedLLMFit(root,exe string,pid int)error{return errors.New("managed llmfit is Windows-only")}
func terminateOwnedLLMFit(root,exe string)(bool,error){return false,errors.New("managed llmfit is Windows-only")}
