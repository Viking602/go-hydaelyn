package orchestrator

import (
	"context"
	"time"

	core "github.com/Viking602/go-hydaelyn/internal/core"
)

type Config struct {
	StoreProvider StoreProvider
	PolicyEngine  PolicyEngine
	OutputGateway OutputGateway
	Pipeline      PipelineComponents
}

type Runner struct {
	inner *core.Runtime
}

// Runtime is kept for source compatibility.
//
// Deprecated: use Runner.
type Runtime = Runner

func DefaultConfig() Config {
	return Config{}
}

func New(configs ...Config) *Runner {
	config := resolveConfig(configs...)
	return &Runner{inner: core.NewRuntime(core.Config{
		StoreProvider: config.StoreProvider,
		PolicyEngine:  config.PolicyEngine,
		OutputGateway: config.OutputGateway,
		Pipeline:      config.Pipeline,
	})}
}

func NewInMemory(configs ...Config) *Runner {
	return New(configs...)
}

// NewMemoryRuntime is kept for source compatibility.
//
// Deprecated: use New or NewInMemory.
func NewMemoryRuntime(configs ...Config) *Runner {
	return New(configs...)
}

// NewRuntime is kept for source compatibility.
//
// Deprecated: use New.
func NewRuntime(configs ...Config) *Runner {
	return New(configs...)
}

func resolveConfig(configs ...Config) Config {
	config := DefaultConfig()
	for _, override := range configs {
		if override.StoreProvider != nil {
			config.StoreProvider = override.StoreProvider
		}
		if override.PolicyEngine != nil {
			config.PolicyEngine = override.PolicyEngine
		}
		if override.OutputGateway != nil {
			config.OutputGateway = override.OutputGateway
		}
		config.Pipeline = mergePipeline(config.Pipeline, override.Pipeline)
	}
	return config
}

func mergePipeline(base, override PipelineComponents) PipelineComponents {
	if override.IntentAnalyzer != nil {
		base.IntentAnalyzer = override.IntentAnalyzer
	}
	if override.Planner != nil {
		base.Planner = override.Planner
	}
	if override.Validator != nil {
		base.Validator = override.Validator
	}
	if override.Router != nil {
		base.Router = override.Router
	}
	if override.Dispatcher != nil {
		base.Dispatcher = override.Dispatcher
	}
	if override.TaskMonitor != nil {
		base.TaskMonitor = override.TaskMonitor
	}
	return base
}

func (r *Runner) StartRun(ctx context.Context, cmd StartRunCommand) (Run, Task, error) {
	result, err := r.inner.ExecuteCommand(ctx, cmd)
	if err != nil {
		return Run{}, Task{}, err
	}
	values := result.([]any)
	return values[0].(Run), values[1].(Task), nil
}

func (r *Runner) CreateTask(ctx context.Context, cmd CreateTaskCommand) (Task, error) {
	result, err := r.inner.ExecuteCommand(ctx, cmd)
	if err != nil {
		return Task{}, err
	}
	return result.(Task), nil
}

func (r *Runner) Run(ctx context.Context, runID string) (Run, error) {
	return r.inner.Run(ctx, runID)
}

func (r *Runner) Task(ctx context.Context, runID, taskID string) (Task, error) {
	return r.inner.Task(ctx, runID, taskID)
}

func (r *Runner) ReadyTasks(runID string) []Task {
	return r.inner.ReadyTasks(runID)
}

func (r *Runner) Events(runID string) []Event {
	return r.inner.Events(runID)
}

func (r *Runner) RunEvents(ctx context.Context, runID string) ([]Event, error) {
	return r.inner.RunEvents(ctx, runID)
}

func (r *Runner) ActiveLeaseCount(runID, taskID string) int {
	return r.inner.ActiveLeaseCount(runID, taskID)
}

func (r *Runner) RegisterTool(tool Tool) {
	r.inner.RegisterTool(tool)
}

func (r *Runner) RegisterAgent(profile AgentProfile) {
	r.inner.RegisterAgent(profile)
}

func (r *Runner) Agents() []AgentProfile {
	return r.inner.Agents()
}

