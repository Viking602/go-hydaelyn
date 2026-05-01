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
	Envelope hydaelyn.TaskEnvelope
	TTL      time.Duration
	Messages []message.Message
}

func (w AgentWorker) ExecuteEnvelope(ctx context.Context, req ExecuteEnvelopeRequest) error {
	if w.Runner == nil {
		return ErrRunnerMissing
	}
	if w.Engine.Provider == nil {
		return ErrProviderMissing
	}
	if strings.TrimSpace(w.AgentID) == "" {
		return ErrAgentIDMissing
	}
	ttl := req.TTL
	if ttl <= 0 {
		ttl = w.TTL
	}
	if ttl <= 0 {
		ttl = time.Minute
	}
	lease, acquired, err := w.Runner.AcquireTaskExecution(ctx, hydaelyn.AcquireTaskExecutionCommand{
		RunID:      req.Envelope.RunID,
		TaskID:     req.Envelope.TaskID,
		EnvelopeID: req.Envelope.ID,
		HolderType: hydaelyn.HolderAgent,
		HolderID:   w.AgentID,
		TTL:        ttl,
	})
	if err != nil {
		return err
	}
	if !acquired {
		return fmt.Errorf("worker: task %s already has an active lease", req.Envelope.TaskID)
	}
	leaseHandled := false
	defer func() {
		if leaseHandled {
			return
		}
		_ = w.Runner.ReleaseTaskExecution(context.WithoutCancel(ctx), hydaelyn.ReleaseTaskExecutionCommand{
			LeaseID:  lease.ID,
			HolderID: w.AgentID,
		})
	}()
	if req.Envelope.ID != "" {
		if err := w.Runner.AckEnvelope(ctx, hydaelyn.AckEnvelopeCommand{EnvelopeID: req.Envelope.ID, HolderID: w.AgentID}); err != nil {
			return err
		}
	}
	task, err := w.Runner.Task(ctx, req.Envelope.RunID, req.Envelope.TaskID)
	if err != nil {
		return err
	}
	run, err := w.Runner.Run(ctx, req.Envelope.RunID)
	if err != nil {
		return err
	}
	inputs, err := w.materializeInputs(ctx, task)
	if err != nil {
		if reportErr := w.submitFailure(ctx, task, lease, err); reportErr == nil {
			leaseHandled = true
		}
		return err
	}
	messages := w.buildMessages(run, task, inputs, req.Messages)
	engine := w.Engine
	if engine.Tools != nil {
		engine.Tools = GovernedToolBus{
			Runner:      w.Runner,
			Bus:         engine.Tools,
			RunID:       task.RunID,
			TaskID:      task.ID,
			LeaseID:     lease.ID,
			HolderType:  hydaelyn.HolderAgent,
			HolderID:    w.AgentID,
			TaskVersion: task.Version,
		}.ToolBus()
	}
	heartbeatCtx, stopHeartbeat := context.WithCancel(ctx)
	heartbeatDone := make(chan struct{})
	go w.heartbeatLoop(heartbeatCtx, lease.ID, ttl, heartbeatDone)
	result, err := engine.Run(ctx, agent.Input{
		Model:         w.Model,
		Messages:      messages,
		ToolMode:      w.ToolMode,
		MaxIterations: w.MaxIterations,
	})
	stopHeartbeat()
	<-heartbeatDone
	if err != nil {
		if reportErr := w.submitFailure(ctx, task, lease, err); reportErr == nil {
			leaseHandled = true
		}
		return err
	}
	summary := finalAssistantText(result.Messages)
	if strings.TrimSpace(summary) == "" {
		summary = "completed"
	}
	if err := w.Runner.WriteItem(ctx, hydaelyn.BlackboardItem{
		RunID:      task.RunID,
		TaskID:     task.ID,
		Type:       hydaelyn.BlackboardItemTaskOutput,
		Source:     hydaelyn.SourceIdentity{Type: hydaelyn.SourceAgent, ID: w.AgentID},
		Visibility: hydaelyn.BlackboardVisibilityAgentVisible,
		Key:        firstWriteTarget(task),
		Payload:    summary,
	}); err != nil {
		if reportErr := w.submitFailure(ctx, task, lease, err); reportErr == nil {
			leaseHandled = true
		}
		return err
	}
	reportErr := w.Runner.SubmitTypedReport(ctx, hydaelyn.SubmitTypedReportCommand{
		RunID:       task.RunID,
		TaskID:      task.ID,
		LeaseID:     lease.ID,
		HolderType:  hydaelyn.HolderAgent,
		HolderID:    w.AgentID,
		TaskVersion: task.Version,
		Report: hydaelyn.TypedReport{
			Status:  hydaelyn.ReportStatusSuccess,
			Summary: summary,
		},
	})
	if reportErr == nil {
		leaseHandled = true
	}
	return reportErr
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
			_ = w.Runner.HeartbeatTaskExecution(ctx, hydaelyn.HeartbeatTaskExecutionCommand{
				LeaseID: leaseID,
				TTL:     ttl,
			})
		}
	}
}

func (w AgentWorker) materializeInputs(ctx context.Context, task hydaelyn.Task) ([]hydaelyn.BlackboardItem, error) {
	selectors := task.ReadSelectors
	items := make([]hydaelyn.BlackboardItem, 0)
	for _, selector := range selectors {
		selected, err := w.Runner.SelectItems(ctx, task.RunID, selector)
		if err != nil {
			return nil, err
		}
		items = append(items, selected...)
	}
	return items, nil
}

func (w AgentWorker) buildMessages(run hydaelyn.Run, task hydaelyn.Task, inputs []hydaelyn.BlackboardItem, extra []message.Message) []message.Message {
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

func (w AgentWorker) submitFailure(ctx context.Context, task hydaelyn.Task, lease hydaelyn.TaskExecutionLease, cause error) error {
	if cause == nil {
		return nil
	}
	return w.Runner.SubmitTypedReport(ctx, hydaelyn.SubmitTypedReportCommand{
		RunID:       task.RunID,
		TaskID:      task.ID,
		LeaseID:     lease.ID,
		HolderType:  hydaelyn.HolderAgent,
		HolderID:    w.AgentID,
		TaskVersion: task.Version,
		Report: hydaelyn.TypedReport{
			Status:  hydaelyn.ReportStatusFailed,
			Summary: cause.Error(),
		},
	})
}

func finalAssistantText(messages []message.Message) string {
	for idx := len(messages) - 1; idx >= 0; idx-- {
		if messages[idx].Role == message.RoleAssistant && strings.TrimSpace(messages[idx].Text) != "" {
			return messages[idx].Text
		}
	}
	return ""
}

func firstWriteTarget(task hydaelyn.Task) string {
	if len(task.WriteTargets) > 0 {
		return task.WriteTargets[0]
	}
	return "task_output"
}
