package venat

import (
	"context"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core"
)

// Runner is the public façade over the internal runtime. All public contract
// values crossing this boundary use api package types, not internal/core types.
type Runner struct {
	rt   *core.Runtime
	mode api.RuntimeMode
}

// Mode reports whether the runner was created with development defaults or
// validated production dependencies.
func (r *Runner) Mode() api.RuntimeMode { return r.mode }

// ExecuteCommand dispatches a Command through the internal command bus and
// returns the typed result. For most use cases prefer the typed methods
// (QueueRun, StartRun, RequestApproval, AcquireTaskExecution, ...) which
// avoid the result-type assertion and provide better compile-time signatures.
// ExecuteCommand exists for tools (replay, migration, admin) that operate
// generically over Commands.
//
// Result types follow the api.<Command>Result naming convention where a
// command produces a structured value (StartRunCommand -> api.StartRunResult,
// RequestApprovalCommand -> api.RequestApprovalResult,
// AcquireTaskExecutionCommand -> api.AcquireTaskExecutionResult). Commands
// that produce a single domain value return that value directly.
//
// Deprecated: use the typed Runner methods so result shapes are checked at
// compile time. Removal is scheduled after those tools have typed
// equivalents (ADR-025).
func (r *Runner) ExecuteCommand(ctx context.Context, command api.Command) (any, error) {
	result, err := r.rt.ExecuteCommand(ctx, command)
	if err != nil {
		return nil, err
	}
	return commandResultFromCore(command, result), nil
}

func commandResultFromCore(command api.Command, result any) any {
	switch command.(type) {
	case api.StartRunCommand:
		if started, ok := result.(core.StartRunResult); ok {
			return api.StartRunResult{Run: started.Run, RootTask: started.Root, Created: started.Created}
		}
	case api.AcquireTaskExecutionCommand:
		if acquired, ok := result.(core.AcquireTaskExecutionResult); ok {
			return api.AcquireTaskExecutionResult{
				Lease:          acquired.Lease,
				Acquired:       acquired.Acquired,
				ResourceClaims: acquired.ResourceClaims,
			}
		}
	case api.RequestApprovalCommand:
		if requested, ok := result.(core.RequestApprovalResult); ok {
			return api.RequestApprovalResult{Approval: requested.Approval, Token: requested.Token}
		}
	}
	return result
}

func cloneAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
