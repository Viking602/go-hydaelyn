package core

import "github.com/Viking602/go-hydaelyn/internal/core/model"

func (r *Runtime) RegisterFlow(flow model.Flow) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if flow.BypassTaskStore ||
		flow.BypassPolicyEngine ||
		flow.BypassTaskExecutionLease ||
		flow.BypassHandoff ||
		flow.BypassResponseLayer ||
		flow.BypassOutputGateway {
		return ErrFlowBypass
	}
	r.flows[flow.Name] = flow
	return nil
}
