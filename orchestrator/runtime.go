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
	return r.inner.StartRun(ctx, cmd)
}

func (r *Runtime) CreateTask(ctx context.Context, cmd CreateTaskCommand) (Task, error) {
	return r.inner.CreateTask(ctx, cmd)
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
	return r.inner.TransitionRun(ctx, cmd)
}

func (r *Runtime) TransitionTask(ctx context.Context, cmd TransitionTaskCommand) error {
	return r.inner.TransitionTask(ctx, cmd)
}

func (r *Runtime) AdvanceRun(ctx context.Context, cmd AdvanceRunCommand) (Run, error) {
	return r.inner.AdvanceRun(ctx, cmd)
}

func (r *Runtime) DispatchTask(ctx context.Context, cmd DispatchTaskCommand) (TaskEnvelope, error) {
	return r.inner.DispatchTask(ctx, cmd)
}

func (r *Runtime) AcquireTaskExecution(ctx context.Context, cmd AcquireTaskExecutionCommand) (TaskExecutionLease, bool, error) {
	return r.inner.AcquireTaskExecution(ctx, cmd)
}

func (r *Runtime) HeartbeatTaskExecution(ctx context.Context, cmd HeartbeatTaskExecutionCommand) error {
	return r.inner.HeartbeatTaskExecution(ctx, cmd)
}

func (r *Runtime) ReleaseTaskExecution(ctx context.Context, cmd ReleaseTaskExecutionCommand) error {
	return r.inner.ReleaseTaskExecution(ctx, cmd)
}

func (r *Runtime) AckEnvelope(ctx context.Context, cmd AckEnvelopeCommand) error {
	return r.inner.AckEnvelope(ctx, cmd)
}

func (r *Runtime) DeadLetter(ctx context.Context, cmd DeadLetterCommand) error {
	return r.inner.DeadLetter(ctx, cmd)
}

func (r *Runtime) SubmitTypedReport(ctx context.Context, cmd SubmitTypedReportCommand) error {
	return r.inner.SubmitTypedReport(ctx, cmd)
}

func (r *Runtime) SubmitUserInput(ctx context.Context, cmd SubmitUserInputCommand) error {
	return r.inner.SubmitUserInput(ctx, cmd)
}

func (r *Runtime) InvokeTool(ctx context.Context, cmd ToolInvocation) (ToolInvocationResult, error) {
	return r.inner.InvokeTool(ctx, cmd)
}

func (r *Runtime) RequestHandoff(ctx context.Context, cmd HandoffCommand) error {
	return r.inner.RequestHandoff(ctx, cmd)
}

func (r *Runtime) SubmitResponseOutput(ctx context.Context, cmd SubmitResponseOutputCommand) error {
	return r.inner.SubmitResponseOutput(ctx, cmd)
}

func (r *Runtime) PublishResponse(ctx context.Context, cmd PublishResponseCommand) error {
	return r.inner.PublishResponse(ctx, cmd)
}

func (r *Runtime) ResponseOutbox(runID string) []UserMessage {
	return r.inner.ResponseOutbox(runID)
}

func (r *Runtime) QueueRun(ctx context.Context, cmd StartRunCommand) (Run, error) {
	return r.inner.QueueRun(ctx, cmd)
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
	return r.inner.RequestApproval(ctx, cmd)
}

func (r *Runtime) DecideApproval(ctx context.Context, cmd DecideApprovalCommand) error {
	return r.inner.DecideApproval(ctx, cmd)
}

func (r *Runtime) RecoverResumeToken(ctx context.Context, cmd RecoverResumeTokenCommand) (ResumeToken, error) {
	return r.inner.RecoverResumeToken(ctx, cmd)
}

func (r *Runtime) StartActionAttempt(ctx context.Context, cmd StartActionAttemptCommand) (ActionAttempt, error) {
	return r.inner.StartActionAttempt(ctx, cmd)
}

func (r *Runtime) CompleteActionAttempt(ctx context.Context, cmd CompleteActionAttemptCommand) (ActionAttempt, error) {
	return r.inner.CompleteActionAttempt(ctx, cmd)
}

func (r *Runtime) StartTraceSpan(ctx context.Context, cmd StartTraceSpanCommand) (TraceSpan, error) {
	return r.inner.StartTraceSpan(ctx, cmd)
}

func (r *Runtime) EndTraceSpan(ctx context.Context, cmd EndTraceSpanCommand) error {
	return r.inner.EndTraceSpan(ctx, cmd)
}

func (r *Runtime) TraceSpans(runID string) []TraceSpan {
	return r.inner.TraceSpans(runID)
}
