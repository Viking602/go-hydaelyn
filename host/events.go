package host

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/Viking602/go-hydaelyn/blackboard"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/internal/middleware"
	"github.com/Viking602/go-hydaelyn/observe"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
)

type EventSink interface {
	AppendEvent(ctx context.Context, event storage.Event) error
	ListEvents(ctx context.Context, runID string) ([]storage.Event, error)
}

type runtimeEventSink struct {
	store storage.EventStore
}

func (s *runtimeEventSink) AppendEvent(ctx context.Context, event storage.Event) error {
	return s.store.Append(ctx, event)
}

func (s *runtimeEventSink) ListEvents(ctx context.Context, runID string) ([]storage.Event, error) {
	return s.store.List(ctx, runID)
}

const (
	metadataCollaborationEvent   = "collaboration_event"
	metadataCollaborationCounter = "collaboration_counter"
	metadataCorrelationID        = "correlation_id"
	metadataLifecycleReason      = "reason"
	metadataLifecycleStatus      = "status"
)

type collaborationEvent struct {
	Stage         middleware.Stage
	Operation     string
	TeamID        string
	TaskID        string
	AgentID       string
	Type          storage.EventType
	Counter       string
	CorrelationID string
	Metadata      map[string]string
	Payload       map[string]any
}

type taskEventContext struct {
	LeaseID  string
	WorkerID string
	Reason   string
}

type taskEventContextKey struct{}

func withTaskEventContext(ctx context.Context, metadata taskEventContext) context.Context {
	return context.WithValue(ctx, taskEventContextKey{}, metadata)
}

func readTaskEventContext(ctx context.Context) taskEventContext {
	metadata, _ := ctx.Value(taskEventContextKey{}).(taskEventContext)
	return metadata
}

func (r *Runtime) appendEvent(ctx context.Context, event storage.Event) error {
	if event.Sequence == 0 && event.RecordedAt.IsZero() {
		event.RecordedAt = time.Now().UTC()
	}
	if r.eventSink != nil {
		return r.eventSink.AppendEvent(ctx, event)
	}
	return (&runtimeEventSink{store: r.storage.Events()}).AppendEvent(ctx, event)
}

func (r *Runtime) RecordPolicyOutcome(ctx context.Context, outcome storage.PolicyOutcomeEvent) {
	metadata := policyOutcomeMetadata(outcome)
	runID := strings.TrimSpace(outcome.RunID)
	teamID := strings.TrimSpace(outcome.TeamID)
	taskID := strings.TrimSpace(outcome.TaskID)
	if runID == "" {
		runID = strings.TrimSpace(metadata["runId"])
	}
	if teamID == "" {
		teamID = strings.TrimSpace(metadata["teamId"])
	}
	if taskID == "" {
		taskID = strings.TrimSpace(metadata["taskId"])
	}
	if runID == "" {
		runID = teamID
	}
	if runID == "" {
		return
	}
	payload := policyOutcomePayload(outcome)
	if traceID := observe.TraceID(ctx); traceID != "" {
		payload["traceId"] = traceID
	}
	_ = r.appendEvent(ctx, storage.Event{
		RunID:   runID,
		TeamID:  teamID,
		TaskID:  taskID,
		Type:    storage.EventPolicyOutcome,
		Payload: payload,
	})
}

