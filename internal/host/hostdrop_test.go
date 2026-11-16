// SPDX-License-Identifier: AGPL-3.0-or-later

package host

import (
	"errors"
	"testing"
	"time"
)

// What a terminal says when the host has hung up on it is not one error but
// several, depending on what was being asked: a write is refused with the
// status line saying "not connected", and the attempt to dial the host again
// is refused with a failed Connect. Callers need one question — "is the host
// gone?" — so that a dropped connection can be reported as that and not as an
// unexplained failure.

func TestIsDisconnectedFromTheErrorAlone(t *testing.T) {
	const connectedStatus = "U F U C(host.example) I 2 24 80 0 11 0x0 0.000"
	const droppedStatus = "L F U N N 2 24 80 0 11 0x0 0.000"
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nothing", nil, false},
		{"an ordinary error", errors.New("boom"), false},
		{"a write refused while not connected", &ActionError{Command: `String("A")`, Status: droppedStatus, Detail: "Canceled"}, true},
		{"a refusal while connected", &ActionError{Command: "Key(PF99)", Status: connectedStatus, Detail: "no such key"}, false},
		{"a failed attempt to dial the host again", &ActionError{Command: "Connect(host.example:23)", Status: connectedStatus, Detail: "Connection failed"}, true},
		{"the subprocess says so", errors.New("not connected"), true},
		{"the pipe is closed", errors.New("write |1: broken pipe"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsDisconnected(tt.err); got != tt.want {
				t.Errorf("IsDisconnected(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// Against a real terminal and a host that hangs up: every way a caller can
// meet the drop has to be recognised, because which one it meets depends on
// what it was doing when the connection went.
func TestIsDisconnectedAgainstAHostThatHangsUp(t *testing.T) {
	screen := newScreen(80).
		at(0, 0).field(faProtected).text("USER").
		at(0, 10).field(0).cursor().text("").
		at(23, 0).field(faProtected).text("PF3").
		bytes()
	host := startConformanceHost(t, screen)
	term := connectTerminal(t, host, "-model", "2")

	// An ordinary refusal on a healthy connection is not a disconnect.
	if err := term.SendKey("PF99"); err != nil && IsDisconnected(err) {
		t.Errorf("a refused key on a live connection was taken for a drop: %v", err)
	}

	host.close()
	time.Sleep(500 * time.Millisecond)

	typed := func(s *Screen) string {
		for _, f := range s.Fields {
			if !f.IsProtected() {
				f.Value = "ADMIN"
				f.Changed = true
			}
		}
		return ""
	}
	if err := term.SubmitOperatorInput(typed); !IsDisconnected(err) {
		t.Errorf("SubmitOperatorInput after the drop: err = %v, want it recognised as a disconnect", err)
	}
	if err := term.SendKey("Enter"); !IsDisconnected(err) {
		t.Errorf("SendKey after the drop: err = %v, want it recognised as a disconnect", err)
	}
	if err := term.UpdateScreen(); !IsDisconnected(err) {
		t.Errorf("UpdateScreen after the drop: err = %v, want it recognised as a disconnect", err)
	}
}
