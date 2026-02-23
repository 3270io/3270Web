package chaos

import "time"

// Hint describes optional user-provided guidance for chaos exploration.
// Transaction is typically a known transaction code, and KnownData contains
// known working values that can be reused as field inputs. KeyAssignments maps
// screen-visible action labels (e.g. "Exit", "Page Forward") to keyboard keys
// (e.g. "PF(3)", "PF(8)", "Enter") that should be preferred when those labels
// appear on the current screen.
type Hint struct {
	Transaction    string            `json:"transaction"`
	KnownData      []string          `json:"knownData,omitempty"`
	KeyAssignments map[string]string `json:"keyAssignments,omitempty"`
}

// ScreenHint describes hints scoped to a specific discovered screen hash.
// KnownData values bias field inputs on that screen, and KnownKeys bias the
// key selection step toward actions known to work for that screen.
type ScreenHint struct {
	KnownData      []string          `json:"knownData,omitempty"`
	KnownKeys      []string          `json:"knownKeys,omitempty"`
	KeyAssignments map[string]string `json:"keyAssignments,omitempty"`
}

// Config holds the configuration for a chaos exploration run.
type Config struct {
	// MaxSteps is the maximum number of AID-key submissions before stopping (0 = unlimited).
	MaxSteps int `json:"maxSteps"`

	// TimeBudget is the maximum wall-clock duration before stopping (0 = unlimited).
	TimeBudget time.Duration `json:"timeBudget"`

	// Seed is the random seed (0 = derive from time.Now()).
	Seed int64 `json:"seed"`

	// StepDelay is the pause between submissions.
	StepDelay time.Duration `json:"stepDelay"`

	// AIDKeyWeights maps AID key names (e.g. "Enter", "PF(1)") to relative integer
	// weights. A key is chosen proportionally to its weight.
	AIDKeyWeights map[string]int `json:"aidKeyWeights"`

	// KeyBlacklist lists keys that chaos must never press (for example, PF(3)
	// on systems where PF3 logs the user off).
	KeyBlacklist []string `json:"keyBlacklist,omitempty"`

	// OutputFile is a path where the learned workflow JSON is persisted on stop
	// (empty = do not persist).
	OutputFile string `json:"outputFile"`

	// MaxFieldLength is the maximum number of characters generated per unprotected
	// field. Defaults to 40.
	MaxFieldLength int `json:"maxFieldLength"`

	// ForceOverrideExistingInputs makes chaos overwrite prefilled field values
	// more aggressively (e.g. clearing trailing characters and avoiding writing
	// the same visible value again).
	ForceOverrideExistingInputs bool `json:"forceOverrideExistingInputs"`

	// Hints are optional user-provided values used to bias generated inputs.
	Hints []Hint `json:"hints,omitempty"`

	// ScreenHints are optional user-provided hints keyed by discovered screen
	// hash and can be updated live while chaos runs.
	ScreenHints map[string]ScreenHint `json:"screenHints,omitempty"`

	// ExcludeNoProgressEvents omits non-error attempts from attempt/event history
	// when no screen transition occurs.
	ExcludeNoProgressEvents bool `json:"excludeNoProgressEvents"`

	// ExportHost and ExportPort are optional metadata used when writing
	// workflow-compatible chaos output files.
	ExportHost string `json:"-"`
	ExportPort int    `json:"-"`
}

// DefaultConfig returns sensible defaults for a chaos exploration run.
func DefaultConfig() Config {
	return Config{
		MaxSteps:                    100,
		TimeBudget:                  5 * time.Minute,
		StepDelay:                   500 * time.Millisecond,
		MaxFieldLength:              40,
		ForceOverrideExistingInputs: true,
		ExcludeNoProgressEvents:     true,
		AIDKeyWeights: map[string]int{
			"Enter":  70,
			"PF(1)":  5,
			"PF(2)":  5,
			"PF(3)":  5,
			"PF(4)":  5,
			"PF(12)": 5,
			"Clear":  5,
		},
	}
}
