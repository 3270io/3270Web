// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package host

import (
	"os/exec"
	"syscall"
)

// configureCmd sets Pdeathsig so the s3270 child is killed by the kernel the
// moment this process dies, even on a hard kill (SIGKILL, OOM, crash) that
// never runs our own shutdown/cleanup code. Without it, s3270 is reparented
// to init and orphaned indefinitely instead of exiting with its parent.
func configureCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}
