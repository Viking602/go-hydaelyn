package memory

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Viking602/venat/internal/core/model"
)

type runStore UnitOfWork

type taskStore UnitOfWork

type eventStore UnitOfWork

type blackboardStore UnitOfWork

type mailboxStore UnitOfWork

type messageStore UnitOfWork

type traceStore UnitOfWork

type leaseStore UnitOfWork

type approvalStore UnitOfWork

type resumeTokenStore UnitOfWork

type actionAttemptStore UnitOfWork

func (s *runStore) uow() *UnitOfWork { return (*UnitOfWork)(s) }

func (s *taskStore) uow() *UnitOfWork { return (*UnitOfWork)(s) }

func (s *eventStore) uow() *UnitOfWork { return (*UnitOfWork)(s) }

func (s *blackboardStore) uow() *UnitOfWork { return (*UnitOfWork)(s) }

func (s *mailboxStore) uow() *UnitOfWork { return (*UnitOfWork)(s) }

func (s *messageStore) uow() *UnitOfWork { return (*UnitOfWork)(s) }

func (s *traceStore) uow() *UnitOfWork { return (*UnitOfWork)(s) }

func (s *leaseStore) uow() *UnitOfWork { return (*UnitOfWork)(s) }

func (s *approvalStore) uow() *UnitOfWork { return (*UnitOfWork)(s) }

func (s *resumeTokenStore) uow() *UnitOfWork { return (*UnitOfWork)(s) }

func (s *actionAttemptStore) uow() *UnitOfWork { return (*UnitOfWork)(s) }

func (s *runStore) SaveRun(_ context.Context, run model.Run) error {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return err
	}
	if run.CreatedAt.IsZero() {
		run.CreatedAt = time.Now().UTC()
	}
	run.UpdatedAt = time.Now().UTC()
	u.staged.Runs[run.ID] = run
	return nil
}

func (s *runStore) LoadRun(_ context.Context, runID string) (model.Run, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return model.Run{}, err
	}
	run, ok := u.staged.Runs[runID]
	if !ok {
		return model.Run{}, model.ErrNotFound
	}
	return run, nil
}

// ListRuns filters runs by RunSelector. All set fields AND-combine. Result
// is sorted by CreatedAt ascending so callers get deterministic order.
func (s *runStore) ListRuns(_ context.Context, sel model.RunSelector) ([]model.Run, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return nil, err
	}
	var out []model.Run
	for _, run := range u.staged.Runs {
		if len(sel.IDs) > 0 && !slices.Contains(sel.IDs, run.ID) {
			continue
		}
		if len(sel.Statuses) > 0 && !slices.Contains(sel.Statuses, run.Status) {
			continue
		}
		if !sel.Since.IsZero() && run.CreatedAt.Before(sel.Since) {
			continue
		}
		if !sel.Until.IsZero() && run.CreatedAt.After(sel.Until) {
			continue
		}
		// AgentID / AgentVersion: Run model doesn't yet carry these in
		// v0.8.0 baseline. When AgentProfile binding lands (doc 03), this
		// match clause should consult run.AgentID / run.AgentVersion.
		out = append(out, run)
	}
	slices.SortFunc(out, func(a, b model.Run) int {
		if a.CreatedAt.Before(b.CreatedAt) {
			return -1
		}
		if a.CreatedAt.After(b.CreatedAt) {
			return 1
		}
		return 0
	})
	if sel.Limit > 0 && len(out) > sel.Limit {
		out = out[:sel.Limit]
	}
	return out, nil
}

func (s *taskStore) SaveTask(_ context.Context, task model.Task) error {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return err
	}
	if u.staged.Tasks[task.RunID] == nil {
		u.staged.Tasks[task.RunID] = map[string]model.Task{}
	}
	task.UpdatedAt = time.Now().UTC()
	u.staged.Tasks[task.RunID][task.ID] = task
	return nil
}

func (s *taskStore) LoadTask(_ context.Context, runID, taskID string) (model.Task, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return model.Task{}, err
	}
	task, ok := u.staged.Tasks[runID][taskID]
	if !ok {
		return model.Task{}, model.ErrNotFound
	}
	return task, nil
}

