package report

import (
	"context"
	"errors"
	"strings"
	"time"

	commandbus "github.com/Viking602/venat/internal/command"
	"github.com/Viking602/venat/internal/core/model"
	"github.com/Viking602/venat/internal/core/ports"
	corestate "github.com/Viking602/venat/internal/core/state"
	"github.com/Viking602/venat/internal/eventpayload"
	"github.com/Viking602/venat/internal/execution"
	"github.com/Viking602/venat/internal/handoff"
	responsesvc "github.com/Viking602/venat/internal/response"
)

type IDGenerator func(string) string

type Authorizer func(context.Context, ports.UnitOfWork, model.PolicyRequest) (model.PolicyDecision, error)

type ApprovalFactory func(model.Task, string, string) (model.ApprovalRequest, model.ResumeToken)

type TraceRecorder func(context.Context, ports.UnitOfWork, string, string, string, string) error

type HandlerOptions struct {
	NewID           IDGenerator
	Authorize       Authorizer
	NewApproval     ApprovalFactory
	RecordTrace     TraceRecorder
	MaxHandoffDepth int
}

func RegisterHandlers(bus *commandbus.Bus, options HandlerOptions) {
	commandbus.Register[SubmitTypedCommand](bus, submitTypedHandler{options: options})
}

type SubmitTypedResult struct {
	Runs         []model.Run
	Tasks        []model.Task
	Leases       []model.TaskExecutionLease
	Envelopes    []model.TaskEnvelope
	Messages     []model.UserMessage
	Approvals    []model.ApprovalRequest
	ResumeTokens []model.ResumeToken
	Blackboard   []model.BlackboardItem
	Events       []model.Event
	TraceSpans   []model.TraceSpan
	NotifyItems  []model.BlackboardItem
	CommitError  error
}

// NotifyBlackboard implements core.BlackboardNotifier. Method name avoids
// the collision with the NotifyItems field above.
func (r SubmitTypedResult) NotifyBlackboard() []model.BlackboardItem {
	return r.NotifyItems
}

type submitTypedHandler struct{ options HandlerOptions }

func (submitTypedHandler) Name() string { return SubmitTypedCommand{}.CommandName() }

func (h submitTypedHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd SubmitTypedCommand) (any, error) {
	run, task, lease, err := execution.ValidateSubmission(ctx, uow, cmd.RunID, cmd.TaskID, cmd.LeaseID, cmd.HolderType, cmd.HolderID, cmd.TaskVersion)
	if err != nil {
		return nil, err
	}
	report := cmd.Report
	if report.Status == "" {
		report.Status = model.ReportStatusSuccess
	}
	m := &SubmitTypedResult{}
	if err := h.recordTrace(ctx, uow, m, cmd.RunID, cmd.TaskID, "report.submit_typed", "report"); err != nil {
		return nil, err
	}
	if err := h.emit(ctx, uow, m, model.Event{RunID: cmd.RunID, TaskID: cmd.TaskID, Type: model.EventTypedReportSubmitted, Payload: map[string]any{"status": string(report.Status), "holderType": string(cmd.HolderType), "holderId": cmd.HolderID, "taskVersion": cmd.TaskVersion}, RecordedAt: time.Now().UTC()}); err != nil {
		return nil, err
	}
	nextTask, nextRun, nextLease, nextReport, err := h.applyActionOutcome(ctx, uow, m, run, task, lease, cmd, report)
	if err != nil {
		if errors.Is(err, model.ErrActionReconcileRequired) {
			m.CommitError = err
			return *m, commandbus.CommitWithError(err)
		}
		return nil, err
	}
	if err := h.applyReportStatus(ctx, uow, m, nextRun, nextTask, nextLease, cmd, nextReport); err != nil {
		return nil, err
	}
	return *m, nil
}

