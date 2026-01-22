package host

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strconv"
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
	stderr *bufio.Scanner

	lastErrMu sync.Mutex
	lastErr   string

	screen *Screen
	mu     sync.Mutex // Protects command execution
}

const (
	waitUnlockTimeoutSeconds = 10
	commandTimeout           = 15 * time.Second
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

	h.cmd = exec.Command(h.ExecPath, h.Args...)

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

	go h.captureStderr()

	// Wait for formatted screen like Java, but keep it bounded.
	return h.waitFormattedLocked()
}

func (h *S3270) Stop() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.stdin != nil {
		// Send quit just in case
		fmt.Fprintln(h.stdin, "quit")
		h.stdin.Close()
		h.stdin = nil
	}
	if h.cmd != nil {
		// Kill if still running
		if h.cmd.ProcessState == nil {
			h.cmd.Process.Kill()
		}
		h.cmd.Wait()
		h.cmd = nil
	}
	return nil
}

func (h *S3270) IsConnected() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cmd != nil && h.cmd.ProcessState == nil && h.stdin != nil
}

func (h *S3270) UpdateScreen() error {
	if err := h.updateScreenOnce(); err != nil {
		// If not connected, try to restart once
		if !h.IsConnected() || isConnectionError(err) {
			if restartErr := h.Start(); restartErr == nil {
				return h.updateScreenOnce()
			}
		}
		return err
	}
	return nil
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

func (h *S3270) SendKey(key string) error {
	if err := h.sendKeyOnce(key); err != nil {
		if !h.IsConnected() || isConnectionError(err) {
			if restartErr := h.Start(); restartErr == nil {
				return h.sendKeyOnce(key)
			}
		}
		return err
	}
	return nil
}

func (h *S3270) sendKeyOnce(key string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if key == "" {
		key = "Enter"
	}

	data, status, err := h.doCommandLocked(key)
	log.Printf("s3270: cmd=%q status=%q", key, status)
	if err == nil && isDisconnectedStatus(status) {
		if err := h.reconnectLocked(); err != nil {
			return err
		}
		return nil
	}
	if err == nil && !isS3270Error(status, data) {
		if isAidKey(key) && !isKeyboardUnlocked(status) {
			return h.waitUnlockLocked()
		}
		return nil
	}

	keySpec := keyToKeySpec(key)
	if keySpec != "" {
		fallback := fmt.Sprintf("Key(%s)", keySpec)
		data, status, err = h.doCommandLocked(fallback)
		log.Printf("s3270: cmd=%q status=%q", fallback, status)
		if err == nil && isDisconnectedStatus(status) {
			if err := h.reconnectLocked(); err != nil {
				return err
			}
			return nil
		}
		if err == nil && !isS3270Error(status, data) {
			if isAidKey(key) && !isKeyboardUnlocked(status) {
				return h.waitUnlockLocked()
			}
			return nil
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

func isAidKey(key string) bool {
	upper := strings.ToUpper(strings.TrimSpace(key))
	return upper == "ENTER" || strings.HasPrefix(upper, "PF") || strings.HasPrefix(upper, "PA") || upper == "CLEAR" || upper == "SYSREQ" || upper == "ATTN"
}

// isKeyboardUnlocked checks if the keyboard is unlocked based on the s3270 status line.
// The first field in the status line indicates keyboard state: "U" = Unlocked, "L" = Locked.
func isKeyboardUnlocked(status string) bool {
	// Status format is space-separated fields, e.g., "U F P C(localhost) I 4 24 80 0 0 0x0 0.000"
	// The first field is the keyboard state, followed by a space
	return len(status) >= 2 && strings.HasPrefix(status, "U ")
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

func isS3270Error(status string, data []string) bool {
	if strings.HasPrefix(status, "E ") {
		return true
	}
	for _, line := range data {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "error") {
			return true
		}
	}
	return false
}

func isDisconnectedStatus(status string) bool {
	parts := strings.Fields(status)
	if len(parts) >= 4 {
		return parts[3] == "N"
	}
	return false
}

func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not connected") ||
		strings.Contains(msg, "terminated") ||
		strings.Contains(msg, "no status received") ||
		strings.Contains(msg, "timed out")
}

func keyToKeySpec(key string) string {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return "Enter"
	}

	upper := strings.ToUpper(trimmed)
	if strings.HasPrefix(upper, "PF(") && strings.HasSuffix(upper, ")") {
		inner := strings.TrimSuffix(strings.TrimPrefix(upper, "PF("), ")")
		if n, err := strconv.Atoi(inner); err == nil {
			return fmt.Sprintf("PF%d", n)
		}
	}
	if strings.HasPrefix(upper, "PA(") && strings.HasSuffix(upper, ")") {
		inner := strings.TrimSuffix(strings.TrimPrefix(upper, "PA("), ")")
		if n, err := strconv.Atoi(inner); err == nil {
			return fmt.Sprintf("PA%d", n)
		}
	}

	return trimmed
}

func (h *S3270) SubmitScreen() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	for _, f := range h.screen.Fields {
		if !f.IsProtected() && f.Changed {
			// Move cursor to field start
			cmd := fmt.Sprintf("movecursor(%d, %d)", f.StartY, f.StartX)
			if _, _, err := h.doCommandLocked(cmd); err != nil {
				return err
			}

			// Erase to End of Field
			if _, _, err := h.doCommandLocked("eraseeof"); err != nil {
				return err
			}

			// Send characters
			// TODO: Optimize by sending strings instead of individual keys if possible?
			// s3270 "string" command exists.
			// But Java used "key(0x..)". Let's try to be safer with "string" or keys.
			// Using "string" command handles escaping.
			// For now, let's mimic Java logic (char by char) for fidelity.
			for _, r := range f.Value {
				if r == '\n' {
					if _, _, err := h.doCommandLocked("newline"); err != nil {
						return err
					}
				} else {
					// Encode as hex key to avoid escaping issues
					if _, _, err := h.doCommandLocked(fmt.Sprintf("key(0x%x)", r)); err != nil {
						return err
					}
				}
			}

			// Reset changed flag
			f.Changed = false
		}
	}
	return nil
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
					if _, _, err := h.doCommandLocked(fmt.Sprintf("key(0x%x)", newCh)); err != nil {
						return err
					}
				}
			}
			index++
		}
		index++ // skip newline
	}

	return nil
}