func (r *Runtime) recordInitialEvents(ctx context.Context, state team.RunState) error {
	workers := make([]map[string]string, 0, len(state.Workers))
	for _, worker := range state.Workers {
		workers = append(workers, map[string]string{
			"id":          worker.ID,
			"role":        string(worker.Role),
			"profileName": worker.ProfileName,
		})
	}
	if err := r.appendEvent(ctx, storage.Event{
		RunID:  state.ID,
		TeamID: state.ID,
		Type:   storage.EventTeamStarted,
		Payload: map[string]any{
			"pattern": state.Pattern,
			"phase":   string(state.Phase),
			"supervisor": map[string]string{
				"id":          state.Supervisor.ID,
				"role":        string(state.Supervisor.Role),
				"profileName": state.Supervisor.ProfileName,
			},
			"workers": workers,
		},
	}); err != nil {
		return err
	}
	if state.Planning != nil {
		if err := r.appendEvent(ctx, storage.Event{
			RunID:  state.ID,
			TeamID: state.ID,
			Type:   storage.EventPlanCreated,
			Payload: map[string]any{
				"planner": state.Planning.PlannerName,
				"goal":    state.Planning.Goal,
			},
		}); err != nil {
			return err
		}
	}
	for _, task := range state.Tasks {
		if err := r.recordTaskScheduledEvent(ctx, state, task); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runtime) recordTaskScheduledEvent(ctx context.Context, state team.RunState, task team.Task) error {
	return r.appendEvent(ctx, storage.Event{
		RunID:   state.ID,
		TeamID:  state.ID,
		TaskID:  task.ID,
		Type:    storage.EventTaskScheduled,
		Payload: taskScheduledPayload(task),
	})
}

func (r *Runtime) recordNewTaskScheduledEvents(ctx context.Context, previous, current team.RunState) error {
	known := make(map[string]struct{}, len(previous.Tasks))
	for _, task := range previous.Tasks {
		known[task.ID] = struct{}{}
	}
	for _, task := range current.Tasks {
		if _, ok := known[task.ID]; ok {
			continue
		}
		if err := r.recordTaskScheduledEvent(ctx, current, task); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runtime) recordTaskLifecycleEvent(ctx context.Context, state team.RunState, before, after team.Task, eventType storage.EventType) {
	payload := taskLifecyclePayload(ctx, before, after, r.workerID)
	mergeResultPayload(payload, after.Result)
	_ = r.appendEvent(ctx, storage.Event{
		RunID:   state.ID,
		TeamID:  state.ID,
		TaskID:  after.ID,
		Type:    eventType,
		Payload: payload,
	})
}

func (r *Runtime) recordTaskToolEvents(ctx context.Context, state team.RunState, task team.Task, results []message.ToolResult) {
	for _, result := range results {
		_ = r.appendEvent(ctx, storage.Event{
			RunID:   state.ID,
			TeamID:  state.ID,
			TaskID:  task.ID,
			Type:    storage.EventToolCalled,
			Payload: map[string]any{"name": result.Name, "toolCallId": result.ToolCallID},
		})
	}
}

func (r *Runtime) recordTaskInputsMaterializedEvent(ctx context.Context, state team.RunState, task team.Task, exchanges []blackboard.Exchange) {
	if len(exchanges) == 0 {
		return
	}
	_ = r.appendEvent(ctx, storage.Event{
		RunID:   state.ID,
		TeamID:  state.ID,
		TaskID:  task.ID,
		Type:    storage.EventTaskInputsMaterialized,
		Payload: map[string]any{"inputs": exchangesPayload(exchanges)},
	})
}

func (r *Runtime) recordTaskOutputsPublishedEvent(ctx context.Context, state team.RunState, task team.Task) {
	if task.Result == nil {
		return
	}
	payload := taskLifecyclePayload(ctx, task, task, r.workerID)
	mergeResultPayload(payload, task.Result)
	if state.Blackboard != nil {
		sources := sourcesForTask(state.Blackboard, task.ID)
		if len(sources) > 0 {
			payload["sources"] = sourcePayload(sources)
		}
		artifacts := artifactsForTask(state.Blackboard, task.ID)
		if len(artifacts) > 0 {
			payload["artifacts"] = artifactPayload(artifacts)
		}
		evidence := evidenceForTask(state.Blackboard, task.ID)
		if len(evidence) > 0 {
			payload["evidence"] = evidencePayload(evidence)
		}
		claims := state.Blackboard.ClaimsForTask(task.ID)
		if len(claims) > 0 {
			payload["claims"] = claimPayload(claims)
		}
		findings := findingsForTask(state.Blackboard, task.ID)
		if len(findings) > 0 {
			payload["findings"] = findingPayload(findings)
		}
		exchanges := state.Blackboard.ExchangesForTask(task.ID)
		if len(exchanges) > 0 {
			payload["exchanges"] = exchangesPayload(exchanges)
		}
		verifications := verificationsForTask(state.Blackboard, task)
		if len(verifications) > 0 {
			payload["verifications"] = verificationPayload(verifications)
		}
	}
	_ = r.appendEvent(ctx, storage.Event{
		RunID:   state.ID,
		TeamID:  state.ID,
		TaskID:  task.ID,
		Type:    storage.EventTaskOutputsPublished,
		Payload: payload,
	})
}

func (r *Runtime) recordTeamTerminalEvent(ctx context.Context, state team.RunState) {
	if !state.IsTerminal() {
		return
	}
	if state.Status == team.StatusPaused {
		_ = r.appendEvent(ctx, storage.Event{
			RunID:  state.ID,
			TeamID: state.ID,
			Type:   storage.EventApprovalRequested,
			Payload: map[string]any{
				"reason": state.Result.Error,
			},
		})
		return
	}
	eventType := storage.EventTeamCompleted
	if state.Status != team.StatusCompleted {
		eventType = storage.EventCheckpointSaved
	}
	payload := map[string]any{}
	mergeResultPayload(payload, state.Result)
	_ = r.appendEvent(ctx, storage.Event{
		RunID:   state.ID,
		TeamID:  state.ID,
		Type:    eventType,
		Payload: payload,
	})
}

func (r *Runtime) recordCollaborationEvent(ctx context.Context, event collaborationEvent) {
	if strings.TrimSpace(event.TeamID) == "" {
		return
	}
	payload := cloneAnyMap(event.Payload)
	metadata := cloneStringMap(event.Metadata)
	if metadata == nil {
		metadata = map[string]string{}
	}
	if payload == nil {
		payload = map[string]any{}
	}
	correlationID := event.CorrelationID
	if correlationID == "" {
		correlationID = collaborationCorrelationID(event.TeamID, event.TaskID)
	}
	metadata[metadataCollaborationEvent] = string(event.Type)
	metadata[metadataCorrelationID] = correlationID
	if event.Counter != "" {
		metadata[metadataCollaborationCounter] = event.Counter
	}
	_ = r.runStage(ctx, &middleware.Envelope{
		Stage:     event.Stage,
		Operation: event.Operation,
		TeamID:    event.TeamID,
		TaskID:    event.TaskID,
		AgentID:   event.AgentID,
		Metadata:  metadata,
		Request:   payload,
	}, func(ctx context.Context, envelope *middleware.Envelope) error {
		traceID := observe.TraceID(ctx)
		payload["traceId"] = traceID
		payload["correlationId"] = correlationID
		payload["teamId"] = event.TeamID
		if event.TaskID != "" {
			payload["taskId"] = event.TaskID
		}
		envelope.Response = payload
		return r.appendEvent(ctx, storage.Event{
			RunID:   event.TeamID,
			TeamID:  event.TeamID,
			TaskID:  event.TaskID,
			Type:    event.Type,
			Payload: payload,
		})
	})
}

func (r *Runtime) recordLeaseAcquiredEvent(ctx context.Context, leaseTeamID, leaseTaskID, ownerID string, ttl time.Duration) {
	r.recordCollaborationEvent(ctx, collaborationEvent{
		Stage:     middleware.StageTask,
		Operation: "queue_lease_acquired",
		TeamID:    leaseTeamID,
		TaskID:    leaseTaskID,
		Type:      storage.EventLeaseAcquired,
		Counter:   "collaboration_leases_acquired",
		Metadata: map[string]string{
			metadataLifecycleStatus: "acquired",
		},
		Payload: map[string]any{
			"ownerId": ownerID,
			"ttlMs":   ttl.Milliseconds(),
		},
	})
}

func (r *Runtime) recordLeaseExpiredEvent(ctx context.Context, teamID, taskID, ownerID, reason string) {
	reason = normalizeLifecycleReason(reason)
	r.recordCollaborationEvent(ctx, collaborationEvent{
		Stage:     middleware.StageTask,
		Operation: "queue_lease_expired",
		TeamID:    teamID,
		TaskID:    taskID,
		Type:      storage.EventLeaseExpired,
		Counter:   "collaboration_leases_expired",
		Metadata: map[string]string{
			metadataLifecycleReason: reason,
			metadataLifecycleStatus: "expired",
		},
		Payload: map[string]any{
			"ownerId": ownerID,
			"reason":  reason,
		},
	})
}

func (r *Runtime) recordStaleWriteRejectedEvent(ctx context.Context, teamID, taskID, workerID, reason string) {
	reason = normalizeLifecycleReason(reason)
	r.recordCollaborationEvent(ctx, collaborationEvent{
		Stage:     middleware.StageTask,
		Operation: "collaboration_stale_write_rejected",
		TeamID:    teamID,
		TaskID:    taskID,
		Type:      storage.EventStaleWriteRejected,
		Counter:   "collaboration_stale_writes_rejected",
		Metadata: map[string]string{
			metadataLifecycleReason: reason,
			metadataLifecycleStatus: "rejected",
		},
		Payload: map[string]any{
			"workerId": workerID,
			"reason":   reason,
		},
	})
	r.RecordPolicyOutcome(ctx, storage.PolicyOutcomeEvent{
		SchemaVersion: storage.PolicyOutcomeEventSchemaVersion,
		Layer:         "capability",
		Stage:         "task",
		Operation:     "invoke",
		Action:        "block",
		Policy:        "capability.stale_write",
		Outcome:       "rejected",
		Severity:      "error",
		Message:       fmt.Sprintf("stale write rejected for %s", taskID),
		Blocking:      true,
		RunID:         teamID,
		TeamID:        teamID,
		TaskID:        taskID,
		Reference:     collaborationCorrelationID(teamID, taskID),
		Timestamp:     time.Now().UTC(),
		Evidence: &storage.PolicyOutcomeEvidence{Metadata: map[string]string{
			"teamId":   teamID,
			"taskId":   taskID,
			"workerId": workerID,
			"reason":   reason,
		}},
	})
}

func (r *Runtime) recordVerifierDecisionEvent(ctx context.Context, state team.RunState, task team.Task) {
	status := blackboard.InferVerificationStatus(task.Result.Summary)
	eventType := storage.EventVerifierPassed
	counter := "collaboration_verifier_passed"
	decision := "passed"
	severity := "info"
	blocking := false
	if status != blackboard.VerificationStatusSupported {
		eventType = storage.EventVerifierBlocked
		counter = "collaboration_verifier_blocked"
		decision = "blocked"
		severity = "warning"
		blocking = true
	}
	outcome := storage.PolicyOutcomeEvent{
		SchemaVersion: storage.PolicyOutcomeEventSchemaVersion,
		Layer:         "stage",
		Stage:         string(middleware.StageVerify),
		Operation:     "check",
		Action:        map[bool]string{true: "block", false: "allow"}[blocking],
		Policy:        "verifier.decision",
		Outcome:       decision,
		Severity:      severity,
		Message:       task.Result.Summary,
		Blocking:      blocking,
		RunID:         state.ID,
		TeamID:        state.ID,
		TaskID:        task.ID,
		Reference:     collaborationCorrelationID(state.ID, task.ID),
		Timestamp:     time.Now().UTC(),
		Evidence: &storage.PolicyOutcomeEvidence{Metadata: map[string]string{
			"runId":              state.ID,
			"teamId":             state.ID,
			"taskId":             task.ID,
			"taskStage":          string(task.Stage),
			"verificationStatus": string(status),
		}},
	}
	r.recordCollaborationEvent(ctx, collaborationEvent{
		Stage:         middleware.StageVerify,
		Operation:     "collaboration_verifier_decision",
		TeamID:        state.ID,
		TaskID:        task.ID,
		Type:          eventType,
		Counter:       counter,
		CorrelationID: collaborationCorrelationID(state.ID, task.ID),
		Metadata: map[string]string{
			metadataLifecycleStatus: decision,
		},
		Payload: map[string]any{
			"decisionOutcome":    decision,
			"policyOutcome":      policyOutcomePayload(outcome),
			"taskStage":          string(task.Stage),
			"verificationStatus": string(status),
			"summary":            task.Result.Summary,
		},
	})
	r.RecordPolicyOutcome(ctx, outcome)
}

func (r *Runtime) recordTaskCancelledEvent(ctx context.Context, state team.RunState, task team.Task, reason string) {
	reason = normalizeLifecycleReason(reason)
	r.recordCollaborationEvent(ctx, collaborationEvent{
		Stage:         middleware.StageTask,
		Operation:     "collaboration_cancelled",
		TeamID:        state.ID,
		TaskID:        task.ID,
		Type:          storage.EventTaskCancelled,
		CorrelationID: collaborationCorrelationID(state.ID, task.ID),
		Metadata: map[string]string{
			metadataLifecycleReason: reason,
			metadataLifecycleStatus: "cancelled",
		},
		Payload: map[string]any{
			"reason":     reason,
			"taskStage":  string(task.Stage),
			"taskStatus": string(task.Status),
		},
	})
}

func (r *Runtime) recordSynthesisCommittedEvent(ctx context.Context, state team.RunState, task team.Task) {
	r.recordCollaborationEvent(ctx, collaborationEvent{
		Stage:         middleware.StageSynthesize,
		Operation:     "collaboration_synthesis_committed",
		TeamID:        state.ID,
		TaskID:        task.ID,
		Type:          storage.EventSynthesisCommitted,
		CorrelationID: collaborationCorrelationID(state.ID, task.ID),
		Metadata: map[string]string{
			metadataLifecycleStatus: "committed",
		},
		Payload: map[string]any{
			"summary":    resultSummary(task.Result),
			"taskStage":  string(task.Stage),
			"taskStatus": string(task.Status),
		},
	})
}

func collaborationCorrelationID(teamID, taskID string) string {
	if taskID == "" {
		return fmt.Sprintf("%s:team", teamID)
	}
	return fmt.Sprintf("%s:%s", teamID, taskID)
}

func cloneAnyMap(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(values))
	maps.Copy(cloned, values)
	return cloned
}

func resultSummary(result *team.Result) string {
	if result == nil {
		return ""
	}
	return result.Summary
}

func (r *Runtime) ReplayTeamState(ctx context.Context, teamID string) (team.RunState, error) {
	events, err := r.listEvents(ctx, teamID)
	if err != nil {
		return team.RunState{}, err
	}
	return storage.ReplayTeam(events), nil
}

func (r *Runtime) listEvents(ctx context.Context, runID string) ([]storage.Event, error) {
	if r.eventSink != nil {
		return r.eventSink.ListEvents(ctx, runID)
	}
	return (&runtimeEventSink{store: r.storage.Events()}).ListEvents(ctx, runID)
}

func mergeResultPayload(payload map[string]any, result *team.Result) {
	if result == nil {
		return
	}
	payload["summary"] = result.Summary
	payload["error"] = result.Error
	if len(result.Structured) > 0 {
		payload["structured"] = result.Structured
	}
	if len(result.ArtifactIDs) > 0 {
		payload["artifactIds"] = result.ArtifactIDs
	}
	if result.Usage != (provider.Usage{}) {
		payload["usage"] = map[string]any{
			"inputTokens":  result.Usage.InputTokens,
			"outputTokens": result.Usage.OutputTokens,
			"totalTokens":  result.Usage.TotalTokens,
		}
	}
	if result.ToolCallCount > 0 {
		payload["toolCallCount"] = result.ToolCallCount
	}
}

func outputVisibilities(items []team.OutputVisibility) []string {
	if len(items) == 0 {
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, string(item))
	}
	return result
}

