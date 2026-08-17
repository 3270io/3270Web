// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !windows && !linux

package printer

import "os/exec"

func configureCmd(cmd *exec.Cmd) {
	// no-op: Pdeathsig (see exec_linux.go) is Linux-only; other
	// non-Windows platforms (e.g. macOS/BSD) have no equivalent.
}
