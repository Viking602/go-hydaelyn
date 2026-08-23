package venat

import (
	"context"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core"
	"github.com/Viking602/venat/internal/core/adapter"
	"github.com/Viking602/venat/internal/core/model"
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
	coreCommand, ok := adapter.CommandToCore(command)
	if !ok {
		return nil, api.ErrInvalidCommand
	}
	result, err := r.rt.ExecuteCommand(ctx, coreCommand)
	if err != nil {
		return nil, adapter.ErrorToAPI(err)
	}
	return commandResultFromCore(command, result), nil
}

func commandResultFromCore(command api.Command, result any) any {
	if converted, ok := runTaskResultFromCore(command, result); ok {
		return converted
	}
	if converted, ok := mailboxResultFromCore(command, result); ok {
		return converted
	}
	if converted, ok := governanceResultFromCore(command, result); ok {
		return converted
	}
	return result
}

func runTaskResultFromCore(command api.Command, result any) (any, bool) {
	switch command.(type) {
	case api.StartRunCommand:
		started, ok := result.(core.StartRunResult)
		if !ok {
			return result, true
		}
		return api.StartRunResult{
			Run:      adapter.RunFromModel(started.Run),
			RootTask: adapter.TaskFromModel(started.Root),
			Created:  started.Created,
		}, true
	case api.CreateTaskCommand:
		if task, ok := result.(model.Task); ok {
			return adapter.TaskFromModel(task), true
		}
		return result, true
	case api.AdvanceRunCommand:
		if run, ok := result.(model.Run); ok {
			return adapter.RunFromModel(run), true
		}
		return result, true
	default:
		return nil, false
	}
}

func mailboxResultFromCore(command api.Command, result any) (any, bool) {
	switch command.(type) {
	case api.DispatchTaskCommand:
		if envelope, ok := result.(model.TaskEnvelope); ok {
			return adapter.TaskEnvelopeFromModel(envelope), true
		}
		return result, true
	case api.FanOutDispatchTaskCommand:
		if envelopes, ok := result.([]model.TaskEnvelope); ok {
			return adapter.TaskEnvelopesFromModel(envelopes), true
		}
		return result, true
	default:
		return nil, false
	}
}

func governanceResultFromCore(command api.Command, result any) (any, bool) {
	switch command.(type) {
	case api.AcquireTaskExecutionCommand:
		if acquired, ok := result.(core.AcquireTaskExecutionResult); ok {
			return api.AcquireTaskExecutionResult{
				Lease:          adapter.TaskExecutionLeaseFromModel(acquired.Lease),
				Acquired:       acquired.Acquired,
				ResourceClaims: adapter.ResourceClaimDecisionFromModel(acquired.ResourceClaims),
			}, true
		}
		return result, true
	case api.ToolInvocation:
		if toolResult, ok := result.(core.ToolInvocationResult); ok {
			return adapter.ToolInvocationResultFromCore(toolResult), true
		}
		return result, true
	case api.RequestApprovalCommand:
		requested, ok := result.(core.RequestApprovalResult)
		if !ok {
			return result, true
		}
		return api.RequestApprovalResult{
			Approval: adapter.ApprovalRequestFromModel(requested.Approval),
			Token:    adapter.ResumeTokenFromModel(requested.Token),
		}, true
	case api.RecoverResumeTokenCommand:
		if token, ok := result.(model.ResumeToken); ok {
			return adapter.ResumeTokenFromModel(token), true
		}
		return result, true
	case api.StartActionAttemptCommand, api.CompleteActionAttemptCommand, api.ResolveActionAttemptCommand:
		if attempt, ok := result.(model.ActionAttempt); ok {
			return adapter.ActionAttemptFromModel(attempt), true
		}
		return result, true
	case api.StartTraceSpanCommand:
		if span, ok := result.(model.TraceSpan); ok {
			return adapter.TraceSpanFromModel(span), true
		}
		return result, true
	default:
		return nil, false
	}
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
