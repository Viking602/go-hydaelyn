package report

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Viking602/venat/api"
	commandbus "github.com/Viking602/venat/internal/command"
	"github.com/Viking602/venat/internal/core/ports"
	corestate "github.com/Viking602/venat/internal/core/state"
	"github.com/Viking602/venat/internal/eventpayload"
	"github.com/Viking602/venat/internal/execution"
	"github.com/Viking602/venat/internal/handoff"
	responsesvc "github.com/Viking602/venat/internal/response"
)

type IDGenerator func(string) string

type Authorizer func(context.Context, ports.UnitOfWork, api.PolicyRequest) (api.PolicyDecision, error)

type ApprovalFactory func(api.Task, string, string) (api.ApprovalRequest, api.ResumeToken)

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
	Runs         []api.Run
	Tasks        []api.Task
	Leases       []api.TaskExecutionLease
	Envelopes    []api.TaskEnvelope
	Messages     []api.UserMessage
	Approvals    []api.ApprovalRequest
	ResumeTokens []api.ResumeToken
	Blackboard   []api.BlackboardItem
	Events       []api.Event
	TraceSpans   []api.TraceSpan
	NotifyItems  []api.BlackboardItem
	CommitError  error
}

// NotifyBlackboard implements core.BlackboardNotifier. Method name avoids
// the collision with the NotifyItems field above.
func (r SubmitTypedResult) NotifyBlackboard() []api.BlackboardItem {
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
		report.Status = api.ReportStatusSuccess
	}
	m := &SubmitTypedResult{}
	if err := h.recordTrace(ctx, uow, m, cmd.RunID, cmd.TaskID, "report.submit_typed", "report"); err != nil {
		return nil, err
	}
	if err := h.emit(ctx, uow, m, api.Event{RunID: cmd.RunID, TaskID: cmd.TaskID, Type: api.EventTypedReportSubmitted, Payload: map[string]any{"status": string(report.Status), "holderType": string(cmd.HolderType), "holderId": cmd.HolderID, "taskVersion": cmd.TaskVersion}, RecordedAt: time.Now().UTC()}); err != nil {
		return nil, err
	}
	nextTask, nextRun, nextLease, nextReport, err := h.applyActionOutcome(ctx, uow, m, run, task, lease, cmd, report)
	if err != nil {
		if errors.Is(err, api.ErrActionReconcileRequired) {
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

func (h submitTypedHandler) applyActionOutcome(ctx context.Context, uow ports.UnitOfWork, m *SubmitTypedResult, run api.Run, task api.Task, lease api.TaskExecutionLease, cmd SubmitTypedCommand, report api.TypedReport) (api.Task, api.Run, api.TaskExecutionLease, api.TypedReport, error) {
	if report.ActionOutcome == nil {
		return task, run, lease, report, nil
	}
	attempt := api.ActionAttempt{AttemptID: report.ActionOutcome.AttemptID, ActionID: report.ActionOutcome.ActionID, RunID: cmd.RunID, TaskID: cmd.TaskID, Status: report.ActionOutcome.Status, ExternalResultRef: report.ActionOutcome.ExternalResultRef}
	if h.options.Authorize != nil {
		if _, err := h.options.Authorize(ctx, uow, api.PolicyRequest{Operation: api.PolicyOperationAction, RunID: cmd.RunID, TaskID: cmd.TaskID, Actor: actorFromHolder(cmd.HolderType, cmd.HolderID), Action: &attempt}); err != nil {
			return api.Task{}, api.Run{}, api.TaskExecutionLease{}, api.TypedReport{}, err
		}
	}
	if !task.AllowsAction {
		return api.Task{}, api.Run{}, api.TaskExecutionLease{}, api.TypedReport{}, api.ErrActionTaskRequired
	}
	switch report.ActionOutcome.Status {
	case api.ActionAttemptUnknown:
		if err := h.recordTrace(ctx, uow, m, task.RunID, task.ID, "action.reconcile_required", "action"); err != nil {
			return api.Task{}, api.Run{}, api.TaskExecutionLease{}, api.TypedReport{}, err
		}
		next, err := corestate.TransitionTask(task, api.TaskStatusReconcileRequired, true)
		if err != nil {
			return api.Task{}, api.Run{}, api.TaskExecutionLease{}, api.TypedReport{}, err
		}
		next.Error = "action attempt requires reconciliation"
		if err := h.saveTask(ctx, uow, m, next); err != nil {
			return api.Task{}, api.Run{}, api.TaskExecutionLease{}, api.TypedReport{}, err
		}
		item := api.BlackboardItem{RunID: next.RunID, TaskID: next.ID, Type: api.BlackboardItemEvidence, Source: api.SourceIdentity{Type: api.SourceTool, ID: report.ActionOutcome.AttemptID}, Visibility: api.BlackboardVisibilityAgentVisible, Key: "action_reconcile_required", Content: report.ActionOutcome.Summary, Payload: report.ActionOutcome.Summary, CreatedAt: time.Now().UTC()}
		if err := h.writeBlackboard(ctx, uow, m, item); err != nil {
			return api.Task{}, api.Run{}, api.TaskExecutionLease{}, api.TypedReport{}, err
		}
		if err := h.emit(ctx, uow, m, api.Event{RunID: next.RunID, TaskID: next.ID, Type: api.EventActionReconcileRequired, Payload: map[string]any{"attemptId": report.ActionOutcome.AttemptID, "status": string(report.ActionOutcome.Status)}, RecordedAt: time.Now().UTC()}); err != nil {
			return api.Task{}, api.Run{}, api.TaskExecutionLease{}, api.TypedReport{}, err
		}
		lease, err = h.releaseLease(ctx, uow, m, lease)
		if err != nil {
			return api.Task{}, api.Run{}, api.TaskExecutionLease{}, api.TypedReport{}, err
		}
		nextRun, err := corestate.TransitionRun(run, api.RunStatusReconcileRequired)
		if err != nil {
			return api.Task{}, api.Run{}, api.TaskExecutionLease{}, api.TypedReport{}, err
		}
		if err := h.saveRun(ctx, uow, m, run, nextRun); err != nil {
			return api.Task{}, api.Run{}, api.TaskExecutionLease{}, api.TypedReport{}, err
		}
		return next, nextRun, lease, report, api.ErrActionReconcileRequired
	case api.ActionAttemptFailed, api.ActionAttemptTimeout, api.ActionAttemptCancelled:
		task.Error = report.ActionOutcome.Error
	case api.ActionAttemptSucceeded:
		if err := h.recordTrace(ctx, uow, m, task.RunID, task.ID, "action.result", "action"); err != nil {
			return api.Task{}, api.Run{}, api.TaskExecutionLease{}, api.TypedReport{}, err
		}
		item := api.BlackboardItem{RunID: task.RunID, TaskID: task.ID, Type: api.BlackboardItemEvidence, Source: api.SourceIdentity{Type: api.SourceTool, ID: report.ActionOutcome.AttemptID}, Visibility: api.BlackboardVisibilityAgentVisible, Key: "action_outcome", Payload: report.ActionOutcome.Output, CreatedAt: time.Now().UTC()}
		if err := h.writeBlackboard(ctx, uow, m, item); err != nil {
			return api.Task{}, api.Run{}, api.TaskExecutionLease{}, api.TypedReport{}, err
		}
	}
	if actionAttemptFailed(report.ActionOutcome.Status) && report.Status == api.ReportStatusSuccess {
		report.Status = api.ReportStatusFailed
	}
	return task, run, lease, report, nil
}

func (h submitTypedHandler) applyReportStatus(ctx context.Context, uow ports.UnitOfWork, m *SubmitTypedResult, run api.Run, task api.Task, lease api.TaskExecutionLease, cmd SubmitTypedCommand, report api.TypedReport) error {
	switch report.Status {
	case api.ReportStatusSuccess:
		return h.applySuccessfulReport(ctx, uow, m, task, lease, report)
	case api.ReportStatusPartialSuccess:
		task.Result = &report
		return h.saveTask(ctx, uow, m, task)
	case api.ReportStatusFailed:
		return h.applyFailedReport(ctx, uow, m, task, lease, report)
	case api.ReportStatusBlocked:
		next, err := corestate.TransitionTask(task, api.TaskStatusBlocked, true)
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
		return h.emit(ctx, uow, m, api.Event{RunID: cmd.RunID, TaskID: cmd.TaskID, Type: api.EventTaskBlocked, Payload: map[string]any{"reason": report.Summary, "task": eventpayload.Task(next)}, RecordedAt: time.Now().UTC()})
	case api.ReportStatusNeedsApproval:
		return h.applyApprovalReport(ctx, uow, m, run, task, lease, cmd, report)
	case api.ReportStatusNeedsClarification:
		return h.applyClarificationReport(ctx, uow, m, run, task, lease, report)
	case api.ReportStatusNeedsHandoff:
		return h.applyHandoffReport(ctx, uow, m, task, lease, cmd, report)
	default:
		return api.ErrInvalidCommand
	}
}

func (h submitTypedHandler) applySuccessfulReport(ctx context.Context, uow ports.UnitOfWork, m *SubmitTypedResult, task api.Task, lease api.TaskExecutionLease, report api.TypedReport) error {
	if !completionCriteriaSatisfied(task, report) {
		return api.ErrCompletionCriteriaUnmet
	}
	next, err := corestate.TransitionTask(task, api.TaskStatusCompleted, true)
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
	return h.emit(ctx, uow, m, api.Event{RunID: task.RunID, TaskID: task.ID, Type: api.EventTaskCompleted, Payload: map[string]any{"summary": report.Summary, "task": eventpayload.Task(next)}, RecordedAt: time.Now().UTC()})
}

func (h submitTypedHandler) applyFailedReport(ctx context.Context, uow ports.UnitOfWork, m *SubmitTypedResult, task api.Task, lease api.TaskExecutionLease, report api.TypedReport) error {
	reason := reportFailureReason(report)
	if report.Retryable && canRetryTask(task) {
		next, err := corestate.TransitionTask(task, api.TaskStatusDispatched, true)
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
		env := api.TaskEnvelope{
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
		return h.queueEnvelope(ctx, uow, m, env, api.EventTaskDispatched)
	}
	next, err := corestate.TransitionTask(task, api.TaskStatusFailed, true)
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
	return h.emit(ctx, uow, m, api.Event{RunID: task.RunID, TaskID: task.ID, Type: api.EventTaskFailed, Payload: map[string]any{"reason": reason, "task": eventpayload.Task(next)}, RecordedAt: time.Now().UTC()})
}

func (h submitTypedHandler) applyApprovalReport(ctx context.Context, uow ports.UnitOfWork, m *SubmitTypedResult, run api.Run, task api.Task, lease api.TaskExecutionLease, cmd SubmitTypedCommand, report api.TypedReport) error {
	approval, token := h.options.NewApproval(task, report.Summary, cmd.HolderID)
	if err := uow.Approvals().SaveApproval(ctx, approval); err != nil {
		return err
	}
	m.Approvals = append(m.Approvals, approval)
	if err := uow.ResumeTokens().SaveResumeToken(ctx, token); err != nil {
		return err
	}
	m.ResumeTokens = append(m.ResumeTokens, token)
	if err := h.emit(ctx, uow, m, api.Event{RunID: token.RunID, TaskID: token.TaskID, Type: api.EventResumeTokenCreated, Payload: map[string]any{"tokenId": token.TokenID, "approvalId": token.ApprovalID, "expiresAt": token.ExpiresAt}, RecordedAt: time.Now().UTC()}); err != nil {
		return err
	}
	next, err := corestate.TransitionTask(task, api.TaskStatusPaused, true)
	if err != nil {
		return err
	}
	next.Result = &report
	if err := h.saveTask(ctx, uow, m, next); err != nil {
		return err
	}
	nextRun, err := corestate.TransitionRun(run, api.RunStatusWaitingApproval)
	if err != nil {
		return err
	}
	if err := h.saveRun(ctx, uow, m, run, nextRun); err != nil {
		return err
	}
	if _, err := h.releaseLease(ctx, uow, m, lease); err != nil {
		return err
	}
	item := responsesvc.CriticalContextItem("", task.RunID, task.ID, api.SourceIdentity{Type: api.SourceAgent, ID: cmd.HolderID}, "approval", report.Summary)
	if err := h.writeBlackboard(ctx, uow, m, item); err != nil {
		return err
	}
	if err := h.emit(ctx, uow, m, api.Event{RunID: cmd.RunID, TaskID: cmd.TaskID, Type: api.EventApprovalRequested, Payload: map[string]any{"approvalId": approval.ApprovalID, "resumeToken": token.TokenID, "reason": report.Summary}, RecordedAt: time.Now().UTC()}); err != nil {
		return err
	}
	return h.emit(ctx, uow, m, api.Event{RunID: cmd.RunID, TaskID: cmd.TaskID, Type: api.EventTaskPaused, Payload: map[string]any{"reason": report.Summary, "task": eventpayload.Task(next)}, RecordedAt: time.Now().UTC()})
}

func (h submitTypedHandler) applyClarificationReport(ctx context.Context, uow ports.UnitOfWork, m *SubmitTypedResult, run api.Run, task api.Task, lease api.TaskExecutionLease, report api.TypedReport) error {
	next, err := corestate.TransitionTask(task, api.TaskStatusWaitingUserInput, true)
	if err != nil {
		return err
	}
	next.Result = &report
	next.Error = report.Summary
	if err := h.saveTask(ctx, uow, m, next); err != nil {
		return err
	}
	nextRun, err := corestate.TransitionRun(run, api.RunStatusWaitingUserInput)
	if err != nil {
		return err
	}
	if err := h.saveRun(ctx, uow, m, run, nextRun); err != nil {
		return err
	}
	if _, err := h.releaseLease(ctx, uow, m, lease); err != nil {
		return err
	}
	if err := h.emit(ctx, uow, m, api.Event{RunID: next.RunID, TaskID: next.ID, Type: api.EventTaskBlocked, Payload: map[string]any{"reason": "needs_clarification", "task": eventpayload.Task(next)}, RecordedAt: time.Now().UTC()}); err != nil {
		return err
	}
	return h.queueSystemResponse(ctx, uow, m, next.RunID, next.ID, api.UserMessageTypeClarificationRequest, "Clarification requested", report.Summary)
}

func (h submitTypedHandler) applyHandoffReport(ctx context.Context, uow ports.UnitOfWork, m *SubmitTypedResult, task api.Task, lease api.TaskExecutionLease, cmd SubmitTypedCommand, report api.TypedReport) error {
	if report.Handoff == nil || report.Handoff.ToAgentID == "" {
		return api.ErrInvalidCommand
	}
	if h.options.Authorize != nil {
		if _, err := h.options.Authorize(ctx, uow, api.PolicyRequest{Operation: api.PolicyOperationHandoff, RunID: cmd.RunID, TaskID: cmd.TaskID, Actor: api.SourceIdentity{Type: api.SourceAgent, ID: cmd.HolderID}, Handoff: report.Handoff}); err != nil {
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

func (h submitTypedHandler) queueSystemResponse(ctx context.Context, uow ports.UnitOfWork, m *SubmitTypedResult, runID, sourceTaskID string, messageType api.UserMessageType, title, payload string) error {
	now := time.Now().UTC()
	task := api.Task{ID: h.options.NewID("response"), RunID: runID, ParentTaskID: sourceTaskID, Type: api.TaskTypeResponse, Goal: string(messageType), OwnerComponent: "response_composer", Status: api.TaskStatusCompleted, Version: 1, CreatedAt: now, UpdatedAt: now, Result: &api.TypedReport{Status: api.ReportStatusSuccess, Summary: payload}}
	if err := h.saveTask(ctx, uow, m, task); err != nil {
		return err
	}
	if err := h.emit(ctx, uow, m, api.Event{RunID: runID, TaskID: task.ID, Type: api.EventResponseTaskCreated, Payload: eventpayload.Task(task), RecordedAt: now}); err != nil {
		return err
	}
	if err := h.emit(ctx, uow, m, api.Event{RunID: runID, TaskID: task.ID, Type: api.EventSystemResponseBypassAudited, Payload: map[string]any{"sourceTaskId": sourceTaskID, "messageType": string(messageType), "reason": "system_response_queued_without_component_lease"}, RecordedAt: now}); err != nil {
		return err
	}
	message := api.UserMessage{ID: h.options.NewID("msg"), RunID: runID, TaskID: task.ID, Type: messageType, Title: title, Payload: responsesvc.RedactUserPayload(payload), Status: api.UserMessageQueued, IdempotencyKey: runID + ":" + sourceTaskID + ":" + string(messageType), CreatedAt: now, UpdatedAt: now}
	if err := uow.UserMessages().QueueMessage(ctx, message); err != nil {
		return err
	}
	m.Messages = append(m.Messages, message)
	return h.emit(ctx, uow, m, api.Event{RunID: runID, TaskID: task.ID, Type: api.EventUserMessageQueued, Payload: map[string]any{"messageId": message.ID, "message": responsesvc.UserMessagePayload(message), "task": eventpayload.Task(task)}, RecordedAt: now})
}

func (h submitTypedHandler) saveTask(ctx context.Context, uow ports.UnitOfWork, m *SubmitTypedResult, task api.Task) error {
	if err := uow.Tasks().SaveTask(ctx, task); err != nil {
		return err
	}
	m.Tasks = append(m.Tasks, task)
	return nil
}

func (h submitTypedHandler) saveRun(ctx context.Context, uow ports.UnitOfWork, m *SubmitTypedResult, previous api.Run, run api.Run) error {
	if err := uow.Runs().SaveRun(ctx, run); err != nil {
		return err
	}
	m.Runs = append(m.Runs, run)
	if previous.Status != run.Status {
		return h.emit(ctx, uow, m, api.Event{RunID: run.ID, TaskID: run.RootTaskID, Type: api.EventRunStatusChanged, Payload: map[string]any{"from": string(previous.Status), "to": string(run.Status), "run": eventpayload.Run(run)}, RecordedAt: time.Now().UTC()})
	}
	return nil
}

func (h submitTypedHandler) releaseLease(ctx context.Context, uow ports.UnitOfWork, m *SubmitTypedResult, lease api.TaskExecutionLease) (api.TaskExecutionLease, error) {
	now := time.Now().UTC()
	lease.Status = api.LeaseStatusReleased
	if err := uow.Leases().SaveLease(ctx, lease); err != nil {
		return api.TaskExecutionLease{}, err
	}
	if err := execution.ReleaseResourceClaims(ctx, uow, lease.ID, now); err != nil {
		return api.TaskExecutionLease{}, err
	}
	m.Leases = append(m.Leases, lease)
	if err := h.emit(ctx, uow, m, api.Event{RunID: lease.RunID, TaskID: lease.TaskID, Type: api.EventTaskExecutionReleased, Payload: map[string]any{"leaseId": lease.ID}, RecordedAt: now}); err != nil {
		return api.TaskExecutionLease{}, err
	}
	return lease, nil
}

func (h submitTypedHandler) queueEnvelope(ctx context.Context, uow ports.UnitOfWork, m *SubmitTypedResult, env api.TaskEnvelope, eventType api.EventType) error {
	if err := uow.MailboxOutbox().QueueEnvelope(ctx, env); err != nil {
		return err
	}
	m.Envelopes = append(m.Envelopes, env)
	return h.emit(ctx, uow, m, api.Event{RunID: env.RunID, TaskID: env.TaskID, Type: eventType, Payload: map[string]any{"envelope": eventpayload.Envelope(env)}, RecordedAt: time.Now().UTC()})
}

func (h submitTypedHandler) writeBlackboard(ctx context.Context, uow ports.UnitOfWork, m *SubmitTypedResult, item api.BlackboardItem) error {
	if err := uow.Blackboard().WriteItem(ctx, item); err != nil {
		return err
	}
	m.Blackboard = append(m.Blackboard, item)
	m.NotifyItems = append(m.NotifyItems, item)
	if err := h.recordTrace(ctx, uow, m, item.RunID, item.TaskID, "blackboard.write", "blackboard"); err != nil {
		return err
	}
	return h.emit(ctx, uow, m, api.Event{RunID: item.RunID, TaskID: item.TaskID, Type: api.EventBlackboardItemWritten, Payload: map[string]any{"itemId": item.ID, "sourceType": string(item.Source.Type), "sourceId": item.Source.ID, "visibility": string(item.Visibility), "key": item.Key}, RecordedAt: time.Now().UTC()})
}

func (h submitTypedHandler) emit(ctx context.Context, uow ports.UnitOfWork, m *SubmitTypedResult, event api.Event) error {
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
	span := api.TraceSpan{RunID: runID, TaskID: taskID, Name: name, Component: component, Status: api.TraceSpanEnded, StartedAt: now, EndedAt: now}
	if err := uow.Trace().SaveTraceSpan(ctx, span); err != nil {
		return err
	}
	m.TraceSpans = append(m.TraceSpans, span)
	return nil
}

func completionCriteriaSatisfied(task api.Task, report api.TypedReport) bool {
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

func canRetryTask(task api.Task) bool {
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

func actionAttemptFailed(status api.ActionAttemptStatus) bool {
	switch status {
	case api.ActionAttemptFailed, api.ActionAttemptTimeout, api.ActionAttemptCancelled:
		return true
	default:
		return false
	}
}

func reportFailureReason(report api.TypedReport) string {
	if report.ActionOutcome != nil && report.ActionOutcome.Error != "" {
		return report.ActionOutcome.Error
	}
	return report.Summary
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
