package handoff

import (
	"context"
	"slices"
	"time"

	commandbus "github.com/Viking602/go-hydaelyn/internal/command"
	"github.com/Viking602/go-hydaelyn/internal/core/model"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
	corestate "github.com/Viking602/go-hydaelyn/internal/core/state"
	"github.com/Viking602/go-hydaelyn/internal/eventpayload"
)

type IDGenerator func(string) string

type Authorizer func(context.Context, ports.UnitOfWork, model.PolicyRequest) (model.PolicyDecision, error)

type TraceRecorder func(context.Context, ports.UnitOfWork, string, string, string, string) error

type HandlerOptions struct {
	NewID       IDGenerator
	Authorize   Authorizer
	RecordTrace TraceRecorder
	MaxDepth    int
}

type HandoffResult struct {
	Task           model.Task
	Envelope       model.TaskEnvelope
	BlackboardItem model.BlackboardItem
	HasContext     bool
	FromAgentID    string
	ToAgentID      string
	Reason         string
}

// NotifyBlackboard implements core.BlackboardNotifier. Handoffs only emit a
// blackboard item when HasContext is true, preserving the original gating
// in command_uow_notifications.go.
func (r HandoffResult) NotifyBlackboard() []model.BlackboardItem {
	if !r.HasContext {
		return nil
	}
	return []model.BlackboardItem{r.BlackboardItem}
}

type Applier struct {
	newID       IDGenerator
	recordTrace TraceRecorder
	maxDepth    int
}

func NewApplier(options HandlerOptions) Applier {
	maxDepth := options.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 8
	}
	return Applier{newID: options.NewID, recordTrace: options.RecordTrace, maxDepth: maxDepth}
}

func RegisterHandlers(bus *commandbus.Bus, options HandlerOptions) {
	commandbus.Register[HandoffCommand](bus, handler{options: options, applier: NewApplier(options)})
}

type handler struct {
	options HandlerOptions
	applier Applier
}

func (handler) Name() string { return HandoffCommand{}.CommandName() }

func (h handler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd HandoffCommand) (any, error) {
	run, err := uow.Runs().LoadRun(ctx, cmd.RunID)
	if err != nil {
		return nil, err
	}
	if corestate.IsTerminalRun(run.Status) {
		return nil, model.ErrTerminalState
	}
	task, err := uow.Tasks().LoadTask(ctx, cmd.RunID, cmd.TaskID)
	if err != nil {
		return nil, err
	}
	request := &model.HandoffRequest{RunID: cmd.RunID, TaskID: cmd.TaskID, FromAgentID: cmd.FromAgentID, ToAgentID: cmd.ToAgentID, ContextSummary: cmd.HandoffContext, TaskVersion: cmd.TaskVersion}
	if h.options.Authorize != nil {
		if _, err := h.options.Authorize(ctx, uow, model.PolicyRequest{Operation: model.PolicyOperationHandoff, RunID: cmd.RunID, TaskID: cmd.TaskID, Actor: model.SourceIdentity{Type: model.SourceAgent, ID: cmd.FromAgentID}, Handoff: request}); err != nil {
			return nil, err
		}
	}
	if h.options.RecordTrace != nil {
		if err := h.options.RecordTrace(ctx, uow, cmd.RunID, cmd.TaskID, "handoff.request", "handoff"); err != nil {
			return nil, err
		}
	}
	return h.applier.Apply(ctx, uow, task, request, cmd.HandoffContext)
}

func (a Applier) Apply(ctx context.Context, uow ports.UnitOfWork, task model.Task, request *model.HandoffRequest, fallbackContext string) (HandoffResult, error) {
	fromAgentID, err := validateTask(task, request, a.maxDepth)
	if err != nil {
		return HandoffResult{}, err
	}
	contextSummary := contextSummary(request.ContextSummary, fallbackContext)
	now := time.Now().UTC()
	if err := appendRequestedEvent(ctx, uow, task, request, fromAgentID, now); err != nil {
		return HandoffResult{}, err
	}
	result := HandoffResult{FromAgentID: fromAgentID, ToAgentID: request.ToAgentID, Reason: request.Reason}
	contextItem, hasContext, err := a.writeContext(ctx, uow, task, fromAgentID, contextSummary, now)
	if err != nil {
		return HandoffResult{}, err
	}
	if hasContext {
		result.BlackboardItem = contextItem
		result.HasContext = true
	}
	next, err := transferOwnership(ctx, uow, task, request, fromAgentID, now)
	if err != nil {
		return HandoffResult{}, err
	}
	env, err := a.queueEnvelope(ctx, uow, next, request)
	if err != nil {
		return HandoffResult{}, err
	}
	result.Task = next
	result.Envelope = env
	return result, nil
}

func validateTask(task model.Task, request *model.HandoffRequest, maxDepth int) (string, error) {
	if corestate.IsTerminalTask(task.Status) {
		return "", model.ErrTerminalState
	}
	if request.TaskVersion != 0 && request.TaskVersion != task.Version {
		return "", model.ErrStaleTaskVersion
	}
	fromAgentID := request.FromAgentID
	if fromAgentID == "" {
		fromAgentID = task.OwnerAgentID
	}
	if task.OwnerAgentID != fromAgentID {
		return "", model.ErrOwnerMismatch
	}
	if request.ToAgentID == "" {
		return "", model.ErrInvalidCommand
	}
	if task.HandoffCount >= maxDepth {
		return "", model.ErrHandoffDepthExceeded
	}
	if containsString(task.OwnerHistory, request.ToAgentID) {
		return "", model.ErrHandoffCycle
	}
	return fromAgentID, nil
}

