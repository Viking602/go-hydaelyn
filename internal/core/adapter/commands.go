package adapter

import (
	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/internal/core"
	"github.com/Viking602/go-hydaelyn/internal/core/model"
)

func CommandToCore(command api.Command) (core.RuntimeCommand, bool) {
	if converted, ok := runTaskCommandToCore(command); ok {
		return converted, true
	}
	if converted, ok := mailboxCommandToCore(command); ok {
		return converted, true
	}
	if converted, ok := responseCommandToCore(command); ok {
		return converted, true
	}
	if converted, ok := governanceCommandToCore(command); ok {
		return converted, true
	}
	return traceCommandToCore(command)
}

func runTaskCommandToCore(command api.Command) (core.RuntimeCommand, bool) {
	switch cmd := command.(type) {
	case api.StartRunCommand:
		return StartRunCommandToCore(cmd), true
	case api.CreateTaskCommand:
		return CreateTaskCommandToCore(cmd), true
	case api.TransitionRunCommand:
		return TransitionRunCommandToCore(cmd), true
	case api.TransitionTaskCommand:
		return TransitionTaskCommandToCore(cmd), true
	case api.AdvanceRunCommand:
		return core.AdvanceRunCommand{RunID: cmd.RunID}, true
	case api.SubmitTypedReportCommand:
		return SubmitTypedReportCommandToCore(cmd), true
	case api.SubmitUserInputCommand:
		return core.SubmitUserInputCommand{RunID: cmd.RunID, TaskID: cmd.TaskID, Input: cmd.Input}, true
	default:
		return nil, false
	}
}

func mailboxCommandToCore(command api.Command) (core.RuntimeCommand, bool) {
	switch cmd := command.(type) {
	case api.DispatchTaskCommand:
		return core.DispatchTaskCommand{RunID: cmd.RunID, TaskID: cmd.TaskID, TargetAgentID: cmd.TargetAgentID, TargetComponent: cmd.TargetComponent, Payload: anyMapToModel(cmd.Payload)}, true
	case api.FanOutDispatchTaskCommand:
		return core.FanOutDispatchTaskCommand{RunID: cmd.RunID, TaskID: cmd.TaskID, To: AddressToModel(cmd.To), Payload: anyMapToModel(cmd.Payload)}, true
	case api.AckEnvelopeCommand:
		return core.AckEnvelopeCommand{EnvelopeID: cmd.EnvelopeID, HolderID: cmd.HolderID}, true
	case api.DeadLetterCommand:
		return core.DeadLetterCommand{EnvelopeID: cmd.EnvelopeID, Reason: cmd.Reason}, true
	default:
		return nil, false
	}
}

func responseCommandToCore(command api.Command) (core.RuntimeCommand, bool) {
	switch cmd := command.(type) {
	case api.WriteBlackboardItemCommand:
		return core.WriteBlackboardItemCommand{Item: BlackboardItemToModel(cmd.Item)}, true
	case api.HandoffCommand:
		return core.HandoffCommand{RunID: cmd.RunID, TaskID: cmd.TaskID, FromAgentID: cmd.FromAgentID, ToAgentID: cmd.ToAgentID, TaskVersion: cmd.TaskVersion, HandoffContext: cmd.HandoffContext}, true
	case api.SubmitResponseOutputCommand:
		return core.SubmitResponseOutputCommand{RunID: cmd.RunID, TaskID: cmd.TaskID, LeaseID: cmd.LeaseID, HolderType: model.HolderType(cmd.HolderType), HolderID: cmd.HolderID, TaskVersion: cmd.TaskVersion, Type: model.UserMessageType(cmd.Type), Title: cmd.Title, Payload: cmd.Payload, IdempotencyKey: cmd.IdempotencyKey}, true
	case api.PublishResponseCommand:
		return core.PublishResponseCommand{RunID: cmd.RunID, MessageID: cmd.MessageID}, true
	default:
		return nil, false
	}
}

func governanceCommandToCore(command api.Command) (core.RuntimeCommand, bool) {
	switch cmd := command.(type) {
	case api.AcquireTaskExecutionCommand:
		return core.AcquireTaskExecutionCommand{RunID: cmd.RunID, TaskID: cmd.TaskID, EnvelopeID: cmd.EnvelopeID, HolderType: model.HolderType(cmd.HolderType), HolderID: cmd.HolderID, TTL: cmd.TTL}, true
	case api.HeartbeatTaskExecutionCommand:
		return core.HeartbeatTaskExecutionCommand{LeaseID: cmd.LeaseID, TTL: cmd.TTL}, true
	case api.ReleaseTaskExecutionCommand:
		return core.ReleaseTaskExecutionCommand{LeaseID: cmd.LeaseID, HolderID: cmd.HolderID}, true
	case api.ToolInvocation:
		return ToolInvocationToCore(cmd), true
	case api.RequestApprovalCommand:
		return core.RequestApprovalCommand{RunID: cmd.RunID, TaskID: cmd.TaskID, ActionID: cmd.ActionID, RequesterAgentID: cmd.RequesterAgentID, Reason: cmd.Reason, RiskSummary: cmd.RiskSummary, RequestedAction: cmd.RequestedAction}, true
	case api.DecideApprovalCommand:
		return core.DecideApprovalCommand{RunID: cmd.RunID, ApprovalID: cmd.ApprovalID, DecidedBy: cmd.DecidedBy, Decision: cmd.Decision, Reason: cmd.Reason}, true
	case api.RecoverResumeTokenCommand:
		return core.RecoverResumeTokenCommand{TokenID: cmd.TokenID}, true
	case api.StartActionAttemptCommand:
		return core.StartActionAttemptCommand{AttemptID: cmd.AttemptID, ActionID: cmd.ActionID, RunID: cmd.RunID, TaskID: cmd.TaskID, LeaseID: cmd.LeaseID, HolderType: model.HolderType(cmd.HolderType), HolderID: cmd.HolderID, TaskVersion: cmd.TaskVersion, ToolName: cmd.ToolName, IdempotencyKey: cmd.IdempotencyKey, InputHash: cmd.InputHash}, true
	case api.CompleteActionAttemptCommand:
		return core.CompleteActionAttemptCommand{RunID: cmd.RunID, TaskID: cmd.TaskID, LeaseID: cmd.LeaseID, HolderType: model.HolderType(cmd.HolderType), HolderID: cmd.HolderID, TaskVersion: cmd.TaskVersion, AttemptID: cmd.AttemptID, Status: model.ActionAttemptStatus(cmd.Status), ExternalRequestID: cmd.ExternalRequestID, ExternalResultRef: cmd.ExternalResultRef, RequiresReconcile: cmd.RequiresReconcile}, true
	default:
		return nil, false
	}
}

