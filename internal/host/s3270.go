package host

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// S3270 implements the Host interface using the s3270 subprocess.
type S3270 struct {
	ExecPath   string
	Args       []string
	TargetHost string

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Scanner
	// stderrRead is this end of the subprocess's standard error. It is a pipe
	// this code owns rather than the one exec.Cmd hands out — see Start.
	stderrRead *os.File

	lastErrMu sync.Mutex
	lastErr   string

	screen         *Screen
	mu             sync.Mutex // Protects command execution
	verboseLogging bool

	connectStart    time.Time
	connectDuration time.Duration
}

const (
	waitUnlockTimeoutSeconds = 10
	commandTimeout           = 15 * time.Second
	// Disconnect and quit are both on the teardown path, where an unresponsive
	// subprocess must not be allowed to hold up session cleanup. They get their
	// own short budgets rather than commandTimeout's fifteen seconds: the point
	// of a graceful close is that it is quick, and the fallback — killing the
	// process — is exactly what used to happen unconditionally.
	disconnectTimeout = 3 * time.Second
	quitGraceTimeout  = 2 * time.Second
	// A file transfer is the one action that legitimately takes minutes. It
	// moves a dataset a record at a time over the same 3270 data stream the
	// screen uses, and how long that takes is a property of the dataset rather
	// than of anything this end can hurry along.
	//
	// The general command budget is fifteen seconds, and running out of it kills
	// the subprocess — which is right for an action that should have answered
	// immediately and wrong for this one. Under the shared budget any transfer
	// longer than fifteen seconds lost the transfer *and* the session, and did
	// it more reliably the more the file was worth moving.
	transferTimeout = 30 * time.Minute
)

// NewS3270 creates a new S3270 host instance.
func NewS3270(execPath string, args ...string) *S3270 {
	targetHost := ""
	if len(args) > 0 {
		targetHost = args[len(args)-1]
	}
	return &S3270{
		ExecPath:   execPath,
		Args:       args,
		TargetHost: targetHost,
		screen:     &Screen{},
	}
}

func (h *S3270) Start() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Clean up any stale process state before starting a new subprocess.
	h.stopLocked()
	h.connectStart = time.Now()
	h.connectDuration = 0

	h.cmd = exec.Command(h.ExecPath, h.Args...)
	// The subprocess inherits this program's environment with the codeset
	// pinned; see locale.go for why a server that inherits no locale at all
	// costs every screen its non-ASCII characters.
	h.cmd.Env = s3270Environment(os.Environ())
	configureCmd(h.cmd)

	var err error
	h.stdin, err = h.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdoutPipe, err := h.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	h.stdout = bufio.NewScanner(stdoutPipe)
	h.stdout.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	// Standard error goes to a pipe this code owns rather than to
	// cmd.StderrPipe(). exec closes the pipe it hands out as soon as Wait sees
	// the process exit, and the goroutine draining it is still reading — which
	// is a documented misuse, and in practice loses the last line written. That
	// line is the one that says why: "Connection refused", a certificate that
	// did not verify, a host name that does not resolve. Owning the pipe means
	// the reader gets to the end of it before it goes away.
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}
	h.cmd.Stderr = stderrWrite
	h.stderrRead = stderrRead

	// Start the process
	if err := h.cmd.Start(); err != nil {
		_ = stderrWrite.Close()
		return fmt.Errorf("failed to start s3270: %w", err)
	}
	// The child holds its own descriptor now. This copy has to go, or the read
	// end never sees end-of-file and the reader outlives the process.
	_ = stderrWrite.Close()

	// Pass the scanner explicitly: the goroutine must not read the mutable
	// h.stderrRead field, which stop/cleanup paths clear under h.mu (a data
	// race and potential nil deref otherwise).
	stderrScanner := bufio.NewScanner(stderrRead)
	stderrScanner.Buffer(make([]byte, 0, 64*1024), 256*1024)
	go h.captureStderr(stderrScanner)

	// The child exists from here on, so starting is a transaction: either it
	// completes and the caller takes ownership of the process, or it takes the
	// process with it on the way out.
	//
	// Without this, a failure after cmd.Start — a host that refuses the
	// connection, a screen that never comes back formatted — returned an error
	// while the subprocess was still running. The caller sees a failure and
	// never registers a session, so nothing holds a reference and nothing ever
	// calls Stop: the s3270 and its three pipes stay for the life of the
	// server, outside every session cap that is supposed to bound them. A loop
	// of failing connects is then a process leak with a rate limit on it.
	handedOver := false
	defer func() {
		if !handedOver {
			_ = h.stopLocked()
		}
	}()

	if h.TargetHost == "" {
		handedOver = true
		return nil
	}

	// If a target host wasn't provided as a command arg, connect explicitly.
	if len(h.Args) == 0 || h.Args[len(h.Args)-1] != h.TargetHost {
		if err := h.reconnectLocked(); err != nil {
			return err
		}
		handedOver = true
		return nil
	}

	// Wait for formatted screen like Java, but keep it bounded.
	if err := h.waitFormattedLocked(); err != nil {
		return err
	}
	if !h.connectStart.IsZero() {
		h.connectDuration = time.Since(h.connectStart)
	}
	handedOver = true
	return nil
}