func (s *taskStore) ListTasks(_ context.Context, runID string) ([]model.Task, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return nil, err
	}
	tasks := make([]model.Task, 0, len(u.staged.Tasks[runID]))
	for _, task := range u.staged.Tasks[runID] {
		tasks = append(tasks, task)
	}
	slices.SortFunc(tasks, func(a, b model.Task) int { return cmpString(a.ID, b.ID) })
	return tasks, nil
}

func (s *eventStore) AppendEvent(_ context.Context, event model.Event) error {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return err
	}
	if event.Sequence == 0 {
		u.staged.Seq[event.RunID]++
		event.Sequence = u.staged.Seq[event.RunID]
	}
	if event.RecordedAt.IsZero() {
		event.RecordedAt = time.Now().UTC()
	}
	u.staged.Events[event.RunID] = append(u.staged.Events[event.RunID], event)
	return nil
}

func (s *eventStore) ListEvents(_ context.Context, runID string) ([]model.Event, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return nil, err
	}
	return slices.Clone(u.staged.Events[runID]), nil
}

// ListAfter returns events with Sequence > afterSeq within the run, in
// Sequence order. Per the storage contract, sequence is per-run monotonic.
func (s *eventStore) ListAfter(_ context.Context, runID string, afterSeq uint64) ([]model.Event, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return nil, err
	}
	all := u.staged.Events[runID]
	var out []model.Event
	for _, ev := range all {
		if uint64(ev.Sequence) > afterSeq {
			out = append(out, ev)
		}
	}
	return out, nil
}

func (s *blackboardStore) WriteItem(_ context.Context, item model.BlackboardItem) error {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return err
	}
	if item.ID == "" {
		item.ID = u.nextID("bb")
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now().UTC()
	}
	u.staged.Blackboard[item.RunID] = append(u.staged.Blackboard[item.RunID], item)
	u.pending = append(u.pending, item)
	return nil
}

func (s *blackboardStore) SelectItems(_ context.Context, runID string, selector model.BlackboardSelector) ([]model.BlackboardItem, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return nil, err
	}
	return selectBlackboardItems(u.staged, runID, selector), nil
}

func (s *mailboxStore) QueueEnvelope(_ context.Context, env model.TaskEnvelope) error {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return err
	}
	if env.ID == "" {
		env.ID = u.nextID("env")
	}
	if env.CreatedAt.IsZero() {
		env.CreatedAt = time.Now().UTC()
	}
	if env.Status == "" {
		env.Status = "pending"
	}
	if _, exists := u.staged.Envelopes[env.ID]; !exists {
		u.staged.EnvelopesByRun[env.RunID] = append(u.staged.EnvelopesByRun[env.RunID], env.ID)
	}
	u.staged.Envelopes[env.ID] = env
	return nil
}

func (s *mailboxStore) LoadEnvelope(_ context.Context, envelopeID string) (model.TaskEnvelope, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return model.TaskEnvelope{}, err
	}
	env, ok := u.staged.Envelopes[envelopeID]
	if !ok {
		return model.TaskEnvelope{}, model.ErrNotFound
	}
	return env, nil
}

func (s *mailboxStore) UpdateEnvelope(_ context.Context, env model.TaskEnvelope) error {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return err
	}
	if _, ok := u.staged.Envelopes[env.ID]; !ok {
		return model.ErrNotFound
	}
	u.staged.Envelopes[env.ID] = env
	return nil
}

func (s *mailboxStore) ListEnvelopes(_ context.Context, runID string) ([]model.TaskEnvelope, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return nil, err
	}
	ids := slices.Clone(u.staged.EnvelopesByRun[runID])
	out := make([]model.TaskEnvelope, 0, len(ids))
	for _, id := range ids {
		out = append(out, u.staged.Envelopes[id])
	}
	return out, nil
}

func (s *messageStore) QueueMessage(_ context.Context, message model.UserMessage) error {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return err
	}
	if message.ID == "" {
		message.ID = u.nextID("msg")
	}
	if message.CreatedAt.IsZero() {
		message.CreatedAt = time.Now().UTC()
	}
	message.Status = model.UserMessageQueued
	message.UpdatedAt = time.Now().UTC()
	if _, exists := u.staged.Messages[message.ID]; !exists {
		u.staged.MessagesByRun[message.RunID] = append(u.staged.MessagesByRun[message.RunID], message.ID)
	}
	u.staged.Messages[message.ID] = message
	return nil
}

