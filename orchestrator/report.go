package orchestrator

import (
	"context"
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
	RunID    string
	TaskID   string
	ToolName string
	Input    any
}

type ToolInvocationResult struct {
	ToolName string
	Output   any
}

func (r *Runtime) SubmitTypedReport(_ context.Context, cmd SubmitTypedReportCommand) error {
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
	r.appendEventLocked(cmd.RunID, cmd.TaskID, EventTypedReportSubmitted, map[string]any{
		"status":      string(report.Status),
		"holderType":  string(cmd.HolderType),
		"holderId":    cmd.HolderID,
		"taskVersion": cmd.TaskVersion,
	})
	if report.ActionResult != nil {
		if err := r.applyActionResultLocked(&task, report.ActionResult); err != nil {
			r.tasks[task.RunID][task.ID] = task
			return err
		}
		if actionAttemptFailed(report.ActionResult.Status) && report.Status == ReportStatusSuccess {
			report.Status = ReportStatusFailed
		}
	}
	switch report.Status {
	case ReportStatusSuccess:
		if !completionCriteriaSatisfied(task, report) {
			return ErrCompletionCriteriaUnmet
		}
		task.Status = TaskStatusCompleted
		task.Result = &report
		task.Version++
		r.saveTaskLocked(task)
		r.releaseLeaseLocked(cmd.LeaseID)
		r.appendEventLocked(cmd.RunID, cmd.TaskID, EventTaskCompleted, map[string]any{
			"summary": report.Summary,
			"task":    taskEventPayload(task),
		})
	case ReportStatusPartialSuccess:
		task.Status = TaskStatusRunning
		task.Result = &report
		r.saveTaskLocked(task)
	case ReportStatusFailed:
		reason := reportFailureReason(report)
		if canRetryTask(task) {
			task.Status = TaskStatusDispatched
			task.Error = reason
			task.Result = &report
			task.Version++
			r.saveTaskLocked(task)
			r.releaseLeaseLocked(cmd.LeaseID)
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
		r.releaseLeaseLocked(cmd.LeaseID)
		r.appendEventLocked(cmd.RunID, cmd.TaskID, EventTaskFailed, map[string]any{
			"reason": reason,
			"task":   taskEventPayload(task),
		})
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
	case ReportStatusNeedsApproval:
		task.Status = TaskStatusPaused
		task.Result = &report
		task.Version++
		r.saveTaskLocked(task)
		r.updateRunLocked(run, RunStatusWaitingApproval)
		r.appendEventLocked(cmd.RunID, cmd.TaskID, EventTaskPaused, map[string]any{
			"reason": report.Summary,
			"task":   taskEventPayload(task),
		})
	case ReportStatusNeedsClarification:
		task.Status = TaskStatusBlocked
		task.Result = &report
		task.Error = report.Summary
		task.Version++
		r.saveTaskLocked(task)
		r.updateRunLocked(run, RunStatusBlocked)
		r.releaseLeaseLocked(cmd.LeaseID)
		r.appendEventLocked(cmd.RunID, cmd.TaskID, EventTaskBlocked, map[string]any{
			"reason": "needs_clarification",
			"task":   taskEventPayload(task),
		})
		r.queueSystemResponseLocked(cmd.RunID, cmd.TaskID, UserMessageTypeClarificationRequest, "Clarification requested", report.Summary)
	case ReportStatusNeedsHandoff:
		if report.Handoff == nil || report.Handoff.ToAgentID == "" {
			return ErrInvalidCommand
		}
		if err := r.applyHandoffLocked(&task, report.Handoff, report.Summary); err != nil {
			return err
		}
		r.releaseLeaseLocked(cmd.LeaseID)
	default:
		return ErrInvalidCommand
	}
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
	r.updateRunLocked(run, RunStatusRunning)
	if task, ok := r.tasks[cmd.RunID][cmd.TaskID]; ok && task.Status == TaskStatusBlocked {
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

func (r *Runtime) InvokeTool(_ context.Context, cmd ToolInvocation) (ToolInvocationResult, error) {
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
	}
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
		task.Status = TaskStatusBlocked
		task.Error = "action attempt requires reconciliation"
		task.Version++
		r.appendEventLocked(task.RunID, task.ID, EventActionReconcileRequired, map[string]any{
			"attemptId": result.AttemptID,
			"status":    string(result.Status),
		})
		return ErrActionReconcileRequired
	case ActionAttemptFailed, ActionAttemptTimeout, ActionAttemptCancelled:
		task.Status = TaskStatusFailed
		task.Error = result.Error
	case ActionAttemptSucceeded:
		r.writeBlackboardLocked(BlackboardItem{
			RunID:      task.RunID,
			TaskID:     task.ID,
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