func (h submitTypedHandler) applyActionOutcome(ctx context.Context, uow ports.UnitOfWork, m *SubmitTypedResult, run model.Run, task model.Task, lease model.TaskExecutionLease, cmd SubmitTypedCommand, report model.TypedReport) (model.Task, model.Run, model.TaskExecutionLease, model.TypedReport, error) {
	if report.ActionOutcome == nil {
		return task, run, lease, report, nil
	}
	attempt := model.ActionAttempt{AttemptID: report.ActionOutcome.AttemptID, ActionID: report.ActionOutcome.ActionID, RunID: cmd.RunID, TaskID: cmd.TaskID, Status: report.ActionOutcome.Status, ExternalResultRef: report.ActionOutcome.ExternalResultRef}
	if h.options.Authorize != nil {
		if _, err := h.options.Authorize(ctx, uow, model.PolicyRequest{Operation: model.PolicyOperationAction, RunID: cmd.RunID, TaskID: cmd.TaskID, Actor: actorFromHolder(cmd.HolderType, cmd.HolderID), Action: &attempt}); err != nil {
			return model.Task{}, model.Run{}, model.TaskExecutionLease{}, model.TypedReport{}, err
		}
	}
	if !task.AllowsAction {
		return model.Task{}, model.Run{}, model.TaskExecutionLease{}, model.TypedReport{}, model.ErrActionTaskRequired
	}
	switch report.ActionOutcome.Status {
	case model.ActionAttemptUnknown:
		if err := h.recordTrace(ctx, uow, m, task.RunID, task.ID, "action.reconcile_required", "action"); err != nil {
			return model.Task{}, model.Run{}, model.TaskExecutionLease{}, model.TypedReport{}, err
		}
		next, err := corestate.TransitionTask(task, model.TaskStatusReconcileRequired, true)
		if err != nil {
			return model.Task{}, model.Run{}, model.TaskExecutionLease{}, model.TypedReport{}, err
		}
		next.Error = "action attempt requires reconciliation"
		if err := h.saveTask(ctx, uow, m, next); err != nil {
			return model.Task{}, model.Run{}, model.TaskExecutionLease{}, model.TypedReport{}, err
		}
		item := model.BlackboardItem{RunID: next.RunID, TaskID: next.ID, Type: model.BlackboardItemEvidence, Source: model.SourceIdentity{Type: model.SourceTool, ID: report.ActionOutcome.AttemptID}, Visibility: model.BlackboardVisibilityAgentVisible, Key: "action_reconcile_required", Content: report.ActionOutcome.Summary, Payload: report.ActionOutcome.Summary, CreatedAt: time.Now().UTC()}
		if err := h.writeBlackboard(ctx, uow, m, item); err != nil {
			return model.Task{}, model.Run{}, model.TaskExecutionLease{}, model.TypedReport{}, err
		}
		if err := h.emit(ctx, uow, m, model.Event{RunID: next.RunID, TaskID: next.ID, Type: model.EventActionReconcileRequired, Payload: map[string]any{"attemptId": report.ActionOutcome.AttemptID, "status": string(report.ActionOutcome.Status)}, RecordedAt: time.Now().UTC()}); err != nil {
			return model.Task{}, model.Run{}, model.TaskExecutionLease{}, model.TypedReport{}, err
		}
		lease, err = h.releaseLease(ctx, uow, m, lease)
		if err != nil {
			return model.Task{}, model.Run{}, model.TaskExecutionLease{}, model.TypedReport{}, err
		}
		nextRun, err := corestate.TransitionRun(run, model.RunStatusReconcileRequired)
		if err != nil {
			return model.Task{}, model.Run{}, model.TaskExecutionLease{}, model.TypedReport{}, err
		}
		if err := h.saveRun(ctx, uow, m, run, nextRun); err != nil {
			return model.Task{}, model.Run{}, model.TaskExecutionLease{}, model.TypedReport{}, err
		}
		return next, nextRun, lease, report, model.ErrActionReconcileRequired
	case model.ActionAttemptFailed, model.ActionAttemptTimeout, model.ActionAttemptCancelled:
		task.Error = report.ActionOutcome.Error
	case model.ActionAttemptSucceeded:
		if err := h.recordTrace(ctx, uow, m, task.RunID, task.ID, "action.result", "action"); err != nil {
			return model.Task{}, model.Run{}, model.TaskExecutionLease{}, model.TypedReport{}, err
		}
		item := model.BlackboardItem{RunID: task.RunID, TaskID: task.ID, Type: model.BlackboardItemEvidence, Source: model.SourceIdentity{Type: model.SourceTool, ID: report.ActionOutcome.AttemptID}, Visibility: model.BlackboardVisibilityAgentVisible, Key: "action_outcome", Payload: report.ActionOutcome.Output, CreatedAt: time.Now().UTC()}
		if err := h.writeBlackboard(ctx, uow, m, item); err != nil {
			return model.Task{}, model.Run{}, model.TaskExecutionLease{}, model.TypedReport{}, err
		}
	}
	if actionAttemptFailed(report.ActionOutcome.Status) && report.Status == model.ReportStatusSuccess {
		report.Status = model.ReportStatusFailed
	}
	return task, run, lease, report, nil
}