func (s *messageStore) LoadMessage(_ context.Context, runID, messageID string) (model.UserMessage, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return model.UserMessage{}, err
	}
	message, ok := u.staged.Messages[messageID]
	if !ok || message.RunID != runID {
		return model.UserMessage{}, model.ErrNotFound
	}
	return message, nil
}

func (s *messageStore) UpdateMessage(_ context.Context, message model.UserMessage) error {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return err
	}
	if _, ok := u.staged.Messages[message.ID]; !ok {
		return model.ErrNotFound
	}
	message.UpdatedAt = time.Now().UTC()
	u.staged.Messages[message.ID] = message
	return nil
}

func (s *messageStore) ListMessages(_ context.Context, runID string) ([]model.UserMessage, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return nil, err
	}
	ids := slices.Clone(u.staged.MessagesByRun[runID])
	out := make([]model.UserMessage, 0, len(ids))
	for _, id := range ids {
		out = append(out, u.staged.Messages[id])
	}
	return out, nil
}

func (s *messageStore) ListQueuedMessages(_ context.Context) ([]model.UserMessage, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return nil, err
	}
	runIDs := make([]string, 0, len(u.staged.MessagesByRun))
	for runID := range u.staged.MessagesByRun {
		runIDs = append(runIDs, runID)
	}
	slices.Sort(runIDs)
	var out []model.UserMessage
	for _, runID := range runIDs {
		for _, id := range u.staged.MessagesByRun[runID] {
			message, ok := u.staged.Messages[id]
			if !ok || message.Status != model.UserMessageQueued {
				continue
			}
			out = append(out, message)
		}
	}
	return out, nil
}

// ListPendingFor returns messages matching the selector in FIFO insertion
// order. Status filter defaults to UserMessageQueued when no statuses are
// specified. Recipient is currently a no-op until UserMessage gains a
// Recipient field; spec callers should treat it as advisory.
func (s *messageStore) ListPendingFor(_ context.Context, sel model.UserMessageSelector) ([]model.UserMessage, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return nil, err
	}
	statusFilter := sel.Statuses
	if len(statusFilter) == 0 {
		statusFilter = []string{string(model.UserMessageQueued)}
	}
	var out []model.UserMessage
	runIDs := make([]string, 0, len(u.staged.MessagesByRun))
	if sel.RunID != "" {
		runIDs = append(runIDs, sel.RunID)
	} else {
		for runID := range u.staged.MessagesByRun {
			runIDs = append(runIDs, runID)
		}
		slices.Sort(runIDs)
	}
	for _, runID := range runIDs {
		for _, id := range u.staged.MessagesByRun[runID] {
			message, ok := u.staged.Messages[id]
			if !ok {
				continue
			}
			if !slices.Contains(statusFilter, string(message.Status)) {
				continue
			}
			if !sel.Since.IsZero() && message.CreatedAt.Before(sel.Since) {
				continue
			}
			if !sel.Until.IsZero() && message.CreatedAt.After(sel.Until) {
				continue
			}
			out = append(out, message)
			if sel.Limit > 0 && len(out) >= sel.Limit {
				return out, nil
			}
		}
	}
	return out, nil
}

func (s *traceStore) SaveTraceSpan(_ context.Context, span model.TraceSpan) error {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return err
	}
	if span.ID == "" {
		span.ID = u.nextID("span")
	}
	if span.StartedAt.IsZero() {
		span.StartedAt = time.Now().UTC()
	}
	if span.Status == "" {
		span.Status = model.TraceSpanStarted
	}
	u.staged.TraceSpans[span.RunID] = append(u.staged.TraceSpans[span.RunID], span)
	return nil
}

func (s *traceStore) ListTraceSpans(_ context.Context, runID string) ([]model.TraceSpan, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return nil, err
	}
	return slices.Clone(u.staged.TraceSpans[runID]), nil
}

