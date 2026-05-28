package multiagent

import (
	"context"
	"encoding/json"
	"fmt"
)

// SupervisorDecision is the typed output of a Scheduler implementation
// that reviews a finished AgentInstance Result before letting it commit
// to the Blackboard. The supervisor agent's OutputSchema produces this
// shape; SupervisorScheduler reads it from the report's Structured payload.
type SupervisorDecision struct {
	Action    SupervisorAction `json:"action"`
	Reason    string           `json:"reason,omitempty"`
	HandoffTo string           `json:"handoffTo,omitempty"`
}

type SupervisorAction string

const (
	SupervisorActionAccept   SupervisorAction = "accept"
	SupervisorActionRetry    SupervisorAction = "retry"
	SupervisorActionHandoff  SupervisorAction = "handoff"
	SupervisorActionEscalate SupervisorAction = "escalate"
	SupervisorActionAbort    SupervisorAction = "abort"
)

// SupervisorScheduler designates one AgentClass as the supervisor: it runs
// first, and its TypedReport carries a SupervisorDecision the Scheduler
// executes. Use it when routing is LLM-driven rather than a fixed
// discriminator (RouterScheduler) or fixed order (SequentialScheduler).
//
// Decision handling: Handoff dispatches the named Worker class; Accept,
// Abort, and Escalate are terminal; Retry re-dispatches the supervisor.
//
// Spec anchor: docs/product-spec/v0.8.0/05-multi-agent-layer.md
// §"Reference implementations".
type SupervisorScheduler struct {
	// Supervisor is the orchestrating class whose report drives dispatch.
	Supervisor AgentClass
	// Workers maps a SupervisorDecision.HandoffTo value to the class to
	// dispatch for it.
	Workers map[string]AgentClass
}

// Next implements Scheduler. Tick one dispatches the supervisor; later
// ticks read its decision and either dispatch a worker, retry the
// supervisor, or terminate.
func (s SupervisorScheduler) Next(_ context.Context, state TeamState) ([]Dispatch, error) {
	if state.hasActiveInstance() || state.hasFailedInstance() {
		return nil, nil
	}
	finished := state.finishedClasses()
	if !finished[s.Supervisor.Name] {
		return []Dispatch{buildDispatch(state.RunID, s.Supervisor, 0, nil)}, nil
	}

	report := state.reportForClass(s.Supervisor.Name)
	if report == nil {
		return nil, nil
	}
	decision, err := decodeSupervisorDecision(report.Structured)
	if err != nil {
		return nil, err
	}

	switch decision.Action {
	case SupervisorActionHandoff:
		worker, ok := s.Workers[decision.HandoffTo]
		if !ok {
			return nil, fmt.Errorf("supervisor: unknown handoff target %q", decision.HandoffTo)
		}
		if finished[worker.Name] {
			return nil, nil
		}
		return []Dispatch{buildDispatch(state.RunID, worker, 1, state.reportInput(s.Supervisor.Name))}, nil
	case SupervisorActionRetry:
		return []Dispatch{buildDispatch(state.RunID, s.Supervisor, len(state.Instances), nil)}, nil
	case SupervisorActionAccept, SupervisorActionAbort, SupervisorActionEscalate:
		return nil, nil
	default:
		return nil, fmt.Errorf("supervisor: unknown action %q", decision.Action)
	}
}

func decodeSupervisorDecision(structured map[string]any) (SupervisorDecision, error) {
	raw, err := json.Marshal(structured)
	if err != nil {
		return SupervisorDecision{}, err
	}
	var decision SupervisorDecision
	if err := json.Unmarshal(raw, &decision); err != nil {
		return SupervisorDecision{}, err
	}
	return decision, nil
}
