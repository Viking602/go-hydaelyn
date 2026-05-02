package core

import (
	"context"
	"errors"
	"time"

	commandbus "github.com/Viking602/go-hydaelyn/internal/core/command"
	"github.com/Viking602/go-hydaelyn/internal/core/ports"
)

func registerReportUoWCommandHandlers(runtime *Runtime) {
	commandbus.Register[SubmitTypedReportCommand](runtime.commandBus, submitTypedReportHandler{runtime: runtime})
}

type submitTypedReportHandler struct{ runtime *Runtime }

func (submitTypedReportHandler) Name() string { return SubmitTypedReportCommand{}.CommandName() }

type submitTypedReportResult struct {
	Runs         []Run
	Tasks        []Task
	Leases       []TaskExecutionLease
	Envelopes    []TaskEnvelope
	Messages     []UserMessage
	Approvals    []ApprovalRequest
	ResumeTokens []ResumeToken
	Blackboard   []BlackboardItem
	Events       []Event
	TraceSpans   []TraceSpan
	NotifyItems  []BlackboardItem
	CommitError  error
}

func (h submitTypedReportHandler) Handle(ctx context.Context, uow ports.UnitOfWork, cmd SubmitTypedReportCommand) (any, error) {
	run, task, lease, err := validateSubmissionUoW(ctx, uow, cmd.RunID, cmd.TaskID, cmd.LeaseID, cmd.HolderType, cmd.HolderID, cmd.TaskVersion)
	if err != nil {
		return nil, err
	}
	report := cmd.Report
	if report.Status == "" {
		report.Status = ReportStatusSuccess
	}
	m := &submitTypedReportResult{}
	if err := h.recordTrace(ctx, uow, m, cmd.RunID, cmd.TaskID, "report.submit_typed", "report"); err != nil {
		return nil, err
	}
	if err := h.emit(ctx, uow, m, Event{RunID: cmd.RunID, TaskID: cmd.TaskID, Type: EventTypedReportSubmitted, Payload: map[string]any{"status": string(report.Status), "holderType": string(cmd.HolderType), "holderId": cmd.HolderID, "taskVersion": cmd.TaskVersion}, RecordedAt: time.Now().UTC()}); err != nil {
		return nil, err
	}
	nextTask, nextRun, nextLease, nextReport, err := h.applyActionOutcome(ctx, uow, m, run, task, lease, cmd, report)
	if err != nil {
		if errors.Is(err, ErrActionReconcileRequired) {
			m.CommitError = err
			return *m, commitWithError(err)
		}
		return nil, err
	}
	if err := h.applyReportStatus(ctx, uow, m, nextRun, nextTask, nextLease, cmd, nextReport); err != nil {
		return nil, err
	}
	return *m, nil
}