func (h *S3270) Stop() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.stopLocked()
}

func (h *S3270) stopLocked() error {
	// Close the host session before the process goes away.
	//
	// Teardown used to be `quit` immediately followed by SIGKILL, which leaves
	// the TCP connection to the host to be noticed and reaped by whatever is in
	// between. That is fine against a host on the same LAN and unfriendly
	// everywhere else: a TN3270 gateway or an SNA-to-IP proxy that never saw the
	// session close can hold the LU until its own idle timer expires, and the
	// next connect on that LU is refused in the meantime. Asking s3270 to
	// disconnect makes it send the close the other end is waiting for.
	h.disconnectLocked()

	if h.stdin != nil {
		_, _ = fmt.Fprintln(h.stdin, "quit")
		_ = h.stdin.Close()
		h.stdin = nil
	}
	if h.cmd != nil {
		h.reapLocked(h.cmd)
		h.cmd = nil
	}
	h.stdout = nil
	// Closed after the process has been reaped, so the drain goroutine has had
	// the whole of standard error to read before this unblocks it.
	if h.stderrRead != nil {
		_ = h.stderrRead.Close()
		h.stderrRead = nil
	}
	return nil
}

// disconnectLocked asks s3270 to close the host session, best effort. A host
// that has already gone away, or a subprocess that is already dead, makes this
// fail — neither is a reason to fail the caller's Stop().
func (h *S3270) disconnectLocked() {
	if h.stdin == nil || h.cmd == nil || h.cmd.ProcessState != nil {
		return
	}
	_, _, err := h.executeCommandLockedTimeout("Disconnect", "Disconnect", disconnectTimeout)
	if err != nil && h.verboseLogging {
		log.Printf("[VERBOSE] s3270 Disconnect on teardown: %v", err)
	}
}

// reapLocked waits for the subprocess to act on quit, and kills it if it does
// not. Wait is called exactly once — from the goroutine — because calling it
// twice on one exec.Cmd is an error, and the kill path still has to wait for
// the process to be collected or it leaves a zombie.
func (h *S3270) reapLocked(cmd *exec.Cmd) {
	if cmd.ProcessState != nil || cmd.Process == nil {
		// Already reaped, or never started — there is nothing to wait for, and
		// a second Wait on one exec.Cmd is itself an error.
		return
	}
	waited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(waited)
	}()
	select {
	case <-waited:
	case <-time.After(quitGraceTimeout):
		_ = cmd.Process.Kill()
		<-waited
	}
}

func (h *S3270) IsConnected() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cmd != nil && h.cmd.ProcessState == nil && h.stdin != nil
}

func (h *S3270) UpdateScreen() error {
	return h.withRetry(func() error {
		return h.updateScreenOnce()
	})
}

