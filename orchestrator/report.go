package orchestrator

import (
	"context"
	"errors"
	"strings"
	"time"
)

type SubmitTypedReportCommand struct {
	RunID       string
	TaskID      string
	LeaseID     string
	HolderType  HolderType
	HolderID    string
	TaskVersion int
	Report      TypedReport
}

type SubmitUserInputCommand struct {
	RunID  string
	TaskID string
	Input  string
}

type ToolInvocation struct {
	RunID       string
	TaskID      string
	LeaseID     string
	HolderType  HolderType
	HolderID    string
	TaskVersion int
	ToolName    string
	Input       any
}

type ToolInvocationResult struct {
	ToolName string
	Output   any
}

func (r *Runtime) SubmitTypedReport(ctx context.Context, cmd SubmitTypedReportCommand) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, task, err := r.validateSubmissionLocked(cmd.RunID, cmd.TaskID, cmd.LeaseID, cmd.HolderType, cmd.HolderID, cmd.TaskVersion)
	if err != nil {
		return err
	}
	report := cmd.Report
	if report.Status == "" {
		report.Status = ReportStatusSuccess
	}
	r.recordTraceLocked(cmd.RunID, cmd.TaskID, "report.submit_typed", "report")
	r.appendEventLocked(cmd.RunID, cmd.TaskID, EventTypedReportSubmitted, map[string]any{
		"status":      string(report.Status),
		"holderType":  string(cmd.HolderType),
		"holderId":    cmd.HolderID,
		"taskVersion": cmd.TaskVersion,
	})
	task, report, err = r.applyReportActionResultLocked(ctx, run, task, cmd, report)
	if err != nil {
		return err
	}
	return r.applyReportStatusLocked(ctx, run, task, cmd, report)
}

func (r *Runtime) applyReportActionResultLocked(ctx context.Context, run Run, task Task, cmd SubmitTypedReportCommand, report TypedReport) (Task, TypedReport, error) {
	if report.ActionResult != nil {
		attempt := ActionAttempt{
			AttemptID:         report.ActionResult.AttemptID,
			ActionID:          report.ActionResult.ActionID,
			RunID:             cmd.RunID,
			TaskID:            cmd.TaskID,
			Status:            report.ActionResult.Status,
			ExternalResultRef: report.ActionResult.ExternalResultRef,
		}
		if _, err := r.authorizeLocked(ctx, PolicyRequest{
			Operation: PolicyOperationAction,
			RunID:     cmd.RunID,
			TaskID:    cmd.TaskID,
			Actor:     actorFromHolder(cmd.HolderType, cmd.HolderID),
			Action:    &attempt,
		}); err != nil {
			return Task{}, TypedReport{}, err
		}
		if err := r.applyActionResultLocked(&task, report.ActionResult); err != nil {
			r.tasks[task.RunID][task.ID] = task
			if errors.Is(err, ErrActionReconcileRequired) {
				if _, runErr := r.updateRunLocked(run, RunStatusReconcileRequired); runErr != nil {
					return Task{}, TypedReport{}, runErr
				}
			}
			return Task{}, TypedReport{}, err
		}
		if actionAttemptFailed(report.ActionResult.Status) && report.Status == ReportStatusSuccess {
			report.Status = ReportStatusFailed
		}
	}
	return task, report, nil
}

func (r *Runtime) applyReportStatusLocked(ctx context.Context, run Run, task Task, cmd SubmitTypedReportCommand, report TypedReport) error {
	switch report.Status {
	case ReportStatusSuccess:
		return r.applySuccessfulReportLocked(task, cmd.LeaseID, report)
	case ReportStatusPartialSuccess:
		task.Status = TaskStatusRunning
		task.Result = &report
		r.saveTaskLocked(task)
		return nil
	case ReportStatusFailed:
		return r.applyFailedReportLocked(task, cmd.LeaseID, report)
	case ReportStatusBlocked:
		task.Status = TaskStatusBlocked
		task.Error = report.Summary
		task.Result = &report
		task.Version++
		r.saveTaskLocked(task)
		r.appendEventLocked(cmd.RunID, cmd.TaskID, EventTaskBlocked, map[string]any{
			"reason": report.Summary,
			"task":   taskEventPayload(task),
		})
		return nil
	case ReportStatusNeedsApproval:
		return r.applyApprovalReportLocked(run, task, cmd, report)
	case ReportStatusNeedsClarification:
		return r.applyClarificationReportLocked(run, task, cmd.LeaseID, report)
	case ReportStatusNeedsHandoff:
		return r.applyHandoffReportLocked(ctx, task, cmd, report)
	default:
		return ErrInvalidCommand
	}
}

