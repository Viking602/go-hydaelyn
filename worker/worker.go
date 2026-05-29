// Package worker provides optional glue between the Hydaelyn runner and the
// single-agent engine.
package worker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Viking602/go-hydaelyn"
	"github.com/Viking602/go-hydaelyn/agent"
	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/tool"
)

var (
	ErrRunnerMissing   = errors.New("worker: runner missing")
	ErrProviderMissing = errors.New("worker: provider missing")
	ErrAgentIDMissing  = errors.New("worker: agent id missing")
)

type AgentWorker struct {
	Runner        *hydaelyn.Runner
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
}

func (w AgentWorker) ExecuteEnvelope(ctx context.Context, req ExecuteEnvelopeRequest) error {
	if err := w.validateExecuteEnvelope(); err != nil {
		return err
	}
	ttl := w.executeEnvelopeTTL(req)
	lease, err := w.acquireEnvelopeLease(ctx, req, ttl)
	if err != nil {
		return err
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
		return err
	}
	task, run, err := w.loadEnvelopeTaskRun(ctx, req.Envelope)
	if err != nil {
		return err
	}

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
	engine.ContextBuilder = workerContextBuilder{worker: w, run: run, extra: req.Messages}

	result, runErr := w.runEngineWithHeartbeat(ctx, engine, task, lease.ID, ttl)
	if runErr != nil {
		leaseHandled = w.submitFailureReportHandled(ctx, task, lease, runErr)
		return runErr
	}

	leaseHandled, err = w.submitSuccessReport(ctx, task, lease, result)
	return err
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

func (w AgentWorker) runEngineWithHeartbeat(ctx context.Context, engine agent.Engine, task api.Task, leaseID string, ttl time.Duration) (agent.Result, error) {
	heartbeatCtx, stopHeartbeat := context.WithCancel(ctx)
	heartbeatDone := make(chan struct{})
	go w.heartbeatLoop(heartbeatCtx, leaseID, ttl, heartbeatDone)
	// The task carries its OutputSchema through the durable store (see
	// api.Task.OutputSchema); rebuild the OutputPolicy from it so structured
	// validation actually runs on the worker path. This mirrors the in-process
	// Dispatch.OutputPolicy that multiagent.buildDispatch constructs (Schema +
	// Validate, no repair).
	result := engine.Run(ctx, task, agent.OutputPolicy{
		Schema:   task.OutputSchema,
		Validate: len(task.OutputSchema) > 0,
	})
	stopHeartbeat()
	<-heartbeatDone
	if result.Failure != nil {
		// AgentFailure satisfies the error interface; its Unwrap chain
		// surfaces the underlying provider/tool error so worker callers
		// can errors.Is against it (e.g. the failure-injection test).
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
	if err := w.Runner.WriteItem(ctx, api.BlackboardItem{
		RunID:      task.RunID,
		TaskID:     task.ID,
		Type:       api.BlackboardItemTaskOutput,
		Source:     api.SourceIdentity{Type: api.SourceAgent, ID: w.AgentID},
		Visibility: api.BlackboardVisibilityAgentVisible,
		Key:        firstWriteTarget(task),
		Payload:    summary,
	}); err != nil {
		return w.submitFailureReportHandled(ctx, task, lease, err), err
	}
	reportErr := w.Runner.SubmitTypedReport(ctx, api.SubmitTypedReportCommand{
		RunID:       task.RunID,
		TaskID:      task.ID,
		LeaseID:     lease.ID,
		HolderType:  api.HolderAgent,
		HolderID:    w.AgentID,
		TaskVersion: task.Version,
		Report: api.TypedReport{
			Status:  api.ReportStatusSuccess,
			Summary: summary,
		},
	})
	return reportErr == nil, reportErr
}

func (w AgentWorker) heartbeatLoop(ctx context.Context, leaseID string, ttl time.Duration, done chan<- struct{}) {
	defer close(done)
	interval := ttl / 3
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = w.Runner.HeartbeatTaskExecution(ctx, api.HeartbeatTaskExecutionCommand{
				LeaseID: leaseID,
				TTL:     ttl,
			})
		}
	}
}

// workerContextBuilder adapts the worker's task-message + blackboard
// fan-in logic into the agent.ContextManager surface agent.Engine.Run
// reads at task start. Compact is a no-op pass-through; tightening it
// lands when LoopPolicy.MaxTokens is wired in Phase 2.
type workerContextBuilder struct {
	worker AgentWorker
	run    api.Run
	extra  []message.Message
}

func (b workerContextBuilder) Build(ctx context.Context, task api.Task) ([]message.Message, error) {
	inputs, err := b.worker.materializeInputs(ctx, task)
	if err != nil {
		return nil, err
	}
	return b.worker.buildMessages(b.run, task, inputs, b.extra), nil
}

func (workerContextBuilder) Compact(_ context.Context, history []message.Message) ([]message.Message, error) {
	return history, nil
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
	messages = append(messages, message.NewText(message.RoleSystem, "You are Hydaelyn agent "+w.AgentID+". Complete the assigned task and return a concise result."))
	prompt := task.Goal
	if strings.TrimSpace(prompt) == "" {
		prompt = run.Request
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
	return w.Runner.SubmitTypedReport(ctx, api.SubmitTypedReportCommand{
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
