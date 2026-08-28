package venat

import (
	"context"

	"github.com/Viking602/venat/api"
)

// RegisterAgent registers an agent profile. It reports api.ErrInvalidCommand
// when the profile carries no ID.
func (r *Runner) RegisterAgent(profile api.AgentProfile) error {
	return r.rt.RegisterAgent(profile)
}

func (r *Runner) Agents() []api.AgentProfile {
	return r.rt.Agents()
}

// RegisterTool registers a tool globally. It reports api.ErrInvalidCommand when
// the tool carries no name.
func (r *Runner) RegisterTool(tool api.Tool) error {
	return r.rt.RegisterTool(tool)
}

// RegisterToolForInvocation scopes governed tool metadata to one run, task,
// holder, and tool name. RegisterTool remains the legacy global registration
// API for direct non-agent callers. It reports api.ErrInvalidCommand when any
// part of the invocation identity or the tool name is missing.
func (r *Runner) RegisterToolForInvocation(runID, taskID string, holderType api.HolderType, holderID string, tool api.Tool) error {
	return r.rt.RegisterToolForInvocation(runID, taskID, api.HolderType(holderType), holderID, tool)
}

// RemoveToolsForInvocation releases all scoped tool metadata for one exact
// invocation identity.
func (r *Runner) RemoveToolsForInvocation(runID, taskID string, holderType api.HolderType, holderID string) {
	r.rt.RemoveToolsForInvocation(runID, taskID, api.HolderType(holderType), holderID)
}

// SetMessagePolicy installs the message-scoped policy checker. It composes with
// SetPolicyEngine rather than replacing it: a message must clear both, and the
// stricter decision wins. Passing nil clears only the message checker.
func (r *Runner) SetMessagePolicy(policy api.MessagePolicyChecker) {
	r.rt.SetMessagePolicy(policy)
}

// SetPolicyEngine installs the engine that authorizes every operation. Passing
// nil clears only the engine, leaving any message checker in force; a
// production runner ignores the nil because NewProduction requires an engine.
func (r *Runner) SetPolicyEngine(policy api.PolicyEngine) {
	if policy == nil && r.mode == api.RuntimeModeProduction {
		return
	}
	r.rt.SetPolicyEngine(policy)
}

func (r *Runner) SetOutputGateway(gateway api.OutputGateway) {
	r.rt.SetOutputGateway(gateway)
}

func (r *Runner) SetPipeline(components api.PipelineComponents) {
	r.rt.SetPipeline(components)
}

// StoreProvider returns the configured provider for low-level host
// administration and contract integration.
func (r *Runner) StoreProvider() api.StoreProvider {
	return r.rt.StoreProvider()
}

func (r *Runner) StoreCapabilities(ctx context.Context) (api.StoreCapabilities, error) {
	capabilities, err := r.rt.StoreCapabilities(ctx)
	if err != nil {
		return api.StoreCapabilities{}, err
	}
	return api.StoreCapabilities(capabilities), nil
}

func (r *Runner) Close(ctx context.Context) error {
	return r.rt.Close(ctx)
}

// Begin opens a host-owned UnitOfWork for low-level administration.
func (r *Runner) Begin(ctx context.Context) (api.UnitOfWork, error) {
	uow, err := r.rt.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return uow, nil
}

// SaveRun writes a run row through the configured provider. Prefer
// QueueRun / StartRun for application lifecycle (ADR-025).
func (r *Runner) SaveRun(ctx context.Context, run api.Run) error {
	return r.rt.SaveRun(ctx, run)
}

func (r *Runner) LoadRun(ctx context.Context, runID string) (api.Run, error) {
	run, err := r.rt.LoadRun(ctx, runID)
	if err != nil {
		return api.Run{}, err
	}
	return run, nil
}

func (r *Runner) AppendEvent(ctx context.Context, event api.Event) error {
	return r.rt.AppendEvent(ctx, event)
}

func (r *Runner) ListEvents(ctx context.Context, runID string) ([]api.Event, error) {
	events, err := r.rt.ListEvents(ctx, runID)
	if err != nil {
		return nil, err
	}
	return events, nil
}
