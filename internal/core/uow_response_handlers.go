package core

import (
	"context"
	"errors"
	"strings"
	"time"

	commandbus "github.com/Viking602/go-hydaelyn/internal/core/command"
	"github.com/Viking602/go-hydaelyn/internal/core/execution"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
)

func registerResponseUoWCommandHandlers(runtime *Runtime) {
	commandbus.Register[SubmitResponseOutputCommand](runtime.commandBus, submitResponseOutputHandler{runtime: runtime})
	commandbus.Register[PublishResponseCommand](runtime.commandBus, publishResponseHandler{runtime: runtime})
}

type submitResponseOutputResult struct {
	ComposedMessage UserMessage
	Message         UserMessage
	Task            Task
	Lease           TaskExecutionLease
	BlackboardItem  BlackboardItem
	Decision        PolicyDecision
}

type publishResponseResult struct {
	Message      UserMessage
	PublishError string
}

type submitResponseOutputHandler struct{ runtime *Runtime }

func (submitResponseOutputHandler) Name() string { return SubmitResponseOutputCommand{}.CommandName() }

func (h submitResponseOutputHandler) Handle(ctx context.Context, uow ports.FullUnitOfWork, cmd SubmitResponseOutputCommand) (any, error) {
	task, err := uow.Tasks().LoadTask(ctx, cmd.RunID, cmd.TaskID)
	if err != nil {
		return nil, err
	}
	if task.Type != TaskTypeResponse {
		return nil, ErrResponseTaskRequired
	}
	_, task, lease, err := validateSubmissionUoW(ctx, uow, cmd.RunID, cmd.TaskID, cmd.LeaseID, cmd.HolderType, cmd.HolderID, cmd.TaskVersion)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	message := UserMessage{ID: h.runtime.newID("msg"), RunID: cmd.RunID, TaskID: cmd.TaskID, Type: cmd.Type, Title: cmd.Title, Payload: cmd.Payload, Status: UserMessageComposed, IdempotencyKey: cmd.IdempotencyKey, CreatedAt: now, UpdatedAt: now}
	if message.Type == "" {
		message.Type = UserMessageTypeFinalAnswer
	}
	if message.IdempotencyKey == "" {
		message.IdempotencyKey = cmd.RunID + ":" + cmd.TaskID + ":" + string(message.Type)
	}
	decision, err := h.runtime.authorizeUoW(ctx, uow, PolicyRequest{Operation: PolicyOperationResponseCompose, RunID: cmd.RunID, TaskID: cmd.TaskID, Actor: actorFromHolder(cmd.HolderType, cmd.HolderID), Message: &message})
	if err != nil {
		return nil, err
	}
	sanitized, err := applyResponseObligationsUoW(ctx, uow, message, decision)
	if err != nil {
		if errors.Is(err, ErrPolicyObligationFailed) {
			return nil, commitWithError(err)
		}
		return nil, err
	}
	if err := h.runtime.recordEndedTraceUoW(ctx, uow, cmd.RunID, cmd.TaskID, "response.compose", "response"); err != nil {
		return nil, err
	}
	contextItem := criticalContextItem("", cmd.RunID, cmd.TaskID, actorFromHolder(cmd.HolderType, cmd.HolderID), "response_payload", sanitized.Payload)
	if err := uow.Blackboard().WriteItem(ctx, contextItem); err != nil {
		return nil, err
	}
	if err := h.runtime.recordEndedTraceUoW(ctx, uow, cmd.RunID, cmd.TaskID, "blackboard.write", "blackboard"); err != nil {
		return nil, err
	}
	if err := appendBlackboardWrittenEventUoW(ctx, uow, contextItem); err != nil {
		return nil, err
	}
	composed := sanitized
	if err := uow.Events().AppendEvent(ctx, Event{RunID: cmd.RunID, TaskID: cmd.TaskID, Type: EventUserMessageComposed, Payload: map[string]any{"message": userMessagePayload(composed)}, RecordedAt: now}); err != nil {
		return nil, err
	}
	if err := uow.Events().AppendEvent(ctx, Event{RunID: cmd.RunID, TaskID: cmd.TaskID, Type: EventUserMessagePolicyChecked, Payload: map[string]any{"decisionId": decision.DecisionID, "effect": string(decision.Effect)}, RecordedAt: now}); err != nil {
		return nil, err
	}
	sanitized.Status = UserMessageQueued
	sanitized.UpdatedAt = time.Now().UTC()
	if err := uow.UserMessages().QueueMessage(ctx, sanitized); err != nil {
		return nil, err
	}
	task, err = transitionTaskPure(task, TaskStatusCompleted, true)
	if err != nil {
		return nil, err
	}
	task.Result = &TypedReport{Status: ReportStatusSuccess, Summary: "response queued"}
	if err := uow.Tasks().SaveTask(ctx, task); err != nil {
		return nil, err
	}
	lease.Status = LeaseStatusReleased
	if err := uow.Leases().SaveLease(ctx, lease); err != nil {
		return nil, err
	}
	if err := uow.Events().AppendEvent(ctx, Event{RunID: lease.RunID, TaskID: lease.TaskID, Type: EventTaskExecutionReleased, Payload: map[string]any{"leaseId": lease.ID}, RecordedAt: time.Now().UTC()}); err != nil {
		return nil, err
	}
	if err := uow.Events().AppendEvent(ctx, Event{RunID: cmd.RunID, TaskID: cmd.TaskID, Type: EventUserMessageQueued, Payload: map[string]any{"messageId": sanitized.ID, "message": userMessagePayload(sanitized), "task": taskEventPayload(task)}, RecordedAt: time.Now().UTC()}); err != nil {
		return nil, err
	}
	return submitResponseOutputResult{ComposedMessage: composed, Message: sanitized, Task: task, Lease: lease, BlackboardItem: contextItem, Decision: decision}, nil
}