func taskScheduledPayload(task team.Task) map[string]any {
	return map[string]any{
		"title":            task.Title,
		"input":            task.Input,
		"status":           string(task.Status),
		"kind":             string(task.Kind),
		"requiredRole":     string(task.RequiredRole),
		"assigneeAgent":    task.AssigneeAgentID,
		"failurePolicy":    string(task.FailurePolicy),
		"dependsOn":        task.DependsOn,
		"reads":            task.Reads,
		"writes":           task.Writes,
		"publish":          outputVisibilities(task.Publish),
		"taskVersion":      task.Version,
		"idempotencyKey":   task.IdempotencyKey,
		"namespace":        task.Namespace,
		"verifierRequired": task.VerifierRequired,
		"budget": map[string]any{
			"tokens":    task.Budget.Tokens,
			"toolCalls": task.Budget.ToolCalls,
		},
	}
}

func taskLifecyclePayload(ctx context.Context, before, after team.Task, defaultWorkerID string) map[string]any {
	metadata := readTaskEventContext(ctx)
	workerID := metadata.WorkerID
	if workerID == "" {
		workerID = after.CompletedBy
	}
	if workerID == "" {
		workerID = defaultWorkerID
	}
	beforeVersion := before.Version
	if beforeVersion <= 0 {
		beforeVersion = after.Version
	}
	afterVersion := after.Version
	if afterVersion <= 0 {
		afterVersion = beforeVersion
	}
	payload := map[string]any{
		"status":            string(after.Status),
		"statusBefore":      string(before.Status),
		"statusAfter":       string(after.Status),
		"taskVersionBefore": beforeVersion,
		"taskVersionAfter":  afterVersion,
		"attempts":          after.Attempts,
		"idempotencyKey":    after.IdempotencyKey,
		"workerId":          workerID,
	}
	if metadata.LeaseID != "" {
		payload["leaseId"] = metadata.LeaseID
	}
	if metadata.Reason != "" {
		payload["reason"] = metadata.Reason
	}
	return payload
}

