package core

import "github.com/Viking602/go-hydaelyn/internal/core/model"

// RegisterFlow stores a Flow definition by name. Flows compose preset
// adapters; they cannot bypass runtime invariants — Runner always enforces
// TaskStore, PolicyEngine, TaskExecutionLease, Handoff, ResponseLayer, and
// OutputGateway, so this function holds no rejection logic for v0.8.0+.
func (r *Runtime) RegisterFlow(flow model.Flow) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.flows[flow.Name] = flow
	return nil
}
