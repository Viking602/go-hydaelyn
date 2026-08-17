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
	ErrRunnerMissing            = errors.New("worker: runner missing")
	ErrProviderMissing          = errors.New("worker: provider missing")
	ErrAgentIDMissing           = errors.New("worker: agent id missing")
	ErrFailedCheckpoint         = errors.New("worker: checkpoint contains failed instance")
	ErrTaskExecutionUnavailable = errors.New("worker: task execution unavailable")
	ErrUsageUnpriced            = api.ErrUsageUnpriced
)

// TaskExecutionUnavailableError reports a transient lease or resource-claim
// denial. The task remains dispatched and may be retried after the conflicting
// owner releases its lease.
type TaskExecutionUnavailableError struct {
	TaskID         string
	ResourceClaims api.ResourceClaimDecision
}

func (e *TaskExecutionUnavailableError) Error() string {
	if e.ResourceClaims.Reason != "" {
		return fmt.Sprintf("%v: task %s resource claims denied: %s", ErrTaskExecutionUnavailable, e.TaskID, e.ResourceClaims.Reason)
	}
	return fmt.Sprintf("%v: task %s already has an active lease", ErrTaskExecutionUnavailable, e.TaskID)
}

func (e *TaskExecutionUnavailableError) Unwrap() error { return ErrTaskExecutionUnavailable }

// UsagePricer assigns deployment-defined abstract credits to one granular
// usage record. A nil pricer leaves Credits at zero.
type UsagePricer func(context.Context, api.UsageRecord) (credits int64, creditsKind string, err error)

type AgentWorker struct {
	Runner        *venat.Runner
	Engine        agent.Engine
	AgentID       string
	Model         string
	ToolMode      tool.Mode
	MaxIterations int
	TTL           time.Duration
	UsagePricer   UsagePricer
}

type ExecuteEnvelopeRequest struct {
	Envelope api.TaskEnvelope
	TTL      time.Duration
	Messages []message.Message
	Lease    api.TaskExecutionLease
	Sink     stream.Sink
	// OnLeaseAcquired runs after the execution lease and resource claims are
	// committed but before the envelope is acknowledged or the agent starts.
	// Hosts may use it to bind lease-scoped tool authorization state.
	OnLeaseAcquired func(api.TaskExecutionLease) error
}

type ExecutionState string

const (
	ExecutionCompleted ExecutionState = "completed"
	ExecutionFailed    ExecutionState = "failed"
	ExecutionSuspended ExecutionState = "suspended"
	ExecutionCancelled ExecutionState = "cancelled"
)

type SuspensionKind string

