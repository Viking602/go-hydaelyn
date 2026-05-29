package agent

import (
	"encoding/json"
	"time"

	"github.com/Viking602/go-hydaelyn/provider"
)

// Step is one iteration of the bounded agent loop. Engine.RunMessages
// emits one Step per model turn (including guardrail-retry turns) so
// multi-agent schedulers can branch on per-step trace data. Steps carry no
// wall-clock timestamps: a replayed loop must reproduce them byte-for-byte
// (ADR-007), so StartedAt/EndedAt are left zero by the loop.
type Step struct {
	Index        int             `json:"index"`
	ModelCall    *ModelCall      `json:"modelCall,omitempty"`
	ToolCalls    []ToolCallTrace `json:"toolCalls,omitempty"`
	Observations []Observation   `json:"observations,omitempty"`
	Decision     StepDecision    `json:"decision,omitempty"`
	BudgetUsed   BudgetUsage     `json:"budgetUsed,omitempty"`
	StartedAt    time.Time       `json:"startedAt,omitempty"`
	EndedAt      time.Time       `json:"endedAt,omitempty"`
}

type ModelCall struct {
	Model        string              `json:"model"`
	InputTokens  int                 `json:"inputTokens,omitempty"`
	OutputTokens int                 `json:"outputTokens,omitempty"`
	StopReason   provider.StopReason `json:"stopReason,omitempty"`
}

type ToolCallTrace struct {
	Name       string          `json:"name"`
	Arguments  json.RawMessage `json:"arguments,omitempty"`
	Output     json.RawMessage `json:"output,omitempty"`
	Error      string          `json:"error,omitempty"`
	DurationMS int64           `json:"durationMs,omitempty"`
}

type Observation struct {
	Kind    string `json:"kind"`
	Message string `json:"message,omitempty"`
}

// StepDecision describes what the loop chose to do at the step boundary.
type StepDecision string

const (
	StepDecisionContinue StepDecision = "continue"
	StepDecisionFinish   StepDecision = "finish"
	StepDecisionHandoff  StepDecision = "handoff"
	StepDecisionFail     StepDecision = "fail"
)

type BudgetUsage struct {
	Tokens    int64         `json:"tokens,omitempty"`
	ToolCalls int           `json:"toolCalls,omitempty"`
	WallClock time.Duration `json:"wallClock,omitempty"`
}

// StepPolicy lets a caller override the model's default next-step
// decision. v0.8.0 ships the contract; Phase 3 wires it into the loop.
type StepPolicy interface {
	Next(state LoopSnapshot) (StepDecision, error)
}

// LoopSnapshot is what a StepPolicy.Next sees at each step boundary.
type LoopSnapshot struct {
	Steps []Step `json:"steps,omitempty"`
}