func (h *S3270) updateScreenOnce() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	for i := 0; i < 50; i++ {
		lines, status, err := h.doCommandLocked("readbuffer ascii")
		if err != nil {
			// A locked keyboard is a wait, not a failure: the host is mid-thought
			// and the buffer will be readable when it finishes. The terminal says
			// so either in a data line or, when it refuses the read outright, in
			// the refusal — both spellings mean the same thing here.
			if refusalMentions(err, "keyboard locked") {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			return err
		}
		if isDisconnectedStatus(status) {
			if err := h.reconnectLocked(); err != nil {
				return err
			}
			continue
		}
		if len(lines) > 0 && strings.HasPrefix(lines[0], "data: Keyboard locked") {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		return h.screen.Update(status, lines)
	}
	return fmt.Errorf("keyboard locked timeout")
}

func (h *S3270) GetScreen() *Screen {
	return h.screen
}

func (h *S3270) GetScreenSnapshot() *Screen {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.screen.Clone()
}

func (h *S3270) SendKey(key string) error {
	return h.withRetry(func() error {
		return h.sendKeyOnce(key)
	})
}

func (h *S3270) WriteStringAt(row, col int, text string) error {
	return h.withRetry(func() error {
		return h.writeStringAtOnce(row, col, text)
	})
}

func (h *S3270) withRetry(op func() error) error {
	if err := op(); err != nil {
		if !h.IsConnected() || isConnectionError(err) {
			_ = h.Stop()
			if restartErr := h.Start(); restartErr == nil {
				return op()
			}
		}
		return err
	}
	return nil
}

// ContainsForbiddenFieldText reports whether value contains CR, LF, or TAB —
// characters the s3270 String() command would interpret as actions rather
// than literal field text. This is the canonical rule for every layer that
// accepts field input (screen write handlers, workflow generation).
func ContainsForbiddenFieldText(value string) bool {
	return strings.ContainsAny(value, "\r\n\t")
}

func (h *S3270) sendKeyOnce(key string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Sentinel: Prevent command injection via s3270 pipe
	if strings.ContainsAny(key, "\n\r\t;") {
		return fmt.Errorf("security error: invalid characters in key command")
	}

	if key == "" {
		key = "Enter"
	}

	isAid := isAidKey(key)
	data, status, err, done := h.executeKeyCommand(key, isAid)
	if done {
		return err
	}

	keySpec := keyToKeySpec(key)
	if keySpec != "" {
		fallback := fmt.Sprintf("Key(%s)", keySpec)
		// Use original key intent (isAid) for checking unlock status even on fallback
		data, status, err, done = h.executeKeyCommand(fallback, isAid)
		if done {
			return err
		}
	}

	if err != nil {
		return err
	}
	if isS3270Error(status, data) {
		return fmt.Errorf("s3270 error: %s", status)
	}
	return nil
}

// executeKeyCommand attempts to execute a key command and handles common status checks.
// It returns done=true if the command succeeded (or reconnection succeeded), and done=false
// if the command failed and a fallback should be attempted.
func (h *S3270) executeKeyCommand(cmd string, isAid bool) ([]string, string, error, bool) {
	data, status, err := h.doCommandLocked(cmd)
	h.logAction(cmd, status)

	if err == nil && isDisconnectedStatus(status) {
		if rErr := h.reconnectLocked(); rErr != nil {
			return data, status, rErr, true
		}
		return data, status, nil, true
	}

	if err == nil && !isS3270Error(status, data) {
		if isAid && !isKeyboardUnlocked(status) {
			if wErr := h.waitUnlockLocked(); wErr != nil {
				return data, status, wErr, true
			}
		}
		return data, status, nil, true
	}

	return data, status, err, false
}

func (h *S3270) writeStringAtOnce(row, col int, text string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if text == "" {
		return nil
	}
	cmd := fmt.Sprintf("movecursor(%d, %d)", row, col)
	_, status, err := h.doCommandLocked(cmd)
	h.logAction(cmd, status)
	if err != nil {
		return err
	}
	return h.writeMultilineLocked(text, false)
}

func (h *S3270) MoveCursor(row, col int) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	cmd := fmt.Sprintf("movecursor(%d, %d)", row, col)
	_, status, err := h.doCommandLocked(cmd)
	h.logAction(cmd, status)
	if err != nil {
		return err
	}
	return nil
}