func (r *Runner) DispatchTaskFanOut(ctx context.Context, cmd FanOutDispatchTaskCommand) ([]TaskEnvelope, error) {
	result, err := r.inner.ExecuteCommand(ctx, cmd)
	if err != nil {
		return nil, err
	}
	return result.([]TaskEnvelope), nil
}

func (r *Runner) Subscribe(ctx context.Context, runID string, filter BlackboardFilter) (<-chan BlackboardItem, func() error, error) {
	return r.inner.Subscribe(ctx, runID, filter)
}

func (r *Runner) WriteItem(ctx context.Context, item BlackboardItem) error {
	_, err := r.inner.ExecuteCommand(ctx, WriteBlackboardItemCommand{Item: item})
	return err
}

func (r *Runner) SelectItems(ctx context.Context, runID string, selector BlackboardSelector) ([]BlackboardItem, error) {
	return r.inner.SelectItems(ctx, runID, selector)
}

func (r *Runner) WaitForBlackboard(ctx context.Context, runID string, filter BlackboardFilter, predicate func([]BlackboardItem) bool, timeout time.Duration) ([]BlackboardItem, error) {
	return r.inner.WaitForBlackboard(ctx, runID, filter, predicate, timeout)
}

func (r *Runner) SetMessagePolicy(policy MessagePolicyChecker) {
	r.inner.SetMessagePolicy(policy)
}

func (r *Runner) SetPolicyEngine(policy PolicyEngine) {
	r.inner.SetPolicyEngine(policy)
}

func (r *Runner) SetOutputGateway(gateway OutputGateway) {
	r.inner.SetOutputGateway(gateway)
}

func (r *Runner) SetPipeline(components PipelineComponents) {
	r.inner.SetPipeline(components)
}

func (r *Runner) ExecuteCommand(ctx context.Context, command Command) (any, error) {
	return r.inner.ExecuteCommand(ctx, command)
}

func (r *Runner) TransitionRun(ctx context.Context, cmd TransitionRunCommand) error {
	_, err := r.inner.ExecuteCommand(ctx, cmd)
	return err
}

func (r *Runner) TransitionTask(ctx context.Context, cmd TransitionTaskCommand) error {
	_, err := r.inner.ExecuteCommand(ctx, cmd)
	return err
}

func (r *Runner) AdvanceRun(ctx context.Context, cmd AdvanceRunCommand) (Run, error) {
	result, err := r.inner.ExecuteCommand(ctx, cmd)
	if err != nil {
		return Run{}, err
	}
	return result.(Run), nil
}

func (r *Runner) DispatchTask(ctx context.Context, cmd DispatchTaskCommand) (TaskEnvelope, error) {
	result, err := r.inner.ExecuteCommand(ctx, cmd)
	if err != nil {
		return TaskEnvelope{}, err
	}
	return result.(TaskEnvelope), nil
}

