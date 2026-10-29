package localai

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/runtimeaccount"
)

var errRuntimeAccountUnavailable = errors.New("model runtime account is unavailable")

// prepareCommand asks the Windows runtime account to drop LocalSystem from a
// model child. The account package no-ops unless
// ONEPANE_RESTRICT_MODEL_RUNTIME=1, so the installed service keeps its
// current child tokens until that switch is set.
func prepareCommand(cmd *exec.Cmd, dataDir, modelRoot, logPath, work, exe string) (func(), error) {
	logDir := ""
	if strings.TrimSpace(logPath) != "" {
		logDir = filepath.Dir(logPath)
	}
	return runtimeaccount.Prepare(cmd, runtimeaccount.Roots{
		DataDir: dataDir, ModelRoot: modelRoot, LogDir: logDir, WorkDir: work, Executable: exe,
	})
}

func (s *Service) prepareManagedCommand(cmd *exec.Cmd, logPath, work, exe string) (func(), error) {
	if s == nil {
		return nil, errRuntimeAccountUnavailable
	}
	return prepareCommand(cmd, s.dataDir, s.modelRoot, logPath, work, exe)
}