func contextSummary(contextSummary, fallbackContext string) string {
	if contextSummary == "" {
		contextSummary = fallbackContext
	}
	return contextSummary
}

func appendRequestedEvent(ctx context.Context, uow ports.UnitOfWork, task model.Task, request *model.HandoffRequest, fromAgentID string, now time.Time) error {
	return uow.Events().AppendEvent(ctx, model.Event{RunID: task.RunID, TaskID: task.ID, Type: model.EventHandoffRequested, Payload: map[string]any{"fromAgentId": fromAgentID, "toAgentId": request.ToAgentID, "reason": request.Reason}, RecordedAt: now})
}

func (a Applier) writeContext(ctx context.Context, uow ports.UnitOfWork, task model.Task, fromAgentID, contextSummary string, now time.Time) (model.BlackboardItem, bool, error) {
	if contextSummary == "" {
		return model.BlackboardItem{}, false, nil
	}
	item := model.BlackboardItem{RunID: task.RunID, TaskID: task.ID, Type: model.BlackboardItemHandoffContext, Source: model.SourceIdentity{Type: model.SourceAgent, ID: fromAgentID}, Visibility: model.BlackboardVisibilityAgentVisible, Key: "handoff_context", Content: contextSummary, Payload: contextSummary, Version: task.Version, CreatedAt: now}
	if err := uow.Blackboard().WriteItem(ctx, item); err != nil {
		return model.BlackboardItem{}, false, err
	}
	if a.recordTrace != nil {
		if err := a.recordTrace(ctx, uow, item.RunID, item.TaskID, "blackboard.write", "blackboard"); err != nil {
			return model.BlackboardItem{}, false, err
		}
	}
	if err := appendBlackboardWrittenEvent(ctx, uow, item); err != nil {
		return model.BlackboardItem{}, false, err
	}
	return item, true, nil
}

func transferOwnership(ctx context.Context, uow ports.UnitOfWork, task model.Task, request *model.HandoffRequest, fromAgentID string, now time.Time) (model.Task, error) {
	task.OwnerAgentID = request.ToAgentID
	task.OwnerComponent = ""
	task.HandoffCount++
	task.OwnerHistory = append(slices.Clone(task.OwnerHistory), request.ToAgentID)
	next, err := corestate.TransitionTask(task, model.TaskStatusDispatched, true)
	if err != nil {
		return model.Task{}, err
	}
	if err := uow.Tasks().SaveTask(ctx, next); err != nil {
		return model.Task{}, err
	}
	if err := uow.Events().AppendEvent(ctx, model.Event{RunID: next.RunID, TaskID: next.ID, Type: model.EventTaskOwnerChanged, Payload: map[string]any{"ownerAgentId": request.ToAgentID, "version": next.Version, "task": eventpayload.Task(next)}, RecordedAt: now}); err != nil {
		return model.Task{}, err
	}
	if err := uow.Events().AppendEvent(ctx, model.Event{RunID: next.RunID, TaskID: next.ID, Type: model.EventHandoffApplied, Payload: map[string]any{"fromAgentId": fromAgentID, "toAgentId": request.ToAgentID}, RecordedAt: time.Now().UTC()}); err != nil {
		return model.Task{}, err
	}
	return next, nil
}

func (a Applier) queueEnvelope(ctx context.Context, uow ports.UnitOfWork, next model.Task, request *model.HandoffRequest) (model.TaskEnvelope, error) {
	env := model.TaskEnvelope{ID: a.newID("env"), RunID: next.RunID, TaskID: next.ID, TargetAgentID: request.ToAgentID, Type: "HandoffEnvelope", Status: "pending", TaskVersion: next.Version, Payload: map[string]any{"handoff": true, "reason": request.Reason}, CreatedAt: next.UpdatedAt}
	if err := uow.MailboxOutbox().QueueEnvelope(ctx, env); err != nil {
		return model.TaskEnvelope{}, err
	}
	if err := uow.Events().AppendEvent(ctx, model.Event{RunID: env.RunID, TaskID: env.TaskID, Type: model.EventHandoffEnvelopeQueued, Payload: map[string]any{"envelope": eventpayload.Envelope(env)}, RecordedAt: time.Now().UTC()}); err != nil {
		return model.TaskEnvelope{}, err
	}
	return env, nil
}

func appendBlackboardWrittenEvent(ctx context.Context, uow ports.UnitOfWork, item model.BlackboardItem) error {
	return uow.Events().AppendEvent(ctx, model.Event{RunID: item.RunID, TaskID: item.TaskID, Type: model.EventBlackboardItemWritten, Payload: map[string]any{"itemId": item.ID, "sourceType": string(item.Source.Type), "sourceId": item.Source.ID, "visibility": string(item.Visibility), "key": item.Key}, RecordedAt: time.Now().UTC()})
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