func (r *Runtime) applySuccessfulReportLocked(task Task, leaseID string, report TypedReport) error {
	if !completionCriteriaSatisfied(task, report) {
		return ErrCompletionCriteriaUnmet
	}
	task.Status = TaskStatusCompleted
	task.Result = &report
	task.Version++
	r.saveTaskLocked(task)
	r.releaseLeaseLocked(leaseID)
	r.appendEventLocked(task.RunID, task.ID, EventTaskCompleted, map[string]any{
		"summary": report.Summary,
		"task":    taskEventPayload(task),
	})
	return nil
}

func (r *Runtime) applyFailedReportLocked(task Task, leaseID string, report TypedReport) error {
	reason := reportFailureReason(report)
	if canRetryTask(task) {
		task.Status = TaskStatusDispatched
		task.Error = reason
		task.Result = &report
		task.Version++
		r.saveTaskLocked(task)
		r.releaseLeaseLocked(leaseID)
		r.writeEnvelopeLocked(TaskEnvelope{
			RunID:           task.RunID,
			TaskID:          task.ID,
			TargetAgentID:   task.OwnerAgentID,
			TargetComponent: task.OwnerComponent,
			Type:            "TaskEnvelope",
			Status:          "pending",
			TaskVersion:     task.Version,
			RetryPolicy:     task.RetryPolicy,
			CreatedAt:       time.Now().UTC(),
		})
		return nil
	}
	task.Status = TaskStatusFailed
	task.Error = reason
	task.Result = &report
	task.Version++
	r.saveTaskLocked(task)
	r.releaseLeaseLocked(leaseID)
	r.appendEventLocked(task.RunID, task.ID, EventTaskFailed, map[string]any{
		"reason": reason,
		"task":   taskEventPayload(task),
	})
	return nil
}

func (r *Runtime) applyApprovalReportLocked(run Run, task Task, cmd SubmitTypedReportCommand, report TypedReport) error {
	approval, token := r.createApprovalLocked(task, report.Summary, cmd.HolderID)
	task.Status = TaskStatusPaused
	task.Result = &report
	task.Version++
	r.saveTaskLocked(task)
	if _, err := r.updateRunLocked(run, RunStatusWaitingApproval); err != nil {
		return err
	}
	r.writeCriticalContextLocked(task.RunID, task.ID, BlackboardItemContext, SourceIdentity{Type: SourceAgent, ID: cmd.HolderID}, "approval", report.Summary)
	r.appendEventLocked(cmd.RunID, cmd.TaskID, EventApprovalRequested, map[string]any{
		"approvalId":  approval.ApprovalID,
		"resumeToken": token.TokenID,
		"reason":      report.Summary,
	})
	r.appendEventLocked(cmd.RunID, cmd.TaskID, EventTaskPaused, map[string]any{
		"reason": report.Summary,
		"task":   taskEventPayload(task),
	})
	return nil
}

func (r *Runtime) applyClarificationReportLocked(run Run, task Task, leaseID string, report TypedReport) error {
	task.Status = TaskStatusWaitingUserInput
	task.Result = &report
	task.Error = report.Summary
	task.Version++
	r.saveTaskLocked(task)
	if _, err := r.updateRunLocked(run, RunStatusWaitingUserInput); err != nil {
		return err
	}
	r.releaseLeaseLocked(leaseID)
	r.appendEventLocked(task.RunID, task.ID, EventTaskBlocked, map[string]any{
		"reason": "needs_clarification",
		"task":   taskEventPayload(task),
	})
	r.queueSystemResponseLocked(task.RunID, task.ID, UserMessageTypeClarificationRequest, "Clarification requested", report.Summary)
	return nil
}