func (h submitTypedReportHandler) applyActionOutcome(ctx context.Context, uow ports.UnitOfWork, m *submitTypedReportResult, run Run, task Task, lease TaskExecutionLease, cmd SubmitTypedReportCommand, report TypedReport) (Task, Run, TaskExecutionLease, TypedReport, error) {
	if report.ActionOutcome == nil {
		return task, run, lease, report, nil
	}
	attempt := ActionAttempt{AttemptID: report.ActionOutcome.AttemptID, ActionID: report.ActionOutcome.ActionID, RunID: cmd.RunID, TaskID: cmd.TaskID, Status: report.ActionOutcome.Status, ExternalResultRef: report.ActionOutcome.ExternalResultRef}
	if _, err := h.runtime.authorizeUoW(ctx, uow, PolicyRequest{Operation: PolicyOperationAction, RunID: cmd.RunID, TaskID: cmd.TaskID, Actor: actorFromHolder(cmd.HolderType, cmd.HolderID), Action: &attempt}); err != nil {
		return Task{}, Run{}, TaskExecutionLease{}, TypedReport{}, err
	}
	if !task.AllowsAction {
		return Task{}, Run{}, TaskExecutionLease{}, TypedReport{}, ErrActionTaskRequired
	}
	switch report.ActionOutcome.Status {
	case ActionAttemptUnknown:
		if err := h.recordTrace(ctx, uow, m, task.RunID, task.ID, "action.reconcile_required", "action"); err != nil {
			return Task{}, Run{}, TaskExecutionLease{}, TypedReport{}, err
		}
		next, err := transitionTaskPure(task, TaskStatusReconcileRequired, true)
		if err != nil {
			return Task{}, Run{}, TaskExecutionLease{}, TypedReport{}, err
		}
		next.Error = "action attempt requires reconciliation"
		if err := h.saveTask(ctx, uow, m, next); err != nil {
			return Task{}, Run{}, TaskExecutionLease{}, TypedReport{}, err
		}
		item := BlackboardItem{RunID: next.RunID, TaskID: next.ID, Type: BlackboardItemEvidence, Source: SourceIdentity{Type: SourceTool, ID: report.ActionOutcome.AttemptID}, Visibility: BlackboardVisibilityAgentVisible, Key: "action_reconcile_required", Content: report.ActionOutcome.Summary, Payload: report.ActionOutcome.Summary, CreatedAt: time.Now().UTC()}
		if err := h.writeBlackboard(ctx, uow, m, item); err != nil {
			return Task{}, Run{}, TaskExecutionLease{}, TypedReport{}, err
		}
		if err := h.emit(ctx, uow, m, Event{RunID: next.RunID, TaskID: next.ID, Type: EventActionReconcileRequired, Payload: map[string]any{"attemptId": report.ActionOutcome.AttemptID, "status": string(report.ActionOutcome.Status)}, RecordedAt: time.Now().UTC()}); err != nil {
			return Task{}, Run{}, TaskExecutionLease{}, TypedReport{}, err
		}
		lease, err = h.releaseLease(ctx, uow, m, lease)
		if err != nil {
			return Task{}, Run{}, TaskExecutionLease{}, TypedReport{}, err
		}
		nextRun, err := transitionRunPure(run, RunStatusReconcileRequired)
		if err != nil {
			return Task{}, Run{}, TaskExecutionLease{}, TypedReport{}, err
		}
		if err := h.saveRun(ctx, uow, m, run, nextRun); err != nil {
			return Task{}, Run{}, TaskExecutionLease{}, TypedReport{}, err
		}
		return next, nextRun, lease, report, ErrActionReconcileRequired
	case ActionAttemptFailed, ActionAttemptTimeout, ActionAttemptCancelled:
		task.Error = report.ActionOutcome.Error
	case ActionAttemptSucceeded:
		if err := h.recordTrace(ctx, uow, m, task.RunID, task.ID, "action.result", "action"); err != nil {
			return Task{}, Run{}, TaskExecutionLease{}, TypedReport{}, err
		}
		item := BlackboardItem{RunID: task.RunID, TaskID: task.ID, Type: BlackboardItemEvidence, Source: SourceIdentity{Type: SourceTool, ID: report.ActionOutcome.AttemptID}, Visibility: BlackboardVisibilityAgentVisible, Key: "action_outcome", Payload: report.ActionOutcome.Output, CreatedAt: time.Now().UTC()}
		if err := h.writeBlackboard(ctx, uow, m, item); err != nil {
			return Task{}, Run{}, TaskExecutionLease{}, TypedReport{}, err
		}
	}
	if actionAttemptFailed(report.ActionOutcome.Status) && report.Status == ReportStatusSuccess {
		report.Status = ReportStatusFailed
	}
	return task, run, lease, report, nil
}

func (h submitTypedReportHandler) applyReportStatus(ctx context.Context, uow ports.UnitOfWork, m *submitTypedReportResult, run Run, task Task, lease TaskExecutionLease, cmd SubmitTypedReportCommand, report TypedReport) error {
	switch report.Status {
	case ReportStatusSuccess:
		return h.applySuccessfulReport(ctx, uow, m, task, lease, report)
	case ReportStatusPartialSuccess:
		task.Result = &report
		return h.saveTask(ctx, uow, m, task)
	case ReportStatusFailed:
		return h.applyFailedReport(ctx, uow, m, task, lease, report)
	case ReportStatusBlocked:
		next, err := transitionTaskPure(task, TaskStatusBlocked, true)
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
		return h.emit(ctx, uow, m, Event{RunID: cmd.RunID, TaskID: cmd.TaskID, Type: EventTaskBlocked, Payload: map[string]any{"reason": report.Summary, "task": taskEventPayload(next)}, RecordedAt: time.Now().UTC()})
	case ReportStatusNeedsApproval:
		return h.applyApprovalReport(ctx, uow, m, run, task, lease, cmd, report)
	case ReportStatusNeedsClarification:
		return h.applyClarificationReport(ctx, uow, m, run, task, lease, report)
	case ReportStatusNeedsHandoff:
		return h.applyHandoffReport(ctx, uow, m, task, lease, cmd, report)
	default:
		return ErrInvalidCommand
	}
}

