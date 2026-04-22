package host

import (
	"context"

	"github.com/Viking602/go-hydaelyn/internal/blackboard"
	"github.com/Viking602/go-hydaelyn/storage"
)

// emitSupervisorObserved writes a SupervisorObserved event summarizing the
// digest. Large slices (Tasks, Findings, Conflicts) are projected down to
// counts + ids so the event row stays compact — the full digest can always
// be re-derived on replay from the cursor it records.
func (r *Runtime) emitSupervisorObserved(ctx context.Context, runID, teamID string, digest SupervisorDigest) error {
	if r == nil {
		return nil
	}
	taskIDs := make([]string, 0, len(digest.Tasks))
	readyCount := 0
	for _, task := range digest.Tasks {
		taskIDs = append(taskIDs, task.ID)
		if task.Ready {
			readyCount++
		}
	}
	findingIDs := make([]string, 0, len(digest.RecentFindings))
	supportedFindings := 0
	for _, f := range digest.RecentFindings {
		findingIDs = append(findingIDs, f.ID)
		if f.Supported {
			supportedFindings++
		}
	}
	payload := map[string]any{
		"sequence":          digest.Sequence,
		"phase":             string(digest.Phase),
		"status":            string(digest.Status),
		"observedAt":        digest.ObservedAt,
		"taskCount":         len(digest.Tasks),
		"readyCount":        readyCount,
		"taskIds":           taskIDs,
		"pendingReadCount":  len(digest.PendingReads),
		"recentFindingIds":  findingIDs,
		"supportedFindings": supportedFindings,
		"conflictCount":     len(digest.Conflicts),
		"budget": map[string]int{
			"tasksCompleted": digest.Budget.TasksCompleted,
			"tasksFailed":    digest.Budget.TasksFailed,
			"tasksPending":   digest.Budget.TasksPending,
			"tasksRunning":   digest.Budget.TasksRunning,
			"tokensUsed":     digest.Budget.TokensUsed,
			"toolCallsUsed":  digest.Budget.ToolCallsUsed,
		},
		"cursor": map[string]any{
			"exchangeIndex":     digest.Cursor.ExchangeIndex,
			"verificationIndex": digest.Cursor.VerificationIndex,
			"findingIndex":      digest.Cursor.FindingIndex,
			"eventSequence":     digest.Cursor.EventSequence,
		},
	}
	return r.appendEvent(ctx, storage.Event{
		RunID:   runID,
		TeamID:  teamID,
		Type:    storage.EventSupervisorObserved,
		Payload: payload,
	})
}

// emitConflictRaised writes one event per conflict in the digest. Emitting
// separately (rather than rolling them into SupervisorObserved) gives
// downstream consumers a per-conflict event to attach adjudication
// decisions to in PR 4's Decision applier.
func (r *Runtime) emitConflictRaised(ctx context.Context, runID, teamID string, conflict blackboard.Conflict) error {
	if r == nil {
		return nil
	}
	exchanges := make([]map[string]any, 0, len(conflict.Exchanges))
	for _, ex := range conflict.Exchanges {
		exchanges = append(exchanges, map[string]any{
			"id":        ex.ID,
			"namespace": ex.Namespace,
			"taskId":    ex.TaskID,
			"version":   ex.Version,
			"etag":      ex.ETag,
			"excerpt":   ex.Excerpt,
		})
	}
	payload := map[string]any{
		"key":        conflict.Key,
		"taskIds":    conflict.TaskIDs,
		"namespaces": conflict.Namespaces,
		"exchanges":  exchanges,
		"reason":     conflict.Reason,
	}
	return r.appendEvent(ctx, storage.Event{
		RunID:   runID,
		TeamID:  teamID,
		Type:    storage.EventConflictRaised,
		Payload: payload,
	})
}
