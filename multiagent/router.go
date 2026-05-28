package multiagent

import (
	"context"
	"fmt"
)

// RouterScheduler runs a single Entry class, then reads a discriminator
// field from the Entry's TypedReport and dispatches the AgentClass keyed by
// that value. Use it for deterministic, data-driven branching (e.g. route
// a triage report by severity). For LLM-driven routing use
// SupervisorScheduler instead.
//
// Spec anchor: docs/product-spec/v0.8.0/05-multi-agent-layer.md
// §"Reference implementations".
type RouterScheduler struct {
	// Entry is the first class to run; its report drives the route.
	Entry AgentClass
	// DiscriminatorField is the top-level field name in the Entry report's
	// Structured payload whose value selects a route.
	DiscriminatorField string
	// Routes maps discriminator values to the AgentClass to dispatch.
	Routes map[string]AgentClass
	// Default is dispatched when no route matches. When nil, an unmatched
	// discriminator makes Next return an error.
	Default *AgentClass
}

// Next implements Scheduler. Tick one dispatches Entry; tick two reads the
// discriminator and dispatches the routed class; once the routed class has
// finished the Team is terminal.
func (s RouterScheduler) Next(_ context.Context, state TeamState) ([]Dispatch, error) {
	if state.hasActiveInstance() || state.hasFailedInstance() {
		return nil, nil
	}
	finished := state.finishedClasses()
	if !finished[s.Entry.Name] {
		return []Dispatch{buildDispatch(state.RunID, s.Entry, 0, nil)}, nil
	}

	report := state.reportForClass(s.Entry.Name)
	if report == nil {
		return nil, nil
	}
	value := discriminatorValue(report.Structured, s.DiscriminatorField)
	target, ok := s.Routes[value]
	if !ok {
		if s.Default == nil {
			return nil, fmt.Errorf("router: no route for %q=%q and no default", s.DiscriminatorField, value)
		}
		target = *s.Default
	}
	if finished[target.Name] {
		return nil, nil
	}
	return []Dispatch{buildDispatch(state.RunID, target, 1, state.reportInput(s.Entry.Name))}, nil
}