func (h submitTypedReportHandler) applySuccessfulReport(ctx context.Context, uow ports.UnitOfWork, m *submitTypedReportResult, task Task, lease TaskExecutionLease, report TypedReport) error {
	if !completionCriteriaSatisfied(task, report) {
		return ErrCompletionCriteriaUnmet
	}
	next, err := transitionTaskPure(task, TaskStatusCompleted, true)
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
	return h.emit(ctx, uow, m, Event{RunID: task.RunID, TaskID: task.ID, Type: EventTaskCompleted, Payload: map[string]any{"summary": report.Summary, "task": taskEventPayload(next)}, RecordedAt: time.Now().UTC()})
}

func (h submitTypedReportHandler) applyFailedReport(ctx context.Context, uow ports.UnitOfWork, m *submitTypedReportResult, task Task, lease TaskExecutionLease, report TypedReport) error {
	reason := reportFailureReason(report)
	if canRetryTask(task) {
		next, err := transitionTaskPure(task, TaskStatusDispatched, true)
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
		env := TaskEnvelope{ID: h.runtime.newID("env"), RunID: next.RunID, TaskID: next.ID, TargetAgentID: next.OwnerAgentID, TargetComponent: next.OwnerComponent, Type: "TaskEnvelope", Status: "pending", TaskVersion: next.Version, RetryPolicy: next.RetryPolicy, CreatedAt: time.Now().UTC()}
		return h.queueEnvelope(ctx, uow, m, env, EventTaskDispatched)
	}
	next, err := transitionTaskPure(task, TaskStatusFailed, true)
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
	return h.emit(ctx, uow, m, Event{RunID: task.RunID, TaskID: task.ID, Type: EventTaskFailed, Payload: map[string]any{"reason": reason, "task": taskEventPayload(next)}, RecordedAt: time.Now().UTC()})
}

func (h submitTypedReportHandler) applyApprovalReport(ctx context.Context, uow ports.UnitOfWork, m *submitTypedReportResult, run Run, task Task, lease TaskExecutionLease, cmd SubmitTypedReportCommand, report TypedReport) error {
	approval, token := h.runtime.newApprovalForTask(task, report.Summary, cmd.HolderID)
	if err := uow.Approvals().SaveApproval(ctx, approval); err != nil {
		return err
	}
	m.Approvals = append(m.Approvals, approval)
	if err := uow.ResumeTokens().SaveResumeToken(ctx, token); err != nil {
		return err
	}
	m.ResumeTokens = append(m.ResumeTokens, token)
	if err := h.emit(ctx, uow, m, Event{RunID: token.RunID, TaskID: token.TaskID, Type: EventResumeTokenCreated, Payload: map[string]any{"tokenId": token.TokenID, "approvalId": token.ApprovalID, "expiresAt": token.ExpiresAt}, RecordedAt: time.Now().UTC()}); err != nil {
		return err
	}
	next, err := transitionTaskPure(task, TaskStatusPaused, true)
	if err != nil {
		return err
	}
	next.Result = &report
	if err := h.saveTask(ctx, uow, m, next); err != nil {
		return err
	}
	nextRun, err := transitionRunPure(run, RunStatusWaitingApproval)
	if err != nil {
		return err
	}
	if err := h.saveRun(ctx, uow, m, run, nextRun); err != nil {
		return err
	}
	if _, err := h.releaseLease(ctx, uow, m, lease); err != nil {
		return err
	}
	item := criticalContextItem("", task.RunID, task.ID, SourceIdentity{Type: SourceAgent, ID: cmd.HolderID}, "approval", report.Summary)
	if err := h.writeBlackboard(ctx, uow, m, item); err != nil {
		return err
	}
	if err := h.emit(ctx, uow, m, Event{RunID: cmd.RunID, TaskID: cmd.TaskID, Type: EventApprovalRequested, Payload: map[string]any{"approvalId": approval.ApprovalID, "resumeToken": token.TokenID, "reason": report.Summary}, RecordedAt: time.Now().UTC()}); err != nil {
		return err
	}
	return h.emit(ctx, uow, m, Event{RunID: cmd.RunID, TaskID: cmd.TaskID, Type: EventTaskPaused, Payload: map[string]any{"reason": report.Summary, "task": taskEventPayload(next)}, RecordedAt: time.Now().UTC()})
}

