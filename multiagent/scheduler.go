package multiagent

import (
	"context"

	"github.com/Viking602/venat/api"
)

// Scheduler decides which AgentInstance executes which api.Task next. The
// built-in reference implementation is SequentialScheduler; applications can
// adapt a function with SchedulerFunc or implement the interface directly.
//
// Spec anchor: docs/product-spec/v0.8.0/05-multi-agent-layer.md.
type Scheduler interface {
	Next(ctx context.Context, state TeamState) ([]Dispatch, error)
}

// TeamState is the snapshot a Scheduler.Next sees on each tick.
type TeamState struct {
	RunID      string               `json:"runId"`
	Tick       int                  `json:"tick,omitempty"`
	Tasks      []api.Task           `json:"tasks,omitempty"`
	Instances  []AgentInstance      `json:"instances,omitempty"`
	Blackboard []api.BlackboardItem `json:"blackboard,omitempty"`
}

// SchedulerFunc adapts a plain function to the Scheduler interface — the
// scheduling counterpart of ExecutorFunc. The function must remain a pure
// function of TeamState (no captured mutable state, no side effects):
// ADR-016 relies on that purity to replay scheduler decisions from the
// event store alone.
type SchedulerFunc func(ctx context.Context, state TeamState) ([]Dispatch, error)

// Next implements Scheduler.
func (fn SchedulerFunc) Next(ctx context.Context, state TeamState) ([]Dispatch, error) {
	return fn(ctx, state)
}
