package core

import "strings"

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
	if report.ActionOutcome != nil && report.ActionOutcome.Error != "" {
		return report.ActionOutcome.Error
	}
	return report.Summary
}
