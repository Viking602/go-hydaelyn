package agent

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/provider"
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

// StepRecorder persists a finalized agent-loop step. RunMessages calls the
// recorder only after the step's final decision is known.
type StepRecorder interface {
	RecordStep(context.Context, Step) error
}

// StepRecorderFunc adapts a function to the StepRecorder interface.
type StepRecorderFunc func(context.Context, Step) error

// RecordStep delegates to f.
func (f StepRecorderFunc) RecordStep(ctx context.Context, step Step) error {
	return f(ctx, step)
}

// TurnCheckpoint is the durable, provider-neutral state at one completed model
// turn boundary. Messages include assistant tool calls and their tool results,
// allowing a later execution to continue without asking the provider to repeat
// already completed work.
type TurnCheckpoint struct {
	Messages          []message.Message `json:"messages"`
	Usage             provider.Usage    `json:"usage,omitempty"`
	Step              Step              `json:"step"`
	ToolCallsUsed     int               `json:"toolCallsUsed,omitempty"`
	NextOperationTurn int               `json:"nextOperationTurn,omitempty"`
	PendingToolCalls  bool              `json:"pendingToolCalls,omitempty"`
}

// CheckpointRecorder persists a completed turn before the loop advances.
type CheckpointRecorder interface {
	RecordCheckpoint(context.Context, TurnCheckpoint) error
}

// CheckpointRecorderFunc adapts a function to CheckpointRecorder.
type CheckpointRecorderFunc func(context.Context, TurnCheckpoint) error

// RecordCheckpoint delegates to f.
func (f CheckpointRecorderFunc) RecordCheckpoint(ctx context.Context, checkpoint TurnCheckpoint) error {
	return f(ctx, checkpoint)
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
// consulted there. An output-guardrail retry turn loops again without
// consulting the policy either — the retry is the guardrail's own decision,
// bounded by its retry limit and MaxIterations — though its step still appears
// in the snapshot at the next tool-turn boundary.
type StepPolicy interface {
	Next(state LoopSnapshot) (StepDecision, error)
}

// LoopSnapshot is what a StepPolicy.Next sees at each step boundary.
type LoopSnapshot struct {
	Steps []Step `json:"steps,omitempty"`
}
