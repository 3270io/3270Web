package host

import (
	"strings"
	"testing"
	"time"
)

// A refused action ends its response with "error" instead of "ok". A reader
// that only knows "ok" is still reading when the next action goes down the
// pipe, so it answers that action's question with this one's output — and if
// nothing follows, it reads until the command budget runs out, which kills the
// subprocess and takes the session with it.
//
// The trigger is not exotic. Typing into a protected field is a refusal. So is
// a key the keyboard has locked out, a PF number that does not exist, a
// transfer the host will not start.
func TestRefusedActionIsReportedRatherThanWaitedOut(t *testing.T) {
	h, _ := startFakeS3270(t, "polite")
	defer h.Stop()

	done := make(chan error, 1)
	go func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		_, _, err := h.doCommandLocked("Refuse()")
		done <- err
	}()

	select {
	case err := <-done:
		actionErr, ok := AsActionError(err)
		if !ok {
			t.Fatalf("a refused action reported %v, want an ActionError", err)
		}
		if actionErr.Command != "Refuse()" {
			t.Errorf("refusal names command %q, want %q", actionErr.Command, "Refuse()")
		}
		if !strings.Contains(actionErr.Detail, "Operator error") {
			t.Errorf("refusal detail is %q, want the terminal's own reason", actionErr.Detail)
		}
		if actionErr.Status != fakeStatus {
			t.Errorf("refusal carries status %q, want %q", actionErr.Status, fakeStatus)
		}
	case <-time.After(5 * time.Second):
		// commandTimeout is fifteen seconds and its expiry kills the process, so
		// waiting that long here would be waiting for the bug.
		t.Fatal("a refused action never returned; the reader is waiting for an \"ok\" that is not coming")
	}
}

// The response after a refusal has to be the response to the command that
// asked for it. Reading past the "error" swallows the next action's output,
// which is worse than an error: every answer after it belongs to the wrong
// question, and nothing reports that anything went wrong.
func TestRefusalDoesNotSwallowTheNextResponse(t *testing.T) {
	h, _ := startFakeS3270(t, "polite")
	defer h.Stop()

	h.mu.Lock()
	defer h.mu.Unlock()

	if _, _, err := h.doCommandLocked("Refuse()"); !IsActionError(err) {
		t.Fatalf("first action: got %v, want a refusal", err)
	}
	data, status, err := h.doCommandLocked("ReadBuffer(Ascii)")
	if err != nil {
		t.Fatalf("the action after a refusal failed: %v", err)
	}
	if status != fakeStatus {
		t.Errorf("status after a refusal is %q, want %q", status, fakeStatus)
	}
	if len(data) != 0 {
		t.Errorf("the action after a refusal came back with %d data lines, want none — it is reading the refusal's output", len(data))
	}
}

// The session survives a refusal. Nothing about an action the terminal will not
// carry out says the pipe is broken, and tearing the session down over one is
// how an operator loses a transaction to a mistyped keystroke.
func TestRefusalLeavesTheSessionUsable(t *testing.T) {
	h, _ := startFakeS3270(t, "polite")
	defer h.Stop()

	h.mu.Lock()
	_, _, err := h.doCommandLocked("Refuse()")
	h.mu.Unlock()
	if !IsActionError(err) {
		t.Fatalf("got %v, want a refusal", err)
	}
	if !h.IsConnected() {
		t.Fatal("a refused action left the session disconnected")
	}
}

func TestActionErrorMessage(t *testing.T) {
	tests := []struct {
		name string
		err  *ActionError
		want string
	}{
		{
			name: "reason from the terminal",
			err:  &ActionError{Command: `String("x")`, Status: "L F P C(h) I 2 24 80 0 0 0x0 -", Detail: "Keyboard locked\nOperator error"},
			want: `s3270 refused String("x"): Keyboard locked; Operator error`,
		},
		{
			name: "no reason falls back to the status",
			err:  &ActionError{Command: "Snap(Ascii)", Status: "L F P C(h) I 2 24 80 0 0 0x0 -"},
			want: "s3270 refused Snap(Ascii): L F P C(h) I 2 24 80 0 0 0x0 -",
		},
		{
			name: "nothing at all still names the action",
			err:  &ActionError{Command: "Quit()"},
			want: "s3270 refused Quit()",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRefusalMentions(t *testing.T) {
	refusal := &ActionError{Command: "ScreenTrace(Off)", Detail: "Screen tracing is already disabled."}
	if !refusalMentions(refusal, "not", "already") {
		t.Error("refusalMentions missed a word that is in the reason")
	}
	if refusalMentions(refusal, "certificate") {
		t.Error("refusalMentions matched a word that is not in the reason")
	}
	if refusalMentions(errTestPlain, "already") {
		t.Error("refusalMentions matched something that is not a refusal at all")
	}
}

var errTestPlain = &plainError{"broken pipe"}

type plainError struct{ msg string }

func (e *plainError) Error() string { return e.msg }

// A busy session is not a broken one. If this error were mistaken for a lost
// connection, the retry path would stop and restart the subprocess — abandoning
// the very file transfer the caller was waiting behind.
func TestSessionBusyIsNotAConnectionError(t *testing.T) {
	if isConnectionError(errSessionBusy) {
		t.Fatalf("%q reads as a lost connection; the retry path would kill the transfer holding the session", errSessionBusy)
	}
}

// When the subprocess has gone, the error the operator sees should be the
// reason it went — which the subprocess wrote to standard error on its way out
// — and not the pipe error that is merely how this end found out. "broken pipe"
// names the symptom; "Connection refused" names the cause.
func TestAWriteThatCannotLandReportsWhyTheSubprocessWent(t *testing.T) {
	h, _ := startFakeS3270(t, "polite")
	h.lastErrMu.Lock()
	h.lastErr = "mainframe.example: Connection refused"
	h.lastErrMu.Unlock()

	// Closing the pipe under it is what a subprocess exiting looks like here.
	h.mu.Lock()
	_ = h.stdin.Close()
	_, _, err := h.doCommandLocked("Query()")
	h.mu.Unlock()
	_ = h.Stop()

	if err == nil {
		t.Fatal("writing to a closed pipe reported success")
	}
	if !strings.Contains(err.Error(), "Connection refused") {
		t.Errorf("error is %q; it should carry what the subprocess said on its way out", err)
	}
	if !isConnectionError(err) {
		t.Errorf("error %q does not read as a lost connection, so nothing will reconnect", err)
	}
}
