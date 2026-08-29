package agent

import "time"

// Request is the application-neutral input for one Engine execution.
// A nil Budget uses Engine.LoopPolicy.Budget. A non-nil Budget replaces the
// engine default as a whole; each zero dimension is unbounded.
type Request struct {
	Prompt string  `json:"prompt,omitempty"`
	Budget *Budget `json:"budget,omitempty"`
}

// Budget bounds cumulative work performed by one Engine execution. Zero means
// unbounded for that dimension.
type Budget struct {
	MaxTokens    int64         `json:"maxTokens,omitempty"`
	MaxToolCalls int           `json:"maxToolCalls,omitempty"`
	MaxSteps     int           `json:"maxSteps,omitempty"`
	MaxWallClock time.Duration `json:"maxWallClock,omitempty"`
}

func cloneBudget(budget *Budget) *Budget {
	if budget == nil {
		return nil
	}
	cloned := *budget
	return &cloned
}