const (
	SuspensionApproval       SuspensionKind = "approval"
	SuspensionReconciliation SuspensionKind = "reconciliation"
	SuspensionUserInput      SuspensionKind = "user_input"
	SuspensionRequested      SuspensionKind = "requested"
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
		if errors.Is(runErr, ErrTaskExecutionUnavailable) {
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

func (w AgentWorker) ExecuteEnvelope(ctx context.Context, req ExecuteEnvelopeRequest) (outcome ExecutionOutcome, err error) {
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
	execCtx, stopHeartbeat, err := startLeaseHeartbeat(ctx, ttl, leaseRenewalPulse(lease.ExpiresAt, ttl, func(hbCtx context.Context) error {
		return w.Runner.HeartbeatTaskExecution(hbCtx, api.HeartbeatTaskExecutionCommand{
			LeaseID:  lease.ID,
			HolderID: w.AgentID,
			TTL:      ttl,
		})
	}, func(hbCtx context.Context) error {
		return leaseStillActive(hbCtx, w.Runner, lease.ID, w.AgentID)
	}))
	if err != nil {
		return ExecutionOutcome{}, err
	}
	heartbeatStopped := false
	defer func() {
		if heartbeatStopped {
			return
		}
		if stopErr := stopHeartbeat(); stopErr != nil {
			err = errors.Join(err, stopErr)
		}
	}()
	if req.OnLeaseAcquired != nil {
		if hookErr := req.OnLeaseAcquired(lease); hookErr != nil {
			return ExecutionOutcome{}, fmt.Errorf("worker: observe acquired execution lease: %w", hookErr)
		}
	}

	if err := w.ackEnvelope(execCtx, req.Envelope); err != nil {
		return ExecutionOutcome{}, err
	}
	task, run, err := w.loadEnvelopeTaskRun(execCtx, req.Envelope)
	if err != nil {
		return ExecutionOutcome{}, err
	}

	engine := w.executionEngine(task, lease)
	defer w.Runner.RemoveToolsForInvocation(task.RunID, task.ID, api.HolderAgent, w.AgentID)
	checkpoint, hasCheckpoint, err := w.latestExecutionCheckpoint(execCtx, task)
	if err != nil {
		heartbeatErr := stopHeartbeat()
		heartbeatStopped = true
		err = combineExecutionErrors(err, heartbeatErr)
		leaseHandled = w.submitFailureReportHandled(ctx, task, lease, err)
		return failedExecutionOutcome(task, lease, agent.Result{}, err), errors.Join(err, heartbeatErr)
	}
	started := time.Now()
	result, runErr := w.runCheckpointedTask(
		execCtx, task, run, lease, engine, checkpoint, hasCheckpoint,
		req.Messages, req.Sink, started,
	)
	// Stop the loop before any terminal report/release so an overlapping
	// heartbeat cannot observe the released lease and poison a committed result.
	// Finalize on the parent context so a heartbeat cancel does not hide
	// SingleRunner suspend/cancel causes or block the durable report.
	heartbeatErr := stopHeartbeat()
	heartbeatStopped = true
	// Heartbeat cancel of execCtx usually surfaces as context.Canceled from
	// the engine. Prefer the heartbeat/store cause so the durable failure
	// report is not stored as kind "cancelled".
	runErr = combineExecutionErrors(runErr, heartbeatErr)
	if runErr != nil {
		outcome, handled, outcomeErr := w.executionErrorOutcome(
			ctx, task, lease, result, runErr, started,
		)
		leaseHandled = handled
		return outcome, errors.Join(outcomeErr, heartbeatErr)
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

func (w AgentWorker) runCheckpointedTask(
	ctx context.Context,
	task api.Task,
	run api.Run,
	lease api.TaskExecutionLease,
	engine agent.Engine,
	checkpoint agent.ExecutionCheckpointRecord,
	hasCheckpoint bool,
	messages []message.Message,
	sink stream.Sink,
	started time.Time,
) (agent.Result, error) {
	baseContextBuilder := engine.ContextBuilder
	if hasCheckpoint {
		engine.OperationTurn = max(engine.OperationTurn, checkpoint.Checkpoint.NextOperationTurn)
	}
	resumeMessages := checkpoint.Checkpoint.Messages
	engine.ContextBuilder = workerContextBuilder{
		worker: w, run: run, inner: baseContextBuilder, extra: messages, resume: resumeMessages,
	}
	engine.StepRecorder = w.stepRecorder(task, lease, started, engine, engine.StepRecorder)
	engine.CheckpointRecorder = w.checkpointRecorder(task, lease, engine.CheckpointRecorder)
	recoveredTerminal := hasCheckpoint &&
		!checkpoint.Checkpoint.PendingToolCalls &&
		checkpoint.Checkpoint.Step.Decision == agent.StepDecisionFinish
	if recoveredTerminal {
		return resultFromTerminalCheckpoint(checkpoint.Checkpoint, task.OutputSchema)
	}

	var result agent.Result
	var runErr error
	if hasCheckpoint && checkpoint.Checkpoint.PendingToolCalls {
		resumeMessages, runErr = w.resumePendingToolCalls(ctx, engine, checkpoint.Checkpoint)
		engine.ContextBuilder = workerContextBuilder{
			worker: w, run: run, inner: baseContextBuilder, extra: messages, resume: resumeMessages,
		}
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
	if runErr != nil {
		return result, runErr
	}
	result, runErr = w.runEngineWithHeartbeat(ctx, engine, task, sink)
	if usageErr := w.appendPartialModelUsage(ctx, task, lease, engine, result); usageErr != nil {
		runErr = errors.Join(runErr, usageErr)
	}
	return result, runErr
}

func (w AgentWorker) executionErrorOutcome(
	ctx context.Context,
	task api.Task,
	lease api.TaskExecutionLease,
	result agent.Result,
	runErr error,
	started time.Time,
) (ExecutionOutcome, bool, error) {
	if cause := context.Cause(ctx); errors.Is(cause, ErrSingleRunCancelled) {
		return ExecutionOutcome{
			State: ExecutionCancelled, RunID: task.RunID, TaskID: task.ID,
			LeaseID: lease.ID, Result: result,
		}, false, errors.Join(runErr, cause)
	}
	if suspension := w.executionSuspension(ctx, task, runErr); suspension != nil {
		checkpointErr := w.recordSuspensionCheckpoint(context.WithoutCancel(ctx), task, lease, result, time.Since(started))
		return ExecutionOutcome{
			State: ExecutionSuspended, RunID: task.RunID, TaskID: task.ID,
			LeaseID: lease.ID, Result: result, Suspension: suspension,
		}, false, errors.Join(runErr, checkpointErr, context.Cause(ctx))
	}
	handled := w.submitFailureReportHandled(ctx, task, lease, runErr)
	return failedExecutionOutcome(task, lease, result, runErr), handled, runErr
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
	if cause := context.Cause(ctx); errors.Is(cause, ErrSingleRunSuspended) {
		return &Suspension{Kind: SuspensionRequested, Reason: cause.Error()}
	}
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

func stableUsageID(parts ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return fmt.Sprintf("usage-%x", digest[:16])
}

func priceUsageRecord(ctx context.Context, pricer UsagePricer, record api.UsageRecord) (api.UsageRecord, error) {
	if pricer == nil {
		record.PricingState = api.UsagePricingStatePriced
		return record, nil
	}
	credits, creditsKind, err := pricer(ctx, record)
	if err != nil {
		record.PricingState = api.UsagePricingStateUnpriced
		if record.Metadata == nil {
			record.Metadata = make(map[string]string)
		}
		record.Metadata["pricingError"] = err.Error()
		return record, fmt.Errorf("%w: price %s usage: %w", ErrUsageUnpriced, record.Kind, err)
	}
	if credits < 0 {
		record.PricingState = api.UsagePricingStateUnpriced
		if record.Metadata == nil {
			record.Metadata = make(map[string]string)
		}
		record.Metadata["pricingError"] = "negative credits"
		return record, fmt.Errorf("%w: price %s usage: negative credits", ErrUsageUnpriced, record.Kind)
	}
	record.Credits = credits
	record.CreditsKind = creditsKind
	record.PricingState = api.UsagePricingStatePriced
	return record, nil
}

func (w AgentWorker) usageRecordsForStep(
	ctx context.Context,
	task api.Task,
	executionID string,
	engine agent.Engine,
	step agent.Step,
) ([]api.UsageRecord, error) {
	providerName := ""
	if engine.Provider != nil {
		providerName = engine.Provider.Metadata().Name
	}
	if call := step.ModelCall; call != nil && call.Provider != "" {
		providerName = call.Provider
	}
	records := make([]api.UsageRecord, 0, 1+len(step.ToolCalls))
	var pricingErr error
	if call := step.ModelCall; call != nil {
		totalTokens := call.TotalTokens
		if totalTokens == 0 {
			totalTokens = call.InputTokens + call.OutputTokens
		}
		record, err := priceUsageRecord(ctx, w.UsagePricer, api.UsageRecord{
			ID: stableUsageID(
				task.RunID, task.ID, w.AgentID, executionID,
				string(api.UsageKindModelCall), fmt.Sprint(step.Index),
			),
			RunID: task.RunID, TaskID: task.ID, AgentID: w.AgentID,
			Kind: api.UsageKindModelCall, Provider: providerName, Model: call.Model,
			InputTokens: call.InputTokens, CachedInputTokens: call.CachedInputTokens,
			CacheWriteInputTokens: call.CacheWriteInputTokens, OutputTokens: call.OutputTokens,
			TotalTokens: totalTokens, Steps: 1,
			Metadata: map[string]string{
				"executionId": executionID,
				"stepIndex":   fmt.Sprint(step.Index),
			},
		})
		if err != nil {
			pricingErr = errors.Join(pricingErr, err)
		}
		records = append(records, record)
	}
	for index, call := range step.ToolCalls {
		inputHash := sha256.Sum256(call.Arguments)
		record, err := priceUsageRecord(ctx, w.UsagePricer, api.UsageRecord{
			ID: stableUsageID(
				task.RunID, task.ID, w.AgentID, executionID,
				string(api.UsageKindToolCall), fmt.Sprint(step.Index), fmt.Sprint(index),
			),
			RunID: task.RunID, TaskID: task.ID, AgentID: w.AgentID,
			Kind: api.UsageKindToolCall, ToolName: call.Name, ToolCalls: 1,
			Metadata: map[string]string{
				"executionId": executionID,
				"stepIndex":   fmt.Sprint(step.Index),
				"ordinal":     fmt.Sprint(index),
				"inputHash":   fmt.Sprintf("%x", inputHash[:]),
			},
		})
		if err != nil {
			pricingErr = errors.Join(pricingErr, err)
		}
		records = append(records, record)
	}
	return records, pricingErr
}

func (w AgentWorker) appendPartialModelUsage(ctx context.Context, task api.Task, lease api.TaskExecutionLease, engine agent.Engine, result agent.Result) error {
	recorded := provider.Usage{}
	for _, step := range result.Steps {
		if step.ModelCall == nil {
			continue
		}
		recorded = recorded.Add(provider.Usage{
			InputTokens: step.ModelCall.InputTokens, CachedInputTokens: step.ModelCall.CachedInputTokens,
			CacheWriteInputTokens: step.ModelCall.CacheWriteInputTokens,
			OutputTokens:          step.ModelCall.OutputTokens, TotalTokens: step.ModelCall.TotalTokens,
		})
	}
	inputTokens := max(0, result.Usage.InputTokens-recorded.InputTokens)
	cachedInputTokens := max(0, result.Usage.CachedInputTokens-recorded.CachedInputTokens)
	cacheWriteInputTokens := max(0, result.Usage.CacheWriteInputTokens-recorded.CacheWriteInputTokens)
	outputTokens := max(0, result.Usage.OutputTokens-recorded.OutputTokens)
	totalTokens := max(0, result.Usage.TotalTokens-recorded.TotalTokens)
	if inputTokens == 0 && outputTokens == 0 && totalTokens == 0 {
		return nil
	}
	providerName := ""
	if engine.Provider != nil {
		providerName = engine.Provider.Metadata().Name
	}
	record, pricingErr := priceUsageRecord(ctx, w.UsagePricer, api.UsageRecord{
		ID:    stableUsageID(task.RunID, task.ID, w.AgentID, string(api.UsageKindModelCall), "partial", lease.ID),
		RunID: task.RunID, TaskID: task.ID, AgentID: w.AgentID,
		Kind: api.UsageKindModelCall, Provider: providerName, Model: engine.Model,
		InputTokens: inputTokens, CachedInputTokens: cachedInputTokens,
		CacheWriteInputTokens: cacheWriteInputTokens, OutputTokens: outputTokens,
		TotalTokens: totalTokens, Metadata: map[string]string{"partial": "true"},
	})
	appendErr := w.Runner.AppendUsage(context.WithoutCancel(ctx), record)
	return errors.Join(appendErr, pricingErr)
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
	result, err := w.Runner.AcquireTaskExecutionWithClaims(ctx, api.AcquireTaskExecutionCommand{
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
	if !result.Acquired {
		return api.TaskExecutionLease{}, &TaskExecutionUnavailableError{
			TaskID: req.Envelope.TaskID, ResourceClaims: result.ResourceClaims,
		}
	}
	return result.Lease, nil
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
		UsagePricer: w.UsagePricer,
	}.ToolBus()
	return engine
}

func (w AgentWorker) stepRecorder(task api.Task, lease api.TaskExecutionLease, started time.Time, engine agent.Engine, caller agent.StepRecorder) agent.StepRecorder {
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
		usageRecords, pricingErr := w.usageRecordsForStep(ctx, task, lease.ID, engine, step)
		appendErr := w.Runner.AppendTaskExecutionEvent(context.WithoutCancel(ctx), api.AppendTaskExecutionEventCommand{
			RunID: task.RunID, TaskID: task.ID, LeaseID: lease.ID,
			HolderType: lease.HolderType, HolderID: lease.HolderID,
			TaskVersion: task.Version, Event: event, UsageRecords: usageRecords,
		})
		return errors.Join(appendErr, pricingErr)
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
	elapsed time.Duration,
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
			WallClock: elapsed,
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

func (w AgentWorker) runEngineWithHeartbeat(ctx context.Context, engine agent.Engine, task api.Task, sink stream.Sink) (agent.Result, error) {
	// The task carries its OutputSchema through the durable store (see
	// api.Task.OutputSchema); rebuild the OutputPolicy from it so structured
	// validation actually runs on the worker path.
	policy := agent.OutputPolicy{
		Schema:   task.OutputSchema,
		Validate: len(task.OutputSchema) > 0,
	}
	var result agent.Result
	if sink == nil {
		result = engine.Run(ctx, task, policy)
	} else {
		result = engine.RunStream(ctx, task, policy, sink)
	}
	if result.Failure != nil {
		// AgentFailure satisfies error and preserves its underlying provider
		// or tool cause for errors.Is/errors.As.
		return result, result.Failure
	}
	return result, nil
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

func leaseHeartbeatInterval(ttl time.Duration) time.Duration {
	interval := ttl / 3
	if interval <= 0 {
		return time.Millisecond
	}
	return interval
}

func combineExecutionErrors(runErr, heartbeatErr error) error {
	if heartbeatErr == nil {
		return runErr
	}
	if runErr == nil || onlyContextCanceled(runErr) {
		return heartbeatErr
	}
	if errors.Is(runErr, heartbeatErr) {
		return runErr
	}
	return errors.Join(heartbeatErr, runErr)
}

func onlyContextCanceled(err error) bool {
	if err == nil {
		return false
	}
	var failure *agent.AgentFailure
	if errors.As(err, &failure) && failure != nil {
		return false
	}
	return canceledLeavesOnly(err)
}

func canceledLeavesOnly(err error) bool {
	if err == nil {
		return true
	}
	if multi, ok := err.(interface{ Unwrap() []error }); ok {
		leaves := multi.Unwrap()
		if len(leaves) == 0 {
			return errors.Is(err, context.Canceled)
		}
		sawLeaf := false
		for _, inner := range leaves {
			if inner == nil {
				continue
			}
			sawLeaf = true
			if !canceledLeavesOnly(inner) {
				return false
			}
		}
		return sawLeaf || errors.Is(err, context.Canceled)
	}
	if single, ok := err.(interface{ Unwrap() error }); ok {
		if inner := single.Unwrap(); inner != nil {
			return canceledLeavesOnly(inner)
		}
	}
	return errors.Is(err, context.Canceled)
}

func leaseStillActive(ctx context.Context, runner *venat.Runner, leaseID, holderID string) error {
	if runner == nil {
		return ErrRunnerMissing
	}
	uow, err := runner.Begin(ctx)
	if err != nil {
		return err
	}
	lease, loadErr := uow.Leases().LoadLease(ctx, leaseID)
	rollbackErr := uow.Rollback(ctx)
	if loadErr != nil {
		return loadErr
	}
	if rollbackErr != nil {
		return rollbackErr
	}
	if lease.HolderID != holderID || lease.Status != api.LeaseStatusActive || lease.ExpiresAt.IsZero() || !lease.ExpiresAt.After(time.Now().UTC()) {
		return api.ErrLeaseNotActive
	}
	return nil
}

func leaseRenewalPulse(expiresAt time.Time, ttl time.Duration, heartbeat, validate func(context.Context) error) func(context.Context) error {
	current := expiresAt
	return func(ctx context.Context) error {
		next := time.Now().UTC().Add(ttl)
		if !next.After(current) {
			if validate == nil {
				return nil
			}
			return validate(ctx)
		}
		if err := heartbeat(ctx); err != nil {
			return err
		}
		current = next
		return nil
	}
}

func pulseLeaseHeartbeat(ctx context.Context, pulse func(context.Context) error) error {
	if err := pulse(ctx); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	return nil
}

func tickLeaseHeartbeat(ctx context.Context, ttl time.Duration, pulse func(context.Context) error) error {
	ticker := time.NewTicker(leaseHeartbeatInterval(ttl))
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := pulseLeaseHeartbeat(ctx, pulse); err != nil {
				return err
			}
		}
	}
}

func runLeaseHeartbeat(ctx context.Context, ttl time.Duration, pulse func(context.Context) error) error {
	if err := pulseLeaseHeartbeat(ctx, pulse); err != nil {
		return err
	}
	return tickLeaseHeartbeat(ctx, ttl, pulse)
}

func startLeaseHeartbeat(ctx context.Context, ttl time.Duration, pulse func(context.Context) error) (context.Context, func() error, error) {
	if err := pulseLeaseHeartbeat(ctx, pulse); err != nil {
		return ctx, func() error { return nil }, fmt.Errorf("worker: lease heartbeat failed: %w", err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		err := tickLeaseHeartbeat(runCtx, ttl, pulse)
		done <- err
		if err != nil {
			cancel()
		}
	}()
	return runCtx, func() error {
		cancel()
		if err := <-done; err != nil {
			return fmt.Errorf("worker: lease heartbeat failed: %w", err)
		}
		return nil
	}, nil
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
	if onlyContextCanceled(cause) {
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
