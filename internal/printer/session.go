// SPDX-License-Identifier: AGPL-3.0-or-later

package printer

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// A printer session is a long-lived subprocess, not a request.
//
// pr3287 binds the LU and then waits: a job may arrive in a minute or at
// three in the morning. That shape is what most of this file is about — the
// process outliving the call that started it, and the two ways it can end
// (asked to, or on its own) having to look different to whoever asks later.
//
// It deliberately does not restart itself. pr3287 will, with -reconnect, and
// leaving that off is a choice: an LU that was refused because somebody else
// holds it, or because the name is wrong, would otherwise retry forever behind
// a status that says "running". A printer session that stops says why, and
// starting it again is one click.

// startupGrace is how long Start waits to see whether the process survives.
//
// Nearly every way a printer session fails — the LU is unknown, the host
// refuses the bind, TLS does not agree — kills pr3287 within a few hundred
// milliseconds of launch. Waiting that long converts the common failure from
// "started, and stopped a moment later for reasons nobody was watching for"
// into an error on the request that asked for it.
var startupGrace = 750 * time.Millisecond

// maxStderrBytes bounds what is kept of the process's diagnostics. pr3287 is
// not chatty, but a session that fails in a loop against a host that answers
// slowly could be; this is the last few messages, not a log.
const maxStderrBytes = 4 << 10

// stopGrace is how long a stopping process is given to go quietly before it
// is killed.
const stopGrace = 2 * time.Second

// commandFor is the process constructor, replaced in tests.
var commandFor = exec.Command

// Session is one running pr3287.
type Session struct {
	execPath string
	opts     Options

	mu      sync.Mutex
	cmd     *exec.Cmd
	running bool
	// stopping records that Stop asked for this exit, so the process ending
	// is not reported as a failure.
	stopping bool
	exitErr  string
	stderr   strings.Builder
	// done closes when the process has been reaped, so Stop can wait for it
	// without polling.
	done chan struct{}
}

// New prepares a printer session. Nothing runs until Start.
func New(execPath string, opts Options) *Session {
	return &Session{execPath: execPath, opts: opts}
}

// Options returns what this session was started with.
func (s *Session) Options() Options { return s.opts }

// Start launches pr3287 and reports a process that did not survive its first
// moments as a failure to start.
func (s *Session) Start() error {
	args, err := s.opts.Args()
	if err != nil {
		return err
	}
	if strings.TrimSpace(s.execPath) == "" {
		return fmt.Errorf("no pr3287 binary was found")
	}

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("this session already has a printer running")
	}
	cmd := commandFor(s.execPath, args...)
	configureCmd(cmd)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("capture the printer's diagnostics: %w", err)
	}
	if err := cmd.Start(); err != nil {
		s.mu.Unlock()
		return fmt.Errorf("start the printer session: %w", err)
	}
	s.cmd = cmd
	s.running = true
	s.stopping = false
	s.exitErr = ""
	s.stderr.Reset()
	done := make(chan struct{})
	s.done = done
	s.mu.Unlock()

	// The pipe is passed in rather than read off the struct: the reaper below
	// clears cmd under the lock, and a goroutine reading the field would be
	// racing it.
	drained := make(chan struct{})
	go func() {
		s.readStderr(stderr)
		close(drained)
	}()
	go s.reap(cmd, drained, done)

	select {
	case <-done:
		// Gone already. Whatever it printed on the way out is the most
		// useful thing anyone will get, so it is the error.
		return fmt.Errorf("the printer session did not start: %s", s.failureReason())
	case <-time.After(startupGrace):
		return nil
	}
}

// readStderr keeps the tail of what the process said.
func (s *Session) readStderr(r io.ReadCloser) {
	defer r.Close()
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 4096), 64*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		s.mu.Lock()
		if s.stderr.Len() < maxStderrBytes {
			if s.stderr.Len() > 0 {
				s.stderr.WriteString("; ")
			}
			s.stderr.WriteString(line)
		}
		s.mu.Unlock()
	}
}

// reap waits for the process and records how it ended.
//
// The stderr pipe is drained first because Wait closes it, and because a
// process that failed on startup is only useful if what it said about that
// arrives before the failure is reported.
func (s *Session) reap(cmd *exec.Cmd, drained <-chan struct{}, done chan struct{}) {
	<-drained
	err := cmd.Wait()
	s.mu.Lock()
	s.running = false
	s.cmd = nil
	if err != nil && !s.stopping {
		s.exitErr = err.Error()
	}
	s.mu.Unlock()
	close(done)
}

// failureReason is the most useful sentence available about a process that
// ended when nobody asked it to.
func (s *Session) failureReason() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.stderr.Len() > 0:
		return s.stderr.String()
	case s.exitErr != "":
		return s.exitErr
	default:
		return "the printer process exited immediately and said nothing about why"
	}
}

// State is what a status request reports.
type State struct {
	Running bool
	// Error is why a session that is not running stopped, empty when it was
	// stopped on purpose or never started.
	Error string
}

// State reports whether the process is alive and, if not, why not.
func (s *Session) State() State {
	s.mu.Lock()
	running := s.running
	stopping := s.stopping
	exitErr := s.exitErr
	diagnostics := s.stderr.String()
	s.mu.Unlock()

	if running {
		return State{Running: true}
	}
	if stopping || (exitErr == "" && diagnostics == "") {
		return State{}
	}
	if exitErr == "" {
		// The process ended cleanly but had something to say. Almost always a
		// host that closed the printer LU, which is worth showing.
		return State{Error: diagnostics}
	}
	if diagnostics != "" {
		return State{Error: diagnostics}
	}
	return State{Error: exitErr}
}

// Stop ends the process and waits for it, killing it if it will not go.
func (s *Session) Stop() error {
	s.mu.Lock()
	cmd := s.cmd
	done := s.done
	if !s.running || cmd == nil || cmd.Process == nil {
		s.mu.Unlock()
		return nil
	}
	s.stopping = true
	process := cmd.Process
	s.mu.Unlock()

	// Interrupt rather than Kill: pr3287 closes the telnet connection on the
	// way out, which is how the host learns the printer LU is free again
	// rather than working it out from a dead socket. Windows has no signal
	// to send, so the kill below is the only path there.
	if err := interrupt(process); err != nil {
		_ = process.Kill()
	}
	select {
	case <-done:
		return nil
	case <-time.After(stopGrace):
	}
	if err := process.Kill(); err != nil {
		return fmt.Errorf("the printer process would not stop: %w", err)
	}
	<-done
	return nil
}