func (h submitTypedHandler) applyReportStatus(ctx context.Context, uow ports.UnitOfWork, m *SubmitTypedResult, run model.Run, task model.Task, lease model.TaskExecutionLease, cmd SubmitTypedCommand, report model.TypedReport) error {
	switch report.Status {
	case model.ReportStatusSuccess:
		return h.applySuccessfulReport(ctx, uow, m, task, lease, report)
	case model.ReportStatusPartialSuccess:
		task.Result = &report
		return h.saveTask(ctx, uow, m, task)
	case model.ReportStatusFailed:
		return h.applyFailedReport(ctx, uow, m, task, lease, report)
	case model.ReportStatusBlocked:
		next, err := corestate.TransitionTask(task, model.TaskStatusBlocked, true)
		if err != nil {
			return err
		}
		next.Error = report.Summary
		next.Result = &report
		if err := h.saveTask(ctx, uow, m, next); err != nil {
			return err
		}
		if _, err := h.releaseLease(ctx, uow, m, lease); err != nil {
			return err
		}
		return h.emit(ctx, uow, m, model.Event{RunID: cmd.RunID, TaskID: cmd.TaskID, Type: model.EventTaskBlocked, Payload: map[string]any{"reason": report.Summary, "task": eventpayload.Task(next)}, RecordedAt: time.Now().UTC()})
	case model.ReportStatusNeedsApproval:
		return h.applyApprovalReport(ctx, uow, m, run, task, lease, cmd, report)
	case model.ReportStatusNeedsClarification:
		return h.applyClarificationReport(ctx, uow, m, run, task, lease, report)
	case model.ReportStatusNeedsHandoff:
		return h.applyHandoffReport(ctx, uow, m, task, lease, cmd, report)
	default:
		return model.ErrInvalidCommand
	}
}

func (h submitTypedHandler) applySuccessfulReport(ctx context.Context, uow ports.UnitOfWork, m *SubmitTypedResult, task model.Task, lease model.TaskExecutionLease, report model.TypedReport) error {
	if !completionCriteriaSatisfied(task, report) {
		return model.ErrCompletionCriteriaUnmet
	}
	next, err := corestate.TransitionTask(task, model.TaskStatusCompleted, true)
	if err != nil {
		return err
	}
	next.Result = &report
	if err := h.saveTask(ctx, uow, m, next); err != nil {
		return err
	}
	if _, err := h.releaseLease(ctx, uow, m, lease); err != nil {
		return err
	}
	return h.emit(ctx, uow, m, model.Event{RunID: task.RunID, TaskID: task.ID, Type: model.EventTaskCompleted, Payload: map[string]any{"summary": report.Summary, "task": eventpayload.Task(next)}, RecordedAt: time.Now().UTC()})
}

