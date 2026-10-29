//go:build windows

package runtimeaccount

import (
	"os"
	"os/exec"
	"testing"
)

func TestPrepareDoesNotRestrictANormalWindowsUser(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	closeToken, err := Prepare(cmd, Roots{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer closeToken()
	if cmd.SysProcAttr != nil && cmd.SysProcAttr.Token != 0 {
		t.Fatal("a non-system test process received a restricted runtime token")
	}
}
