package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
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
	Chaos                    *ChaosState
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
	ColorScheme    string
	FontName       string
	UseKeypad      bool
	VerboseLogging bool
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

// WorkflowParameter documents a business input that was resolved into one or
// more FillString steps of a generated workflow (which value went into which
// field). It is metadata only: playback ignores it.
type WorkflowParameter struct {
	Name        string `json:"Name"`
	Description string `json:"Description,omitempty"`
	Value       string `json:"Value,omitempty"`
	Row         int    `json:"Row,omitempty"`
	Column      int    `json:"Column,omitempty"`
	Length      int    `json:"Length,omitempty"`
	// Sensitive marks parameters whose value goes into a hidden or
	// AI-flagged-sensitive field; Value is omitted from the metadata for
	// these (the FillString step still carries it for playback).
	Sensitive bool `json:"Sensitive,omitempty"`
}

type WorkflowRecording struct {
	Active         bool
	Host           string
	Port           int
	OutputFilePath string
	Steps          []WorkflowStep
	FilePath       string
	StartedAt      time.Time
	LastStepAt     time.Time
	DelayMin       float64
	DelayMax       float64
	DelaySamples   int
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
	Name      string
	Payload   []byte
	Preview   string
	StepTotal int
	LoadedAt  time.Time
}

// ChaosState holds the persisted status snapshot for a chaos exploration
// run attached to a session. The actual engine lives in the application
// layer; this struct carries only the observable fields needed by API
// handlers.
type ChaosState struct {
	Active         bool
	StepsRun       int
	StartedAt      time.Time
	StoppedAt      time.Time
	MaxSteps       int
	TimeBudget     time.Duration
	Transitions    int
	UniqueScreens  int
	UniqueInputs   int
	AIDKeyCounts   map[string]int
	LoadedRunID    string
	LastAttempt    *ChaosAttempt
	RecentAttempts []ChaosAttempt
	MindMap        json.RawMessage
	Error          string
}

// ChaosFieldWrite captures one field write operation attempted in a chaos step.
type ChaosFieldWrite struct {
	Row     int
	Column  int
	Length  int
	Value   string
	Success bool
	Error   string
}

// ChaosAttempt captures granular details for one chaos exploration step.
type ChaosAttempt struct {
	Attempt        int
	Time           time.Time
	FromHash       string
	ToHash         string
	AIDKey         string
	FieldsTargeted int
	FieldsWritten  int
	Transitioned   bool
	Error          string
	FieldWrites    []ChaosFieldWrite
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

// ListSessions returns a snapshot of all active sessions. The slice is safe
// to range over; the underlying sessions still share state with the manager.
func (m *Manager) ListSessions() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s)
	}
	return out
}

// RemoveSession removes a session. Host.Stop performs I/O (sending Quit,
// killing the s3270 subprocess, closing pipes) and can take seconds when
// the subprocess is unresponsive, so it runs after the manager lock is
// released — otherwise a single slow disconnect would block every other
// session operation.
func (m *Manager) RemoveSession(id string) {
	m.mu.Lock()
	s, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	if ok {
		s.Host.Stop()
	}
}

func generateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fail closed rather than silently generating a weak predictable ID.
		log.Panicf("failed to generate session ID: %v", err)
	}
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
