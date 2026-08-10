//go:build windows

package printer

import (
	"fmt"
	"os"
)

// interrupt has no counterpart on Windows: a console process cannot be sent a
// signal from outside its console group, and os.Process.Signal rejects
// everything except Kill. Reporting that plainly is what makes the caller fall
// through to Kill rather than believing a graceful stop happened.
func interrupt(p *os.Process) error {
	return fmt.Errorf("windows has no signal to interrupt a process with")
}
