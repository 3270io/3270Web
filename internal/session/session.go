// SPDX-License-Identifier: AGPL-3.0-or-later

package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/jnnngs/3270Web/internal/host"
)

// Session represents a user session.
type Session struct {
	mu sync.Mutex
	ID string
	// OwnerID names the account this session belongs to. It is set once at
	// creation and never changes, so a session cannot be handed to another
	// user after the fact.
	//
	// Empty means unowned, which is what sessions created before ownership
	// existed look like. Unowned is deliberately not "owned by everyone":
	// authz.Principal.Owns rejects it, so the fail-open reading is
	// unavailable to callers.
	OwnerID                  string
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
	ScreenHistory            []ScreenHistoryEntry
}

// RecordScreen appends a screen to the session's history, ignoring a repeat of
// the screen already at the top. Every display path calls this and several can
// fire for the same screen (a submit's refresh, then the idle SSE poll), so
// deduplicating here is what keeps the history a record of screens the
// operator saw rather than of how often the client asked for them.
//
// Caller must NOT hold the session lock.
func (s *Session) RecordScreen(entry ScreenHistoryEntry) {
	if s == nil || strings.TrimSpace(entry.Text) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if n := len(s.ScreenHistory); n > 0 && s.ScreenHistory[n-1].Text == entry.Text {
		return
	}
	s.ScreenHistory = append(s.ScreenHistory, entry)
	if over := len(s.ScreenHistory) - ScreenHistoryLimit; over > 0 {
		// Reslice into a fresh backing array rather than just advancing the
		// slice header: keeping the old array alive would pin every evicted
		// screen's string for as long as the session lives.
		trimmed := make([]ScreenHistoryEntry, ScreenHistoryLimit)
		copy(trimmed, s.ScreenHistory[over:])
		s.ScreenHistory = trimmed
	}
}

// ScreenHistorySnapshot returns a copy of the session's screen history,
// oldest first.
func (s *Session) ScreenHistorySnapshot() []ScreenHistoryEntry {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.ScreenHistory) == 0 {
		return nil
	}
	out := make([]ScreenHistoryEntry, len(s.ScreenHistory))
	copy(out, s.ScreenHistory)
	return out
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

	// The rest are the decision steps: SetVariable, If/Else/EndIf,
	// While/EndWhile and Stop. They are fields on the same struct rather
	// than a step type of their own because everything that reads a
	// recording — playback, the chaos engine, the tools an assistant is
	// given — reads this one flat list of steps, and a nested shape would
	// mean teaching all of them a second one. A recording that makes no
	// decisions carries none of these fields and serialises exactly as it
	// did before.

	// Variable names the variable a SetVariable step writes, or the one an
	// If or While step reads for the left-hand side of its comparison.
	Variable string `json:"Variable,omitempty"`
	// Operator is the comparison an If or While step makes. See
	// workflow_control.go for the set; an operator outside it is refused
	// when the recording is loaded rather than at the moment it runs.
	Operator string `json:"Operator,omitempty"`
	// IgnoreCase folds case for that comparison. Off by default, because a
	// host that answers in upper case answers in upper case every time and
	// a comparison that quietly matches more than it was asked to is worse
	// than one that says it found nothing.
	IgnoreCase bool `json:"IgnoreCase,omitempty"`
	// MaxIterations bounds a While loop. Zero means the default bound: a
	// loop against a host that never changes its screen is otherwise a
	// session wedged until somebody notices.
	MaxIterations int `json:"MaxIterations,omitempty"`
	// Sensitive marks a SetVariable step whose value must not be written
	// down. The run still uses it — ${name} fills the field it was read for
	// — but the run log and the status endpoint say it was set rather than
	// what it was set to. A variable read from a password field would
	// otherwise put that password in a log somebody is meant to be able to
	// read over a shoulder.
	Sensitive bool `json:"Sensitive,omitempty"`
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
	// Screens records what the host had painted immediately before each
	// submit, captured before the operator's own input is written back into
	// the buffer. A recording's steps say which keys were pressed and which
	// fields were filled, but not what the screen looked like — and without
	// that, a business task built from a recording has nothing to guard its
	// steps with. Kept beside Steps rather than inside them so the exported
	// workflow JSON keeps its existing shape.
	Screens []RecordedScreen
}