func sourcePayload(items []blackboard.Source) []map[string]any {
	payload := make([]map[string]any, 0, len(items))
	for _, item := range items {
		payload = append(payload, map[string]any{
			"id":       item.ID,
			"taskId":   item.TaskID,
			"title":    item.Title,
			"metadata": cloneStringMap(item.Metadata),
		})
	}
	return payload
}

func artifactPayload(items []blackboard.Artifact) []map[string]any {
	payload := make([]map[string]any, 0, len(items))
	for _, item := range items {
		payload = append(payload, map[string]any{
			"id":       item.ID,
			"taskId":   item.TaskID,
			"name":     item.Name,
			"content":  item.Content,
			"metadata": cloneStringMap(item.Metadata),
		})
	}
	return payload
}

func evidencePayload(items []blackboard.Evidence) []map[string]any {
	payload := make([]map[string]any, 0, len(items))
	for _, item := range items {
		payload = append(payload, map[string]any{
			"id":         item.ID,
			"taskId":     item.TaskID,
			"sourceId":   item.SourceID,
			"artifactId": item.ArtifactID,
			"summary":    item.Summary,
			"snippet":    item.Snippet,
			"score":      item.Score,
		})
	}
	return payload
}

func claimPayload(items []blackboard.Claim) []map[string]any {
	payload := make([]map[string]any, 0, len(items))
	for _, item := range items {
		payload = append(payload, map[string]any{
			"id":          item.ID,
			"taskId":      item.TaskID,
			"summary":     item.Summary,
			"evidenceIds": item.EvidenceIDs,
			"confidence":  item.Confidence,
		})
	}
	return payload
}