func (h submitTypedHandler) applyFailedReport(ctx context.Context, uow ports.UnitOfWork, m *SubmitTypedResult, task model.Task, lease model.TaskExecutionLease, report model.TypedReport) error {
	reason := reportFailureReason(report)
	if report.Retryable && canRetryTask(task) {
		next, err := corestate.TransitionTask(task, model.TaskStatusDispatched, true)
		if err != nil {
			return err
		}
		next.Error = reason
		next.Result = &report
		if err := h.saveTask(ctx, uow, m, next); err != nil {
			return err
		}
		if _, err := h.releaseLease(ctx, uow, m, lease); err != nil {
			return err
		}
		now := time.Now().UTC()
		env := model.TaskEnvelope{
			ID:              h.options.NewID("env"),
			RunID:           next.RunID,
			TaskID:          next.ID,
			TargetAgentID:   next.OwnerAgentID,
			TargetComponent: next.OwnerComponent,
			Type:            "TaskEnvelope",
			Status:          "pending",
			TaskVersion:     next.Version,
			RetryPolicy:     next.RetryPolicy,
			Attempts:        next.Attempts,
			CreatedAt:       now,
		}
		if delay := retryBackoff(next.RetryPolicy.Backoff, next.RetryPolicy.MaxBackoff, next.Attempts); delay > 0 {
			env.NextRetryAt = now.Add(delay)
		}
		return h.queueEnvelope(ctx, uow, m, env, model.EventTaskDispatched)
	}
	next, err := corestate.TransitionTask(task, model.TaskStatusFailed, true)
	if err != nil {
		return err
	}
	next.Error = reason
	next.Result = &report
	if err := h.saveTask(ctx, uow, m, next); err != nil {
		return err
	}
	if _, err := h.releaseLease(ctx, uow, m, lease); err != nil {
		return err
	}
	return h.emit(ctx, uow, m, model.Event{RunID: task.RunID, TaskID: task.ID, Type: model.EventTaskFailed, Payload: map[string]any{"reason": reason, "task": eventpayload.Task(next)}, RecordedAt: time.Now().UTC()})
}

func (h submitTypedHandler) applyApprovalReport(ctx context.Context, uow ports.UnitOfWork, m *SubmitTypedResult, run model.Run, task model.Task, lease model.TaskExecutionLease, cmd SubmitTypedCommand, report model.TypedReport) error {
	approval, token := h.options.NewApproval(task, report.Summary, cmd.HolderID)
	if err := uow.Approvals().SaveApproval(ctx, approval); err != nil {
		return err
	}
	m.Approvals = append(m.Approvals, approval)
	if err := uow.ResumeTokens().SaveResumeToken(ctx, token); err != nil {
		return err
	}
	m.ResumeTokens = append(m.ResumeTokens, token)
	if err := h.emit(ctx, uow, m, model.Event{RunID: token.RunID, TaskID: token.TaskID, Type: model.EventResumeTokenCreated, Payload: map[string]any{"tokenId": token.TokenID, "approvalId": token.ApprovalID, "expiresAt": token.ExpiresAt}, RecordedAt: time.Now().UTC()}); err != nil {
		return err
	}
	next, err := corestate.TransitionTask(task, model.TaskStatusPaused, true)
	if err != nil {
		return err
	}
	next.Result = &report
	if err := h.saveTask(ctx, uow, m, next); err != nil {
		return err
	}
	nextRun, err := corestate.TransitionRun(run, model.RunStatusWaitingApproval)
	if err != nil {
		return err
	}
	if err := h.saveRun(ctx, uow, m, run, nextRun); err != nil {
		return err
	}
	if _, err := h.releaseLease(ctx, uow, m, lease); err != nil {
		return err
	}
	item := responsesvc.CriticalContextItem("", task.RunID, task.ID, model.SourceIdentity{Type: model.SourceAgent, ID: cmd.HolderID}, "approval", report.Summary)
	if err := h.writeBlackboard(ctx, uow, m, item); err != nil {
		return err
	}
	if err := h.emit(ctx, uow, m, model.Event{RunID: cmd.RunID, TaskID: cmd.TaskID, Type: model.EventApprovalRequested, Payload: map[string]any{"approvalId": approval.ApprovalID, "resumeToken": token.TokenID, "reason": report.Summary}, RecordedAt: time.Now().UTC()}); err != nil {
		return err
	}
	return h.emit(ctx, uow, m, model.Event{RunID: cmd.RunID, TaskID: cmd.TaskID, Type: model.EventTaskPaused, Payload: map[string]any{"reason": report.Summary, "task": eventpayload.Task(next)}, RecordedAt: time.Now().UTC()})
}

