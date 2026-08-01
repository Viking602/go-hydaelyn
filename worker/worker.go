// Package worker provides optional glue between the Venat runner and the
// single-agent engine.
package worker

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Viking602/venat"
	"github.com/Viking602/venat/agent"
	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/stream"
	"github.com/Viking602/venat/tool"
)

var (
	ErrRunnerMissing    = errors.New("worker: runner missing")
	ErrProviderMissing  = errors.New("worker: provider missing")
	ErrAgentIDMissing   = errors.New("worker: agent id missing")
	ErrFailedCheckpoint = errors.New("worker: checkpoint contains failed instance")
)

type AgentWorker struct {
	Runner        *venat.Runner
	Engine        agent.Engine
	AgentID       string
	Model         string
	ToolMode      tool.Mode
	MaxIterations int
	TTL           time.Duration
}

type ExecuteEnvelopeRequest struct {
	Envelope api.TaskEnvelope
	TTL      time.Duration
	Messages []message.Message
	Lease    api.TaskExecutionLease
	Sink     stream.Sink
}

type ExecutionState string

const (
	ExecutionCompleted ExecutionState = "completed"
	ExecutionFailed    ExecutionState = "failed"
	ExecutionSuspended ExecutionState = "suspended"
)

type SuspensionKind string

const (
	SuspensionApproval       SuspensionKind = "approval"
	SuspensionReconciliation SuspensionKind = "reconciliation"
	SuspensionUserInput      SuspensionKind = "user_input"
)

type Suspension struct {
	Kind   SuspensionKind `json:"kind"`
	Reason string         `json:"reason,omitempty"`
}

// ExecutionOutcome is the durable worker result. Domain suspension is
// distinguished from failure so hosts can resume approval, reconciliation, or
// user-input flows without treating them as failed task executions.
type ExecutionOutcome struct {
	State      ExecutionState      `json:"state"`
	RunID      string              `json:"runId,omitempty"`
	TaskID     string              `json:"taskId,omitempty"`
	LeaseID    string              `json:"leaseId,omitempty"`
	Result     agent.Result        `json:"result,omitempty"`
	Failure    *agent.AgentFailure `json:"failure,omitempty"`
	Suspension *Suspension         `json:"suspension,omitempty"`
}

// ExecuteContinuing runs an envelope and follows durable retry envelopes until
// the task completes, suspends, or reaches a terminal failure.
func (w AgentWorker) ExecuteContinuing(ctx context.Context, req ExecuteEnvelopeRequest) (ExecutionOutcome, error) {
	for {
		outcome, runErr := w.ExecuteEnvelope(ctx, req)
		if runErr == nil || outcome.State == ExecutionSuspended {
			return outcome, runErr
		}
		envelope, ok, loadErr := taskEnvelope(ctx, w.Runner, req.Envelope.RunID, req.Envelope.TaskID, "pending")
		if loadErr != nil {
			return outcome, errors.Join(runErr, loadErr)
		}
		if !ok {
			return outcome, runErr
		}
		if waitErr := waitForEnvelopeReady(ctx, envelope); waitErr != nil {
			return outcome, errors.Join(runErr, waitErr)
		}
		req.Envelope = envelope
		req.Lease = api.TaskExecutionLease{}
	}
}

