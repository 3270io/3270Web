package task

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// StoreFileName is the catalogue file inside the store's base directory.
const StoreFileName = "tasks.json"

// MaxTasks bounds the catalogue. A Tasks menu is only useful while someone
// can find the task they want in it.
const MaxTasks = 200

// Store is the server-side task catalogue.
//
// Server-side rather than per-browser, unlike the keyboard mapping: a task is
// something a team shares. The analyst who works out how to check a balance
// does it once, and everyone else picks it off a menu. A catalogue that lived
// in localStorage would make that impossible, and it is the whole point.
type Store struct {
	mu   sync.Mutex
	path string
	now  func() time.Time
}

// NewStore returns a catalogue backed by baseDir/tasks.json.
func NewStore(baseDir string) *Store {
	return &Store{path: filepath.Join(baseDir, StoreFileName)}
}

func (s *Store) timestamp() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now().UTC()
}

// List returns every task, name-sorted.
func (s *Store) List() ([]Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

func (s *Store) loadLocked() ([]Task, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []Task{}, nil
		}
		return nil, err
	}
	var tasks []Task
	if err := json.Unmarshal(data, &tasks); err != nil {
		return nil, fmt.Errorf("the task catalogue is not valid JSON: %w", err)
	}
	if tasks == nil {
		tasks = []Task{}
	}
	sortTasks(tasks)
	return tasks, nil
}

func (s *Store) saveLocked(tasks []Task) error {
	sortTasks(tasks)
	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil {
		return err
	}
	// Temp file then rename, so an interrupted write cannot leave the
	// deployment with a truncated catalogue.
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Upsert validates and stores a task, replacing any existing one with the
// same normalised name.
func (s *Store) Upsert(t Task) ([]Task, error) {
	if err := t.Validate(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tasks, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	t.UpdatedAt = s.timestamp()
	key := NormalizeName(t.Name)
	replaced := false
	for i := range tasks {
		if NormalizeName(tasks[i].Name) == key {
			tasks[i] = t
			replaced = true
			break
		}
	}
	if !replaced {
		if len(tasks) >= MaxTasks {
			return nil, fmt.Errorf("the task catalogue is full (max %d); delete a task first", MaxTasks)
		}
		tasks = append(tasks, t)
	}
	if err := s.saveLocked(tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

// Delete removes a task by name. Removing something that is not there is not
// an error: the caller wanted it gone, and it is.
func (s *Store) Delete(name string) ([]Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tasks, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	key := NormalizeName(name)
	out := tasks[:0]
	for _, existing := range tasks {
		if NormalizeName(existing.Name) == key {
			continue
		}
		out = append(out, existing)
	}
	if err := s.saveLocked(out); err != nil {
		return nil, err
	}
	return out, nil
}

// Find returns a task by name.
func (s *Store) Find(name string) (Task, bool) {
	tasks, err := s.List()
	if err != nil {
		return Task{}, false
	}
	key := NormalizeName(name)
	for _, t := range tasks {
		if NormalizeName(t.Name) == key {
			return t.Clone(), true
		}
	}
	return Task{}, false
}

func sortTasks(tasks []Task) {
	sort.Slice(tasks, func(i, j int) bool {
		return strings.ToLower(tasks[i].Name) < strings.ToLower(tasks[j].Name)
	})
}