// RecordedScreen is one screen captured during recording, tied to the group
// of steps that were performed on it.
type RecordedScreen struct {
	// StepIndex is len(Steps) at capture time, so it marks where this
	// screen's step group BEGINS — the first FillString if any fields were
	// filled, otherwise the key step itself. It is not the index of the key:
	// the fills for this screen are appended between capture and the key.
	StepIndex int    `json:"stepIndex"`
	Text      string `json:"text"`
	Rows      int    `json:"rows"`
	Cols      int    `json:"cols"`
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
	// Variables holds what the recording has read or been told so far, so a
	// run that made a decision can be asked what it decided on. Kept on the
	// playback rather than on the session because it belongs to one run:
	// the next run starts knowing nothing, which is what makes a replay a
	// replay.
	Variables map[string]string
}

type WorkflowEvent struct {
	Time    time.Time
	Message string
}

// ScreenHistoryLimit is how many past screens a session keeps. A screen is
// stored as plain text (~2KB at 24x80, ~7KB at 43x132), so 50 costs at most a
// few hundred KB per session — cheap against being unable to answer "what did
// the previous screen say?", which is otherwise unanswerable once the host has
// repainted.
const ScreenHistoryLimit = 50

// ScreenHistoryEntry is one past screen, kept as text rather than rendered
// HTML. History is read-only by definition — you cannot type into last
// Tuesday's screen — so storing the interactive form would cost an order of
// magnitude more memory for markup that must be inert anyway.
type ScreenHistoryEntry struct {
	Text   string    `json:"text"`
	Rows   int       `json:"rows"`
	Cols   int       `json:"cols"`
	Cursor string    `json:"cursor,omitempty"`
	At     time.Time `json:"at"`
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
// CreateSession starts an unowned session.
//
// Production code should call CreateSessionFor so the session is attributable
// to a principal; this remains for callers that have no principal to hand,
// which today means tests.
func (m *Manager) CreateSession(h host.Host) *Session {
	return m.CreateSessionFor("", h)
}

// CreateSessionFor starts a session owned by ownerID.
func (m *Manager) CreateSessionFor(ownerID string, h host.Host) *Session {
	id := generateID()
	s := &Session{
		ID:         id,
		OwnerID:    ownerID,
		Host:       h,
		LastAccess: time.Now(),
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[id] = s
	return s
}

// ListSessionsFor returns a snapshot of the sessions owned by ownerID.
//
// An empty ownerID matches nothing rather than everything: "show me the
// sessions belonging to nobody in particular" is never a question a caller
// legitimately asks, and answering it with the full set is how enumeration
// bugs happen.
func (m *Manager) ListSessionsFor(ownerID string) []*Session {
	if ownerID == "" {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		if s.OwnerID == ownerID {
			out = append(out, s)
		}
	}
	return out
}

// CountFor reports how many sessions ownerID currently holds.
func (m *Manager) CountFor(ownerID string) int {
	if ownerID == "" {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	n := 0
	for _, s := range m.sessions {
		if s.OwnerID == ownerID {
			n++
		}
	}
	return n
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

// PeekSession retrieves a session by ID without updating LastAccess. Use
// this (rather than GetSession) whenever the lookup itself must not count
// as activity — e.g. idle-session reaping, where using GetSession would
// make every session look freshly active and never actually reap.
func (m *Manager) PeekSession(id string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	return s, ok
}

// IdleSessionIDs returns the IDs of sessions whose LastAccess is at least
// maxIdle in the past, relative to now.
func (m *Manager) IdleSessionIDs(maxIdle time.Duration) []string {
	cutoff := time.Now().Add(-maxIdle)
	m.mu.RLock()
	defer m.mu.RUnlock()
	var ids []string
	for id, s := range m.sessions {
		s.mu.Lock()
		last := s.LastAccess
		s.mu.Unlock()
		if last.Before(cutoff) {
			ids = append(ids, id)
		}
	}
	return ids
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

// Count reports how many sessions exist, owned or not. Used for the
// instance-wide cap on concurrent s3270 subprocesses.
func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}
