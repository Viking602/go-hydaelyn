package multiagent

import "context"

// SequentialScheduler advances through a fixed list of AgentClasses,
// dispatching the next one once the previous instance has finished. Each
// step receives the previous step's TypedReport as its Input, threading a
// pipeline of agents. A failed instance terminates the Team (the snapshot
// carries no retryable signal); all classes finished is the success
// terminal (Next returns no dispatches).
//
// Spec anchor: docs/product-spec/v0.8.0/05-multi-agent-layer.md
// §"Reference implementations".
type SequentialScheduler struct {
	// Classes is the execution order. Construct with the AgentClass
	// definitions (not just names) so the Scheduler can build dispatches
	// with each class's schemas without consulting the Team.
	Classes []AgentClass
}

// Next implements Scheduler. It returns at most one Dispatch per tick: the
// next class whose instance has not finished, but only once no instance is
// active and none has failed.
func (s SequentialScheduler) Next(_ context.Context, state TeamState) ([]Dispatch, error) {
	if len(s.Classes) == 0 || state.hasActiveInstance() || state.hasFailedInstance() {
		return nil, nil
	}
	finished := state.finishedClasses()
	for index, class := range s.Classes {
		if finished[class.Name] {
			continue
		}
		var input []byte
		if index > 0 {
			var err error
			input, err = state.reportInput(s.Classes[index-1].Name)
			if err != nil {
				return nil, err
			}
		}
		return []Dispatch{state.buildDispatch(class, input)}, nil
	}
	return nil, nil
}
