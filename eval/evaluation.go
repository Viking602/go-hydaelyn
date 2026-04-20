package eval

import (
	"time"

	"github.com/Viking602/go-hydaelyn/internal/blackboard"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
)

type Report struct {
	TeamID                 string        `json:"teamId,omitempty"`
	TaskCount              int           `json:"taskCount,omitempty"`
	CompletedTasks         int           `json:"completedTasks,omitempty"`
	BlockingFailures       int           `json:"blockingFailures,omitempty"`
	TaskCompletionRate     float64       `json:"taskCompletionRate,omitempty"`
	BlockingFailureRate    float64       `json:"blockingFailureRate,omitempty"`
	RetrySuccessRate       float64       `json:"retrySuccessRate,omitempty"`
	SupportedClaimRatio    float64       `json:"supportedClaimRatio,omitempty"`
	SynthesisInputCoverage float64       `json:"synthesisInputCoverage,omitempty"`
	EndToEndLatency        time.Duration `json:"endToEndLatency,omitempty"`
	ToolCallCount          int           `json:"toolCallCount,omitempty"`
	TokenBudgetHitRate     float64       `json:"tokenBudgetHitRate,omitempty"`
}

func Evaluate(events []storage.Event) Report {
	report := Report{}
	if len(events) == 0 {
		return report
	}
	state := storage.ReplayTeam(events)
	report.TeamID = state.ID
	report.TaskCount = len(state.Tasks)
	for _, task := range state.Tasks {
		if task.Status == team.TaskStatusCompleted {
			report.CompletedTasks++
		}
		if task.Status == team.TaskStatusFailed && task.BlocksTeamOnFailure() {
			report.BlockingFailures++
		}
	}
	report.TaskCompletionRate = ratio(report.CompletedTasks, report.TaskCount)
	report.BlockingFailureRate = ratio(report.BlockingFailures, report.TaskCount)
	report.RetrySuccessRate = retrySuccessRate(state)
	report.SupportedClaimRatio = supportedClaimRatio(state)
	report.SynthesisInputCoverage = synthesisInputCoverage(state)
	report.EndToEndLatency = eventLatency(events)
	report.ToolCallCount = toolCallCount(events, state)
	report.TokenBudgetHitRate = tokenBudgetHitRate(events)
	return report
}

func retrySuccessRate(state team.RunState) float64 {
	candidates := 0
	successes := 0
	for _, task := range state.Tasks {
		if task.Attempts <= 1 {
			continue
		}
		candidates++
		if task.Status == team.TaskStatusCompleted {
			successes++
		}
	}
	return ratio(successes, candidates)
}

func supportedClaimRatio(state team.RunState) float64 {
	if state.Blackboard == nil || len(state.Blackboard.Verifications) == 0 {
		return 0
	}
	supported := 0
	for _, verification := range state.Blackboard.Verifications {
		if verification.SupportsClaim(blackboard.DefaultVerificationConfidence) {
			supported++
		}
	}
	return ratio(supported, len(state.Blackboard.Verifications))
}

func synthesisInputCoverage(state team.RunState) float64 {
	if state.Blackboard == nil {
		return 0
	}
	total := 0
	covered := 0
	for _, task := range state.Tasks {
		if task.Kind != team.TaskKindSynthesize {
			continue
		}
		for _, key := range task.Reads {
			total++
			if len(state.Blackboard.ExchangesForKey(key)) > 0 {
				covered++
			}
		}
	}
	return ratio(covered, total)
}

func eventLatency(events []storage.Event) time.Duration {
	first := time.Time{}
	last := time.Time{}
	for _, event := range events {
		if event.RecordedAt.IsZero() {
			continue
		}
		if first.IsZero() || event.RecordedAt.Before(first) {
			first = event.RecordedAt
		}
		if last.IsZero() || event.RecordedAt.After(last) {
			last = event.RecordedAt
		}
	}
	if first.IsZero() || last.IsZero() {
		return 0
	}
	return last.Sub(first)
}

func toolCallCount(events []storage.Event, state team.RunState) int {
	count := 0
	for _, event := range events {
		if event.Type == storage.EventToolCalled {
			count++
		}
	}
	if count > 0 {
		return count
	}
	for _, task := range state.Tasks {
		if task.Result != nil {
			count += task.Result.ToolCallCount
		}
	}
	return count
}

func tokenBudgetHitRate(events []storage.Event) float64 {
	budgets := map[string]int{}
	usages := map[string]int{}
	for _, event := range events {
		switch event.Type {
		case storage.EventTaskScheduled:
			if budget, ok := event.Payload["budget"].(map[string]any); ok {
				budgets[event.TaskID] = intValue(budget["tokens"])
			}
		case storage.EventTaskCompleted:
			if usage, ok := event.Payload["usage"].(map[string]any); ok {
				usages[event.TaskID] = intValue(usage["totalTokens"])
			}
		}
	}
	eligible := 0
	hit := 0
	for taskID, budget := range budgets {
		if budget <= 0 {
			continue
		}
		total, ok := usages[taskID]
		if !ok {
			continue
		}
		eligible++
		if total >= budget {
			hit++
		}
	}
	return ratio(hit, eligible)
}

func ratio(numerator, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func intValue(value any) int {
	switch current := value.(type) {
	case int:
		return current
	case int64:
		return int(current)
	case float64:
		return int(current)
	case float32:
		return int(current)
	default:
		return 0
	}
}