func (w AgentWorker) ExecuteEnvelope(ctx context.Context, req ExecuteEnvelopeRequest) (ExecutionOutcome, error) {
	if err := w.validateExecuteEnvelope(); err != nil {
		return ExecutionOutcome{}, err
	}
	ttl := w.executeEnvelopeTTL(req)
	lease, err := w.envelopeLease(ctx, req, ttl)
	if err != nil {
		return ExecutionOutcome{}, err
	}
	leaseHandled := false
	defer func() {
		if leaseHandled {
			return
		}
		_ = w.Runner.ReleaseTaskExecution(context.WithoutCancel(ctx), api.ReleaseTaskExecutionCommand{
			LeaseID:  lease.ID,
			HolderID: w.AgentID,
		})
	}()

	if err := w.ackEnvelope(ctx, req.Envelope); err != nil {
		return ExecutionOutcome{}, err
	}
	task, run, err := w.loadEnvelopeTaskRun(ctx, req.Envelope)
	if err != nil {
		return ExecutionOutcome{}, err
	}

	engine := w.executionEngine(task, lease)
	baseContextBuilder := engine.ContextBuilder
	checkpoint, hasCheckpoint, err := w.latestExecutionCheckpoint(ctx, task)
	if err != nil {
		leaseHandled = w.submitFailureReportHandled(ctx, task, lease, err)
		return failedExecutionOutcome(task, lease, agent.Result{}, err), err
	}
	if hasCheckpoint {
		engine.OperationTurn = max(engine.OperationTurn, checkpoint.Checkpoint.NextOperationTurn)
	}
	resumeMessages := checkpoint.Checkpoint.Messages
	engine.ContextBuilder = workerContextBuilder{worker: w, run: run, inner: baseContextBuilder, extra: req.Messages, resume: resumeMessages}
	started := time.Now()
	engine.StepRecorder = w.stepRecorder(task, lease, started, engine.StepRecorder)
	engine.CheckpointRecorder = w.checkpointRecorder(task, lease, engine.CheckpointRecorder)

	var result agent.Result
	var runErr error
	recoveredTerminal := hasCheckpoint &&
		!checkpoint.Checkpoint.PendingToolCalls &&
		checkpoint.Checkpoint.Step.Decision == agent.StepDecisionFinish
	if recoveredTerminal {
		result, runErr = resultFromTerminalCheckpoint(checkpoint.Checkpoint, task.OutputSchema)
	} else {
		if hasCheckpoint && checkpoint.Checkpoint.PendingToolCalls {
			resumeMessages, runErr = withLeaseHeartbeat(ctx, w, lease.ID, ttl, func(runCtx context.Context) ([]message.Message, error) {
				return w.resumePendingToolCalls(runCtx, engine, checkpoint.Checkpoint)
			})
			engine.ContextBuilder = workerContextBuilder{worker: w, run: run, inner: baseContextBuilder, extra: req.Messages, resume: resumeMessages}
			result.Messages = resumeMessages
			result.Usage = checkpoint.Checkpoint.Usage
			result.ToolCallsUsed = checkpoint.Checkpoint.ToolCallsUsed
		}
		// A pending checkpoint has already charged its tool-call slots. Resume
		// those calls before rejecting an exhausted durable budget; only fresh
		// model/tool work is gated by the remaining balance.
		if runErr == nil {
			task, runErr = w.taskWithRemainingBudget(ctx, task)
		}
		if runErr == nil {
			result, runErr = w.runEngineWithHeartbeat(ctx, engine, task, lease.ID, ttl, req.Sink)
			w.appendUsage(ctx, task, lease.ID, engine, result, time.Since(started))
		}
	}
	if runErr != nil {
		if suspension := w.executionSuspension(ctx, task, runErr); suspension != nil {
			checkpointErr := w.recordSuspensionCheckpoint(ctx, task, lease, result)
			return ExecutionOutcome{
				State:      ExecutionSuspended,
				RunID:      task.RunID,
				TaskID:     task.ID,
				LeaseID:    lease.ID,
				Result:     result,
				Suspension: suspension,
			}, errors.Join(runErr, checkpointErr)
		}
		leaseHandled = w.submitFailureReportHandled(ctx, task, lease, runErr)
		return failedExecutionOutcome(task, lease, result, runErr), runErr
	}

	leaseHandled, err = w.submitSuccessReport(ctx, task, lease, result)
	if err != nil {
		return failedExecutionOutcome(task, lease, result, err), err
	}
	return ExecutionOutcome{
		State:   ExecutionCompleted,
		RunID:   task.RunID,
		TaskID:  task.ID,
		LeaseID: lease.ID,
		Result:  result,
	}, nil
}

func (w AgentWorker) executionEngine(task api.Task, lease api.TaskExecutionLease) agent.Engine {
	engine := w.governedEngine(task, lease)
	if engine.Model == "" {
		engine.Model = w.Model
	}
	if engine.ToolMode == "" {
		engine.ToolMode = w.ToolMode
	}
	if engine.LoopPolicy.MaxIterations == 0 {
		engine.LoopPolicy.MaxIterations = w.MaxIterations
	}
	return engine
}

func failedExecutionOutcome(task api.Task, lease api.TaskExecutionLease, result agent.Result, cause error) ExecutionOutcome {
	outcome := ExecutionOutcome{
		State:   ExecutionFailed,
		RunID:   task.RunID,
		TaskID:  task.ID,
		LeaseID: lease.ID,
		Result:  result,
	}
	var failure *agent.AgentFailure
	if errors.As(cause, &failure) {
		outcome.Failure = failure
	}
	return outcome
}

func (w AgentWorker) executionSuspension(ctx context.Context, task api.Task, cause error) *Suspension {
	if errors.Is(cause, api.ErrActionReconcileRequired) {
		return &Suspension{Kind: SuspensionReconciliation, Reason: cause.Error()}
	}
	currentTask, taskErr := w.Runner.Task(ctx, task.RunID, task.ID)
	if taskErr == nil {
		switch currentTask.Status {
		case api.TaskStatusReconcileRequired:
			return &Suspension{Kind: SuspensionReconciliation, Reason: cause.Error()}
		case api.TaskStatusWaitingUserInput:
			return &Suspension{Kind: SuspensionUserInput, Reason: cause.Error()}
		}
	}
	run, runErr := w.Runner.Run(ctx, task.RunID)
	if runErr == nil {
		switch run.Status {
		case api.RunStatusReconcileRequired:
			return &Suspension{Kind: SuspensionReconciliation, Reason: cause.Error()}
		case api.RunStatusWaitingApproval:
			return &Suspension{Kind: SuspensionApproval, Reason: cause.Error()}
		case api.RunStatusWaitingUserInput:
			return &Suspension{Kind: SuspensionUserInput, Reason: cause.Error()}
		}
	}
	return nil
}