// doCommandLocked executes a command and reads response until "ok".
func (h *S3270) doCommandLocked(cmd string) ([]string, string, error) {
	if h.stdin == nil {
		return nil, "", fmt.Errorf("not connected")
	}

	_, err := fmt.Fprintln(h.stdin, cmd)
	if err != nil {
		return nil, "", err
	}

	type commandResult struct {
		data   []string
		status string
		err    error
	}

	resultCh := make(chan commandResult, 1)
	go func() {
		var lines []string
		for {
			if !h.stdout.Scan() {
				if err := h.stdout.Err(); err != nil {
					resultCh <- commandResult{err: err}
					return
				}
				resultCh <- commandResult{err: h.terminalError("s3270 terminated")}
				return
			}
			line := h.stdout.Text()
			if line == "ok" {
				break
			}
			lines = append(lines, line)
		}

		if len(lines) == 0 {
			resultCh <- commandResult{err: h.terminalError("no status received")}
			return
		}

		status := lines[len(lines)-1]
		data := lines[:len(lines)-1]
		resultCh <- commandResult{data: data, status: status}
	}()

	select {
	case result := <-resultCh:
		return result.data, result.status, result.err
	case <-time.After(commandTimeout):
		if h.cmd != nil && h.cmd.Process != nil {
			_ = h.cmd.Process.Kill()
		}
		// Clean up stdin to prevent "broken pipe" on subsequent calls
		if h.stdin != nil {
			h.stdin.Close()
			h.stdin = nil
		}
		return nil, "", fmt.Errorf("s3270 command timed out")
	}
}

func (h *S3270) captureStderr() {
	for h.stderr.Scan() {
		msg := strings.TrimSpace(h.stderr.Text())
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
	if _, _, err := h.doCommandLocked(fmt.Sprintf("Connect(%s)", h.TargetHost)); err != nil {
		return err
	}
	return h.waitFormattedLocked()
}
