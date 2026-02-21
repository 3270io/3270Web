package chaos

import (
	"math"
	"time"

	"github.com/jnnngs/3270Web/internal/session"
)

// WorkflowHeader captures workflow-level metadata that should be preserved
// alongside chaos-generated workflow steps.
type WorkflowHeader struct {
	EveryStepDelay  *session.WorkflowDelayRange `json:"everyStepDelay,omitempty"`
	OutputFilePath  string                      `json:"outputFilePath,omitempty"`
	RampUpBatchSize int                         `json:"rampUpBatchSize,omitempty"`
	RampUpDelay     float64                     `json:"rampUpDelay,omitempty"`
	EndOfTaskDelay  *session.WorkflowDelayRange `json:"endOfTaskDelay,omitempty"`
}

func (h *WorkflowHeader) clone() *WorkflowHeader {
	if h == nil {
		return nil
	}
	return &WorkflowHeader{
		EveryStepDelay:  cloneWorkflowDelayRange(h.EveryStepDelay),
		OutputFilePath:  h.OutputFilePath,
		RampUpBatchSize: h.RampUpBatchSize,
		RampUpDelay:     h.RampUpDelay,
		EndOfTaskDelay:  cloneWorkflowDelayRange(h.EndOfTaskDelay),
	}
}

func workflowHeaderFromConfig(cfg Config) *WorkflowHeader {
	header := &WorkflowHeader{
		RampUpBatchSize: 50,
		RampUpDelay:     1.5,
		EndOfTaskDelay:  &session.WorkflowDelayRange{Min: 60, Max: 120},
	}

	stepDelaySec := roundDurationSeconds(cfg.StepDelay)
	if stepDelaySec > 0 {
		header.EveryStepDelay = &session.WorkflowDelayRange{
			Min: stepDelaySec,
			Max: stepDelaySec,
		}
	}

	if cfg.OutputFile != "" {
		header.OutputFilePath = cfg.OutputFile
	}

	return header
}

func cloneWorkflowDelayRange(delay *session.WorkflowDelayRange) *session.WorkflowDelayRange {
	if delay == nil {
		return nil
	}
	copyDelay := *delay
	return &copyDelay
}

func roundDurationSeconds(d time.Duration) float64 {
	if d <= 0 {
		return 0
	}
	return math.Round(d.Seconds()*1000) / 1000
}