func findingPayload(items []blackboard.Finding) []map[string]any {
	payload := make([]map[string]any, 0, len(items))
	for _, item := range items {
		payload = append(payload, map[string]any{
			"id":          item.ID,
			"taskId":      item.TaskID,
			"summary":     item.Summary,
			"claimIds":    item.ClaimIDs,
			"evidenceIds": item.EvidenceIDs,
			"confidence":  item.Confidence,
		})
	}
	return payload
}

func exchangesPayload(items []blackboard.Exchange) []map[string]any {
	payload := make([]map[string]any, 0, len(items))
	for _, item := range items {
		payload = append(payload, map[string]any{
			"id":          item.ID,
			"key":         item.Key,
			"namespace":   item.Namespace,
			"taskId":      item.TaskID,
			"version":     item.Version,
			"etag":        item.ETag,
			"valueType":   string(item.ValueType),
			"text":        item.Text,
			"structured":  item.Structured,
			"artifactIds": item.ArtifactIDs,
			"claimIds":    item.ClaimIDs,
			"findingIds":  item.FindingIDs,
			"metadata":    cloneStringMap(item.Metadata),
		})
	}
	return payload
}

func verificationPayload(items []blackboard.VerificationResult) []map[string]any {
	payload := make([]map[string]any, 0, len(items))
	for _, item := range items {
		payload = append(payload, map[string]any{
			"claimId":     item.ClaimID,
			"status":      string(item.Status),
			"confidence":  item.Confidence,
			"evidenceIds": item.EvidenceIDs,
			"rationale":   item.Rationale,
		})
	}
	return payload
}