func (h submitTypedHandler) applyClarificationReport(ctx context.Context, uow ports.UnitOfWork, m *SubmitTypedResult, run model.Run, task model.Task, lease model.TaskExecutionLease, report model.TypedReport) error {
	next, err := corestate.TransitionTask(task, model.TaskStatusWaitingUserInput, true)
	if err != nil {
		return err
	}
	next.Result = &report
	next.Error = report.Summary
	if err := h.saveTask(ctx, uow, m, next); err != nil {
		return err
	}
	nextRun, err := corestate.TransitionRun(run, model.RunStatusWaitingUserInput)
	if err != nil {
		return err
	}
	if err := h.saveRun(ctx, uow, m, run, nextRun); err != nil {
		return err
	}
	if _, err := h.releaseLease(ctx, uow, m, lease); err != nil {
		return err
	}
	if err := h.emit(ctx, uow, m, model.Event{RunID: next.RunID, TaskID: next.ID, Type: model.EventTaskBlocked, Payload: map[string]any{"reason": "needs_clarification", "task": eventpayload.Task(next)}, RecordedAt: time.Now().UTC()}); err != nil {
		return err
	}
	return h.queueSystemResponse(ctx, uow, m, next.RunID, next.ID, model.UserMessageTypeClarificationRequest, "Clarification requested", report.Summary)
}

func (h submitTypedHandler) applyHandoffReport(ctx context.Context, uow ports.UnitOfWork, m *SubmitTypedResult, task model.Task, lease model.TaskExecutionLease, cmd SubmitTypedCommand, report model.TypedReport) error {
	if report.Handoff == nil || report.Handoff.ToAgentID == "" {
		return model.ErrInvalidCommand
	}
	if h.options.Authorize != nil {
		if _, err := h.options.Authorize(ctx, uow, model.PolicyRequest{Operation: model.PolicyOperationHandoff, RunID: cmd.RunID, TaskID: cmd.TaskID, Actor: model.SourceIdentity{Type: model.SourceAgent, ID: cmd.HolderID}, Handoff: report.Handoff}); err != nil {
			return err
		}
	}
	applier := handoff.NewApplier(handoff.HandlerOptions{
		NewID: func(prefix string) string {
			return h.options.NewID(prefix)
		},
		RecordTrace: func(ctx context.Context, uow ports.UnitOfWork, runID, taskID, name, component string) error {
			if h.options.RecordTrace == nil {
				return nil
			}
			return h.options.RecordTrace(ctx, uow, runID, taskID, name, component)
		},
		MaxDepth: h.options.MaxHandoffDepth,
	})
	result, err := applier.Apply(ctx, uow, task, report.Handoff, report.Summary)
	if err != nil {
		return err
	}
	m.Tasks = append(m.Tasks, result.Task)
	m.Envelopes = append(m.Envelopes, result.Envelope)
	if result.HasContext {
		m.Blackboard = append(m.Blackboard, result.BlackboardItem)
		m.NotifyItems = append(m.NotifyItems, result.BlackboardItem)
	}
	_, err = h.releaseLease(ctx, uow, m, lease)
	return err
}

