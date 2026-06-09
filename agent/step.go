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

// StepPolicy lets a caller override the loop's natural next-step decision at a
// continue boundary — the point after a non-terminal tool turn where the loop
// would otherwise iterate again. RunMessages consults it there (when set) and
// honors the returned decision:
//
//   - StepDecisionContinue (or "") keeps the loop's natural behavior.
//   - StepDecisionFinish / StepDecisionHandoff stop the loop cleanly, recording
//     the override on the final Step; the run completes with StopReasonComplete.
//   - StepDecisionFail stops the loop with a typed failure (ErrStepAborted,
//     classified FailureKindStepAborted).
//
// It is the substrate for goal-style control — iterate until a predicate over
// the step trace holds, then stop. The policy cannot resurrect a loop that
// already finished naturally (a final answer with no tool calls, or a terminal
// tool): those boundaries are not continue boundaries, so the policy is not
// consulted there.
type StepPolicy interface {
	Next(state LoopSnapshot) (StepDecision, error)
}

// LoopSnapshot is what a StepPolicy.Next sees at each step boundary.
type LoopSnapshot struct {
	Steps []Step `json:"steps,omitempty"`
}
