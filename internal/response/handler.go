package response

import (
	"context"
	"errors"
	"time"

	commandbus "github.com/Viking602/venat/internal/command"
	"github.com/Viking602/venat/internal/core/model"
	"github.com/Viking602/venat/internal/core/ports"
	corestate "github.com/Viking602/venat/internal/core/state"
	"github.com/Viking602/venat/internal/eventpayload"
	"github.com/Viking602/venat/internal/execution"
)

type IDGenerator func(string) string

type Authorizer func(context.Context, ports.UnitOfWork, model.PolicyRequest) (model.PolicyDecision, error)

type TraceRecorder func(context.Context, ports.UnitOfWork, string, string, string, string) error

type HandlerOptions struct {
	NewID       IDGenerator
	Authorize   Authorizer
	RecordTrace TraceRecorder
}

func RegisterSubmitHandler(bus *commandbus.Bus, options HandlerOptions) {
	commandbus.Register[SubmitOutputCommand](bus, submitOutputHandler{options: options})
}

type SubmitOutputResult struct {
	ComposedMessage model.UserMessage
	Message         model.UserMessage
	Task            model.Task
	Lease           model.TaskExecutionLease
	BlackboardItem  model.BlackboardItem
	Decision        model.PolicyDecision
}

// NotifyBlackboard implements core.BlackboardNotifier so the runtime can
// fan out the queued response item to subscribers at commit time.
func (r SubmitOutputResult) NotifyBlackboard() []model.BlackboardItem {
	return []model.BlackboardItem{r.BlackboardItem}
}

type submitOutputHandler struct{ options HandlerOptions }

func (submitOutputHandler) Name() string { return SubmitOutputCommand{}.CommandName() }

func (h submitOutputHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd SubmitOutputCommand) (any, error) {
	if err := ensureResponseTask(ctx, uow, cmd); err != nil {
		return nil, err
	}
	_, task, lease, err := execution.ValidateSubmission(ctx, uow, cmd.RunID, cmd.TaskID, cmd.LeaseID, cmd.HolderType, cmd.HolderID, cmd.TaskVersion)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	message := h.newMessage(cmd, now)
	composed, queued, contextItem, decision, err := h.composeAndQueue(ctx, uow, cmd, message, now)
	if err != nil {
		return nil, err
	}
	task, lease, err = h.completeSubmission(ctx, uow, cmd, task, lease, queued)
	if err != nil {
		return nil, err
	}
	return SubmitOutputResult{ComposedMessage: composed, Message: queued, Task: task, Lease: lease, BlackboardItem: contextItem, Decision: decision}, nil
}

func ensureResponseTask(ctx context.Context, uow ports.UnitOfWork, cmd SubmitOutputCommand) error {
	task, err := uow.Tasks().LoadTask(ctx, cmd.RunID, cmd.TaskID)
	if err != nil {
		return err
	}
	if task.Type != model.TaskTypeResponse {
		return model.ErrResponseTaskRequired
	}
	return nil
}

func (h submitOutputHandler) newMessage(cmd SubmitOutputCommand, now time.Time) model.UserMessage {
	message := model.UserMessage{ID: h.options.NewID("msg"), RunID: cmd.RunID, TaskID: cmd.TaskID, Type: cmd.Type, Title: cmd.Title, Payload: cmd.Payload, Status: model.UserMessageComposed, IdempotencyKey: cmd.IdempotencyKey, CreatedAt: now, UpdatedAt: now}
	if message.Type == "" {
		message.Type = model.UserMessageTypeFinalAnswer
	}
	if message.IdempotencyKey == "" {
		message.IdempotencyKey = cmd.RunID + ":" + cmd.TaskID + ":" + string(message.Type)
	}
	return message
}

