package response

import (
	"context"
	"errors"
	"time"

	"github.com/Viking602/venat/api"
	commandbus "github.com/Viking602/venat/internal/command"
	"github.com/Viking602/venat/internal/core/ports"
	corestate "github.com/Viking602/venat/internal/core/state"
	"github.com/Viking602/venat/internal/eventpayload"
	"github.com/Viking602/venat/internal/execution"
)

type IDGenerator func(string) string

type Authorizer func(context.Context, ports.UnitOfWork, api.PolicyRequest) (api.PolicyDecision, error)

type ObligationEnforcer func(context.Context, ports.UnitOfWork, api.PolicyDecision, api.UserMessage) (api.UserMessage, error)

type TraceRecorder func(context.Context, ports.UnitOfWork, string, string, string, string) error

type HandlerOptions struct {
	NewID              IDGenerator
	Authorize          Authorizer
	EnforceObligations ObligationEnforcer
	RecordTrace        TraceRecorder
}

func RegisterSubmitHandler(bus *commandbus.Bus, options HandlerOptions) {
	commandbus.Register[SubmitOutputCommand](bus, submitOutputHandler{options: options})
}

type SubmitOutputResult struct {
	ComposedMessage api.UserMessage
	Message         api.UserMessage
	Task            api.Task
	Lease           api.TaskExecutionLease
	BlackboardItem  api.BlackboardItem
	Decision        api.PolicyDecision
}

// NotifyBlackboard implements core.BlackboardNotifier so the runtime can
// fan out the queued response item to subscribers at commit time.
func (r SubmitOutputResult) NotifyBlackboard() []api.BlackboardItem {
	return []api.BlackboardItem{r.BlackboardItem}
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
	if task.Type != api.TaskTypeResponse {
		return api.ErrResponseTaskRequired
	}
	return nil
}

func (h submitOutputHandler) newMessage(cmd SubmitOutputCommand, now time.Time) api.UserMessage {
	message := api.UserMessage{ID: h.options.NewID("msg"), RunID: cmd.RunID, TaskID: cmd.TaskID, Type: cmd.Type, Title: cmd.Title, Payload: cmd.Payload, Status: api.UserMessageComposed, IdempotencyKey: cmd.IdempotencyKey, CreatedAt: now, UpdatedAt: now}
	if message.Type == "" {
		message.Type = api.UserMessageTypeFinalAnswer
	}
	if message.IdempotencyKey == "" {
		message.IdempotencyKey = cmd.RunID + ":" + cmd.TaskID + ":" + string(message.Type)
	}
	return message
}

func (h submitOutputHandler) composeAndQueue(ctx context.Context, uow ports.UnitOfWork, cmd SubmitOutputCommand, message api.UserMessage, now time.Time) (api.UserMessage, api.UserMessage, api.BlackboardItem, api.PolicyDecision, error) {
	var decision api.PolicyDecision
	var err error
	if h.options.Authorize != nil {
		decision, err = h.options.Authorize(ctx, uow, api.PolicyRequest{Operation: api.PolicyOperationResponseCompose, RunID: cmd.RunID, TaskID: cmd.TaskID, Actor: actorFromHolder(cmd.HolderType, cmd.HolderID), Message: &message})
		if err != nil {
			return api.UserMessage{}, api.UserMessage{}, api.BlackboardItem{}, api.PolicyDecision{}, err
		}
	}
	sanitized := message
	if h.options.EnforceObligations != nil {
		sanitized, err = h.options.EnforceObligations(ctx, uow, decision, message)
		if err != nil {
			if errors.Is(err, api.ErrPolicyObligationFailed) {
				return api.UserMessage{}, api.UserMessage{}, api.BlackboardItem{}, api.PolicyDecision{}, commandbus.CommitWithError(err)
			}
			return api.UserMessage{}, api.UserMessage{}, api.BlackboardItem{}, api.PolicyDecision{}, err
		}
	}
	if h.options.RecordTrace != nil {
		if err := h.options.RecordTrace(ctx, uow, cmd.RunID, cmd.TaskID, "response.compose", "response"); err != nil {
			return api.UserMessage{}, api.UserMessage{}, api.BlackboardItem{}, api.PolicyDecision{}, err
		}
	}
	contextItem := CriticalContextItem("", cmd.RunID, cmd.TaskID, actorFromHolder(cmd.HolderType, cmd.HolderID), "response_payload", sanitized.Payload)
	if err := uow.Blackboard().WriteItem(ctx, contextItem); err != nil {
		return api.UserMessage{}, api.UserMessage{}, api.BlackboardItem{}, api.PolicyDecision{}, err
	}
	if h.options.RecordTrace != nil {
		if err := h.options.RecordTrace(ctx, uow, cmd.RunID, cmd.TaskID, "blackboard.write", "blackboard"); err != nil {
			return api.UserMessage{}, api.UserMessage{}, api.BlackboardItem{}, api.PolicyDecision{}, err
		}
	}
	if err := AppendBlackboardWrittenEvent(ctx, uow, contextItem); err != nil {
		return api.UserMessage{}, api.UserMessage{}, api.BlackboardItem{}, api.PolicyDecision{}, err
	}
	composed := sanitized
	if err := uow.Events().AppendEvent(ctx, api.Event{RunID: cmd.RunID, TaskID: cmd.TaskID, Type: api.EventUserMessageComposed, Payload: map[string]any{"message": UserMessagePayload(composed)}, RecordedAt: now}); err != nil {
		return api.UserMessage{}, api.UserMessage{}, api.BlackboardItem{}, api.PolicyDecision{}, err
	}
	if err := uow.Events().AppendEvent(ctx, api.Event{RunID: cmd.RunID, TaskID: cmd.TaskID, Type: api.EventUserMessagePolicyChecked, Payload: map[string]any{"decisionId": decision.DecisionID, "effect": string(decision.Effect)}, RecordedAt: now}); err != nil {
		return api.UserMessage{}, api.UserMessage{}, api.BlackboardItem{}, api.PolicyDecision{}, err
	}
	sanitized.Status = api.UserMessageQueued
	sanitized.UpdatedAt = time.Now().UTC()
	if err := uow.UserMessages().QueueMessage(ctx, sanitized); err != nil {
		return api.UserMessage{}, api.UserMessage{}, api.BlackboardItem{}, api.PolicyDecision{}, err
	}
	return composed, sanitized, contextItem, decision, nil
}

