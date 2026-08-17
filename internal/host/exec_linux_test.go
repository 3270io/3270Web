// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package host

import (
	"os/exec"
	"syscall"
	"testing"
)

// TestConfigureCmdSetsPdeathsig guards against s3270 subprocesses being
// orphaned (reparented to init) when 3270Web is killed without running its
// own shutdown/cleanup code (SIGKILL, OOM, crash).
func TestConfigureCmdSetsPdeathsig(t *testing.T) {
	cmd := exec.Command("true")
	configureCmd(cmd)

	if cmd.SysProcAttr == nil {
		t.Fatal("expected SysProcAttr to be set")
	}
	if cmd.SysProcAttr.Pdeathsig != syscall.SIGKILL {
		t.Errorf("Pdeathsig = %v, want %v", cmd.SysProcAttr.Pdeathsig, syscall.SIGKILL)
	}
}
