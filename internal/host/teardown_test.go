// SPDX-License-Identifier: AGPL-3.0-or-later

package host

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// The teardown path is the one part of the s3270 wrapper that cannot be tested
// against a mock: what is being asserted is the order of what goes down the pipe
// and what happens to the process afterwards. So these tests drive a fake s3270,
// which is this test binary re-executed with an env var set — portable, and with
// no shell buffering to fight (os.Stdout in Go is unbuffered, a shell's printf
// builtin is not).
const (
	fakeS3270Env = "HOST_TEST_FAKE_S3270"
	fakeLogEnv   = "HOST_TEST_FAKE_S3270_LOG"
	// A status line shaped like the real thing, followed by "ok" — see
	// readResponse.
	fakeStatus = "U F U C(fake) I 4 24 80 0 0 0x0 -"
)

// TestFakeS3270Helper is not a test. It is the fake s3270 process: when the
// harness env var is set it reads commands, records them, and answers each one.
func TestFakeS3270Helper(t *testing.T) {
	mode := os.Getenv(fakeS3270Env)
	if mode == "" {
		t.Skip("not running as the fake s3270")
	}
	log, err := os.OpenFile(os.Getenv(fakeLogEnv), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake s3270: "+err.Error())
		os.Exit(1)
	}
	in := bufio.NewScanner(os.Stdin)
	for in.Scan() {
		cmd := strings.TrimSpace(in.Text())
		fmt.Fprintln(log, cmd)
		_ = log.Sync()
		if cmd == "quit" {
			// "wedged" ignores quit AND the stdin close that follows it, which is
			// what forces the kill fallback. Merely `continue`ing here would not:
			// closing stdin ends the scan loop and the process would exit on its
			// own, and the test would pass without ever exercising the kill.
			if mode == "wedged" {
				time.Sleep(time.Hour)
			}
			os.Exit(0)
		}
		// Any action whose name begins with "Refuse" is answered the way the
		// real terminal answers one it will not carry out: reasons, status, and
		// "error" where "ok" would have been. It is spelled into the command
		// rather than into the mode so one fake can be asked for a refusal and a
		// success in the same session, which is the whole point of the test that
		// uses it.
		if strings.HasPrefix(strings.ToLower(cmd), "refuse") {
			fmt.Println("data: Keyboard locked")
			fmt.Println("data: Operator error")
			fmt.Println(fakeStatus)
			fmt.Println("error")
			continue
		}
		// "lockedkeyboard" answers a screen read the way the terminal does while
		// the host still has the keyboard: a refusal, meaning "not yet, ask
		// again".
		if mode == "lockedkeyboard" && strings.HasPrefix(strings.ToLower(cmd), "readbuffer") {
			fmt.Println("data: Keyboard locked")
			fmt.Println(fakeStatus)
			fmt.Println("error")
			continue
		}
		// "unformatted" never reports a formatted screen, which is what a
		// connection that does not come up looks like from here.
		if mode == "unformatted" {
			fmt.Println(strings.Replace(fakeStatus, "U F", "U U", 1))
		} else {
			fmt.Println(fakeStatus)
		}
		fmt.Println("ok")
	}
	os.Exit(0)
}

// startFakeS3270 returns a started host backed by the fake, and the path of the
// file recording every command it received.
func startFakeS3270(t *testing.T, mode string) (*S3270, string) {
	t.Helper()
	logPath := t.TempDir() + "/commands.log"
	// The fake inherits its configuration through the environment, because
	// exec.Cmd inherits os.Environ() and S3270 has no hook for passing one — and
	// adding a test-only hook to production code to make a test easier is the
	// wrong trade.
	t.Setenv(fakeS3270Env, mode)
	t.Setenv(fakeLogEnv, logPath)
	// TargetHost is deliberately empty: Start() then returns as soon as the
	// process is up, without trying to negotiate a connection, so the command
	// log contains only what teardown puts there.
	h := &S3270{
		ExecPath: os.Args[0],
		Args:     []string{"-test.run=TestFakeS3270Helper"},
		screen:   &Screen{},
	}
	if err := h.Start(); err != nil {
		t.Fatalf("start fake s3270: %v", err)
	}
	return h, logPath
}