// logAction records one action and the status it came back with. It is behind
// the verbose switch because it fires on every keystroke and cursor move: at
// six sessions this is the loudest thing in the log, and a log nobody can read
// is one nobody reads when it matters.
func (h *S3270) logAction(cmd, status string) {
	if h.verboseLogging {
		log.Printf("[VERBOSE] s3270: cmd=%q status=%q", cmd, status)
	}
}

// waitUnlockLocked gives the host a bounded moment to release the keyboard
// after an AID key.
//
// Running out of that moment is not a failure of the key press. The key was
// sent, the host has it, and the keyboard is still locked — which is precisely
// what the OIA's "X SYSTEM" exists to say, and what the next screen read will
// report on its own. Turning that into an error would fail the operator's Enter
// on every host that thinks for longer than ten seconds.
func (h *S3270) waitUnlockLocked() error {
	cmd := h.waitUnlockCommand()
	_, status, err := h.doCommandLocked(cmd)
	h.logAction(cmd, status)
	if IsActionError(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return nil
}

// waitUnlockCommand returns a bounded Wait(Unlock) command to avoid indefinite hangs.
func (h *S3270) waitUnlockCommand() string {
	return fmt.Sprintf("Wait(Unlock,%d)", waitUnlockTimeoutSeconds)
}

func (h *S3270) SubmitScreen() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	for _, f := range h.screen.Fields {
		if !f.IsProtected() && f.Changed {
			if err := h.writeFieldValueLocked(f); err != nil {
				return err
			}
			f.Changed = false
		}
	}
	return nil
}

// writeFieldValueLocked positions the cursor at the start of f, erases to the
// end of the field, and writes f.Value via the s3270 String() action. Embedded
// newlines split the value into multiple String() segments separated by the
// s3270 newline action. Hidden fields are logged as String("***"). Caller must
// hold h.mu.
func (h *S3270) writeFieldValueLocked(f *Field) error {
	cmd := fmt.Sprintf("movecursor(%d, %d)", f.StartY, f.StartX)
	if _, _, err := h.doCommandLocked(cmd); err != nil {
		return err
	}
	if _, _, err := h.doCommandLocked("eraseeof"); err != nil {
		return err
	}
	return h.writeMultilineLocked(f.Value, f.IsHidden())
}

// writeMultilineLocked splits s on '\n' and emits one s3270 String() per
// segment, separated by newline actions. Caller must hold h.mu.
func (h *S3270) writeMultilineLocked(s string, redact bool) error {
	segments := strings.Split(s, "\n")
	for i, seg := range segments {
		if i > 0 {
			if _, _, err := h.doCommandLocked("newline"); err != nil {
				return err
			}
		}
		if seg == "" {
			continue
		}
		if err := h.writeStringLocked(seg, redact); err != nil {
			return err
		}
	}
	return nil
}

// writeStringLocked emits a single s3270 String("...") command. s must not
// contain newlines (callers use writeMultilineLocked to handle those). When
// redact is true, the command is logged as String("***") instead of the
// quoted value. Caller must hold h.mu.
func (h *S3270) writeStringLocked(s string, redact bool) error {
	if s == "" {
		return nil
	}
	cmd := fmt.Sprintf(`String("%s")`, escapeForS3270String(s))
	if redact {
		_, _, err := h.doCommandLockedRedacted(cmd, `String("***")`)
		return err
	}
	_, _, err := h.doCommandLocked(cmd)
	return err
}

