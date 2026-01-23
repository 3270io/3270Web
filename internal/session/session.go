package session

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/jnnngs/3270Web/internal/host"
)

// Session represents a user session.
type Session struct {
	mu                       sync.Mutex
	ID                       string
	Host                     host.Host
	LastAccess               time.Time
	Prefs                    Preferences
	TargetHost               string
	TargetPort               int
	Recording                *WorkflowRecording
	Playback                 *WorkflowPlayback
	LoadedWorkflow           *LoadedWorkflow
	PlaybackCompletedAt      time.Time
	PlaybackEvents           []WorkflowEvent
	LastPlaybackStep         int
	LastPlaybackStepType     string
	LastPlaybackStepTotal    int
	LastPlaybackDelayRange   string
	LastPlaybackDelayApplied string
}

type Preferences struct {
	ColorScheme string
	FontName    string
	UseKeypad   bool
}

type WorkflowCoordinates struct {
	Row    int `json:"Row"`
	Column int `json:"Column"`
	Length int `json:"Length,omitempty"`
}

type WorkflowDelayRange struct {
	Min float64 `json:"Min,omitempty"`
	Max float64 `json:"Max,omitempty"`
}

type WorkflowStep struct {
	Type        string               `json:"Type"`
	Coordinates *WorkflowCoordinates `json:"Coordinates,omitempty"`
	Text        string               `json:"Text,omitempty"`
	StepDelay   *WorkflowDelayRange  `json:"StepDelay,omitempty"`
}

type WorkflowRecording struct {
	Active         bool
	Host           string
	Port           int
	OutputFilePath string
	Steps          []WorkflowStep
	FilePath       string
	StartedAt      time.Time
}

type WorkflowPlayback struct {
	Active           bool
	PendingInput     bool
	Paused           bool
	StopRequested    bool
	StartedAt        time.Time
	Mode             string
	CurrentStep      int
	CurrentStepType  string
	TotalSteps       int
	StepRequested    bool
	CurrentDelayMin  float64
	CurrentDelayMax  float64
	CurrentDelayUsed time.Duration
}

type WorkflowEvent struct {
	Time    time.Time
	Message string
}

type LoadedWorkflow struct {
	Name     string
	Payload  []byte
	Preview  string
	LoadedAt time.Time
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
		s.mu.Lock()
		s.LastAccess = time.Now()
		s.mu.Unlock()
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

// Lock guards session state mutations.
func (s *Session) Lock() {
	if s == nil {
		return
	}
	s.mu.Lock()
}

// Unlock releases the session state lock.
func (s *Session) Unlock() {
	if s == nil {
		return
	}
	s.mu.Unlock()
}
