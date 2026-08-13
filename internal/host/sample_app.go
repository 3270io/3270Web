package host

import (
	"fmt"

	"github.com/jnnngs/3270Web/internal/sampleapps"
)

// GoSampleAppHost runs a Go-based sample application and connects using s3270.
type GoSampleAppHost struct {
	AppID    string
	Port     int
	ExecPath string
	Args     []string
	Target   string

	server         *sampleapps.Server
	client         *S3270
	verboseLogging bool
}

const sampleAppClientNotStarted = "sample app client not started"

func NewGoSampleAppHost(appID string, port int, execPath string, args []string, target string) (*GoSampleAppHost, error) {
	if appID == "" {
		return nil, fmt.Errorf("missing sample app id")
	}
	if port <= 0 {
		return nil, fmt.Errorf("invalid sample app port %d", port)
	}
	if execPath == "" {
		return nil, fmt.Errorf("missing s3270 executable path")
	}
	if target == "" {
		return nil, fmt.Errorf("missing sample app target host")
	}
	return &GoSampleAppHost{
		AppID:    appID,
		Port:     port,
		ExecPath: execPath,
		Args:     args,
		Target:   target,
	}, nil
}

// Start joins the listener for this sample app, opening one if this is the
// first session to ask for it, and connects an s3270 to it.
//
// The listener is shared rather than owned: see sample_app_pool.go. Every path
// out of here that has taken a claim on it gives that claim back, or a session
// that failed to start would keep a port open until the process ended.
func (h *GoSampleAppHost) Start() error {
	if h.server == nil {
		server, err := acquireSampleAppServer(h.AppID, h.Port)
		if err != nil {
			return err
		}
		h.server = server
	}
	h.client = NewS3270(h.ExecPath, h.Args...)
	h.client.TargetHost = h.Target
	h.client.SetVerboseLogging(h.verboseLogging)
	if err := h.client.Start(); err != nil {
		h.client = nil
		h.releaseServer()
		return err
	}
	if err := h.client.UpdateScreen(); err != nil {
		_ = h.client.Stop()
		h.client = nil
		h.releaseServer()
		return err
	}
	return nil
}

func (h *GoSampleAppHost) Stop() error {
	if h.client != nil {
		_ = h.client.Stop()
		h.client = nil
	}
	h.releaseServer()
	return nil
}

// releaseServer gives up this session's claim on the shared listener, once.
//
// Clearing the field before releasing is what makes a second Stop — or a Stop
// after a failed Start — harmless rather than a decrement somebody else's
// session pays for.
func (h *GoSampleAppHost) releaseServer() {
	server := h.server
	if server == nil {
		return
	}
	h.server = nil
	releaseSampleAppServer(h.Port, server)
}

func (h *GoSampleAppHost) IsConnected() bool {
	if h.client == nil {
		return false
	}
	return h.client.IsConnected()
}

func (h *GoSampleAppHost) UpdateScreen() error {
	if h.client == nil {
		return fmt.Errorf(sampleAppClientNotStarted)
	}
	return h.client.UpdateScreen()
}

func (h *GoSampleAppHost) SendKey(key string) error {
	if h.client == nil {
		return fmt.Errorf(sampleAppClientNotStarted)
	}
	return h.client.SendKey(key)
}

func (h *GoSampleAppHost) MoveCursor(row, col int) error {
	if h.client == nil {
		return fmt.Errorf(sampleAppClientNotStarted)
	}
	return h.client.MoveCursor(row, col)
}

func (h *GoSampleAppHost) WriteStringAt(row, col int, text string) error {
	if h.client == nil {
		return fmt.Errorf(sampleAppClientNotStarted)
	}
	return h.client.WriteStringAt(row, col, text)
}

func (h *GoSampleAppHost) SubmitScreen() error {
	if h.client == nil {
		return fmt.Errorf(sampleAppClientNotStarted)
	}
	return h.client.SubmitScreen()
}