// escapeForS3270String escapes s for use as the quoted argument to the s3270
// String() action. Printable ASCII (other than backslash and double-quote) is
// emitted literally; backslash and double-quote are backslash-escaped; control
// characters go out as \xNN; everything above ASCII goes out as \uXXXX.
//
// The distinction between the two escapes is the whole point of this function.
// \xNN names a *character*, not a byte, so spelling a character out as its
// UTF-8 bytes does not reassemble it at the other end — it types each byte as a
// character of its own. An é sent as \xc3\xa9 arrives as "Ã©": wrong text, and
// one cell of the field eaten by a character the operator never typed, which
// shifts everything after it. \uXXXX names the character itself and arrives as
// one.
//
// Runes outside the Basic Multilingual Plane have no spelling here at all: the
// escape takes four hex digits, and the terminal drops a surrogate pair rather
// than composing it. They become U+FFFD, so a field keeps its shape and the
// substitution is visible, rather than silently swallowing a character and
// pulling the rest of the value left. Nothing in a 3270 code page lives up
// there. s must not contain newlines.
func escapeForS3270String(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 8)
	for _, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == '"':
			b.WriteString(`\"`)
		case r >= 0x20 && r <= 0x7E:
			b.WriteRune(r)
		case r < 0x20 || r == 0x7F:
			fmt.Fprintf(&b, `\x%02x`, byte(r))
		case r > 0xFFFF:
			b.WriteString(`�`)
		default:
			fmt.Fprintf(&b, `\u%04x`, r)
		}
	}
	return b.String()
}

func (h *S3270) SubmitFieldUpdates(updates map[string]string) error {
	// Not implemented yet
	return nil
}

func (h *S3270) SubmitUnformatted(data string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.screen == nil {
		return fmt.Errorf("screen not initialized")
	}

	data = strings.ReplaceAll(data, "\r\n", "\n")
	data = strings.ReplaceAll(data, "\r", "\n")

	index := 0
	runes := []rune(data)
	// Consecutive changed cells go out as one positioned write rather than as
	// one per character. Every command here is a round trip down the control
	// pipe, and a line of thirty changed characters was sixty of them; a screen
	// of them was thousands, each one a wait on a subprocess. The result is the
	// same because these are contiguous cells of an unformatted screen, where
	// typing advances the cursor one cell at a time with nothing to skip over.
	run := make([]rune, 0, 80)
	runY, runX := 0, 0
	flush := func() error {
		if len(run) == 0 {
			return nil
		}
		text := string(run)
		run = run[:0]
		cmd := fmt.Sprintf("movecursor(%d, %d)", runY, runX)
		if _, _, err := h.doCommandLocked(cmd); err != nil {
			return err
		}
		return h.writeStringLocked(text, false)
	}
	for y := 0; y < h.screen.Height && index < len(runes); y++ {
		for x := 0; x < h.screen.Width && index < len(runes); x++ {
			// A row separator ends the row wherever it falls. Reading it as a
			// character instead — which is what happens if only a full-width row
			// is expected to have one — types a newline into the display and
			// pushes the rest of the text one cell along, on every line shorter
			// than the screen is wide.
			if runes[index] == '\n' {
				break
			}
			newCh := runes[index]
			index++
			// A null is not a character to type: it is the absence of one, and
			// the old per-character path moved the cursor there and wrote
			// nothing. It ends the run instead.
			if newCh == 0 || newCh == h.screen.CharAt(x, y) {
				if err := flush(); err != nil {
					return err
				}
				continue
			}
			if len(run) == 0 {
				runY, runX = y, x
			}
			run = append(run, newCh)
		}
		if err := flush(); err != nil {
			return err
		}
		// Skip the row separator only when one is actually present. An
		// unconditional increment assumes every row is newline-terminated and
		// full-width; when that does not hold it would swallow a real character
		// and shift every subsequent write by one cell.
		if index < len(runes) && runes[index] == '\n' {
			index++
		}
	}

	return nil
}

// doCommandLocked executes one action and reads its response, which ends
// either in "ok" or, when the terminal refuses it, in an ActionError.
func (h *S3270) doCommandLocked(cmd string) ([]string, string, error) {
	return h.executeCommandLocked(cmd, cmd)
}

func (h *S3270) doCommandLockedRedacted(cmd string, logCmd string) ([]string, string, error) {
	return h.executeCommandLocked(cmd, logCmd)
}

func (h *S3270) executeCommandLocked(cmd string, logCmd string) ([]string, string, error) {
	return h.executeCommandLockedTimeout(cmd, logCmd, commandTimeout)
}

