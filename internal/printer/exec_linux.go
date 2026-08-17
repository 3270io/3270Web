// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux

package printer

import (
	"os/exec"
	"syscall"
)

// configureCmd sets Pdeathsig so the pr3287 child dies with this process even
// on a hard kill that never runs our own cleanup. The same reasoning as
// internal/host: a printer session orphaned to init holds an LU bound on the
// host, and the host has no way of knowing nobody is listening any more.
func configureCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}