// appendUsage best-effort persists one execution's cumulative budget spend.
// Finalized step events are the crash-safe source used during resume; this
// record also covers executions that fail before producing their first step.
func (w AgentWorker) appendUsage(ctx context.Context, task api.Task, executionID string, engine agent.Engine, result agent.Result, elapsed time.Duration) {
	usage := result.Usage
	totalTokens := usage.TotalTokens
	if totalTokens == 0 {
		totalTokens = usage.InputTokens + usage.OutputTokens
	}
	providerName := ""
	if engine.Provider != nil {
		providerName = engine.Provider.Metadata().Name
	}
	toolCalls := result.ToolCallsUsed
	for _, step := range result.Steps {
		if step.BudgetUsed.ToolCalls > toolCalls {
			toolCalls = step.BudgetUsed.ToolCalls
		}
	}
	_ = w.Runner.AppendUsage(context.WithoutCancel(ctx), api.UsageRecord{
		RunID:                 task.RunID,
		TaskID:                task.ID,
		AgentID:               w.AgentID,
		Provider:              providerName,
		Model:                 engine.Model,
		InputTokens:           usage.InputTokens,
		CachedInputTokens:     usage.CachedInputTokens,
		CacheWriteInputTokens: usage.CacheWriteInputTokens,
		OutputTokens:          usage.OutputTokens,
		TotalTokens:           totalTokens,
		ToolCalls:             toolCalls,
		Steps:                 len(result.Steps),
		DurationMS:            elapsed.Milliseconds(),
		Metadata:              map[string]string{"executionId": executionID},
	})
}

type durableTaskBudgetUsage struct {
	tokens    int64
	toolCalls int
	steps     int
	wallClock time.Duration
}

func (w AgentWorker) taskWithRemainingBudget(ctx context.Context, task api.Task) (api.Task, error) {
	if task.Budget == nil {
		return task, nil
	}
	consumed, err := w.durableTaskBudgetUsage(ctx, task.RunID, task.ID)
	if err != nil {
		return task, err
	}
	remaining := *task.Budget
	if remaining.MaxTokens > 0 {
		remaining.MaxTokens -= consumed.tokens
		if remaining.MaxTokens <= 0 {
			return task, budgetExhaustedFailure("max tokens")
		}
	}
	if remaining.MaxToolCalls > 0 {
		remaining.MaxToolCalls -= consumed.toolCalls
		if remaining.MaxToolCalls <= 0 {
			return task, budgetExhaustedFailure("max tool calls")
		}
	}
	if remaining.MaxSteps > 0 {
		remaining.MaxSteps -= consumed.steps
		if remaining.MaxSteps <= 0 {
			return task, budgetExhaustedFailure("max steps")
		}
	}
	if remaining.MaxWallClock > 0 {
		remaining.MaxWallClock -= consumed.wallClock
		if remaining.MaxWallClock <= 0 {
			return task, budgetExhaustedFailure("max wall clock")
		}
	}
	task.Budget = &remaining
	return task, nil
}