func policyOutcomePayload(outcome storage.PolicyOutcomeEvent) map[string]any {
	payload := map[string]any{
		"schemaVersion": outcome.SchemaVersion,
		"layer":         outcome.Layer,
		"stage":         outcome.Stage,
		"operation":     outcome.Operation,
		"action":        outcome.Action,
		"policy":        outcome.Policy,
		"outcome":       outcome.Outcome,
		"severity":      outcome.Severity,
		"message":       outcome.Message,
		"blocking":      outcome.Blocking,
		"runId":         outcome.RunID,
		"teamId":        outcome.TeamID,
		"taskId":        outcome.TaskID,
		"agentId":       outcome.AgentID,
		"reference":     outcome.Reference,
		"timestamp":     outcome.Timestamp,
	}
	if outcome.Attempt > 0 {
		payload["attempt"] = outcome.Attempt
	}
	if outcome.Evidence != nil {
		evidence := map[string]any{}
		if len(outcome.Evidence.EventSequences) > 0 {
			evidence["eventSequences"] = append([]int{}, outcome.Evidence.EventSequences...)
		}
		if outcome.Evidence.Excerpt != "" {
			evidence["excerpt"] = outcome.Evidence.Excerpt
		}
		if len(outcome.Evidence.Metadata) > 0 {
			evidence["metadata"] = cloneStringMap(outcome.Evidence.Metadata)
		}
		if len(evidence) > 0 {
			payload["evidence"] = evidence
		}
	}
	return payload
}