func traceCommandToCore(command api.Command) (core.RuntimeCommand, bool) {
	switch cmd := command.(type) {
	case api.StartTraceSpanCommand:
		return core.StartTraceSpanCommand{RunID: cmd.RunID, TaskID: cmd.TaskID, TraceID: cmd.TraceID, ParentID: cmd.ParentID, Name: cmd.Name, Component: cmd.Component, Metadata: stringMapToModel(cmd.Metadata)}, true
	case api.EndTraceSpanCommand:
		return core.EndTraceSpanCommand{SpanID: cmd.SpanID, Error: cmd.Error}, true
	default:
		return nil, false
	}
}

func StartRunCommandToCore(cmd api.StartRunCommand) core.StartRunCommand {
	return core.StartRunCommand{RunID: cmd.RunID, RootTaskID: cmd.RootTaskID, Request: cmd.Request, Metadata: stringMapToModel(cmd.Metadata)}
}

func CreateTaskCommandToCore(cmd api.CreateTaskCommand) core.CreateTaskCommand {
	return core.CreateTaskCommand{
		RunID:              cmd.RunID,
		TaskID:             cmd.TaskID,
		ParentTaskID:       cmd.ParentTaskID,
		Type:               model.TaskType(cmd.Type),
		Goal:               cmd.Goal,
		AssignedAgentID:    cmd.AssignedAgentID,
		OwnerAgentID:       cmd.OwnerAgentID,
		OwnerComponent:     cmd.OwnerComponent,
		AllowsAction:       cmd.AllowsAction,
		Tags:               cloneStrings(cmd.Tags),
		CompletionCriteria: cloneStrings(cmd.CompletionCriteria),
		DependsOn:          cloneStrings(cmd.DependsOn),
		AwaitMode:          model.AwaitMode(cmd.AwaitMode),
		AwaitQuorum:        cmd.AwaitQuorum,
		OnDependencyFailed: model.OnDependencyFailed(cmd.OnDependencyFailed),
		ReadSelectors:      BlackboardSelectorsToModel(cmd.ReadSelectors),
		WriteTargets:       cloneStrings(cmd.WriteTargets),
		RetryPolicy:        RetryPolicyToModel(cmd.RetryPolicy),
		PolicyDecisions:    PolicyDecisionsToModel(cmd.PolicyDecisions),
		InputSchema:        cloneBytes(cmd.InputSchema),
		OutputSchema:       cloneBytes(cmd.OutputSchema),
		Budget:             TaskBudgetPtrToModel(cmd.Budget),
	}
}

func TransitionRunCommandToCore(cmd api.TransitionRunCommand) core.TransitionRunCommand {
	return core.TransitionRunCommand{RunID: cmd.RunID, To: model.RunStatus(cmd.To)}
}

func TransitionTaskCommandToCore(cmd api.TransitionTaskCommand) core.TransitionTaskCommand {
	return core.TransitionTaskCommand{RunID: cmd.RunID, TaskID: cmd.TaskID, To: model.TaskStatus(cmd.To)}
}

func SubmitTypedReportCommandToCore(cmd api.SubmitTypedReportCommand) core.SubmitTypedReportCommand {
	return core.SubmitTypedReportCommand{RunID: cmd.RunID, TaskID: cmd.TaskID, LeaseID: cmd.LeaseID, HolderType: model.HolderType(cmd.HolderType), HolderID: cmd.HolderID, TaskVersion: cmd.TaskVersion, Report: TypedReportToModel(cmd.Report)}
}

func ToolInvocationToCore(cmd api.ToolInvocation) core.ToolInvocation {
	return core.ToolInvocation{RunID: cmd.RunID, TaskID: cmd.TaskID, LeaseID: cmd.LeaseID, HolderType: model.HolderType(cmd.HolderType), HolderID: cmd.HolderID, TaskVersion: cmd.TaskVersion, ToolName: cmd.ToolName, Input: cmd.Input}
}

func ToolInvocationResultFromCore(in core.ToolInvocationResult) api.ToolInvocationResult {
	return api.ToolInvocationResult{ToolName: in.ToolName, Output: in.Output}
}
