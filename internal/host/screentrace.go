package host

import (
	"fmt"
	"strings"
)

// The terminal can append every screen it draws to a file as it draws it.
// That is a different thing from asking for the screen: a poller sees the
// screens it happened to ask about, and a screen the host painted and
// replaced between two polls leaves no trace at all. A transient error
// message is exactly the screen that goes missing that way, and exactly the
// one an operator later needs to see.
//
// Turning it on is a privileged act — it writes a file, and that file holds
// everything that crossed the display, including whatever was typed into a
// field the host did not mark hidden. The wrapper's job is to make the
// *destination* impossible to choose from outside: the path comes from the
// server, and this layer only refuses anything that could not be a path it
// chose.

// ScreenTraceFormat is how the terminal writes each captured screen.
type ScreenTraceFormat string

const (
	// ScreenTraceText writes the screens as plain text.
	ScreenTraceText ScreenTraceFormat = "text"
	// ScreenTraceHTML writes them as HTML, preserving colour and highlighting.
	ScreenTraceHTML ScreenTraceFormat = "html"
)

// validScreenTracePath reports whether path can be sent to the terminal. The
// path travels as a double-quoted action argument, so the quoting rules
// handle spaces, backslashes and the comma and parenthesis that would
// otherwise be read as the end of an argument. What quoting cannot rescue is
// a control character: a newline ends the command on the control pipe before
// the quote is ever closed, so those are refused outright rather than escaped
// into something the terminal would accept as a filename.
func validScreenTracePath(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("screen trace path must not be empty")
	}
	if strings.ContainsAny(path, "\n\r") {
		return fmt.Errorf("screen trace path must not contain a line break")
	}
	return nil
}

// StartScreenTrace begins appending every screen the terminal draws to path,
// in the given format. path must be a location the server chose; see
// validScreenTracePath for what this layer will pass on.
func (h *S3270) StartScreenTrace(path string, format ScreenTraceFormat) error {
	if err := validScreenTracePath(path); err != nil {
		return err
	}
	quoted := `"` + escapeForS3270String(path) + `"`
	var cmd string
	switch format {
	case ScreenTraceHTML:
		cmd = fmt.Sprintf("ScreenTrace(On,Html,File,%s)", quoted)
	case ScreenTraceText, "":
		cmd = fmt.Sprintf("ScreenTrace(On,File,%s)", quoted)
	default:
		return fmt.Errorf("unsupported screen trace format %q (allowed: text, html)", format)
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	data, status, err := h.doCommandLocked(cmd)
	if err != nil {
		return err
	}
	if isS3270Error(status, data) {
		return fmt.Errorf("s3270 ScreenTrace error: %s", strings.TrimSpace(status+" "+strings.Join(dataLines(data), " ")))
	}
	return nil
}

// StopScreenTrace stops capturing and closes the trace file. Stopping a trace
// that was never started is not an error: the caller's intent — that nothing
// is being captured when this returns — is satisfied either way, and the
// alternative is that a cleanup path has to know whether it is cleaning up.
func (h *S3270) StopScreenTrace() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	data, status, err := h.doCommandLocked("ScreenTrace(Off)")
	if err != nil {
		return err
	}
	if isS3270Error(status, data) {
		joined := strings.ToLower(strings.Join(dataLines(data), " "))
		if strings.Contains(joined, "not") {
			return nil
		}
		return fmt.Errorf("s3270 ScreenTrace(Off) error: %s", strings.TrimSpace(status))
	}
	return nil
}