func (w AgentWorker) durableTaskBudgetUsage(ctx context.Context, runID, taskID string) (durableTaskBudgetUsage, error) {
	events, err := w.Runner.ListEvents(ctx, runID)
	if err != nil {
		return durableTaskBudgetUsage{}, err
	}
	steps, err := agent.ReconstructStepTrace(events, agent.StepSelector{RunID: runID, TaskID: taskID})
	if err != nil {
		return durableTaskBudgetUsage{}, err
	}
	byExecution := make(map[string]durableTaskBudgetUsage)
	for _, record := range steps {
		current := byExecution[record.ExecutionID]
		current.tokens = max(current.tokens, record.Step.BudgetUsed.Tokens)
		current.toolCalls = max(current.toolCalls, record.Step.BudgetUsed.ToolCalls)
		current.wallClock = max(current.wallClock, record.Step.BudgetUsed.WallClock)
		current.steps++
		byExecution[record.ExecutionID] = current
	}
	checkpoints, err := agent.ReconstructExecutionCheckpoints(events, agent.StepSelector{RunID: runID, TaskID: taskID})
	if err != nil {
		return durableTaskBudgetUsage{}, err
	}
	for _, record := range checkpoints {
		current := byExecution[record.ExecutionID]
		checkpoint := record.Checkpoint
		tokens := int64(checkpoint.Usage.TotalTokens)
		if tokens == 0 {
			tokens = int64(checkpoint.Usage.InputTokens + checkpoint.Usage.OutputTokens)
		}
		current.tokens = max(current.tokens, tokens, checkpoint.Step.BudgetUsed.Tokens)
		current.toolCalls = max(current.toolCalls, checkpoint.ToolCallsUsed, checkpoint.Step.BudgetUsed.ToolCalls)
		current.steps = max(current.steps, checkpoint.Step.Index+1)
		current.wallClock = max(current.wallClock, checkpoint.Step.BudgetUsed.WallClock)
		byExecution[record.ExecutionID] = current
	}
	records, err := w.Runner.QueryUsage(ctx, api.UsageSelector{RunID: runID, TaskID: taskID})
	if err != nil {
		return durableTaskBudgetUsage{}, err
	}
	for index, record := range records {
		executionID := record.Metadata["executionId"]
		if executionID == "" {
			if len(steps) > 0 || len(checkpoints) > 0 {
				continue
			}
			executionID = fmt.Sprintf("legacy-usage-%d", index)
		}
		current := byExecution[executionID]
		tokens := int64(record.TotalTokens)
		if tokens == 0 {
			tokens = int64(record.InputTokens + record.OutputTokens)
		}
		current.tokens = max(current.tokens, tokens)
		current.toolCalls = max(current.toolCalls, record.ToolCalls)
		current.steps = max(current.steps, record.Steps)
		current.wallClock = max(current.wallClock, time.Duration(record.DurationMS)*time.Millisecond)
		byExecution[executionID] = current
	}
	var total durableTaskBudgetUsage
	for _, execution := range byExecution {
		total.tokens += execution.tokens
		total.toolCalls += execution.toolCalls
		total.steps += execution.steps
		total.wallClock += execution.wallClock
	}
	return total, nil
}

func budgetExhaustedFailure(dimension string) error {
	return &agent.AgentFailure{
		Kind:   agent.FailureKindBudgetExhausted,
		Reason: "durable task budget exhausted: " + dimension,
	}
}

func (w AgentWorker) validateExecuteEnvelope() error {
	if w.Runner == nil {
		return ErrRunnerMissing
	}
	if w.Engine.Provider == nil {
		return ErrProviderMissing
	}
	if strings.TrimSpace(w.AgentID) == "" {
		return ErrAgentIDMissing
	}
	return nil
}

func (w AgentWorker) executeEnvelopeTTL(req ExecuteEnvelopeRequest) time.Duration {
	ttl := req.TTL
	if ttl <= 0 {
		ttl = w.TTL
	}
	if ttl <= 0 {
		ttl = time.Minute
	}
	return ttl
}

func (w AgentWorker) envelopeLease(ctx context.Context, req ExecuteEnvelopeRequest, ttl time.Duration) (api.TaskExecutionLease, error) {
	if req.Lease.ID == "" {
		return w.acquireEnvelopeLease(ctx, req, ttl)
	}
	uow, err := w.Runner.Begin(ctx)
	if err != nil {
		return api.TaskExecutionLease{}, err
	}
	lease, loadErr := uow.Leases().LoadLease(ctx, req.Lease.ID)
	rollbackErr := uow.Rollback(ctx)
	if loadErr != nil {
		return api.TaskExecutionLease{}, loadErr
	}
	if rollbackErr != nil {
		return api.TaskExecutionLease{}, rollbackErr
	}
	if lease.RunID != req.Envelope.RunID || lease.TaskID != req.Envelope.TaskID ||
		lease.HolderType != api.HolderAgent || lease.HolderID != w.AgentID {
		return api.TaskExecutionLease{}, fmt.Errorf("worker: supplied lease does not match envelope or agent")
	}
	if lease.Status != api.LeaseStatusActive || lease.ExpiresAt.IsZero() || !lease.ExpiresAt.After(time.Now().UTC()) {
		return api.TaskExecutionLease{}, api.ErrLeaseNotActive
	}
	if req.Envelope.TaskVersion != 0 && lease.TaskVersion != req.Envelope.TaskVersion {
		return api.TaskExecutionLease{}, api.ErrStaleTaskVersion
	}
	return lease, nil
}

func (w AgentWorker) acquireEnvelopeLease(ctx context.Context, req ExecuteEnvelopeRequest, ttl time.Duration) (api.TaskExecutionLease, error) {
	lease, acquired, err := w.Runner.AcquireTaskExecution(ctx, api.AcquireTaskExecutionCommand{
		RunID:      req.Envelope.RunID,
		TaskID:     req.Envelope.TaskID,
		EnvelopeID: req.Envelope.ID,
		HolderType: api.HolderAgent,
		HolderID:   w.AgentID,
		TTL:        ttl,
	})
	if err != nil {
		return api.TaskExecutionLease{}, err
	}
	if !acquired {
		return api.TaskExecutionLease{}, fmt.Errorf("worker: task %s already has an active lease", req.Envelope.TaskID)
	}
	return lease, nil
}