func (h submitTypedHandler) queueSystemResponse(ctx context.Context, uow ports.UnitOfWork, m *SubmitTypedResult, runID, sourceTaskID string, messageType model.UserMessageType, title, payload string) error {
	now := time.Now().UTC()
	task := model.Task{ID: h.options.NewID("response"), RunID: runID, ParentTaskID: sourceTaskID, Type: model.TaskTypeResponse, Goal: string(messageType), OwnerComponent: "response_composer", Status: model.TaskStatusCompleted, Version: 1, CreatedAt: now, UpdatedAt: now, Result: &model.TypedReport{Status: model.ReportStatusSuccess, Summary: payload}}
	if err := h.saveTask(ctx, uow, m, task); err != nil {
		return err
	}
	if err := h.emit(ctx, uow, m, model.Event{RunID: runID, TaskID: task.ID, Type: model.EventResponseTaskCreated, Payload: eventpayload.Task(task), RecordedAt: now}); err != nil {
		return err
	}
	if err := h.emit(ctx, uow, m, model.Event{RunID: runID, TaskID: task.ID, Type: model.EventSystemResponseBypassAudited, Payload: map[string]any{"sourceTaskId": sourceTaskID, "messageType": string(messageType), "reason": "system_response_queued_without_component_lease"}, RecordedAt: now}); err != nil {
		return err
	}
	message := model.UserMessage{ID: h.options.NewID("msg"), RunID: runID, TaskID: task.ID, Type: messageType, Title: title, Payload: responsesvc.RedactUserPayload(payload), Status: model.UserMessageQueued, IdempotencyKey: runID + ":" + sourceTaskID + ":" + string(messageType), CreatedAt: now, UpdatedAt: now}
	if err := uow.UserMessages().QueueMessage(ctx, message); err != nil {
		return err
	}
	m.Messages = append(m.Messages, message)
	return h.emit(ctx, uow, m, model.Event{RunID: runID, TaskID: task.ID, Type: model.EventUserMessageQueued, Payload: map[string]any{"messageId": message.ID, "message": responsesvc.UserMessagePayload(message), "task": eventpayload.Task(task)}, RecordedAt: now})
}

func (h submitTypedHandler) saveTask(ctx context.Context, uow ports.UnitOfWork, m *SubmitTypedResult, task model.Task) error {
	if err := uow.Tasks().SaveTask(ctx, task); err != nil {
		return err
	}
	m.Tasks = append(m.Tasks, task)
	return nil
}

func (h submitTypedHandler) saveRun(ctx context.Context, uow ports.UnitOfWork, m *SubmitTypedResult, previous model.Run, run model.Run) error {
	if err := uow.Runs().SaveRun(ctx, run); err != nil {
		return err
	}
	m.Runs = append(m.Runs, run)
	if previous.Status != run.Status {
		return h.emit(ctx, uow, m, model.Event{RunID: run.ID, TaskID: run.RootTaskID, Type: model.EventRunStatusChanged, Payload: map[string]any{"from": string(previous.Status), "to": string(run.Status), "run": eventpayload.Run(run)}, RecordedAt: time.Now().UTC()})
	}
	return nil
}

func (h submitTypedHandler) releaseLease(ctx context.Context, uow ports.UnitOfWork, m *SubmitTypedResult, lease model.TaskExecutionLease) (model.TaskExecutionLease, error) {
	lease.Status = model.LeaseStatusReleased
	if err := uow.Leases().SaveLease(ctx, lease); err != nil {
		return model.TaskExecutionLease{}, err
	}
	m.Leases = append(m.Leases, lease)
	if err := h.emit(ctx, uow, m, model.Event{RunID: lease.RunID, TaskID: lease.TaskID, Type: model.EventTaskExecutionReleased, Payload: map[string]any{"leaseId": lease.ID}, RecordedAt: time.Now().UTC()}); err != nil {
		return model.TaskExecutionLease{}, err
	}
	return lease, nil
}

