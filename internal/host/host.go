package host

import (
	"fmt"
	"os"
)

// Host represents a connection to a 3270 host.
type Host interface {
	Start() error
	Stop() error
	IsConnected() bool
	UpdateScreen() error
	GetScreen() *Screen
	SendKey(key string) error
	MoveCursor(row, col int) error
	WriteStringAt(row, col int, text string) error
	SubmitScreen() error
	SubmitUnformatted(data string) error
	// PrintText returns the current screen rendered by s3270's PrintText
	// action. Supported formats are "html", "rtf", and "string"; the
	// implementation rejects anything else.
	PrintText(format string) (string, error)
	// Query sends an s3270 Query(arg) action and returns the joined response
	// lines with the "data: " prefix stripped. Returns an empty string and a
	// nil error if the host does not answer the query.
	Query(arg string) (string, error)
}

// MockHost is a mock implementation of Host for testing.
type MockHost struct {
	Screen    *Screen
	DumpFile  string
	Connected bool
	Commands  []string
	// QueryResponses lets tests stub Query() responses by argument. A missing
	// entry yields an empty string and a nil error, matching the
	// "host does not answer" contract.
	QueryResponses map[string]string
	// QueryErr, if set, is returned from Query() for every argument.
	QueryErr error
	// SnapErr, if set, is returned from Snap().
	SnapErr error
	// ToggleValues is the display-toggle state the mock reports and updates.
	ToggleValues map[string]bool
	// ToggleErr, if set, is returned from Toggles() and SetToggle().
	ToggleErr error
	// TracePath, TraceFormat and TraceRunning record what the last screen
	// trace was asked to do, so a test can assert on the destination the
	// server chose rather than only on the status code.
	TracePath    string
	TraceFormat  ScreenTraceFormat
	TraceRunning bool
	// ScreenTraceErr, if set, is returned from both screen-trace calls.
	ScreenTraceErr error
}

func NewMockHost(dumpFile string) (*MockHost, error) {
	m := &MockHost{
		DumpFile: dumpFile,
	}
	if dumpFile != "" {
		if err := m.loadDump(); err != nil {
			return nil, err
		}
	} else {
		m.Screen = &Screen{Width: 80, Height: 24, IsFormatted: true}
		m.Screen.Buffer = make([][]rune, m.Screen.Height)
		for i := range m.Screen.Buffer {
			m.Screen.Buffer[i] = make([]rune, m.Screen.Width)
		}
	}
	return m, nil
}

func (m *MockHost) loadDump() error {
	f, err := os.Open(m.DumpFile)
	if err != nil {
		return err
	}
	defer f.Close()
	m.Screen, err = NewScreenFromDump(f)
	return err
}

func (m *MockHost) Start() error {
	m.Connected = true
	return nil
}

func (m *MockHost) Stop() error {
	m.Connected = false
	return nil
}

func (m *MockHost) IsConnected() bool {
	return m.Connected
}

func (m *MockHost) UpdateScreen() error {
	// In a real mock, maybe rotate through dumps?
	// For now, just reload the same dump or do nothing.
	if m.DumpFile != "" {
		return m.loadDump()
	}
	return nil
}

func (m *MockHost) GetScreen() *Screen {
	return m.Screen
}

func (m *MockHost) GetScreenSnapshot() *Screen {
	if m.Screen == nil {
		return nil
	}
	return m.Screen.Clone()
}

func (m *MockHost) SendKey(key string) error {
	m.Commands = append(m.Commands, "key:"+key)
	return nil
}

func (m *MockHost) MoveCursor(row, col int) error {
	m.Commands = append(m.Commands, "movecursor")
	if m.Screen != nil {
		m.Screen.CursorY = row
		m.Screen.CursorX = col
	}
	return nil
}

func (m *MockHost) WriteStringAt(row, col int, text string) error {
	m.Commands = append(m.Commands, "write")
	if m.Screen == nil {
		m.Screen = &Screen{Width: 80, Height: 24, IsFormatted: true}
	}
	if m.Screen.Buffer == nil {
		m.Screen.Buffer = make([][]rune, m.Screen.Height)
		for i := range m.Screen.Buffer {
			m.Screen.Buffer[i] = make([]rune, m.Screen.Width)
		}
	}
	if row < 0 || col < 0 {
		return nil
	}
	if row >= m.Screen.Height {
		for i := m.Screen.Height; i <= row; i++ {
			m.Screen.Buffer = append(m.Screen.Buffer, make([]rune, m.Screen.Width))
		}
		m.Screen.Height = row + 1
	}
	if col+len([]rune(text)) > m.Screen.Width {
		newWidth := col + len([]rune(text))
		for y := 0; y < m.Screen.Height; y++ {
			rowBuf := make([]rune, newWidth)
			if y < len(m.Screen.Buffer) {
				copy(rowBuf, m.Screen.Buffer[y])
			}
			if y < len(m.Screen.Buffer) {
				m.Screen.Buffer[y] = rowBuf
			} else {
				m.Screen.Buffer = append(m.Screen.Buffer, rowBuf)
			}
		}
		m.Screen.Width = newWidth
	}
	for i, r := range []rune(text) {
		m.Screen.Buffer[row][col+i] = r
	}
	return nil
}