func (w AgentWorker) ackEnvelope(ctx context.Context, envelope api.TaskEnvelope) error {
	if envelope.ID == "" {
		return nil
	}
	return w.Runner.AckEnvelope(ctx, api.AckEnvelopeCommand{EnvelopeID: envelope.ID, HolderID: w.AgentID})
}

func (w AgentWorker) loadEnvelopeTaskRun(ctx context.Context, envelope api.TaskEnvelope) (api.Task, api.Run, error) {
	task, err := w.Runner.Task(ctx, envelope.RunID, envelope.TaskID)
	if err != nil {
		return api.Task{}, api.Run{}, err
	}
	run, err := w.Runner.Run(ctx, envelope.RunID)
	if err != nil {
		return api.Task{}, api.Run{}, err
	}
	return task, run, nil
}

func (w AgentWorker) governedEngine(task api.Task, lease api.TaskExecutionLease) agent.Engine {
	engine := w.Engine
	if engine.Tools == nil {
		return engine
	}
	engine.Tools = GovernedToolBus{
		Runner:      w.Runner,
		Bus:         engine.Tools,
		RunID:       task.RunID,
		TaskID:      task.ID,
		LeaseID:     lease.ID,
		HolderType:  api.HolderAgent,
		HolderID:    w.AgentID,
		TaskVersion: task.Version,
	}.ToolBus()
	return engine
}

func (w AgentWorker) stepRecorder(task api.Task, lease api.TaskExecutionLease, started time.Time, caller agent.StepRecorder) agent.StepRecorder {
	durable := agent.StepRecorderFunc(func(ctx context.Context, step agent.Step) error {
		step.BudgetUsed.WallClock = time.Since(started)
		event, err := agent.NewStepCompletedEvent(agent.StepRecord{
			RunID:       task.RunID,
			TaskID:      task.ID,
			AgentID:     w.AgentID,
			ExecutionID: lease.ID,
			Step:        step,
		})
		if err != nil {
			return err
		}
		return w.Runner.AppendTaskExecutionEvent(context.WithoutCancel(ctx), api.AppendTaskExecutionEventCommand{
			RunID: task.RunID, TaskID: task.ID, LeaseID: lease.ID,
			HolderType: lease.HolderType, HolderID: lease.HolderID,
			TaskVersion: task.Version, Event: event,
		})
	})
	if caller == nil {
		return durable
	}
	return agent.StepRecorderFunc(func(ctx context.Context, step agent.Step) error {
		if err := durable.RecordStep(ctx, step); err != nil {
			return err
		}
		return caller.RecordStep(ctx, step)
	})
}

func (w AgentWorker) checkpointRecorder(task api.Task, lease api.TaskExecutionLease, caller agent.CheckpointRecorder) agent.CheckpointRecorder {
	durable := agent.CheckpointRecorderFunc(func(ctx context.Context, checkpoint agent.TurnCheckpoint) error {
		event, err := agent.NewExecutionCheckpointedEvent(agent.ExecutionCheckpointRecord{
			RunID:       task.RunID,
			TaskID:      task.ID,
			AgentID:     w.AgentID,
			ExecutionID: lease.ID,
			Checkpoint:  checkpoint,
		})
		if err != nil {
			return err
		}
		return w.Runner.AppendTaskExecutionEvent(context.WithoutCancel(ctx), api.AppendTaskExecutionEventCommand{
			RunID: task.RunID, TaskID: task.ID, LeaseID: lease.ID,
			HolderType: lease.HolderType, HolderID: lease.HolderID,
			TaskVersion: task.Version, Event: event,
		})
	})
	if caller == nil {
		return durable
	}
	return agent.CheckpointRecorderFunc(func(ctx context.Context, checkpoint agent.TurnCheckpoint) error {
		if err := durable.RecordCheckpoint(ctx, checkpoint); err != nil {
			return err
		}
		return caller.RecordCheckpoint(ctx, checkpoint)
	})
}

func (w AgentWorker) latestExecutionCheckpoint(ctx context.Context, task api.Task) (agent.ExecutionCheckpointRecord, bool, error) {
	events, err := w.Runner.ListEvents(ctx, task.RunID)
	if err != nil {
		return agent.ExecutionCheckpointRecord{}, false, err
	}
	return agent.LatestExecutionCheckpoint(events, agent.StepSelector{
		RunID:   task.RunID,
		TaskID:  task.ID,
		AgentID: w.AgentID,
	})
}

