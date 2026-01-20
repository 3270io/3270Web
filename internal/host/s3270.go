package host

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

// S3270 implements the Host interface using the s3270 subprocess.
type S3270 struct {
	ExecPath string
	Args     []string

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Scanner

	screen *Screen
	mu     sync.Mutex // Protects command execution
}

// NewS3270 creates a new S3270 host instance.
func NewS3270(execPath string, args ...string) *S3270 {
	return &S3270{
		ExecPath: execPath,
		Args:     args,
		screen:   &Screen{},
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

	// Start the process
	if err := h.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start s3270: %w", err)
	}

	// Ideally we should wait for a prompt or "ok", but s3270 starts quietly until we send commands?
	// Java code sends commands immediately.
	return nil
}

func (h *S3270) Stop() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.stdin != nil {
		// Send quit just in case
		fmt.Fprintln(h.stdin, "quit")
		h.stdin.Close()
	}
	if h.cmd != nil {
		// Kill if still running
		if h.cmd.ProcessState == nil {
			h.cmd.Process.Kill()
		}
		h.cmd.Wait()
	}
	return nil
}

func (h *S3270) IsConnected() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cmd != nil && h.cmd.ProcessState == nil
}

func (h *S3270) UpdateScreen() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	lines, status, err := h.doCommandLocked("readbuffer ascii")
	if err != nil {
		return err
	}

	return h.screen.Update(status, lines)
}

func (h *S3270) GetScreen() *Screen {
	return h.screen
}

func (h *S3270) SendKey(key string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	_, _, err := h.doCommandLocked(key)
	return err
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
					h.doCommandLocked("newline")
				} else {
					// Encode as hex key to avoid escaping issues
					h.doCommandLocked(fmt.Sprintf("key(0x%x)", r))
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

// doCommandLocked executes a command and reads response until "ok".
func (h *S3270) doCommandLocked(cmd string) ([]string, string, error) {
	if h.stdin == nil {
		return nil, "", fmt.Errorf("not connected")
	}

	_, err := fmt.Fprintln(h.stdin, cmd)
	if err != nil {
		return nil, "", err
	}

	var lines []string
	for h.stdout.Scan() {
		line := h.stdout.Text()
		if line == "ok" {
			break
		}
		lines = append(lines, line)
	}
	if err := h.stdout.Err(); err != nil {
		return nil, "", err
	}

	if len(lines) == 0 {
		// It's possible to get just "ok"?
		// If just "ok", then data is empty, status is empty?
		// Actually s3270 usually returns status line before ok.
		// If command fails, it might output error.
		return nil, "", nil
	}

	status := lines[len(lines)-1]
	data := lines[:len(lines)-1]

	return data, status, nil
}