func (s *traceStore) LoadTraceSpan(_ context.Context, spanID string) (model.TraceSpan, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return model.TraceSpan{}, err
	}
	for _, spans := range u.staged.TraceSpans {
		for _, span := range spans {
			if span.ID == spanID {
				return span, nil
			}
		}
	}
	return model.TraceSpan{}, model.ErrNotFound
}

func (s *traceStore) UpdateTraceSpan(_ context.Context, span model.TraceSpan) error {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return err
	}
	for runID, spans := range u.staged.TraceSpans {
		for idx, current := range spans {
			if current.ID == span.ID {
				u.staged.TraceSpans[runID][idx] = span
				return nil
			}
		}
	}
	return model.ErrNotFound
}

func (s *leaseStore) SaveLease(_ context.Context, lease model.TaskExecutionLease) error {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return err
	}
	if lease.ID == "" {
		lease.ID = u.nextID("lease")
	}
	model.SyncLeaseExpiry(&lease)
	key := activeLeaseKey(lease.RunID, lease.TaskID)
	latestID := u.staged.ActiveLeaseByTask[key]
	if latestID != "" && latestID != lease.ID {
		return model.ErrLeaseNotActive
	}
	if existing, ok := u.staged.Leases[lease.ID]; ok {
		lease.Version = existing.Version + 1
	} else {
		lease.Version = 1
	}
	u.staged.Leases[lease.ID] = lease
	u.staged.ActiveLeaseByTask[key] = lease.ID
	return nil
}

// AcquireWithExpectedVersion atomically persists lease iff the latest lease
// slot for the same task has Version == expectedVersion and is not live.
// Satisfies ports.LeaseCAS — see api/store.go for the full contract.
func (s *leaseStore) AcquireWithExpectedVersion(_ context.Context, lease model.TaskExecutionLease, expectedVersion uint64) (bool, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return false, err
	}
	if lease.ID == "" {
		return false, fmt.Errorf("lease.ID required for AcquireWithExpectedVersion: %w", model.ErrInvalidCommand)
	}
	model.SyncLeaseExpiry(&lease)
	key := activeLeaseKey(lease.RunID, lease.TaskID)
	latestID := u.staged.ActiveLeaseByTask[key]
	var latest model.TaskExecutionLease
	if latestID != "" {
		latest = u.staged.Leases[latestID]
	}
	if latest.Version != expectedVersion {
		return false, nil
	}
	now := time.Now().UTC()
	if latest.Status == model.LeaseStatusActive {
		expiry := model.LeaseExpiry(latest)
		if !expiry.IsZero() && expiry.After(now) {
			return false, nil
		}
	}
	if existing, ok := u.staged.Leases[lease.ID]; ok && lease.ID != latestID && existing.Version > 0 {
		return false, nil
	}
	lease.Version = latest.Version + 1
	u.staged.Leases[lease.ID] = lease
	u.staged.ActiveLeaseByTask[key] = lease.ID
	return true, nil
}

// ExtendLease atomically advances Expiry iff the current holder == workerID
// and the lease has not expired. Returns (false, nil) on rotation/expiry.
// Satisfies ports.LeaseCAS.
func (s *leaseStore) ExtendLease(_ context.Context, leaseID string, workerID string, newExpiry time.Time) (bool, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return false, err
	}
	existing, ok := u.staged.Leases[leaseID]
	if !ok {
		return false, nil
	}
	if u.staged.ActiveLeaseByTask[activeLeaseKey(existing.RunID, existing.TaskID)] != leaseID ||
		existing.Status != model.LeaseStatusActive ||
		existing.HolderID != workerID {
		return false, nil
	}
	now := time.Now().UTC()
	expiry := model.LeaseExpiry(existing)
	if expiry.IsZero() || !expiry.After(now) || !newExpiry.After(expiry) {
		return false, nil
	}
	existing.ExpiresAt = newExpiry
	existing.Expiry = newExpiry
	existing.HeartbeatAt = now
	existing.Version++
	u.staged.Leases[leaseID] = existing
	return true, nil
}