func (w AgentWorker) recordSuspensionCheckpoint(
	ctx context.Context,
	task api.Task,
	lease api.TaskExecutionLease,
	result agent.Result,
) error {
	if len(result.Messages) == 0 {
		return nil
	}
	currentTask, err := w.Runner.Task(ctx, task.RunID, task.ID)
	if err != nil {
		return err
	}
	task = currentTask
	step := agent.Step{
		Index:    len(result.Steps),
		Decision: agent.StepDecisionContinue,
		BudgetUsed: agent.BudgetUsage{
			Tokens:    int64(result.Usage.TotalTokens),
			ToolCalls: result.ToolCallsUsed,
		},
	}
	checkpoint := agent.TurnCheckpoint{
		Messages:         append([]message.Message(nil), result.Messages...),
		Usage:            result.Usage,
		Step:             step,
		ToolCallsUsed:    result.ToolCallsUsed,
		PendingToolCalls: pendingToolCallCount(result.Messages) > 0,
	}
	return w.checkpointRecorder(task, lease, nil).RecordCheckpoint(ctx, checkpoint)
}

func (w AgentWorker) resumePendingToolCalls(
	ctx context.Context,
	engine agent.Engine,
	checkpoint agent.TurnCheckpoint,
) ([]message.Message, error) {
	messages := append([]message.Message(nil), checkpoint.Messages...)
	calls := pendingToolCalls(messages)
	if len(calls) == 0 {
		return messages, nil
	}
	results, err := engine.ResumeToolCalls(ctx, calls)
	for _, result := range results {
		messages = append(messages, message.NewToolResult(result))
	}
	return messages, err
}

func pendingToolCallCount(messages []message.Message) int {
	return len(pendingToolCalls(messages))
}

func resultFromTerminalCheckpoint(checkpoint agent.TurnCheckpoint, schema json.RawMessage) (agent.Result, error) {
	text := ""
	thinking := ""
	for index := len(checkpoint.Messages) - 1; index >= 0; index-- {
		msg := checkpoint.Messages[index]
		if msg.Role != message.RoleAssistant {
			continue
		}
		text = msg.Text
		thinking = msg.Thinking
		break
	}
	var stopReason provider.StopReason
	if checkpoint.Step.ModelCall != nil {
		stopReason = checkpoint.Step.ModelCall.StopReason
	}
	result := agent.Result{
		Text:          text,
		Valid:         true,
		Steps:         []agent.Step{checkpoint.Step},
		Usage:         checkpoint.Usage,
		ToolCallsUsed: checkpoint.ToolCallsUsed,
		StopReason:    stopReason,
		Messages:      append([]message.Message(nil), checkpoint.Messages...),
		Thinking:      thinking,
	}
	if len(schema) == 0 {
		return result, nil
	}
	if err := agent.ValidateJSON(schema, json.RawMessage(text)); err != nil {
		failure := &agent.AgentFailure{
			Kind:      agent.FailureKindSchemaInvalid,
			Reason:    err.Error(),
			Retryable: false,
		}
		result.Valid = false
		result.Failure = failure
		return result, failure
	}
	result.Structured = append(json.RawMessage(nil), text...)
	return result, nil
}

func pendingToolCalls(messages []message.Message) []tool.Call {
	assistantIndex := -1
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role == message.RoleAssistant && len(messages[index].ToolCalls) > 0 {
			assistantIndex = index
			break
		}
	}
	if assistantIndex < 0 {
		return nil
	}
	completed := make(map[string]struct{})
	for _, msg := range messages[assistantIndex+1:] {
		if msg.Role == message.RoleTool && msg.ToolResult != nil {
			completed[msg.ToolResult.ToolCallID] = struct{}{}
		}
	}
	pending := make([]tool.Call, 0, len(messages[assistantIndex].ToolCalls))
	for _, call := range messages[assistantIndex].ToolCalls {
		if _, ok := completed[call.ID]; !ok {
			pending = append(pending, call)
		}
	}
	return pending
}

func (w AgentWorker) runEngineWithHeartbeat(ctx context.Context, engine agent.Engine, task api.Task, leaseID string, ttl time.Duration, sink stream.Sink) (agent.Result, error) {
	return withLeaseHeartbeat(ctx, w, leaseID, ttl, func(runCtx context.Context) (agent.Result, error) {
		// The task carries its OutputSchema through the durable store (see
		// api.Task.OutputSchema); rebuild the OutputPolicy from it so structured
		// validation actually runs on the worker path.
		policy := agent.OutputPolicy{
			Schema:   task.OutputSchema,
			Validate: len(task.OutputSchema) > 0,
		}
		var result agent.Result
		if sink == nil {
			result = engine.Run(runCtx, task, policy)
		} else {
			result = engine.RunStream(runCtx, task, policy, sink)
		}
		if result.Failure != nil {
			// AgentFailure satisfies error and preserves its underlying provider
			// or tool cause for errors.Is/errors.As.
			return result, result.Failure
		}
		return result, nil
	})
}