func (r *Runtime) applyHandoffReportLocked(ctx context.Context, task Task, cmd SubmitTypedReportCommand, report TypedReport) error {
	if report.Handoff == nil || report.Handoff.ToAgentID == "" {
		return ErrInvalidCommand
	}
	if _, err := r.authorizeLocked(ctx, PolicyRequest{
		Operation: PolicyOperationHandoff,
		RunID:     cmd.RunID,
		TaskID:    cmd.TaskID,
		Actor:     SourceIdentity{Type: SourceAgent, ID: cmd.HolderID},
		Handoff:   report.Handoff,
	}); err != nil {
		return err
	}
	if err := r.applyHandoffLocked(&task, report.Handoff, report.Summary); err != nil {
		return err
	}
	r.releaseLeaseLocked(cmd.LeaseID)
	return nil
}

func (r *Runtime) SubmitUserInput(_ context.Context, cmd SubmitUserInputCommand) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.runs[cmd.RunID]
	if !ok {
		return ErrNotFound
	}
	if isTerminalRun(run.Status) {
		return ErrTerminalState
	}
	item := BlackboardItem{
		RunID:      cmd.RunID,
		TaskID:     cmd.TaskID,
		Source:     SourceIdentity{Type: SourceSystem, ID: "user"},
		Visibility: BlackboardVisibilityAgentVisible,
		Key:        "user_input",
		Payload:    cmd.Input,
	}
	r.writeBlackboardLocked(item)
	if _, err := r.updateRunLocked(run, RunStatusRunning); err != nil {
		return err
	}
	if task, ok := r.tasks[cmd.RunID][cmd.TaskID]; ok && (task.Status == TaskStatusBlocked || task.Status == TaskStatusWaitingUserInput) {
		task.Status = TaskStatusRunning
		task.Version++
		r.saveTaskLocked(task)
	}
	r.appendEventLocked(cmd.RunID, cmd.TaskID, EventUserInputSubmitted, map[string]any{
		"input": cmd.Input,
	})
	return nil
}

