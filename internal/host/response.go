// SPDX-License-Identifier: AGPL-3.0-or-later

package host

import (
	"errors"
	"fmt"
	"strings"
)

// Every action sent down the terminal's control pipe is answered the same way:
// zero or more "data: " lines, then the status line, then a single word saying
// how it went — "ok" if the action ran, "error" if it did not.
//
// Reading only for "ok" is the mistake this file exists to prevent. An action
// that fails ends its response with "error", so a reader waiting for "ok" is
// still waiting when the next action is sent, and answers *that* command's
// question with the previous one's output. If nothing follows, it waits until
// the command timeout expires — and that path kills the subprocess, so one
// refused action takes the whole session with it.
//
// None of that needs an exotic host to happen. Typing into a protected field,
// pressing a key the keyboard has locked out, naming a PF key that does not
// exist, a transfer the host rejects, a reconnect to a port with nothing behind
// it: all of them are ordinary, and all of them end in "error".

const (
	// responseOK terminates the response to an action that ran.
	responseOK = "ok"
	// responseError terminates the response to an action that did not.
	responseError = "error"
)

// ActionError is a refusal from the terminal: the action was understood, the
// pipe is healthy, and the answer is no. It is deliberately distinct from a
// transport failure — a broken pipe or a dead subprocess means the session is
// gone, while this means the last thing asked for did not happen and the
// session is fine.
type ActionError struct {
	// Command is the action as it was sent, with any redaction already applied.
	Command string
	// Status is the status line that came back with the refusal.
	Status string
	// Detail is what the terminal said about it, one reason per line, with the
	// "data: " prefix stripped. Empty when it said nothing.
	Detail string
}

func (e *ActionError) Error() string {
	reason := strings.TrimSpace(e.Detail)
	if reason == "" {
		reason = strings.TrimSpace(e.Status)
	}
	if reason == "" {
		return fmt.Sprintf("s3270 refused %s", e.Command)
	}
	return fmt.Sprintf("s3270 refused %s: %s", e.Command, strings.Join(strings.Split(reason, "\n"), "; "))
}

// AsActionError reports whether err is a refusal from the terminal, and returns
// it when it is. Callers use it to tell "this action did not happen" apart from
// "this session is over", which is the difference between reporting something
// to the operator and tearing the session down.
func AsActionError(err error) (*ActionError, bool) {
	var actionErr *ActionError
	if errors.As(err, &actionErr) {
		return actionErr, true
	}
	return nil, false
}

// IsActionError reports whether err is a refusal from the terminal.
func IsActionError(err error) bool {
	_, ok := AsActionError(err)
	return ok
}

// refusalMentions reports whether a refusal's reason contains any of words,
// case-insensitively. It is how the few callers that have a reason to carry on
// past a refusal — stopping a trace that was never started, say — recognise the
// one they mean without matching every refusal the terminal can produce.
func refusalMentions(err error, words ...string) bool {
	actionErr, ok := AsActionError(err)
	if !ok {
		return false
	}
	detail := strings.ToLower(actionErr.Detail)
	for _, word := range words {
		if strings.Contains(detail, strings.ToLower(word)) {
			return true
		}
	}
	return false
}