func (s *leaseStore) ReleaseExpiredLease(_ context.Context, leaseID string, expectedVersion uint64, releasedAt time.Time) (bool, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return false, err
	}
	existing, ok := u.staged.Leases[leaseID]
	if !ok ||
		u.staged.ActiveLeaseByTask[activeLeaseKey(existing.RunID, existing.TaskID)] != leaseID ||
		existing.Status != model.LeaseStatusActive ||
		existing.Version != expectedVersion {
		return false, nil
	}
	expiry := model.LeaseExpiry(existing)
	if expiry.IsZero() || expiry.After(releasedAt) {
		return false, nil
	}
	existing.Status = model.LeaseStatusReleased
	existing.Version++
	u.staged.Leases[leaseID] = existing
	return true, nil
}

func (s *leaseStore) LoadLease(_ context.Context, leaseID string) (model.TaskExecutionLease, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return model.TaskExecutionLease{}, err
	}
	lease, ok := u.staged.Leases[leaseID]
	if !ok {
		return model.TaskExecutionLease{}, model.ErrNotFound
	}
	return lease, nil
}

func (s *leaseStore) ActiveLeaseForTask(_ context.Context, runID, taskID string) (model.TaskExecutionLease, bool, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return model.TaskExecutionLease{}, false, err
	}
	leaseID := u.staged.ActiveLeaseByTask[activeLeaseKey(runID, taskID)]
	if leaseID == "" {
		return model.TaskExecutionLease{}, false, nil
	}
	lease, ok := u.staged.Leases[leaseID]
	return lease, ok, nil
}

func (s *approvalStore) SaveApproval(_ context.Context, approval model.ApprovalRequest) error {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return err
	}
	u.staged.Approvals[approval.ApprovalID] = approval
	return nil
}

func (s *approvalStore) LoadApproval(_ context.Context, approvalID string) (model.ApprovalRequest, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return model.ApprovalRequest{}, err
	}
	approval, ok := u.staged.Approvals[approvalID]
	if !ok {
		return model.ApprovalRequest{}, model.ErrNotFound
	}
	return approval, nil
}

func (s *resumeTokenStore) SaveResumeToken(_ context.Context, token model.ResumeToken) error {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return err
	}
	u.staged.ResumeTokens[token.TokenID] = token
	return nil
}

func (s *resumeTokenStore) LoadResumeToken(_ context.Context, tokenID string) (model.ResumeToken, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return model.ResumeToken{}, err
	}
	token, ok := u.staged.ResumeTokens[tokenID]
	if !ok {
		return model.ResumeToken{}, model.ErrNotFound
	}
	return token, nil
}

// ListPending returns resume tokens that have not yet expired. Tokens whose
// ExpiresAt has passed are filtered out — the runtime treats expired
// tokens as already-consumed.
func (s *resumeTokenStore) ListPending(_ context.Context, sel model.ResumeTokenSelector) ([]model.ResumeToken, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return nil, err
	}
	now := time.Now()
	var out []model.ResumeToken
	for _, token := range u.staged.ResumeTokens {
		if !token.ExpiresAt.IsZero() && token.ExpiresAt.Before(now) {
			continue
		}
		if sel.RunID != "" && token.RunID != sel.RunID {
			continue
		}
		if sel.TaskID != "" && token.TaskID != sel.TaskID {
			continue
		}
		if !sel.Since.IsZero() && token.ExpiresAt.Before(sel.Since) {
			continue
		}
		if !sel.Until.IsZero() && token.ExpiresAt.After(sel.Until) {
			continue
		}
		out = append(out, token)
	}
	slices.SortFunc(out, func(a, b model.ResumeToken) int {
		if a.ExpiresAt.Before(b.ExpiresAt) {
			return -1
		}
		if a.ExpiresAt.After(b.ExpiresAt) {
			return 1
		}
		return strings.Compare(a.TokenID, b.TokenID)
	})
	if sel.Limit > 0 && len(out) > sel.Limit {
		out = out[:sel.Limit]
	}
	return out, nil
}