func (h submitTypedReportHandler) applyClarificationReport(ctx context.Context, uow ports.UnitOfWork, m *submitTypedReportResult, run Run, task Task, lease TaskExecutionLease, report TypedReport) error {
	next, err := transitionTaskPure(task, TaskStatusWaitingUserInput, true)
	if err != nil {
		return err
	}
	next.Result = &report
	next.Error = report.Summary
	if err := h.saveTask(ctx, uow, m, next); err != nil {
		return err
	}
	nextRun, err := transitionRunPure(run, RunStatusWaitingUserInput)
	if err != nil {
		return err
	}
	if err := h.saveRun(ctx, uow, m, run, nextRun); err != nil {
		return err
	}
	if _, err := h.releaseLease(ctx, uow, m, lease); err != nil {
		return err
	}
	if err := h.emit(ctx, uow, m, Event{RunID: next.RunID, TaskID: next.ID, Type: EventTaskBlocked, Payload: map[string]any{"reason": "needs_clarification", "task": taskEventPayload(next)}, RecordedAt: time.Now().UTC()}); err != nil {
		return err
	}
	return h.queueSystemResponse(ctx, uow, m, next.RunID, next.ID, UserMessageTypeClarificationRequest, "Clarification requested", report.Summary)
}

func (h submitTypedReportHandler) applyHandoffReport(ctx context.Context, uow ports.UnitOfWork, m *submitTypedReportResult, task Task, lease TaskExecutionLease, cmd SubmitTypedReportCommand, report TypedReport) error {
	if report.Handoff == nil || report.Handoff.ToAgentID == "" {
		return ErrInvalidCommand
	}
	if _, err := h.runtime.authorizeUoW(ctx, uow, PolicyRequest{Operation: PolicyOperationHandoff, RunID: cmd.RunID, TaskID: cmd.TaskID, Actor: SourceIdentity{Type: SourceAgent, ID: cmd.HolderID}, Handoff: report.Handoff}); err != nil {
		return err
	}
	if err := h.applyHandoff(ctx, uow, m, task, report.Handoff, report.Summary); err != nil {
		return err
	}
	_, err := h.releaseLease(ctx, uow, m, lease)
	return err
}

func (h submitTypedReportHandler) applyHandoff(ctx context.Context, uow ports.UnitOfWork, m *submitTypedReportResult, task Task, request *HandoffRequest, fallbackContext string) error {
	result, err := handoffHandler(h).apply(ctx, uow, task, request, fallbackContext)
	if err != nil {
		return err
	}
	m.Tasks = append(m.Tasks, result.Task)
	m.Envelopes = append(m.Envelopes, result.Envelope)
	if result.HasContext {
		m.Blackboard = append(m.Blackboard, result.BlackboardItem)
		m.NotifyItems = append(m.NotifyItems, result.BlackboardItem)
	}
	return nil
}

func (h submitTypedReportHandler) queueSystemResponse(ctx context.Context, uow ports.UnitOfWork, m *submitTypedReportResult, runID, sourceTaskID string, messageType UserMessageType, title, payload string) error {
	now := time.Now().UTC()
	task := Task{ID: h.runtime.newID("response"), RunID: runID, ParentTaskID: sourceTaskID, Type: TaskTypeResponse, Goal: string(messageType), OwnerComponent: "response_composer", Status: TaskStatusCompleted, Version: 1, CreatedAt: now, UpdatedAt: now, Result: &TypedReport{Status: ReportStatusSuccess, Summary: payload}}
	if err := h.saveTask(ctx, uow, m, task); err != nil {
		return err
	}
	if err := h.emit(ctx, uow, m, Event{RunID: runID, TaskID: task.ID, Type: EventResponseTaskCreated, Payload: taskEventPayload(task), RecordedAt: now}); err != nil {
		return err
	}
	if err := h.emit(ctx, uow, m, Event{RunID: runID, TaskID: task.ID, Type: EventSystemResponseBypassAudited, Payload: map[string]any{"sourceTaskId": sourceTaskID, "messageType": string(messageType), "reason": "system_response_queued_without_component_lease"}, RecordedAt: now}); err != nil {
		return err
	}
	message := UserMessage{ID: h.runtime.newID("msg"), RunID: runID, TaskID: task.ID, Type: messageType, Title: title, Payload: redactUserPayload(payload), Status: UserMessageQueued, IdempotencyKey: runID + ":" + sourceTaskID + ":" + string(messageType), CreatedAt: now, UpdatedAt: now}
	if err := uow.UserMessages().QueueMessage(ctx, message); err != nil {
		return err
	}
	m.Messages = append(m.Messages, message)
	return h.emit(ctx, uow, m, Event{RunID: runID, TaskID: task.ID, Type: EventUserMessageQueued, Payload: map[string]any{"messageId": message.ID, "message": userMessagePayload(message), "task": taskEventPayload(task)}, RecordedAt: now})
}

