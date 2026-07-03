package chaos

import "time"

// FirstScreenHintKey is a reserved ScreenHints map key used to apply hints only
// to the very first screen processed when a chaos run starts or resumes.
// It is intentionally non-hash-like so it cannot collide with normal screen
// hashes (which are short lowercase hex values).
const FirstScreenHintKey = "__FIRST_SCREEN__"

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
	BlockedKeys    []string          `json:"blockedKeys,omitempty"`
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
	// weights. A key is chosen proportionally to its weight. DefaultConfig
	// deliberately excludes PF(3) and Clear: PF3 commonly logs the user off
	// and Clear wipes the screen on many 3270 applications, and
	// AutoBlockExitKeys only catches this when the current screen's help
	// legend literally names the key (e.g. "PF3 Exit") — a screen with no
	// legend, or an abbreviated/non-English one, leaves them fully live.
	// Add them back explicitly (here, or per-screen via a hint) once you've
	// confirmed they're safe on the target application.
	AIDKeyWeights map[string]int `json:"aidKeyWeights"`

	// KeyBlacklist lists keys that chaos must never press (for example, a
	// host-specific key that logs the user off). PF(3)/Clear are excluded
	// from DefaultConfig's AIDKeyWeights rather than pre-populated here —
	// this field is for explicit, user-driven overrides and starts empty.
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

	// LearnedInputReuseBias controls how strongly chaos prefers previously
	// observed working input values. Range 0..1 where 0 favors fresh exploration
	// and 1 preserves the default learned-input bias.
	LearnedInputReuseBias float64 `json:"learnedInputReuseBias,omitempty"`

	// LearnedKeyReuseBias controls how strongly chaos prefers previously
	// observed working keys (Enter/PF/etc.) via learned boosting. Range 0..1
	// where 0 favors fresh key exploration and 1 preserves the default
	// learned-key bias.
	LearnedKeyReuseBias float64 `json:"learnedKeyReuseBias,omitempty"`

	// Hints are optional user-provided values used to bias generated inputs.
	Hints []Hint `json:"hints,omitempty"`

	// ScreenHints are optional user-provided hints keyed by discovered screen
	// hash and can be updated live while chaos runs.
	ScreenHints map[string]ScreenHint `json:"screenHints,omitempty"`

	// ExcludeNoProgressEvents omits non-error attempts from attempt/event history
	// when no screen transition occurs.
	ExcludeNoProgressEvents bool `json:"excludeNoProgressEvents"`

	// ScreenDedupSimilarity is the threshold (0-1) used to merge highly similar
	// screens into one discovered area in the mind map, reducing false
	// "new screens" caused by echoed values. Higher values are stricter.
	ScreenDedupSimilarity float64 `json:"screenDedupSimilarity,omitempty"`

	// DedupMode controls how screen hashes are canonicalised for mind-map
	// area keying. "structural" (the default) treats screens with the same
	// layout signature as the same area regardless of echoed values;
	// "exact" disables structural merging and only the raw hash is used.
	DedupMode string `json:"dedupMode,omitempty"`

	// SaturationSteps stops chaos when this many consecutive steps yield no
	// new (fromHash, toHash, aidKey) tuple and no new field value caused a
	// transition. 0 disables the heuristic. Default 15.
	SaturationSteps int `json:"saturationSteps,omitempty"`

	// AutoBlockExitKeys auto-blocks PF keys whose on-screen labels match
	// Exit/Quit/Cancel/Logoff/Logout/End/Return patterns on first visit to
	// a new area. Default true.
	AutoBlockExitKeys bool `json:"autoBlockExitKeys,omitempty"`

	// AutoPreferNavigationKeys auto-boosts PF keys whose on-screen labels
	// match Help/Menu/Next/Prev/Forward/Back patterns. Default true.
	AutoPreferNavigationKeys bool `json:"autoPreferNavigationKeys,omitempty"`

	// TransitionLogPath, when set, makes the engine append one JSON object
	// per step to the given file (newline-delimited / JSONL). 0 disables.
	TransitionLogPath string `json:"transitionLogPath,omitempty"`

	// ExportHost and ExportPort are optional metadata used when writing
	// workflow-compatible chaos output files.
	ExportHost string `json:"-"`
	ExportPort int    `json:"-"`
}

// Dedup modes for Config.DedupMode.
const (
	DedupModeStructural = "structural"
	DedupModeExact      = "exact"
)

// DefaultConfig returns sensible defaults for a chaos exploration run.
func DefaultConfig() Config {
	return Config{
		MaxSteps:                    100,
		TimeBudget:                  5 * time.Minute,
		StepDelay:                   500 * time.Millisecond,
		MaxFieldLength:              40,
		ForceOverrideExistingInputs: true,
		LearnedInputReuseBias:       1.0,
		LearnedKeyReuseBias:         1.0,
		ExcludeNoProgressEvents:     true,
		ScreenDedupSimilarity:       0.95,
		DedupMode:                   DedupModeStructural,
		SaturationSteps:             15,
		AutoBlockExitKeys:           true,
		AutoPreferNavigationKeys:    true,
		AIDKeyWeights: map[string]int{
			"Enter":  70,
			"PF(1)":  5,
			"PF(2)":  5,
			"PF(4)":  5,
			"PF(12)": 5,
		},
	}
}