func (h *GoSampleAppHost) SubmitUnformatted(data string) error {
	if h.client == nil {
		return fmt.Errorf(sampleAppClientNotStarted)
	}
	return h.client.SubmitUnformatted(data)
}

func (h *GoSampleAppHost) SubmitOperatorInput(edit func(*Screen) string) error {
	if h.client == nil {
		return fmt.Errorf(sampleAppClientNotStarted)
	}
	return h.client.SubmitOperatorInput(edit)
}

func (h *GoSampleAppHost) PrintText(format string) (string, error) {
	if h.client == nil {
		return "", fmt.Errorf(sampleAppClientNotStarted)
	}
	return h.client.PrintText(format)
}

// Query satisfies the Host interface by asking the same s3270 every other
// method here goes through.
//
// It used to return an empty string unconditionally, on the stated grounds that
// "the in-process sample app does not implement s3270 Query actions". That was
// wrong about its own plumbing: a sample-app session is a real s3270 subprocess
// connected to a local go3270 server, so the queries are answerable and were
// simply being thrown away. The visible cost was that the compatibility
// profiler reported every capability as unknown when pointed at a sample app —
// the one target anyone can run — and the Query API surface answered nothing.
func (h *GoSampleAppHost) Query(arg string) (string, error) {
	if h.client == nil {
		return "", fmt.Errorf(sampleAppClientNotStarted)
	}
	return h.client.Query(arg)
}

// Snap, Toggles, SetToggle and the screen-trace pair are delegated for the
// same reason Query is: a sample-app session is a real s3270 subprocess
// talking to a local go3270 server, so every one of these is answerable here.
// Leaving them off would make the one target anybody can run without a
// mainframe the one target where these features do not work — which is
// exactly the target somebody evaluating them will reach for first.

// Snap freezes the display and reads it back.
func (h *GoSampleAppHost) Snap() (*Snapshot, error) {
	if h.client == nil {
		return nil, fmt.Errorf(sampleAppClientNotStarted)
	}
	return h.client.Snap()
}

// Toggles reports the terminal's display toggles.
func (h *GoSampleAppHost) Toggles() ([]Toggle, error) {
	if h.client == nil {
		return nil, fmt.Errorf(sampleAppClientNotStarted)
	}
	return h.client.Toggles()
}

// SetToggle changes one allowlisted display toggle.
func (h *GoSampleAppHost) SetToggle(name string, value bool) (*Toggle, error) {
	if h.client == nil {
		return nil, fmt.Errorf(sampleAppClientNotStarted)
	}
	return h.client.SetToggle(name, value)
}

// StartScreenTrace begins capturing every screen the terminal draws.
func (h *GoSampleAppHost) StartScreenTrace(path string, format ScreenTraceFormat) error {
	if h.client == nil {
		return fmt.Errorf(sampleAppClientNotStarted)
	}
	return h.client.StartScreenTrace(path, format)
}

// StopScreenTrace ends a capture.
func (h *GoSampleAppHost) StopScreenTrace() error {
	if h.client == nil {
		return fmt.Errorf(sampleAppClientNotStarted)
	}
	return h.client.StopScreenTrace()
}

// GetScreenSnapshot returns a deep copy of the current screen, so a caller
// reading it is not racing the parser that fills it in.
func (h *GoSampleAppHost) GetScreenSnapshot() *Screen {
	if h.client == nil {
		return nil
	}
	return h.client.GetScreenSnapshot()
}

// SetVerboseLogging enables or disables verbose logging for the underlying client.
func (h *GoSampleAppHost) SetVerboseLogging(enabled bool) {
	h.verboseLogging = enabled
	if h.client != nil {
		h.client.SetVerboseLogging(enabled)
	}
}

// GetVerboseLogging returns the current verbose logging setting.
func (h *GoSampleAppHost) GetVerboseLogging() bool {
	if h.client != nil {
		return h.client.GetVerboseLogging()
	}
	return h.verboseLogging
}