func (h submitTypedReportHandler) saveTask(ctx context.Context, uow ports.UnitOfWork, m *submitTypedReportResult, task Task) error {
	if err := uow.Tasks().SaveTask(ctx, task); err != nil {
		return err
	}
	m.Tasks = append(m.Tasks, task)
	return nil
}

func (h submitTypedReportHandler) saveRun(ctx context.Context, uow ports.UnitOfWork, m *submitTypedReportResult, previous Run, run Run) error {
	if err := uow.Runs().SaveRun(ctx, run); err != nil {
		return err
	}
	m.Runs = append(m.Runs, run)
	if previous.Status != run.Status {
		return h.emit(ctx, uow, m, Event{RunID: run.ID, TaskID: run.RootTaskID, Type: EventRunStatusChanged, Payload: map[string]any{"from": string(previous.Status), "to": string(run.Status), "run": runPayload(run)}, RecordedAt: time.Now().UTC()})
	}
	return nil
}

func (h submitTypedReportHandler) releaseLease(ctx context.Context, uow ports.UnitOfWork, m *submitTypedReportResult, lease TaskExecutionLease) (TaskExecutionLease, error) {
	lease.Status = LeaseStatusReleased
	if err := uow.Leases().SaveLease(ctx, lease); err != nil {
		return TaskExecutionLease{}, err
	}
	m.Leases = append(m.Leases, lease)
	if err := h.emit(ctx, uow, m, Event{RunID: lease.RunID, TaskID: lease.TaskID, Type: EventTaskExecutionReleased, Payload: map[string]any{"leaseId": lease.ID}, RecordedAt: time.Now().UTC()}); err != nil {
		return TaskExecutionLease{}, err
	}
	return lease, nil
}

func (h submitTypedReportHandler) queueEnvelope(ctx context.Context, uow ports.UnitOfWork, m *submitTypedReportResult, env TaskEnvelope, eventType EventType) error {
	if err := uow.MailboxOutbox().QueueEnvelope(ctx, env); err != nil {
		return err
	}
	m.Envelopes = append(m.Envelopes, env)
	return h.emit(ctx, uow, m, Event{RunID: env.RunID, TaskID: env.TaskID, Type: eventType, Payload: map[string]any{"envelope": envPayload(env)}, RecordedAt: time.Now().UTC()})
}

func (h submitTypedReportHandler) writeBlackboard(ctx context.Context, uow ports.UnitOfWork, m *submitTypedReportResult, item BlackboardItem) error {
	if err := uow.Blackboard().WriteItem(ctx, item); err != nil {
		return err
	}
	m.Blackboard = append(m.Blackboard, item)
	m.NotifyItems = append(m.NotifyItems, item)
	if err := h.recordTrace(ctx, uow, m, item.RunID, item.TaskID, "blackboard.write", "blackboard"); err != nil {
		return err
	}
	return h.emit(ctx, uow, m, Event{RunID: item.RunID, TaskID: item.TaskID, Type: EventBlackboardItemWritten, Payload: map[string]any{"itemId": item.ID, "sourceType": string(item.Source.Type), "sourceId": item.Source.ID, "visibility": string(item.Visibility), "key": item.Key}, RecordedAt: time.Now().UTC()})
}

func (h submitTypedReportHandler) emit(ctx context.Context, uow ports.UnitOfWork, m *submitTypedReportResult, event Event) error {
	if event.RecordedAt.IsZero() {
		event.RecordedAt = time.Now().UTC()
	}
	if err := uow.Events().AppendEvent(ctx, event); err != nil {
		return err
	}
	m.Events = append(m.Events, event)
	return nil
}

func (h submitTypedReportHandler) recordTrace(ctx context.Context, uow ports.UnitOfWork, m *submitTypedReportResult, runID, taskID, name, component string) error {
	now := time.Now().UTC()
	span := TraceSpan{RunID: runID, TaskID: taskID, Name: name, Component: component, Status: TraceSpanEnded, StartedAt: now, EndedAt: now}
	if err := uow.Trace().SaveTraceSpan(ctx, span); err != nil {
		return err
	}
	m.TraceSpans = append(m.TraceSpans, span)
	return nil
}
