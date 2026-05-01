package core

func (r *Runtime) RegisterFlow(flow Flow) error {
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
