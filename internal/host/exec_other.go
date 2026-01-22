//go:build !windows

package host

import "os/exec"

func configureCmd(cmd *exec.Cmd) {
	// no-op on non-Windows platforms
}
