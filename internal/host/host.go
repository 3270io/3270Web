package host

import (
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
}

// MockHost is a mock implementation of Host for testing.
type MockHost struct {
	Screen    *Screen
	DumpFile  string
	Connected bool
	Commands  []string
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
