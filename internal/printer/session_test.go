// SPDX-License-Identifier: AGPL-3.0-or-later

package printer

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// The process under test is this test binary, re-run in a mode chosen by an
// environment variable. It is the standard way to test process handling
// without shipping a fixture binary, and unlike a shell script it works on
// every platform the server builds for.
const helperModeEnv = "PRINTER_TEST_HELPER_MODE"

func TestHelperProcess(t *testing.T) {
	switch os.Getenv(helperModeEnv) {
	case "linger":
		// Long enough that the test decides when it ends, short enough that a
		// leaked one does not outlive the run.
		time.Sleep(30 * time.Second)
		os.Exit(0)
	case "refused":
		fmt.Fprintln(os.Stderr, "pr3287: LU PRTZZZ is not known to the host")
		os.Exit(1)
	default:
		// Not running as a helper: an ordinary, empty test.
	}
}

func stubProcess(t *testing.T, mode string) {
	t.Helper()
	previous := commandFor
	commandFor = func(string, ...string) *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
		cmd.Env = append(os.Environ(), helperModeEnv+"="+mode)
		return cmd
	}
	t.Cleanup(func() { commandFor = previous })
}

func stubOptions() Options {
	return Options{
		Host:         "mainframe.example",
		Port:         23,
		LU:           "PRT001",
		SpoolCommand: "true",
	}
}

func TestSessionStartAndStop(t *testing.T) {
	stubProcess(t, "linger")
	shortGrace(t)

	s := New("pr3287", stubOptions())
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if state := s.State(); !state.Running {
		t.Fatalf("State() = %+v, want a running session", state)
	}

	if err := s.Start(); err == nil {
		t.Error("a second Start should be refused while one is running")
	}

	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	state := s.State()
	if state.Running {
		t.Error("State() reports running after Stop")
	}
	// A session stopped on purpose is not a session that failed, even though
	// the process was killed to make it happen.
	if state.Error != "" {
		t.Errorf("State().Error = %q after a deliberate stop, want none", state.Error)
	}
	// Stopping twice is what the session-teardown path does when a stop
	// already happened, and must not error.
	if err := s.Stop(); err != nil {
		t.Errorf("second Stop: %v", err)
	}
}

// Nearly every real failure — an unknown LU, a refused bind, a TLS mismatch —
// kills pr3287 within a moment of launch. Start waits long enough to see that
// and reports it, rather than answering "started" to a request whose printer
// was already gone.
func TestSessionStartReportsAnImmediateExit(t *testing.T) {
	stubProcess(t, "refused")
	shortGrace(t)

	s := New("pr3287", stubOptions())
	err := s.Start()
	if err == nil {
		t.Fatal("Start() = nil, want the failure the process reported")
	}
	if !strings.Contains(err.Error(), "PRTZZZ") {
		t.Errorf("Start() = %q, want it to carry what the process said", err)
	}
	if state := s.State(); state.Running {
		t.Error("State() reports running after a failed start")
	}
}

func TestSessionStartRefusesInvalidOptions(t *testing.T) {
	stubProcess(t, "linger")
	opts := stubOptions()
	opts.LU = "-command"

	if err := New("pr3287", opts).Start(); err == nil {
		t.Fatal("Start() accepted an LU name that would read as an option")
	}
}

func TestSessionStartWithoutABinary(t *testing.T) {
	if err := New("  ", stubOptions()).Start(); err == nil {
		t.Fatal("Start() = nil with no binary to run")
	}
}

// shortGrace keeps the startup wait out of the test's wall-clock time while
// leaving it long enough for a process to fail in.
func shortGrace(t *testing.T) {
	t.Helper()
	previous := startupGrace
	startupGrace = 250 * time.Millisecond
	t.Cleanup(func() { startupGrace = previous })
}