func (h submitOutputHandler) completeSubmission(ctx context.Context, uow ports.UnitOfWork, cmd SubmitOutputCommand, task api.Task, lease api.TaskExecutionLease, message api.UserMessage) (api.Task, api.TaskExecutionLease, error) {
	task, err := corestate.TransitionTask(task, api.TaskStatusCompleted, true)
	if err != nil {
		return api.Task{}, api.TaskExecutionLease{}, err
	}
	task.Result = &api.TypedReport{Status: api.ReportStatusSuccess, Summary: "response queued"}
	if err := uow.Tasks().SaveTask(ctx, task); err != nil {
		return api.Task{}, api.TaskExecutionLease{}, err
	}
	lease.Status = api.LeaseStatusReleased
	if err := uow.Leases().SaveLease(ctx, lease); err != nil {
		return api.Task{}, api.TaskExecutionLease{}, err
	}
	if err := execution.ReleaseResourceClaims(ctx, uow, lease.ID, time.Now().UTC()); err != nil {
		return api.Task{}, api.TaskExecutionLease{}, err
	}
	if err := uow.Events().AppendEvent(ctx, api.Event{RunID: lease.RunID, TaskID: lease.TaskID, Type: api.EventTaskExecutionReleased, Payload: map[string]any{"leaseId": lease.ID}, RecordedAt: time.Now().UTC()}); err != nil {
		return api.Task{}, api.TaskExecutionLease{}, err
	}
	if err := uow.Events().AppendEvent(ctx, api.Event{RunID: cmd.RunID, TaskID: cmd.TaskID, Type: api.EventUserMessageQueued, Payload: map[string]any{"messageId": message.ID, "message": UserMessagePayload(message), "task": eventpayload.Task(task)}, RecordedAt: time.Now().UTC()}); err != nil {
		return api.Task{}, api.TaskExecutionLease{}, err
	}
	return task, lease, nil
}

func UserMessagePayload(message api.UserMessage) map[string]any {
	return map[string]any{"messageId": message.ID, "runId": message.RunID, "taskId": message.TaskID, "type": string(message.Type), "title": message.Title, "payload": message.Payload, "status": string(message.Status), "idempotencyKey": message.IdempotencyKey, "publishedAt": message.PublishedAt, "createdAt": message.CreatedAt, "updatedAt": message.UpdatedAt}
}

func actorFromHolder(holderType api.HolderType, holderID string) api.SourceIdentity {
	switch holderType {
	case api.HolderAgent:
		return api.SourceIdentity{Type: api.SourceAgent, ID: holderID}
	case api.HolderComponent:
		return api.SourceIdentity{Type: api.SourceComponent, ID: holderID}
	default:
		return api.SourceIdentity{Type: api.SourceSystem, ID: holderID}
	}
}