func (m *MockHost) SubmitScreen() error {
	m.Commands = append(m.Commands, "submit")
	// Reset changed flags
	for _, f := range m.Screen.Fields {
		if f.Changed {
			f.Changed = false
		}
	}
	return nil
}

func (m *MockHost) SubmitUnformatted(data string) error {
	m.Commands = append(m.Commands, "submit-unformatted")
	if m.Screen != nil {
		m.Screen.UpdateFromText(data)
	}
	return nil
}

func (m *MockHost) PrintText(format string) (string, error) {
	m.Commands = append(m.Commands, "printtext:"+format)
	if m.Screen == nil {
		return "", nil
	}
	return m.Screen.Text(), nil
}

func (m *MockHost) Query(arg string) (string, error) {
	m.Commands = append(m.Commands, "query:"+arg)
	if m.QueryErr != nil {
		return "", m.QueryErr
	}
	if m.QueryResponses == nil {
		return "", nil
	}
	return m.QueryResponses[arg], nil
}

// Snap freezes the mock's current screen. Unlike the real thing there is no
// separate buffer to freeze into — nothing is racing this screen — so it
// simply reports what is on it, which is what a caller comparing two
// snapshots is testing against anyway.
func (m *MockHost) Snap() (*Snapshot, error) {
	m.Commands = append(m.Commands, "snap")
	if m.SnapErr != nil {
		return nil, m.SnapErr
	}
	if m.Screen == nil {
		return &Snapshot{}, nil
	}
	return &Snapshot{
		Rows:   m.Screen.Height,
		Cols:   m.Screen.Width,
		Status: m.Screen.Status,
		Text:   m.Screen.Text(),
	}, nil
}

// Toggles reports the mock's display toggles. An unset ToggleValues map
// answers with every allowlisted toggle off, so a test that only cares that
// the list comes back does not have to populate one.
func (m *MockHost) Toggles() ([]Toggle, error) {
	m.Commands = append(m.Commands, "toggles")
	if m.ToggleErr != nil {
		return nil, m.ToggleErr
	}
	out := make([]Toggle, 0, len(safeToggles))
	for _, name := range SafeToggleNames() {
		out = append(out, Toggle{Name: name, Value: m.ToggleValues[name], Description: safeToggles[name]})
	}
	return out, nil
}

// SetToggle applies one display toggle, refusing names outside the allowlist
// exactly as the real wrapper does — that refusal is the security-relevant
// behaviour, so the double must not be more permissive than the thing.
func (m *MockHost) SetToggle(name string, value bool) (*Toggle, error) {
	m.Commands = append(m.Commands, "settoggle:"+name)
	if m.ToggleErr != nil {
		return nil, m.ToggleErr
	}
	canonical, ok := canonicalToggleName(name)
	if !ok {
		return nil, fmt.Errorf("%q is not a settable display toggle", name)
	}
	if m.ToggleValues == nil {
		m.ToggleValues = make(map[string]bool)
	}
	m.ToggleValues[canonical] = value
	return &Toggle{Name: canonical, Value: value, Description: safeToggles[canonical]}, nil
}

// StartScreenTrace records where a trace was asked to go. It writes the file
// too, empty, so that a test covering the download path has something to
// read where the server said it would be.
func (m *MockHost) StartScreenTrace(path string, format ScreenTraceFormat) error {
	m.Commands = append(m.Commands, "screentrace:on:"+path)
	if m.ScreenTraceErr != nil {
		return m.ScreenTraceErr
	}
	if err := validScreenTracePath(path); err != nil {
		return err
	}
	m.TracePath = path
	m.TraceFormat = format
	m.TraceRunning = true
	return os.WriteFile(path, nil, 0o600)
}

// StopScreenTrace ends a capture. Stopping one that was never started
// succeeds, matching the real wrapper.
func (m *MockHost) StopScreenTrace() error {
	m.Commands = append(m.Commands, "screentrace:off")
	if m.ScreenTraceErr != nil {
		return m.ScreenTraceErr
	}
	m.TraceRunning = false
	return nil
}
