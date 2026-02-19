package chaos

import "time"

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

	// OutputFile is a path where the learned workflow JSON is persisted on stop
	// (empty = do not persist).
	OutputFile string `json:"outputFile"`
}

// DefaultConfig returns sensible defaults for a chaos exploration run.
func DefaultConfig() Config {
	return Config{
		MaxSteps:  100,
		TimeBudget: 5 * time.Minute,
		StepDelay:  500 * time.Millisecond,
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
