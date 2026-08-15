package host

import (
	"fmt"
	"os"
	"strings"
)

// Host represents a connection to a 3270 host.
type Host interface {
	Start() error
	Stop() error
	IsConnected() bool
	UpdateScreen() error
	// GetScreenSnapshot returns a copy of the screen taken under the session's
	// own lock. There is no accessor for the live screen on purpose: it is
	// rebuilt in place on every read from the host, so anything reading it from
	// outside can see half of one screen and half of the next.
	GetScreenSnapshot() *Screen
	SendKey(key string) error
	MoveCursor(row, col int) error
	WriteStringAt(row, col int, text string) error
	SubmitScreen() error
	SubmitUnformatted(data string) error
	// SubmitOperatorInput applies the operator's input to the screen and writes
	// it, both under one hold of the session's lock. Anything that marks fields
	// and then submits them has to go through here — see the implementation on
	// S3270 for what happens when those are two separate holds.
	SubmitOperatorInput(edit func(*Screen) string) error
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
	// ReadBufferErr, if set, is returned from ReadRegion() and ReadFieldAt().
	ReadBufferErr error
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

// GetScreen hands back the double's own screen so a test can arrange it and
// then assert on it. The real thing has no such accessor, deliberately; a mock
// driven from one goroutine has nothing to race with.
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

// SubmitOperatorInput runs the edit against the mock's screen and then submits
// it the way the real thing decides: marked fields when the screen is
// formatted, the returned text when it is not.
func (m *MockHost) SubmitOperatorInput(edit func(*Screen) string) error {
	text := ""
	if edit != nil && m.Screen != nil {
		text = edit(m.Screen)
	}
	if m.Screen != nil && !m.Screen.IsFormatted {
		return m.SubmitUnformatted(text)
	}
	return m.SubmitScreen()
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

// ReadRegion serves a region out of the mock's own screen buffer.
//
// The EBCDIC encoding reports the display's code points, because the double has
// no EBCDIC translation to report anything else with. That makes it useful for
// the shape of the response — one code per cell, in reading order, masked where
// the region crosses a hidden field — and useless for the values, so a test
// asserting on a particular byte here is asserting about this function rather
// than about a terminal.
func (m *MockHost) ReadRegion(row, col, length int, enc RegionEncoding) (*RegionRead, error) {
	m.Commands = append(m.Commands, fmt.Sprintf("readregion:%d,%d,%d", row, col, length))
	if m.ReadBufferErr != nil {
		return nil, m.ReadBufferErr
	}
	if _, err := regionCommand(enc, row, col, length); err != nil {
		return nil, err
	}
	if m.Screen == nil || m.Screen.Width <= 0 {
		return nil, fmt.Errorf("the terminal reported an empty screen buffer")
	}
	out := &RegionRead{Row: row, Col: col, Length: length, Encoding: string(enc)}
	if out.Encoding == "" {
		out.Encoding = string(RegionASCII)
	}
	size := m.Screen.Width * m.Screen.Height
	var text strings.Builder
	for i := 0; i < length; i++ {
		pos := (row*m.Screen.Width + col + i) % size
		ch := m.Screen.CharAt(pos%m.Screen.Width, pos/m.Screen.Width)
		if ch == 0 {
			ch = ' '
		}
		if enc == RegionEBCDIC {
			out.Codes = append(out.Codes, fmt.Sprintf("%02x", ch))
			continue
		}
		text.WriteRune(ch)
	}
	out.Text = text.String()
	return out, nil
}

// ReadFieldAt reports the field containing a position, from the mock's own
// field list. The real thing asks the terminal for its buffer and finds the
// field in that; both answer the same question about the same screen, which is
// what a handler test needs.
func (m *MockHost) ReadFieldAt(row, col int) (*BufferField, error) {
	m.Commands = append(m.Commands, fmt.Sprintf("readfield:%d,%d", row, col))
	if m.ReadBufferErr != nil {
		return nil, m.ReadBufferErr
	}
	if m.Screen == nil || m.Screen.Width <= 0 {
		return nil, fmt.Errorf("the terminal reported an empty screen buffer")
	}
	for _, f := range m.Screen.Fields {
		if f == nil || !m.Screen.contains(f, col, row) {
			continue
		}
		return &BufferField{
			Row:         f.StartY,
			Col:         f.StartX,
			Length:      len([]rune(f.GetValue())),
			Attribute:   fmt.Sprintf("%02x", f.FieldCode),
			Protected:   f.IsProtected(),
			Numeric:     f.IsNumeric(),
			Hidden:      f.IsHidden(),
			Intensified: f.IsIntensified(),
			Modified:    f.FieldCode&AttrModified != 0,
			Formatted:   true,
			Text:        f.GetValue(),
		}, nil
	}
	// No field covers the position, which on a screen with no fields at all is
	// not a miss — it is what an unformatted screen is.
	return &BufferField{
		Length:    m.Screen.Width * m.Screen.Height,
		Formatted: false,
		Text:      m.Screen.Text(),
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
