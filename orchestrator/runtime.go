package orchestrator

import (
	"context"

	runtimeimpl "github.com/Viking602/go-hydaelyn/internal/runtime"
)

type Config struct {
	PolicyEngine  PolicyEngine
	OutputGateway OutputGateway
	Pipeline      PipelineComponents
}

type Runtime struct {
	inner *runtimeimpl.Runtime
}

func NewMemoryRuntime() *Runtime {
	return NewRuntime(Config{})
}

func NewRuntime(config Config) *Runtime {
	return &Runtime{inner: runtimeimpl.NewRuntime(runtimeimpl.Config{
		PolicyEngine:  config.PolicyEngine,
		OutputGateway: config.OutputGateway,
		Pipeline:      config.Pipeline,
	})}
}

func (r *Runtime) StartRun(ctx context.Context, cmd StartRunCommand) (Run, Task, error) {
	result, err := r.inner.ExecuteCommand(ctx, cmd)
	if err != nil {
		return Run{}, Task{}, err
	}
	values := result.([]any)
	return values[0].(Run), values[1].(Task), nil
}

func (r *Runtime) CreateTask(ctx context.Context, cmd CreateTaskCommand) (Task, error) {
	result, err := r.inner.ExecuteCommand(ctx, cmd)
	if err != nil {
		return Task{}, err
	}
	return result.(Task), nil
}

func (r *Runtime) Run(ctx context.Context, runID string) (Run, error) {
	return r.inner.Run(ctx, runID)
}

func (r *Runtime) Task(ctx context.Context, runID, taskID string) (Task, error) {
	return r.inner.Task(ctx, runID, taskID)
}

func (r *Runtime) ReadyTasks(runID string) []Task {
	return r.inner.ReadyTasks(runID)
}

func (r *Runtime) Events(runID string) []Event {
	return r.inner.Events(runID)
}

func (r *Runtime) RunEvents(ctx context.Context, runID string) ([]Event, error) {
	return r.inner.RunEvents(ctx, runID)
}

func (r *Runtime) ActiveLeaseCount(runID, taskID string) int {
	return r.inner.ActiveLeaseCount(runID, taskID)
}

func (r *Runtime) RegisterTool(tool Tool) {
	r.inner.RegisterTool(tool)
}

func (r *Runtime) SetMessagePolicy(policy MessagePolicyChecker) {
	r.inner.SetMessagePolicy(policy)
}

func (r *Runtime) SetPolicyEngine(policy PolicyEngine) {
	r.inner.SetPolicyEngine(policy)
}

func (r *Runtime) SetOutputGateway(gateway OutputGateway) {
	r.inner.SetOutputGateway(gateway)
}

func (r *Runtime) SetPipeline(components PipelineComponents) {
	r.inner.SetPipeline(components)
}

func (r *Runtime) ExecuteCommand(ctx context.Context, command RuntimeCommand) (any, error) {
	return r.inner.ExecuteCommand(ctx, command)
}

func (r *Runtime) TransitionRun(ctx context.Context, cmd TransitionRunCommand) error {
	_, err := r.inner.ExecuteCommand(ctx, cmd)
	return err
}

func (r *Runtime) TransitionTask(ctx context.Context, cmd TransitionTaskCommand) error {
	_, err := r.inner.ExecuteCommand(ctx, cmd)
	return err
}

func (r *Runtime) AdvanceRun(ctx context.Context, cmd AdvanceRunCommand) (Run, error) {
	result, err := r.inner.ExecuteCommand(ctx, cmd)
	if err != nil {
		return Run{}, err
	}
	return result.(Run), nil
}

func (r *Runtime) DispatchTask(ctx context.Context, cmd DispatchTaskCommand) (TaskEnvelope, error) {
	result, err := r.inner.ExecuteCommand(ctx, cmd)
	if err != nil {
		return TaskEnvelope{}, err
	}
	return result.(TaskEnvelope), nil
}

func (r *Runtime) AcquireTaskExecution(ctx context.Context, cmd AcquireTaskExecutionCommand) (TaskExecutionLease, bool, error) {
	result, err := r.inner.ExecuteCommand(ctx, cmd)
	if err != nil {
		return TaskExecutionLease{}, false, err
	}
	acquired := result.(struct {
		Lease    TaskExecutionLease
		Acquired bool
	})
	return acquired.Lease, acquired.Acquired, nil
}

func (r *Runtime) HeartbeatTaskExecution(ctx context.Context, cmd HeartbeatTaskExecutionCommand) error {
	_, err := r.inner.ExecuteCommand(ctx, cmd)
	return err
}

func (r *Runtime) ReleaseTaskExecution(ctx context.Context, cmd ReleaseTaskExecutionCommand) error {
	_, err := r.inner.ExecuteCommand(ctx, cmd)
	return err
}