func (s *actionAttemptStore) SaveActionAttempt(_ context.Context, attempt model.ActionAttempt) error {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return err
	}
	if attempt.IdempotencyKey != "" {
		for _, existing := range u.staged.ActionAttempts {
			if existing.AttemptID != attempt.AttemptID &&
				existing.RunID == attempt.RunID &&
				existing.TaskID == attempt.TaskID &&
				existing.ToolName == attempt.ToolName &&
				existing.IdempotencyKey == attempt.IdempotencyKey {
				return model.ErrIdempotencyConflict
			}
		}
	}
	u.staged.ActionAttempts[attempt.AttemptID] = cloneActionAttempt(attempt)
	return nil
}

func (s *actionAttemptStore) LoadActionAttempt(_ context.Context, attemptID string) (model.ActionAttempt, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return model.ActionAttempt{}, err
	}
	attempt, ok := u.staged.ActionAttempts[attemptID]
	if !ok {
		return model.ActionAttempt{}, model.ErrNotFound
	}
	return cloneActionAttempt(attempt), nil
}

func (s *actionAttemptStore) LoadActionAttemptByIdempotencyKey(_ context.Context, runID string, taskID string, toolName string, key string) (model.ActionAttempt, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return model.ActionAttempt{}, err
	}
	if key == "" {
		return model.ActionAttempt{}, model.ErrNotFound
	}
	for _, attempt := range u.staged.ActionAttempts {
		if attempt.RunID == runID &&
			attempt.TaskID == taskID &&
			attempt.ToolName == toolName &&
			attempt.IdempotencyKey == key {
			return cloneActionAttempt(attempt), nil
		}
	}
	return model.ActionAttempt{}, model.ErrNotFound
}

func (s *actionAttemptStore) ListActionAttempts(_ context.Context, sel model.ActionAttemptSelector) ([]model.ActionAttempt, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return nil, err
	}
	statuses := make(map[model.ActionAttemptStatus]struct{}, len(sel.Statuses))
	for _, status := range sel.Statuses {
		statuses[status] = struct{}{}
	}
	out := make([]model.ActionAttempt, 0, len(u.staged.ActionAttempts))
	for _, attempt := range u.staged.ActionAttempts {
		if sel.RunID != "" && attempt.RunID != sel.RunID {
			continue
		}
		if sel.TaskID != "" && attempt.TaskID != sel.TaskID {
			continue
		}
		if sel.ToolName != "" && attempt.ToolName != sel.ToolName {
			continue
		}
		if len(statuses) > 0 {
			if _, ok := statuses[attempt.Status]; !ok {
				continue
			}
		}
		if sel.RequiresReconcile != nil && attempt.RequiresReconcile != *sel.RequiresReconcile {
			continue
		}
		out = append(out, cloneActionAttempt(attempt))
	}
	slices.SortFunc(out, func(a, b model.ActionAttempt) int {
		return strings.Compare(a.AttemptID, b.AttemptID)
	})
	if sel.Limit > 0 && len(out) > sel.Limit {
		out = out[:sel.Limit]
	}
	return out, nil
}

func (s *actionAttemptStore) ResolveActionAttempt(_ context.Context, attempt model.ActionAttempt) (bool, error) {
	u := s.uow()
	if err := u.ensureOpen(); err != nil {
		return false, err
	}
	current, ok := u.staged.ActionAttempts[attempt.AttemptID]
	if !ok {
		return false, model.ErrNotFound
	}
	if current.Status != model.ActionAttemptUnknown || !current.RequiresReconcile {
		return false, nil
	}
	if attempt.Status == model.ActionAttemptUnknown ||
		attempt.Status == model.ActionAttemptRunning ||
		attempt.Status == model.ActionAttemptCreated ||
		attempt.RequiresReconcile ||
		attempt.ActionID != current.ActionID ||
		attempt.RunID != current.RunID ||
		attempt.TaskID != current.TaskID ||
		attempt.LeaseID != current.LeaseID ||
		attempt.ToolName != current.ToolName ||
		attempt.IdempotencyKey != current.IdempotencyKey ||
		attempt.InputHash != current.InputHash {
		return false, nil
	}
	u.staged.ActionAttempts[attempt.AttemptID] = cloneActionAttempt(attempt)
	return true, nil
}

func cmpString(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func activeLeaseKey(runID, taskID string) string {
	return runID + "\x00" + taskID
}
