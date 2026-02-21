package chaos

import (
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jnnngs/3270Web/internal/session"
)

// SavedRunMeta holds lightweight metadata for listing saved chaos runs.
type SavedRunMeta struct {
	ID            string    `json:"id"`
	StartedAt     time.Time `json:"startedAt"`
	StoppedAt     time.Time `json:"stoppedAt,omitempty"`
	StepsRun      int       `json:"stepsRun"`
	Transitions   int       `json:"transitions"`
	UniqueScreens int       `json:"uniqueScreens"`
	UniqueInputs  int       `json:"uniqueInputs"`
	Error         string    `json:"error,omitempty"`
}

// SavedRun is a complete snapshot of a finished (or in-progress) chaos run,
// compatible with the existing workflow export format.
type SavedRun struct {
	SavedRunMeta
	ScreenHashes      map[string]bool        `json:"screenHashes"`
	TransitionList    []Transition           `json:"transitionList"`
	Steps             []session.WorkflowStep `json:"steps"`
	AIDKeyCounts      map[string]int         `json:"aidKeyCounts"`
	UniqueInputValues map[string]bool        `json:"uniqueInputValues,omitempty"`
	Attempts          []Attempt              `json:"attempts,omitempty"`
}

// runFileName returns the file name for a given run ID.
func runFileName(runID string) string {
	return runID + ".json"
}

// SaveRun persists a SavedRun to dir/<runID>.json.
// The directory is created if it does not exist.
func SaveRun(dir string, run *SavedRun) error {
	if dir == "" {
		return fmt.Errorf("chaos runs directory not configured")
	}
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("create chaos runs dir: %w", err)
	}
	data, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal run: %w", err)
	}
	path := filepath.Join(dir, runFileName(run.ID))
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write run file: %w", err)
	}
	return nil
}

// ListRuns returns lightweight metadata for every saved run in dir, sorted
// newest-first by StartedAt.
func ListRuns(dir string) ([]SavedRunMeta, error) {
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read chaos runs dir: %w", err)
	}

	var metas []SavedRunMeta
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var run SavedRun
		if err := json.Unmarshal(data, &run); err != nil {
			continue
		}
		metas = append(metas, run.SavedRunMeta)
	}

	sort.Slice(metas, func(i, j int) bool {
		return metas[i].StartedAt.After(metas[j].StartedAt)
	})
	return metas, nil
}

// LoadRun reads and returns the full SavedRun for the given run ID from dir.
func LoadRun(dir, runID string) (*SavedRun, error) {
	if dir == "" {
		return nil, fmt.Errorf("chaos runs directory not configured")
	}
	path := filepath.Join(dir, runFileName(runID))
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("run %q not found", runID)
		}
		return nil, fmt.Errorf("read run file: %w", err)
	}
	var run SavedRun
	if err := json.Unmarshal(data, &run); err != nil {
		return nil, fmt.Errorf("parse run file: %w", err)
	}
	return &run, nil
}

// NewRunID generates a sortable, unique identifier for a chaos run using
// the current time and a short random suffix.
func NewRunID() string {
	return time.Now().UTC().Format("20060102-150405") + "-" + randomHex(4)
}

// randomHex returns n random bytes encoded as a lowercase hex string (2n chars).
func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = cryptorand.Read(b)
	return hex.EncodeToString(b)
}