func (r *Runtime) AckEnvelope(ctx context.Context, cmd AckEnvelopeCommand) error {
	_, err := r.inner.ExecuteCommand(ctx, cmd)
	return err
}

func (r *Runtime) DeadLetter(ctx context.Context, cmd DeadLetterCommand) error {
	_, err := r.inner.ExecuteCommand(ctx, cmd)
	return err
}

func (r *Runtime) SubmitTypedReport(ctx context.Context, cmd SubmitTypedReportCommand) error {
	_, err := r.inner.ExecuteCommand(ctx, cmd)
	return err
}

func (r *Runtime) SubmitUserInput(ctx context.Context, cmd SubmitUserInputCommand) error {
	_, err := r.inner.ExecuteCommand(ctx, cmd)
	return err
}

func (r *Runtime) InvokeTool(ctx context.Context, cmd ToolInvocation) (ToolInvocationResult, error) {
	result, err := r.inner.ExecuteCommand(ctx, cmd)
	if err != nil {
		return ToolInvocationResult{}, err
	}
	return result.(ToolInvocationResult), nil
}

func (r *Runtime) RequestHandoff(ctx context.Context, cmd HandoffCommand) error {
	_, err := r.inner.ExecuteCommand(ctx, cmd)
	return err
}

func (r *Runtime) SubmitResponseOutput(ctx context.Context, cmd SubmitResponseOutputCommand) error {
	_, err := r.inner.ExecuteCommand(ctx, cmd)
	return err
}

func (r *Runtime) PublishResponse(ctx context.Context, cmd PublishResponseCommand) error {
	_, err := r.inner.ExecuteCommand(ctx, cmd)
	return err
}

func (r *Runtime) ResponseOutbox(runID string) []UserMessage {
	return r.inner.ResponseOutbox(runID)
}

func (r *Runtime) QueueRun(ctx context.Context, cmd StartRunCommand) (Run, error) {
	run, _, err := r.StartRun(ctx, cmd)
	if err != nil {
		return Run{}, err
	}
	return r.AdvanceRun(ctx, AdvanceRunCommand{RunID: run.ID})
}

func (r *Runtime) RunTimeline(ctx context.Context, runID string) ([]RunTimelineItem, error) {
	return r.inner.RunTimeline(ctx, runID)
}

func (r *Runtime) RegisterFlow(flow Flow) error {
	return r.inner.RegisterFlow(flow)
}

func (r *Runtime) Replay(runID string, mode ReplayMode) (Projection, error) {
	return r.inner.Replay(runID, mode)
}

func (r *Runtime) ReplayRunState(runID string) (Projection, error) {
	return r.inner.ReplayRunState(runID)
}

func (r *Runtime) Recover(ctx context.Context, runID string) (Projection, error) {
	return r.inner.Recover(ctx, runID)
}

func (r *Runtime) RequestApproval(ctx context.Context, cmd RequestApprovalCommand) (ApprovalRequest, ResumeToken, error) {
	result, err := r.inner.ExecuteCommand(ctx, cmd)
	if err != nil {
		return ApprovalRequest{}, ResumeToken{}, err
	}
	values := result.([]any)
	return values[0].(ApprovalRequest), values[1].(ResumeToken), nil
}

func (r *Runtime) DecideApproval(ctx context.Context, cmd DecideApprovalCommand) error {
	_, err := r.inner.ExecuteCommand(ctx, cmd)
	return err
}

func (r *Runtime) RecoverResumeToken(ctx context.Context, cmd RecoverResumeTokenCommand) (ResumeToken, error) {
	return r.inner.RecoverResumeToken(ctx, cmd)
}

func (r *Runtime) StartActionAttempt(ctx context.Context, cmd StartActionAttemptCommand) (ActionAttempt, error) {
	result, err := r.inner.ExecuteCommand(ctx, cmd)
	if err != nil {
		return ActionAttempt{}, err
	}
	return result.(ActionAttempt), nil
}

func (r *Runtime) CompleteActionAttempt(ctx context.Context, cmd CompleteActionAttemptCommand) (ActionAttempt, error) {
	result, err := r.inner.ExecuteCommand(ctx, cmd)
	if err != nil {
		return ActionAttempt{}, err
	}
	return result.(ActionAttempt), nil
}

func (r *Runtime) StartTraceSpan(ctx context.Context, cmd StartTraceSpanCommand) (TraceSpan, error) {
	result, err := r.inner.ExecuteCommand(ctx, cmd)
	if err != nil {
		return TraceSpan{}, err
	}
	return result.(TraceSpan), nil
}

func (r *Runtime) EndTraceSpan(ctx context.Context, cmd EndTraceSpanCommand) error {
	_, err := r.inner.ExecuteCommand(ctx, cmd)
	return err
}

func (r *Runtime) TraceSpans(runID string) []TraceSpan {
	return r.inner.TraceSpans(runID)
}
