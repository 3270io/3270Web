package session

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/jnnngs/h3270/internal/host"
)

// Session represents a user session.
type Session struct {
	ID         string
	Host       host.Host
	LastAccess time.Time
	Prefs      Preferences
}

type Preferences struct {
	ColorScheme string
	FontName    string
	UseKeypad   bool
}

// Manager manages sessions.
type Manager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
}

// NewManager creates a new session manager.
func NewManager() *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
	}
}

// GetSession retrieves a session by ID.
func (m *Manager) GetSession(id string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	if ok {
		s.LastAccess = time.Now()
	}
	return s, ok
}

// CreateSession creates a new session with the given host.
func (m *Manager) CreateSession(h host.Host) *Session {
	id := generateID()
	s := &Session{
		ID:         id,
		Host:       h,
		LastAccess: time.Now(),
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[id] = s
	return s
}

// RemoveSession removes a session.
func (m *Manager) RemoveSession(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[id]; ok {
		s.Host.Stop()
		delete(m.sessions, id)
	}
}

func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