func withLeaseHeartbeat[T any](
	ctx context.Context,
	worker AgentWorker,
	leaseID string,
	ttl time.Duration,
	run func(context.Context) (T, error),
) (T, error) {
	runCtx, stopRun := context.WithCancel(ctx)
	heartbeatDone := make(chan error, 1)
	go func() {
		err := worker.heartbeatLoop(runCtx, leaseID, ttl)
		heartbeatDone <- err
		if err != nil {
			stopRun()
		}
	}()
	result, runErr := run(runCtx)
	stopRun()
	if err := <-heartbeatDone; err != nil {
		return result, fmt.Errorf("worker: lease heartbeat failed: %w", err)
	}
	return result, runErr
}

func (w AgentWorker) submitFailureReportHandled(ctx context.Context, task api.Task, lease api.TaskExecutionLease, cause error) bool {
	return w.submitFailure(ctx, task, lease, cause) == nil
}

func (w AgentWorker) submitSuccessReport(ctx context.Context, task api.Task, lease api.TaskExecutionLease, result agent.Result) (bool, error) {
	summary := strings.TrimSpace(result.Text)
	if summary == "" {
		summary = "completed"
	}
	report := api.TypedReport{
		Status:  api.ReportStatusSuccess,
		Summary: summary,
	}
	// Carry the schema-validated structured output onto the report so durable
	// downstream readers (routers, graph edges) observe the same
	// Task.Result.Structured the in-process report path exposes. result.Structured
	// is populated only when the terminal output validated against the task's
	// OutputSchema; without this, a schema-backed worker output is validated and
	// then silently dropped. The report contract is object-shaped (map[string]any),
	// matching the in-process path (see multiagent voting/router/graph consumers),
	// so a validated non-object output is surfaced as a worker failure rather than
	// quietly discarded.
	if len(result.Structured) > 0 {
		structured := map[string]any{}
		if err := json.Unmarshal(result.Structured, &structured); err != nil {
			err = fmt.Errorf("worker: validated structured output is not a JSON object: %w", err)
			return w.submitFailureReportHandled(ctx, task, lease, err), err
		}
		report.Structured = structured
	}
	output := api.BlackboardItem{
		ID:         taskOutputItemID(task),
		RunID:      task.RunID,
		TaskID:     task.ID,
		Type:       api.BlackboardItemTaskOutput,
		Source:     api.SourceIdentity{Type: api.SourceAgent, ID: w.AgentID},
		Visibility: api.BlackboardVisibilityAgentVisible,
		Key:        firstWriteTarget(task),
		Payload:    summary,
	}
	if err := w.writeTaskOutput(ctx, output); err != nil {
		return w.submitFailureReportHandled(ctx, task, lease, err), err
	}
	reportErr := w.Runner.SubmitTypedReport(ctx, api.SubmitTypedReportCommand{
		RunID:       task.RunID,
		TaskID:      task.ID,
		LeaseID:     lease.ID,
		HolderType:  api.HolderAgent,
		HolderID:    w.AgentID,
		TaskVersion: task.Version,
		Report:      report,
	})
	return reportErr == nil, reportErr
}

func taskOutputItemID(task api.Task) string {
	digest := sha256.Sum256([]byte(task.RunID + "\x00" + task.ID))
	return fmt.Sprintf("task-output-%x", digest)
}

func (w AgentWorker) writeTaskOutput(ctx context.Context, item api.BlackboardItem) error {
	items, err := w.Runner.SelectItems(ctx, item.RunID, api.BlackboardSelector{TaskID: item.TaskID})
	if err != nil {
		return fmt.Errorf("worker: inspect existing task output: %w", err)
	}
	for _, current := range items {
		sameSlot := current.RunID == item.RunID &&
			current.TaskID == item.TaskID &&
			current.Type == item.Type &&
			current.Source == item.Source &&
			current.Visibility == item.Visibility &&
			current.Key == item.Key
		if current.ID != item.ID && !sameSlot {
			continue
		}
		if sameSlot && current.Payload == item.Payload {
			return nil
		}
		return fmt.Errorf("worker: recovered task output %q conflicts with existing item %q", item.ID, current.ID)
	}
	return w.Runner.WriteItem(ctx, item)
}

func (w AgentWorker) heartbeatLoop(ctx context.Context, leaseID string, ttl time.Duration) error {
	interval := ttl / 3
	if interval <= 0 {
		interval = time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			err := w.Runner.HeartbeatTaskExecution(ctx, api.HeartbeatTaskExecutionCommand{
				LeaseID:  leaseID,
				HolderID: w.AgentID,
				TTL:      ttl,
			})
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
		}
	}
}