func policyOutcomeMetadata(outcome storage.PolicyOutcomeEvent) map[string]string {
	metadata := map[string]string{}
	if outcome.Evidence != nil && len(outcome.Evidence.Metadata) > 0 {
		metadata = cloneStringMap(outcome.Evidence.Metadata)
	}
	if outcome.RunID != "" {
		metadata["runId"] = outcome.RunID
	}
	if outcome.TeamID != "" {
		metadata["teamId"] = outcome.TeamID
	}
	if outcome.TaskID != "" {
		metadata["taskId"] = outcome.TaskID
	}
	if outcome.AgentID != "" {
		metadata["agentId"] = outcome.AgentID
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

func verificationsForTask(board *blackboard.State, task team.Task) []blackboard.VerificationResult {
	if board == nil || task.Kind != team.TaskKindVerify {
		return nil
	}
	claimIDs := map[string]struct{}{}
	for _, dependencyID := range task.DependsOn {
		for _, claim := range board.ClaimsForTask(dependencyID) {
			claimIDs[claim.ID] = struct{}{}
		}
	}
	items := make([]blackboard.VerificationResult, 0, len(claimIDs))
	for _, verification := range board.Verifications {
		if _, ok := claimIDs[verification.ClaimID]; ok {
			items = append(items, verification)
		}
	}
	return items
}

func sourcesForTask(board *blackboard.State, taskID string) []blackboard.Source {
	if board == nil {
		return nil
	}
	items := make([]blackboard.Source, 0, len(board.Sources))
	for _, item := range board.Sources {
		if item.TaskID == taskID {
			items = append(items, item)
		}
	}
	return items
}

func artifactsForTask(board *blackboard.State, taskID string) []blackboard.Artifact {
	if board == nil {
		return nil
	}
	items := make([]blackboard.Artifact, 0, len(board.Artifacts))
	for _, item := range board.Artifacts {
		if item.TaskID == taskID {
			items = append(items, item)
		}
	}
	return items
}

func evidenceForTask(board *blackboard.State, taskID string) []blackboard.Evidence {
	if board == nil {
		return nil
	}
	items := make([]blackboard.Evidence, 0, len(board.Evidence))
	for _, item := range board.Evidence {
		if item.TaskID == taskID {
			items = append(items, item)
		}
	}
	return items
}

func findingsForTask(board *blackboard.State, taskID string) []blackboard.Finding {
	if board == nil {
		return nil
	}
	items := make([]blackboard.Finding, 0, len(board.Findings))
	for _, item := range board.Findings {
		if item.TaskID == taskID {
			items = append(items, item)
		}
	}
	return items
}