func (h submitOutputHandler) composeAndQueue(ctx context.Context, uow ports.UnitOfWork, cmd SubmitOutputCommand, message model.UserMessage, now time.Time) (model.UserMessage, model.UserMessage, model.BlackboardItem, model.PolicyDecision, error) {
	var decision model.PolicyDecision
	var err error
	if h.options.Authorize != nil {
		decision, err = h.options.Authorize(ctx, uow, model.PolicyRequest{Operation: model.PolicyOperationResponseCompose, RunID: cmd.RunID, TaskID: cmd.TaskID, Actor: actorFromHolder(cmd.HolderType, cmd.HolderID), Message: &message})
		if err != nil {
			return model.UserMessage{}, model.UserMessage{}, model.BlackboardItem{}, model.PolicyDecision{}, err
		}
	}
	sanitized, err := ApplyObligations(ctx, uow, message, decision)
	if err != nil {
		if errors.Is(err, model.ErrPolicyObligationFailed) {
			return model.UserMessage{}, model.UserMessage{}, model.BlackboardItem{}, model.PolicyDecision{}, commandbus.CommitWithError(err)
		}
		return model.UserMessage{}, model.UserMessage{}, model.BlackboardItem{}, model.PolicyDecision{}, err
	}
	if h.options.RecordTrace != nil {
		if err := h.options.RecordTrace(ctx, uow, cmd.RunID, cmd.TaskID, "response.compose", "response"); err != nil {
			return model.UserMessage{}, model.UserMessage{}, model.BlackboardItem{}, model.PolicyDecision{}, err
		}
	}
	contextItem := CriticalContextItem("", cmd.RunID, cmd.TaskID, actorFromHolder(cmd.HolderType, cmd.HolderID), "response_payload", sanitized.Payload)
	if err := uow.Blackboard().WriteItem(ctx, contextItem); err != nil {
		return model.UserMessage{}, model.UserMessage{}, model.BlackboardItem{}, model.PolicyDecision{}, err
	}
	if h.options.RecordTrace != nil {
		if err := h.options.RecordTrace(ctx, uow, cmd.RunID, cmd.TaskID, "blackboard.write", "blackboard"); err != nil {
			return model.UserMessage{}, model.UserMessage{}, model.BlackboardItem{}, model.PolicyDecision{}, err
		}
	}
	if err := AppendBlackboardWrittenEvent(ctx, uow, contextItem); err != nil {
		return model.UserMessage{}, model.UserMessage{}, model.BlackboardItem{}, model.PolicyDecision{}, err
	}
	composed := sanitized
	if err := uow.Events().AppendEvent(ctx, model.Event{RunID: cmd.RunID, TaskID: cmd.TaskID, Type: model.EventUserMessageComposed, Payload: map[string]any{"message": UserMessagePayload(composed)}, RecordedAt: now}); err != nil {
		return model.UserMessage{}, model.UserMessage{}, model.BlackboardItem{}, model.PolicyDecision{}, err
	}
	if err := uow.Events().AppendEvent(ctx, model.Event{RunID: cmd.RunID, TaskID: cmd.TaskID, Type: model.EventUserMessagePolicyChecked, Payload: map[string]any{"decisionId": decision.DecisionID, "effect": string(decision.Effect)}, RecordedAt: now}); err != nil {
		return model.UserMessage{}, model.UserMessage{}, model.BlackboardItem{}, model.PolicyDecision{}, err
	}
	sanitized.Status = model.UserMessageQueued
	sanitized.UpdatedAt = time.Now().UTC()
	if err := uow.UserMessages().QueueMessage(ctx, sanitized); err != nil {
		return model.UserMessage{}, model.UserMessage{}, model.BlackboardItem{}, model.PolicyDecision{}, err
	}
	return composed, sanitized, contextItem, decision, nil
}

func (h submitOutputHandler) completeSubmission(ctx context.Context, uow ports.UnitOfWork, cmd SubmitOutputCommand, task model.Task, lease model.TaskExecutionLease, message model.UserMessage) (model.Task, model.TaskExecutionLease, error) {
	task, err := corestate.TransitionTask(task, model.TaskStatusCompleted, true)
	if err != nil {
		return model.Task{}, model.TaskExecutionLease{}, err
	}
	task.Result = &model.TypedReport{Status: model.ReportStatusSuccess, Summary: "response queued"}
	if err := uow.Tasks().SaveTask(ctx, task); err != nil {
		return model.Task{}, model.TaskExecutionLease{}, err
	}
	lease.Status = model.LeaseStatusReleased
	if err := uow.Leases().SaveLease(ctx, lease); err != nil {
		return model.Task{}, model.TaskExecutionLease{}, err
	}
	if err := uow.Events().AppendEvent(ctx, model.Event{RunID: lease.RunID, TaskID: lease.TaskID, Type: model.EventTaskExecutionReleased, Payload: map[string]any{"leaseId": lease.ID}, RecordedAt: time.Now().UTC()}); err != nil {
		return model.Task{}, model.TaskExecutionLease{}, err
	}
	if err := uow.Events().AppendEvent(ctx, model.Event{RunID: cmd.RunID, TaskID: cmd.TaskID, Type: model.EventUserMessageQueued, Payload: map[string]any{"messageId": message.ID, "message": UserMessagePayload(message), "task": eventpayload.Task(task)}, RecordedAt: time.Now().UTC()}); err != nil {
		return model.Task{}, model.TaskExecutionLease{}, err
	}
	return task, lease, nil
}

func UserMessagePayload(message model.UserMessage) map[string]any {
	return map[string]any{"messageId": message.ID, "runId": message.RunID, "taskId": message.TaskID, "type": string(message.Type), "title": message.Title, "payload": message.Payload, "status": string(message.Status), "idempotencyKey": message.IdempotencyKey, "publishedAt": message.PublishedAt, "createdAt": message.CreatedAt, "updatedAt": message.UpdatedAt}
}

func actorFromHolder(holderType model.HolderType, holderID string) model.SourceIdentity {
	switch holderType {
	case model.HolderAgent:
		return model.SourceIdentity{Type: model.SourceAgent, ID: holderID}
	case model.HolderComponent:
		return model.SourceIdentity{Type: model.SourceComponent, ID: holderID}
	default:
		return model.SourceIdentity{Type: model.SourceSystem, ID: holderID}
	}
}