func completionCriteriaSatisfied(task Task, report TypedReport) bool {
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

func canRetryTask(task Task) bool {
	maxAttempts := task.RetryPolicy.MaxAttempts
	return maxAttempts > 0 && task.Attempts < maxAttempts
}

func actionAttemptFailed(status ActionAttemptStatus) bool {
	switch status {
	case ActionAttemptFailed, ActionAttemptTimeout, ActionAttemptCancelled:
		return true
	default:
		return false
	}
}

func reportFailureReason(report TypedReport) string {
	if report.ActionResult != nil && report.ActionResult.Error != "" {
		return report.ActionResult.Error
	}
	return report.Summary
}

func (r *Runtime) InvokeTool(ctx context.Context, cmd ToolInvocation) (ToolInvocationResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	tool, ok := r.tools[cmd.ToolName]
	if !ok {
		return ToolInvocationResult{}, ErrNotFound
	}
	task, ok := r.tasks[cmd.RunID][cmd.TaskID]
	if !ok {
		return ToolInvocationResult{}, ErrNotFound
	}
	if tool.RequiresActionTask || tool.EffectType == ToolEffectWrite || tool.EffectType == ToolEffectExternalSideEffect {
		if task.Type != TaskTypeAction {
			return ToolInvocationResult{}, ErrActionTaskRequired
		}
		if _, _, err := r.validateSubmissionLocked(cmd.RunID, cmd.TaskID, cmd.LeaseID, cmd.HolderType, cmd.HolderID, cmd.TaskVersion); err != nil {
			return ToolInvocationResult{}, err
		}
	}
	if _, err := r.authorizeLocked(ctx, PolicyRequest{
		Operation: PolicyOperationToolCall,
		RunID:     cmd.RunID,
		TaskID:    cmd.TaskID,
		Actor:     SourceIdentity{Type: SourceAgent, ID: task.OwnerAgentID},
		Tool:      &tool,
	}); err != nil {
		return ToolInvocationResult{}, err
	}
	r.recordTraceLocked(cmd.RunID, cmd.TaskID, "tool.call", "tool")
	return ToolInvocationResult{ToolName: cmd.ToolName, Output: cmd.Input}, nil
}

func (r *Runtime) validateSubmissionLocked(runID, taskID, leaseID string, holderType HolderType, holderID string, taskVersion int) (Run, Task, error) {
	run, ok := r.runs[runID]
	if !ok {
		return Run{}, Task{}, ErrNotFound
	}
	if isTerminalRun(run.Status) {
		return Run{}, Task{}, ErrTerminalState
	}
	task, ok := r.tasks[runID][taskID]
	if !ok {
		return Run{}, Task{}, ErrNotFound
	}
	if isTerminalTask(task.Status) {
		return Run{}, Task{}, ErrTerminalState
	}
	if taskVersion != task.Version {
		return Run{}, Task{}, ErrStaleTaskVersion
	}
	if err := validateTaskHolder(task, holderType, holderID); err != nil {
		return Run{}, Task{}, err
	}
	lease, ok := r.leases[leaseID]
	if !ok || lease.Status != LeaseStatusActive {
		return Run{}, Task{}, ErrLeaseNotActive
	}
	if lease.RunID != runID || lease.TaskID != taskID || lease.HolderType != holderType || lease.HolderID != holderID {
		return Run{}, Task{}, ErrLeaseHolderMismatch
	}
	if lease.ExpiresAt.Before(time.Now().UTC()) {
		lease.Status = LeaseStatusExpired
		r.leases[lease.ID] = lease
		delete(r.activeLeaseByTask, activeLeaseKey(runID, taskID))
		return Run{}, Task{}, ErrLeaseNotActive
	}
	return run, task, nil
}

func (r *Runtime) applyActionResultLocked(task *Task, result *ActionResult) error {
	if task.Type != TaskTypeAction {
		return ErrActionTaskRequired
	}
	switch result.Status {
	case ActionAttemptUnknown:
		r.recordTraceLocked(task.RunID, task.ID, "action.reconcile_required", "action")
		task.Status = TaskStatusReconcileRequired
		task.Error = "action attempt requires reconciliation"
		task.Version++
		r.writeCriticalContextLocked(task.RunID, task.ID, BlackboardItemActionResult, SourceIdentity{Type: SourceTool, ID: result.AttemptID}, "action_reconcile_required", result.Summary)
		r.appendEventLocked(task.RunID, task.ID, EventActionReconcileRequired, map[string]any{
			"attemptId": result.AttemptID,
			"status":    string(result.Status),
		})
		return ErrActionReconcileRequired
	case ActionAttemptFailed, ActionAttemptTimeout, ActionAttemptCancelled:
		task.Status = TaskStatusFailed
		task.Error = result.Error
	case ActionAttemptSucceeded:
		r.recordTraceLocked(task.RunID, task.ID, "action.result", "action")
		r.writeBlackboardLocked(BlackboardItem{
			RunID:      task.RunID,
			TaskID:     task.ID,
			Type:       BlackboardItemActionResult,
			Source:     SourceIdentity{Type: SourceTool, ID: result.AttemptID},
			Visibility: BlackboardVisibilityAgentVisible,
			Key:        "action_result",
			Payload:    result.Output,
		})
	}
	return nil
}

func (r *Runtime) releaseLeaseLocked(leaseID string) {
	lease, ok := r.leases[leaseID]
	if !ok {
		return
	}
	lease.Status = LeaseStatusReleased
	r.leases[lease.ID] = lease
	delete(r.activeLeaseByTask, activeLeaseKey(lease.RunID, lease.TaskID))
	r.appendEventLocked(lease.RunID, lease.TaskID, EventTaskExecutionReleased, map[string]any{
		"leaseId": lease.ID,
	})
}