func (h *S3270) executeCommandLockedTimeout(cmd string, logCmd string, timeout time.Duration) ([]string, string, error) {
	if h.stdin == nil {
		return nil, "", fmt.Errorf("not connected")
	}

	if h.verboseLogging {
		log.Printf("[VERBOSE] s3270 command: %q", logCmd)
	}

	_, err := fmt.Fprintln(h.stdin, cmd)
	if err != nil {
		h.stdin = nil
		return nil, "", err
	}

	type commandResult struct {
		data   []string
		status string
		err    error
	}

	resultCh := make(chan commandResult, 1)
	stdout := h.stdout
	proc := h.cmd
	stdin := h.stdin
	go func() {
		data, status, err := h.readResponse(stdout)
		resultCh <- commandResult{data: data, status: status, err: err}
	}()

	select {
	case result := <-resultCh:
		// Name the action on the way out. readResponse knows the response was a
		// refusal but not what was refused, and a refusal that cannot say which
		// action it belongs to is most of the way to being no error at all. The
		// redacted spelling is used deliberately: this string reaches logs and
		// HTTP responses.
		if actionErr, ok := AsActionError(result.err); ok {
			actionErr.Command = logCmd
		}
		if h.verboseLogging {
			log.Printf("[VERBOSE] s3270 response - status: %q, data lines: %d", result.status, len(result.data))
			for i, line := range result.data {
				log.Printf("[VERBOSE] s3270 response data[%d]: %s", i, line)
			}
			if result.err != nil {
				log.Printf("[VERBOSE] s3270 error: %v", result.err)
			}
		}
		return result.data, result.status, result.err
	case <-time.After(timeout):
		if proc != nil && proc.Process != nil {
			_ = proc.Process.Kill()
		}
		// Clean up stdin to prevent "broken pipe" on subsequent calls
		if stdin != nil {
			_ = stdin.Close()
			if h.stdin == stdin {
				h.stdin = nil
			}
		}
		if proc != nil {
			_ = proc.Wait()
			if h.cmd == proc {
				h.cmd = nil
				h.stdout = nil
				if h.stderrRead != nil {
					_ = h.stderrRead.Close()
					h.stderrRead = nil
				}
			}
		}
		if h.verboseLogging {
			log.Printf("[VERBOSE] s3270 command timed out")
		}
		return nil, "", fmt.Errorf("s3270 command timed out")
	}
}

// readResponse reads one action's response off the control pipe: data lines,
// then the status line, then the word that says how it went. Both terminators
// are honoured — see response.go for why reading only for "ok" is a session
// killer rather than a missing feature.
//
// A refusal still returns its data and status, because the reason the terminal
// gives for saying no is the useful part, and the status line that came with it
// is the one describing the screen at that moment.
func (h *S3270) readResponse(stdout *bufio.Scanner) ([]string, string, error) {
	if stdout == nil {
		return nil, "", h.terminalError("s3270 stdout not initialized")
	}
	var lines []string
	refused := false
	for {
		if !stdout.Scan() {
			if err := stdout.Err(); err != nil {
				return nil, "", err
			}
			return nil, "", h.terminalError("s3270 terminated")
		}
		line := stdout.Text()
		if line == responseOK {
			break
		}
		if line == responseError {
			refused = true
			break
		}
		lines = append(lines, line)
	}

	if len(lines) == 0 {
		return nil, "", h.terminalError("no status received")
	}

	status := lines[len(lines)-1]
	data := lines[:len(lines)-1]
	if refused {
		return data, status, &ActionError{
			Status: status,
			Detail: strings.Join(dataLines(data), "\n"),
		}
	}
	return data, status, nil
}

func (h *S3270) captureStderr(scanner *bufio.Scanner) {
	if scanner == nil {
		return
	}
	for scanner.Scan() {
		msg := strings.TrimSpace(scanner.Text())
		if msg == "" {
			continue
		}
		h.lastErrMu.Lock()
		h.lastErr = msg
		h.lastErrMu.Unlock()
	}
}

