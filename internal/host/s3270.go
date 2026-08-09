package host

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// S3270 implements the Host interface using the s3270 subprocess.
type S3270 struct {
	ExecPath   string
	Args       []string
	TargetHost string

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Scanner
	stderr *bufio.Scanner

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

	stderrPipe, err := h.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}
	h.stderr = bufio.NewScanner(stderrPipe)
	h.stderr.Buffer(make([]byte, 0, 64*1024), 256*1024)

	// Start the process
	if err := h.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start s3270: %w", err)
	}

	// Pass the scanner explicitly: the goroutine must not read the mutable
	// h.stderr field, which stop/cleanup paths set to nil under h.mu (a data
	// race and potential nil deref otherwise).
	go h.captureStderr(h.stderr)

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
	h.stderr = nil
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
	log.Printf("s3270: cmd=%q status=%q", cmd, status)

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
	log.Printf("s3270: cmd=%q status=%q", cmd, status)
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
	log.Printf("s3270: cmd=%q status=%q", cmd, status)
	if err != nil {
		return err
	}
	return nil
}

func (h *S3270) waitUnlockLocked() error {
	cmd := h.waitUnlockCommand()
	_, status, err := h.doCommandLocked(cmd)
	log.Printf("s3270: cmd=%q status=%q", cmd, status)
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
// emitted literally; backslash and double-quote are backslash-escaped; all
// other runes (control characters and anything above 0x7E) are emitted as
// \xNN for each UTF-8 byte, matching what the previous per-rune
// key(0x..) loop sent over the wire. s must not contain newlines.
func escapeForS3270String(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 8)
	buf := make([]byte, utf8.UTFMax)
	for _, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == '"':
			b.WriteString(`\"`)
		case r >= 0x20 && r <= 0x7E:
			b.WriteRune(r)
		default:
			n := utf8.EncodeRune(buf, r)
			for i := 0; i < n; i++ {
				fmt.Fprintf(&b, `\x%02x`, buf[i])
			}
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
	for y := 0; y < h.screen.Height && index < len(runes); y++ {
		for x := 0; x < h.screen.Width && index < len(runes); x++ {
			newCh := runes[index]
			oldCh := h.screen.CharAt(x, y)
			if newCh != oldCh {
				cmd := fmt.Sprintf("movecursor(%d, %d)", y, x)
				if _, _, err := h.doCommandLocked(cmd); err != nil {
					return err
				}
				if newCh != 0 {
					if err := h.writeStringLocked(string(newCh), false); err != nil {
						return err
					}
				}
			}
			index++
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

// doCommandLocked executes a command and reads response until "ok".
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
				h.stderr = nil
			}
		}
		if h.verboseLogging {
			log.Printf("[VERBOSE] s3270 command timed out")
		}
		return nil, "", fmt.Errorf("s3270 command timed out")
	}
}

func (h *S3270) readResponse(stdout *bufio.Scanner) ([]string, string, error) {
	if stdout == nil {
		return nil, "", h.terminalError("s3270 stdout not initialized")
	}
	var lines []string
	for {
		if !stdout.Scan() {
			if err := stdout.Err(); err != nil {
				return nil, "", err
			}
			return nil, "", h.terminalError("s3270 terminated")
		}
		line := stdout.Text()
		if line == "ok" {
			break
		}
		lines = append(lines, line)
	}

	if len(lines) == 0 {
		return nil, "", h.terminalError("no status received")
	}

	status := lines[len(lines)-1]
	data := lines[:len(lines)-1]
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
	log.Printf("s3270: cmd=%q status=%q", cmd, status)
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
	if err != nil {
		return "", err
	}
	if isS3270Error(status, data) {
		// Treat as "unknown query" rather than fatal — newer/older s3270
		// versions may simply not implement the requested query.
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