func recordedCommands(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read command log: %v", err)
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

// Teardown used to be `quit` immediately followed by SIGKILL, which leaves the
// TCP connection to the host for something else to notice. A gateway that never
// saw the session close can hold the LU until its own idle timer expires, and
// the next connect on that LU is refused meanwhile.
func TestStopDisconnectsBeforeQuitting(t *testing.T) {
	h, logPath := startFakeS3270(t, "polite")

	if err := h.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	cmds := recordedCommands(t, logPath)
	var disconnectAt, quitAt = -1, -1
	for i, cmd := range cmds {
		switch strings.ToLower(cmd) {
		case "disconnect":
			if disconnectAt == -1 {
				disconnectAt = i
			}
		case "quit":
			if quitAt == -1 {
				quitAt = i
			}
		}
	}
	if disconnectAt == -1 {
		t.Fatalf("Stop never asked s3270 to disconnect; commands = %q", cmds)
	}
	if quitAt == -1 {
		t.Fatalf("Stop never sent quit; commands = %q", cmds)
	}
	if disconnectAt > quitAt {
		t.Errorf("Disconnect came after quit (%d > %d) — by then there is no session left to close; commands = %q",
			disconnectAt, quitAt, cmds)
	}
}

// Stop must not be able to hang on a subprocess that ignores quit. The kill is
// the fallback, not the default.
func TestStopKillsASubprocessThatIgnoresQuit(t *testing.T) {
	h, _ := startFakeS3270(t, "wedged")

	start := time.Now()
	if err := h.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	elapsed := time.Since(start)

	// disconnectTimeout is not spent here: the fake answers Disconnect. What is
	// spent is quitGraceTimeout, once.
	if elapsed > quitGraceTimeout+5*time.Second {
		t.Errorf("Stop took %s on a wedged subprocess, which is long enough to hold up session cleanup", elapsed)
	}
	if h.IsConnected() {
		t.Error("still reports connected after Stop")
	}
}

// reapLocked calls Wait exactly once, because a second Wait on one exec.Cmd is
// an error and the kill path still has to collect the process or it leaves a
// zombie.
func TestReapLockedCollectsAKilledProcess(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestFakeS3270Helper")
	cmd.Env = append(os.Environ(),
		fakeS3270Env+"=wedged",
		fakeLogEnv+"="+t.TempDir()+"/commands.log",
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()

	h := &S3270{}
	start := time.Now()
	h.reapLocked(cmd)

	if cmd.ProcessState == nil {
		t.Fatal("the process was not collected — Wait never completed")
	}
	if elapsed := time.Since(start); elapsed > quitGraceTimeout+5*time.Second {
		t.Errorf("reapLocked took %s", elapsed)
	}
	// Calling it again must be a no-op rather than a second Wait.
	h.reapLocked(cmd)
}

// Starting is a transaction. Everything after cmd.Start can fail — the host
// refuses the connection, the screen never comes back formatted — and until
// Start unwound its own child, those failures returned an error with the
// subprocess still running and nobody holding a reference to it.
//
// That is the leak: the caller sees a failure and never registers a session, so
// nothing ever calls Stop. The s3270 and its three pipes stay for the life of
// the server, outside every session cap meant to bound them, and a loop of
// failing connects is a process leak with a rate limit on it.
func TestStartReapsItsChildWhenTheConnectionFails(t *testing.T) {
	logPath := t.TempDir() + "/commands.log"
	t.Setenv(fakeS3270Env, "unformatted")
	t.Setenv(fakeLogEnv, logPath)

	h := &S3270{
		ExecPath:   os.Args[0],
		Args:       []string{"-test.run=TestFakeS3270Helper", "fake.host:3270"},
		TargetHost: "fake.host:3270",
		screen:     &Screen{},
	}

	if err := h.Start(); err == nil {
		_ = h.Stop()
		t.Fatal("Start reported success against a screen that never became formatted")
	}

	if h.cmd != nil {
		t.Error("Start left the subprocess attached after failing; nothing will ever call Stop on it")
	}
	if h.stdin != nil || h.stdout != nil || h.stderrRead != nil {
		t.Error("Start left its pipes open after failing")
	}
}
