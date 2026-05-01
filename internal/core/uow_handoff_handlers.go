package core

import (
	"context"
	"slices"
	"time"

	commandbus "github.com/Viking602/go-hydaelyn/internal/core/command"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
)

func registerHandoffUoWCommandHandlers(runtime *Runtime) {
	commandbus.Register[HandoffCommand](runtime.commandBus, handoffHandler{runtime: runtime})
}

type handoffResult struct {
	Task           Task
	Envelope       TaskEnvelope
	BlackboardItem BlackboardItem
	HasContext     bool
	FromAgentID    string
	ToAgentID      string
	Reason         string
}

type handoffHandler struct{ runtime *Runtime }

func (handoffHandler) Name() string { return HandoffCommand{}.CommandName() }

func (h handoffHandler) Handle(ctx context.Context, uow ports.FullUnitOfWork, cmd HandoffCommand) (any, error) {
	run, err := uow.Runs().LoadRun(ctx, cmd.RunID)
	if err != nil {
		return nil, err
	}
	if isTerminalRun(run.Status) {
		return nil, ErrTerminalState
	}
	task, err := uow.Tasks().LoadTask(ctx, cmd.RunID, cmd.TaskID)
	if err != nil {
		return nil, err
	}
	request := &HandoffRequest{RunID: cmd.RunID, TaskID: cmd.TaskID, FromAgentID: cmd.FromAgentID, ToAgentID: cmd.ToAgentID, ContextSummary: cmd.HandoffContext, TaskVersion: cmd.TaskVersion}
	if _, err := h.runtime.authorizeUoW(ctx, uow, PolicyRequest{Operation: PolicyOperationHandoff, RunID: cmd.RunID, TaskID: cmd.TaskID, Actor: SourceIdentity{Type: SourceAgent, ID: cmd.FromAgentID}, Handoff: request}); err != nil {
		return nil, err
	}
	if err := h.runtime.recordEndedTraceUoW(ctx, uow, cmd.RunID, cmd.TaskID, "handoff.request", "handoff"); err != nil {
		return nil, err
	}
	return h.apply(ctx, uow, task, request, cmd.HandoffContext)
}

func (h handoffHandler) apply(ctx context.Context, uow ports.FullUnitOfWork, task Task, request *HandoffRequest, fallbackContext string) (handoffResult, error) {
	if isTerminalTask(task.Status) {
		return handoffResult{}, ErrTerminalState
	}
	if request.TaskVersion != 0 && request.TaskVersion != task.Version {
		return handoffResult{}, ErrStaleTaskVersion
	}
	fromAgentID := request.FromAgentID
	if fromAgentID == "" {
		fromAgentID = task.OwnerAgentID
	}
	if task.OwnerAgentID != fromAgentID {
		return handoffResult{}, ErrOwnerMismatch
	}
	if request.ToAgentID == "" {
		return handoffResult{}, ErrInvalidCommand
	}
	if task.HandoffCount >= maxHandoffDepth {
		return handoffResult{}, ErrHandoffDepthExceeded
	}
	if containsString(task.OwnerHistory, request.ToAgentID) {
		return handoffResult{}, ErrHandoffCycle
	}
	contextSummary := request.ContextSummary
	if contextSummary == "" {
		contextSummary = fallbackContext
	}
	now := time.Now().UTC()
	if err := uow.Events().AppendEvent(ctx, Event{RunID: task.RunID, TaskID: task.ID, Type: EventHandoffRequested, Payload: map[string]any{"fromAgentId": fromAgentID, "toAgentId": request.ToAgentID, "reason": request.Reason}, RecordedAt: now}); err != nil {
		return handoffResult{}, err
	}
	result := handoffResult{FromAgentID: fromAgentID, ToAgentID: request.ToAgentID, Reason: request.Reason}
	if contextSummary != "" {
		item := BlackboardItem{RunID: task.RunID, TaskID: task.ID, Type: BlackboardItemHandoffContext, Source: SourceIdentity{Type: SourceAgent, ID: fromAgentID}, Visibility: BlackboardVisibilityAgentVisible, Key: "handoff_context", Content: contextSummary, Payload: contextSummary, Version: task.Version, CreatedAt: now}
		if err := uow.Blackboard().WriteItem(ctx, item); err != nil {
			return handoffResult{}, err
		}
		if err := h.runtime.recordEndedTraceUoW(ctx, uow, item.RunID, item.TaskID, "blackboard.write", "blackboard"); err != nil {
			return handoffResult{}, err
		}
		if err := appendBlackboardWrittenEventUoW(ctx, uow, item); err != nil {
			return handoffResult{}, err
		}
		result.BlackboardItem = item
		result.HasContext = true
	}
	task.OwnerAgentID = request.ToAgentID
	task.OwnerComponent = ""
	task.HandoffCount++
	task.OwnerHistory = append(slices.Clone(task.OwnerHistory), request.ToAgentID)
	next, err := transitionTaskPure(task, TaskStatusDispatched, true)
	if err != nil {
		return handoffResult{}, err
	}
	if err := uow.Tasks().SaveTask(ctx, next); err != nil {
		return handoffResult{}, err
	}
	if err := uow.Events().AppendEvent(ctx, Event{RunID: next.RunID, TaskID: next.ID, Type: EventTaskOwnerChanged, Payload: map[string]any{"ownerAgentId": request.ToAgentID, "version": next.Version, "task": taskEventPayload(next)}, RecordedAt: time.Now().UTC()}); err != nil {
		return handoffResult{}, err
	}
	if err := uow.Events().AppendEvent(ctx, Event{RunID: next.RunID, TaskID: next.ID, Type: EventHandoffApplied, Payload: map[string]any{"fromAgentId": fromAgentID, "toAgentId": request.ToAgentID}, RecordedAt: time.Now().UTC()}); err != nil {
		return handoffResult{}, err
	}
	env := TaskEnvelope{RunID: next.RunID, TaskID: next.ID, TargetAgentID: request.ToAgentID, Type: "HandoffEnvelope", Status: "pending", TaskVersion: next.Version, Payload: map[string]any{"handoff": true, "reason": request.Reason}, CreatedAt: next.UpdatedAt}
	if err := uow.MailboxOutbox().QueueEnvelope(ctx, env); err != nil {
		return handoffResult{}, err
	}
	if err := uow.Events().AppendEvent(ctx, Event{RunID: env.RunID, TaskID: env.TaskID, Type: EventHandoffEnvelopeQueued, Payload: map[string]any{"envelope": envPayload(env)}, RecordedAt: time.Now().UTC()}); err != nil {
		return handoffResult{}, err
	}
	result.Task = next
	result.Envelope = env
	return result, nil
}