// workerContextBuilder layers durable checkpoint replay over a host context
// manager. A resumed execution starts from the last complete turn; a fresh
// execution preserves the host's prompt, compaction, and retrieval behavior.
type workerContextBuilder struct {
	worker AgentWorker
	run    api.Run
	inner  agent.ContextManager
	extra  []message.Message
	resume []message.Message
}

func (b workerContextBuilder) Build(ctx context.Context, task api.Task) ([]message.Message, error) {
	if len(b.resume) > 0 {
		return append([]message.Message(nil), b.resume...), nil
	}
	if b.inner != nil {
		messages, err := b.inner.Build(ctx, task)
		if err != nil {
			return nil, err
		}
		return append(messages, b.extra...), nil
	}
	inputs, err := b.worker.materializeInputs(ctx, task)
	if err != nil {
		return nil, err
	}
	return b.worker.buildMessages(b.run, task, inputs, b.extra), nil
}

func (b workerContextBuilder) Compact(ctx context.Context, history []message.Message) ([]message.Message, error) {
	if b.inner == nil {
		return history, nil
	}
	return b.inner.Compact(ctx, history)
}

func (b workerContextBuilder) CompactTo(ctx context.Context, history []message.Message, targetTokens int) ([]message.Message, error) {
	if inner, ok := b.inner.(agent.TargetContextManager); ok {
		return inner.CompactTo(ctx, history, targetTokens)
	}
	return b.Compact(ctx, history)
}

func (w AgentWorker) materializeInputs(ctx context.Context, task api.Task) ([]api.BlackboardItem, error) {
	selectors := task.ReadSelectors
	items := make([]api.BlackboardItem, 0)
	for _, selector := range selectors {
		selected, err := w.Runner.SelectItems(ctx, task.RunID, selector)
		if err != nil {
			return nil, err
		}
		items = append(items, selected...)
	}
	return items, nil
}

func (w AgentWorker) buildMessages(run api.Run, task api.Task, inputs []api.BlackboardItem, extra []message.Message) []message.Message {
	messages := make([]message.Message, 0, len(extra)+2)
	messages = append(messages, message.NewText(message.RoleSystem, "You are Venat agent "+w.AgentID+". Complete the assigned task and return a concise result."))
	prompt := task.Goal
	if strings.TrimSpace(prompt) == "" {
		prompt = run.Request
	}
	if len(task.Input) > 0 {
		prompt += "\n\nTask input:\n" + string(task.Input)
	}
	if len(inputs) > 0 {
		var b strings.Builder
		b.WriteString(prompt)
		b.WriteString("\n\nBlackboard inputs:")
		for _, item := range inputs {
			b.WriteString("\n- ")
			if item.Key != "" {
				b.WriteString(item.Key)
				b.WriteString(": ")
			}
			if item.Payload != "" {
				b.WriteString(item.Payload)
			} else {
				b.WriteString(item.Content)
			}
		}
		prompt = b.String()
	}
	messages = append(messages, message.NewText(message.RoleUser, prompt))
	messages = append(messages, extra...)
	return messages
}

func (w AgentWorker) submitFailure(ctx context.Context, task api.Task, lease api.TaskExecutionLease, cause error) error {
	if cause == nil {
		return nil
	}
	return w.Runner.SubmitTypedReport(context.WithoutCancel(ctx), api.SubmitTypedReportCommand{
		RunID:       task.RunID,
		TaskID:      task.ID,
		LeaseID:     lease.ID,
		HolderType:  api.HolderAgent,
		HolderID:    w.AgentID,
		TaskVersion: task.Version,
		Report:      failureReport(cause),
	})
}

// failureReport builds the failed TypedReport for a cause. When the cause is
// or wraps an AgentFailure it carries the agent loop's typed classification —
// the failure Kind plus its retry/escalate disposition — so a scheduler can
// branch on the failure mode rather than re-parsing Summary. A plain error
// leaves those fields empty.
func failureReport(cause error) api.TypedReport {
	report := api.TypedReport{
		Status:  api.ReportStatusFailed,
		Summary: cause.Error(),
	}
	if errors.Is(cause, context.Canceled) {
		report.Kind = "cancelled"
		return report
	}
	var failure *agent.AgentFailure
	if errors.As(cause, &failure) && failure != nil {
		report.Kind = string(failure.Kind)
		report.Retryable = failure.Retryable
		report.Escalatable = failure.Escalatable
	}
	return report
}

func firstWriteTarget(task api.Task) string {
	if len(task.WriteTargets) > 0 {
		return task.WriteTargets[0]
	}
	return "task_output"
}