func (h submitTypedHandler) queueEnvelope(ctx context.Context, uow ports.UnitOfWork, m *SubmitTypedResult, env model.TaskEnvelope, eventType model.EventType) error {
	if err := uow.MailboxOutbox().QueueEnvelope(ctx, env); err != nil {
		return err
	}
	m.Envelopes = append(m.Envelopes, env)
	return h.emit(ctx, uow, m, model.Event{RunID: env.RunID, TaskID: env.TaskID, Type: eventType, Payload: map[string]any{"envelope": eventpayload.Envelope(env)}, RecordedAt: time.Now().UTC()})
}

func (h submitTypedHandler) writeBlackboard(ctx context.Context, uow ports.UnitOfWork, m *SubmitTypedResult, item model.BlackboardItem) error {
	if err := uow.Blackboard().WriteItem(ctx, item); err != nil {
		return err
	}
	m.Blackboard = append(m.Blackboard, item)
	m.NotifyItems = append(m.NotifyItems, item)
	if err := h.recordTrace(ctx, uow, m, item.RunID, item.TaskID, "blackboard.write", "blackboard"); err != nil {
		return err
	}
	return h.emit(ctx, uow, m, model.Event{RunID: item.RunID, TaskID: item.TaskID, Type: model.EventBlackboardItemWritten, Payload: map[string]any{"itemId": item.ID, "sourceType": string(item.Source.Type), "sourceId": item.Source.ID, "visibility": string(item.Visibility), "key": item.Key}, RecordedAt: time.Now().UTC()})
}

func (h submitTypedHandler) emit(ctx context.Context, uow ports.UnitOfWork, m *SubmitTypedResult, event model.Event) error {
	if event.RecordedAt.IsZero() {
		event.RecordedAt = time.Now().UTC()
	}
	if err := uow.Events().AppendEvent(ctx, event); err != nil {
		return err
	}
	m.Events = append(m.Events, event)
	return nil
}

func (h submitTypedHandler) recordTrace(ctx context.Context, uow ports.UnitOfWork, m *SubmitTypedResult, runID, taskID, name, component string) error {
	now := time.Now().UTC()
	span := model.TraceSpan{RunID: runID, TaskID: taskID, Name: name, Component: component, Status: model.TraceSpanEnded, StartedAt: now, EndedAt: now}
	if err := uow.Trace().SaveTraceSpan(ctx, span); err != nil {
		return err
	}
	m.TraceSpans = append(m.TraceSpans, span)
	return nil
}

func completionCriteriaSatisfied(task model.Task, report model.TypedReport) bool {
	for _, criterion := range task.CompletionCriteria {
		criterion = strings.TrimSpace(criterion)
		if criterion == "" {
			continue
		}
		if !strings.Contains(strings.ToLower(report.Summary), strings.ToLower(criterion)) {
			return false
		}
	}
	return true
}

func canRetryTask(task model.Task) bool {
	maxAttempts := task.RetryPolicy.MaxAttempts
	return maxAttempts > 0 && task.Attempts < maxAttempts
}

func retryBackoff(base, maximum time.Duration, attempt int) time.Duration {
	if base <= 0 {
		return 0
	}
	const maxDuration = time.Duration(1<<63 - 1)
	limit := maxDuration
	if maximum > 0 {
		limit = maximum
	}
	if base >= limit {
		return limit
	}
	delay := base
	for retry := 1; retry < attempt; retry++ {
		if delay > limit/2 {
			return limit
		}
		delay *= 2
	}
	return delay
}

func actionAttemptFailed(status model.ActionAttemptStatus) bool {
	switch status {
	case model.ActionAttemptFailed, model.ActionAttemptTimeout, model.ActionAttemptCancelled:
		return true
	default:
		return false
	}
}

func reportFailureReason(report model.TypedReport) string {
	if report.ActionOutcome != nil && report.ActionOutcome.Error != "" {
		return report.ActionOutcome.Error
	}
	return report.Summary
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