func (r *Runner) AcquireTaskExecution(ctx context.Context, cmd AcquireTaskExecutionCommand) (TaskExecutionLease, bool, error) {
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

func (r *Runner) HeartbeatTaskExecution(ctx context.Context, cmd HeartbeatTaskExecutionCommand) error {
	_, err := r.inner.ExecuteCommand(ctx, cmd)
	return err
}

func (r *Runner) ReleaseTaskExecution(ctx context.Context, cmd ReleaseTaskExecutionCommand) error {
	_, err := r.inner.ExecuteCommand(ctx, cmd)
	return err
}

func (r *Runner) AckEnvelope(ctx context.Context, cmd AckEnvelopeCommand) error {
	_, err := r.inner.ExecuteCommand(ctx, cmd)
	return err
}

func (r *Runner) DeadLetter(ctx context.Context, cmd DeadLetterCommand) error {
	_, err := r.inner.ExecuteCommand(ctx, cmd)
	return err
}

func (r *Runner) SubmitTypedReport(ctx context.Context, cmd SubmitTypedReportCommand) error {
	_, err := r.inner.ExecuteCommand(ctx, cmd)
	return err
}

func (r *Runner) SubmitUserInput(ctx context.Context, cmd SubmitUserInputCommand) error {
	_, err := r.inner.ExecuteCommand(ctx, cmd)
	return err
}

func (r *Runner) InvokeTool(ctx context.Context, cmd ToolInvocation) (ToolInvocationResult, error) {
	result, err := r.inner.ExecuteCommand(ctx, cmd)
	if err != nil {
		return ToolInvocationResult{}, err
	}
	return result.(ToolInvocationResult), nil
}

func (r *Runner) RequestHandoff(ctx context.Context, cmd HandoffCommand) error {
	_, err := r.inner.ExecuteCommand(ctx, cmd)
	return err
}

func (r *Runner) SubmitResponseOutput(ctx context.Context, cmd SubmitResponseOutputCommand) error {
	_, err := r.inner.ExecuteCommand(ctx, cmd)
	return err
}

func (r *Runner) PublishResponse(ctx context.Context, cmd PublishResponseCommand) error {
	return r.inner.PublishResponse(ctx, cmd)
}

func (r *Runner) ResponseOutbox(runID string) []UserMessage {
	return r.inner.ResponseOutbox(runID)
}

func (r *Runner) QueueRun(ctx context.Context, cmd StartRunCommand) (Run, error) {
	run, _, err := r.StartRun(ctx, cmd)
	if err != nil {
		return Run{}, err
	}
	return r.AdvanceRun(ctx, AdvanceRunCommand{RunID: run.ID})
}

func (r *Runner) RunTimeline(ctx context.Context, runID string) ([]RunTimelineItem, error) {
	return r.inner.RunTimeline(ctx, runID)
}

func (r *Runner) RegisterFlow(flow Flow) error {
	return r.inner.RegisterFlow(flow)
}

func (r *Runner) Replay(runID string, mode ReplayMode) (Projection, error) {
	return r.inner.Replay(runID, mode)
}

func (r *Runner) ReplayRunState(runID string) (Projection, error) {
	return r.inner.ReplayRunState(runID)
}

func (r *Runner) Recover(ctx context.Context, runID string) (Projection, error) {
	return r.inner.Recover(ctx, runID)
}

func (r *Runner) RequestApproval(ctx context.Context, cmd RequestApprovalCommand) (ApprovalRequest, ResumeToken, error) {
	result, err := r.inner.ExecuteCommand(ctx, cmd)
	if err != nil {
		return ApprovalRequest{}, ResumeToken{}, err
	}
	values := result.([]any)
	return values[0].(ApprovalRequest), values[1].(ResumeToken), nil
}

func (r *Runner) DecideApproval(ctx context.Context, cmd DecideApprovalCommand) error {
	_, err := r.inner.ExecuteCommand(ctx, cmd)
	return err
}

func (r *Runner) RecoverResumeToken(ctx context.Context, cmd RecoverResumeTokenCommand) (ResumeToken, error) {
	return r.inner.RecoverResumeToken(ctx, cmd)
}

func (r *Runner) StartActionAttempt(ctx context.Context, cmd StartActionAttemptCommand) (ActionAttempt, error) {
	result, err := r.inner.ExecuteCommand(ctx, cmd)
	if err != nil {
		return ActionAttempt{}, err
	}
	return result.(ActionAttempt), nil
}

func (r *Runner) CompleteActionAttempt(ctx context.Context, cmd CompleteActionAttemptCommand) (ActionAttempt, error) {
	result, err := r.inner.ExecuteCommand(ctx, cmd)
	if err != nil {
		return ActionAttempt{}, err
	}
	return result.(ActionAttempt), nil
}

func (r *Runner) StartTraceSpan(ctx context.Context, cmd StartTraceSpanCommand) (TraceSpan, error) {
	result, err := r.inner.ExecuteCommand(ctx, cmd)
	if err != nil {
		return TraceSpan{}, err
	}
	return result.(TraceSpan), nil
}

func (r *Runner) EndTraceSpan(ctx context.Context, cmd EndTraceSpanCommand) error {
	_, err := r.inner.ExecuteCommand(ctx, cmd)
	return err
}

func (r *Runner) TraceSpans(runID string) []TraceSpan {
	return r.inner.TraceSpans(runID)
}
