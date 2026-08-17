// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build windows

package printer

import (
	"os/exec"
	"syscall"
)

func configureCmd(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
