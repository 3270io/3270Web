// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !windows

package printer

import (
	"os"
	"syscall"
)

// interrupt asks the process to end. SIGTERM rather than SIGKILL so pr3287
// gets to close its telnet connection, which is what tells the host the
// printer LU is free.
func interrupt(p *os.Process) error {
	return p.Signal(syscall.SIGTERM)
}