type publishResponseHandler struct{ runtime *Runtime }

func (publishResponseHandler) Name() string { return PublishResponseCommand{}.CommandName() }

func (h publishResponseHandler) Handle(ctx context.Context, uow ports.FullUnitOfWork, cmd PublishResponseCommand) (any, error) {
	message, err := uow.UserMessages().LoadMessage(ctx, cmd.RunID, cmd.MessageID)
	if err != nil {
		return nil, err
	}
	if message.Status == UserMessagePublished {
		return publishResponseResult{Message: message}, nil
	}
	if message.Status != UserMessageQueued {
		return nil, ErrInvalidCommand
	}
	if _, err := h.runtime.authorizeUoW(ctx, uow, PolicyRequest{Operation: PolicyOperationResponsePublish, RunID: cmd.RunID, TaskID: message.TaskID, Message: &message}); err != nil {
		return nil, err
	}
	gateway := h.runtime.currentOutputGateway()
	if err := gateway.Publish(ctx, message); err != nil {
		if appendErr := uow.Events().AppendEvent(ctx, Event{RunID: cmd.RunID, TaskID: message.TaskID, Type: EventResponsePublishFailed, Payload: map[string]any{"messageId": message.ID, "reason": err.Error()}, RecordedAt: time.Now().UTC()}); appendErr != nil {
			return nil, appendErr
		}
		return publishResponseResult{Message: message, PublishError: err.Error()}, commitWithError(err)
	}
	message, err = uow.UserMessages().LoadMessage(ctx, cmd.RunID, cmd.MessageID)
	if err != nil {
		return nil, err
	}
	if message.Status == UserMessagePublished {
		return publishResponseResult{Message: message}, nil
	}
	if message.Status != UserMessageQueued {
		return nil, ErrInvalidCommand
	}
	if err := h.runtime.recordEndedTraceUoW(ctx, uow, cmd.RunID, message.TaskID, "response.publish", "response"); err != nil {
		return nil, err
	}
	message.Status = UserMessagePublished
	message.PublishedAt = time.Now().UTC()
	message.UpdatedAt = time.Now().UTC()
	if err := uow.UserMessages().UpdateMessage(ctx, message); err != nil {
		return nil, err
	}
	if err := uow.Events().AppendEvent(ctx, Event{RunID: cmd.RunID, TaskID: message.TaskID, Type: EventResponsePublished, Payload: map[string]any{"messageId": message.ID, "message": userMessagePayload(message)}, RecordedAt: time.Now().UTC()}); err != nil {
		return nil, err
	}
	return publishResponseResult{Message: message}, nil
}

func validateSubmissionUoW(ctx context.Context, uow ports.FullUnitOfWork, runID, taskID, leaseID string, holderType HolderType, holderID string, taskVersion int) (Run, Task, TaskExecutionLease, error) {
	return execution.ValidateSubmission(ctx, uow, runID, taskID, leaseID, holderType, holderID, taskVersion)
}

func applyResponseObligationsUoW(ctx context.Context, uow ports.FullUnitOfWork, message UserMessage, decision PolicyDecision) (UserMessage, error) {
	out := message
	for _, obligation := range decision.Obligations {
		switch obligation.Kind {
		case ObligationRedactFields:
			out.Payload = redactUserPayload(out.Payload)
		case ObligationHideInternalTrace:
			out.Payload = hideInternalTrace(out.Payload)
		case ObligationMaskToolOutput:
			out.Payload = strings.ReplaceAll(out.Payload, "tool output:", "tool output: [masked]")
		case ObligationSelectorOnly, ObligationRequireHumanApproval, ObligationRestrictHandoffContext:
		default:
			if err := uow.Events().AppendEvent(ctx, Event{RunID: message.RunID, TaskID: message.TaskID, Type: EventPolicyObligationFailed, Payload: map[string]any{"decisionId": decision.DecisionID, "obligation": string(obligation.Kind), "target": obligation.Target, "reason": "unsupported obligation", "effectiveEffect": string(PolicyEffectDeny)}, RecordedAt: time.Now().UTC()}); err != nil {
				return UserMessage{}, err
			}
			return UserMessage{}, ErrPolicyObligationFailed
		}
	}
	if containsString(decision.Redactions, "email") {
		out.Payload = redactEmail(out.Payload)
	}
	return out, nil
}

func criticalContextItem(id, runID, taskID string, source SourceIdentity, key, payload string) BlackboardItem {
	if source.Type == "" {
		source = SourceIdentity{Type: SourceSystem, ID: "orchestrator"}
	}
	return BlackboardItem{ID: id, RunID: runID, TaskID: taskID, Type: BlackboardItemContext, Source: source, Visibility: BlackboardVisibilityAgentVisible, Key: key, Content: payload, Payload: payload, CreatedAt: time.Now().UTC()}
}

func appendBlackboardWrittenEventUoW(ctx context.Context, uow ports.FullUnitOfWork, item BlackboardItem) error {
	return uow.Events().AppendEvent(ctx, Event{RunID: item.RunID, TaskID: item.TaskID, Type: EventBlackboardItemWritten, Payload: map[string]any{"itemId": item.ID, "sourceType": string(item.Source.Type), "sourceId": item.Source.ID, "visibility": string(item.Visibility), "key": item.Key}, RecordedAt: time.Now().UTC()})
}