func (h *S3270) terminalError(fallback string) error {
	h.lastErrMu.Lock()
	defer h.lastErrMu.Unlock()
	if h.lastErr != "" {
		return fmt.Errorf("%s: %s", fallback, h.lastErr)
	}
	return fmt.Errorf("%s", fallback)
}

func (h *S3270) waitFormattedLocked() error {
	for i := 0; i < 50; i++ {
		_, status, err := h.doCommandLocked("")
		if err != nil {
			return err
		}
		if strings.HasPrefix(status, "U F") {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("formatted screen not ready")
}

func (h *S3270) reconnectLocked() error {
	if h.TargetHost == "" {
		return fmt.Errorf("target host not set")
	}
	connectStart := time.Now()
	// The refusal is the whole answer here. A connect that is turned away says
	// why — "Connection refused", a name that does not resolve, a certificate
	// that did not verify — and dropping that on the floor left the caller
	// waiting out the formatted-screen budget only to be told the screen was not
	// ready, which is true and useless.
	if _, _, err := h.doCommandLocked(fmt.Sprintf("Connect(%s)", h.TargetHost)); err != nil {
		return err
	}
	if err := h.waitFormattedLocked(); err != nil {
		return err
	}
	h.connectDuration = time.Since(connectStart)
	return nil
}

// PrintText renders the current screen via the s3270 PrintText action and
// returns the rendered text. Supported formats: "html", "rtf", "string".
// The action's "string" modifier is used so output is returned inline in the
// response (data: lines) rather than written to a file on the s3270 host.
func (h *S3270) PrintText(format string) (string, error) {
	switch format {
	case "html", "rtf", "string":
	default:
		return "", fmt.Errorf("unsupported PrintText format %q (allowed: html, rtf, string)", format)
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	cmd := fmt.Sprintf("PrintText(%s,string)", format)
	data, status, err := h.doCommandLocked(cmd)
	h.logAction(cmd, status)
	if err != nil {
		return "", err
	}
	if isS3270Error(status, data) {
		return "", fmt.Errorf("s3270 PrintText error: %s", status)
	}
	// s3270 prefixes each output line with "data: ". Strip it.
	lines := make([]string, 0, len(data))
	for _, line := range data {
		lines = append(lines, strings.TrimPrefix(line, "data: "))
	}
	return strings.Join(lines, "\n"), nil
}

// SetVerboseLogging enables or disables verbose logging of S3270 commands and responses.
func (h *S3270) SetVerboseLogging(enabled bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.verboseLogging = enabled
}

// GetVerboseLogging returns whether verbose logging is enabled.
func (h *S3270) GetVerboseLogging() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.verboseLogging
}

// LastConnectDuration returns the elapsed time of the most recent successful
// connect (Start or reconnect). Zero if no connect has completed.
func (h *S3270) LastConnectDuration() time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.connectDuration
}

// Query sends an s3270 Query(arg) action and returns the response with the
// "data: " prefix stripped from each line and lines joined by '\n'. Returns
// ("", nil) if the host responds with no data lines (i.e. it does not answer
// the query), so callers can treat unknown capabilities as soft-degraded.
func (h *S3270) Query(arg string) (string, error) {
	arg = strings.TrimSpace(arg)
	// Sentinel: prevent command injection via the s3270 pipe.
	if strings.ContainsAny(arg, "\n\r\t;()") {
		return "", fmt.Errorf("security error: invalid characters in query argument")
	}
	cmd := "Query"
	if arg != "" {
		cmd = fmt.Sprintf("Query(%s)", arg)
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	data, status, err := h.doCommandLocked(cmd)
	if IsActionError(err) {
		// Treat as "unknown query" rather than fatal — newer/older s3270
		// versions may simply not implement the requested query, and answering
		// "the terminal does not say" is what the caller can actually use.
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if isS3270Error(status, data) {
		return "", nil
	}
	if len(data) == 0 {
		return "", nil
	}
	lines := make([]string, 0, len(data))
	for _, line := range data {
		lines = append(lines, strings.TrimPrefix(line, "data: "))
	}
	return strings.Join(lines, "\n"), nil
}
