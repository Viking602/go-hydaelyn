package core

import (
	"context"
)

// store_delegates.go makes *Runtime implement the store interfaces by
// delegating to the underlying memProvider. This preserves the existing
// test surface where *Runtime is used as a RunStore / TaskStore / etc.
// All mutations go through a single-use transaction; reads use a snapshot.

func (r *Runtime) SaveRun(ctx context.Context, run Run) error {
	uow, err := r.memProvider.BeginFull(ctx)
	if err != nil {
		return err
	}
	if err := uow.Runs().SaveRun(ctx, run); err != nil {
		_ = uow.Rollback(ctx)
		return err
	}
	return uow.Commit(ctx)
}

func (r *Runtime) LoadRun(ctx context.Context, runID string) (Run, error) {
	snap := r.memProvider.CommittedSnapshot()
	run, ok := snap.Runs[runID]
	if !ok {
		return Run{}, ErrNotFound
	}
	return run, nil
}

func (r *Runtime) SaveTask(ctx context.Context, task Task) error {
	uow, err := r.memProvider.BeginFull(ctx)
	if err != nil {
		return err
	}
	if err := uow.Tasks().SaveTask(ctx, task); err != nil {
		_ = uow.Rollback(ctx)
		return err
	}
	return uow.Commit(ctx)
}

func (r *Runtime) LoadTask(ctx context.Context, runID, taskID string) (Task, error) {
	snap := r.memProvider.CommittedSnapshot()
	tasks, ok := snap.Tasks[runID]
	if !ok {
		return Task{}, ErrNotFound
	}
	task, ok := tasks[taskID]
	if !ok {
		return Task{}, ErrNotFound
	}
	return task, nil
}

func (r *Runtime) ListTasks(ctx context.Context, runID string) ([]Task, error) {
	uow, done, err := r.beginReadUoW(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	return uow.Tasks().ListTasks(ctx, runID)
}

func (r *Runtime) AppendEvent(ctx context.Context, event Event) error {
	uow, err := r.memProvider.BeginFull(ctx)
	if err != nil {
		return err
	}
	if err := uow.Events().AppendEvent(ctx, event); err != nil {
		_ = uow.Rollback(ctx)
		return err
	}
	return uow.Commit(ctx)
}

func (r *Runtime) ListEvents(ctx context.Context, runID string) ([]Event, error) {
	uow, done, err := r.beginReadUoW(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	return uow.Events().ListEvents(ctx, runID)
}

func (r *Runtime) SaveTraceSpan(ctx context.Context, span TraceSpan) error {
	uow, err := r.memProvider.BeginFull(ctx)
	if err != nil {
		return err
	}
	if err := uow.Trace().SaveTraceSpan(ctx, span); err != nil {
		_ = uow.Rollback(ctx)
		return err
	}
	return uow.Commit(ctx)
}

func (r *Runtime) ListTraceSpans(ctx context.Context, runID string) ([]TraceSpan, error) {
	uow, done, err := r.beginReadUoW(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	return uow.Trace().ListTraceSpans(ctx, runID)
}

func (r *Runtime) QueueMessage(ctx context.Context, message UserMessage) error {
	uow, err := r.memProvider.BeginFull(ctx)
	if err != nil {
		return err
	}
	if err := uow.UserMessages().QueueMessage(ctx, message); err != nil {
		_ = uow.Rollback(ctx)
		return err
	}
	return uow.Commit(ctx)
}

func (r *Runtime) LoadMessage(ctx context.Context, runID, messageID string) (UserMessage, error) {
	uow, done, err := r.beginReadUoW(ctx)
	if err != nil {
		return UserMessage{}, err
	}
	defer done()
	return uow.UserMessages().LoadMessage(ctx, runID, messageID)
}

func (r *Runtime) UpdateMessage(ctx context.Context, message UserMessage) error {
	uow, err := r.memProvider.BeginFull(ctx)
	if err != nil {
		return err
	}
	if err := uow.UserMessages().UpdateMessage(ctx, message); err != nil {
		_ = uow.Rollback(ctx)
		return err
	}
	return uow.Commit(ctx)
}

func (r *Runtime) ListMessages(ctx context.Context, runID string) ([]UserMessage, error) {
	uow, done, err := r.beginReadUoW(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	return uow.UserMessages().ListMessages(ctx, runID)
}

// ResumeTokens returns a snapshot of all current resume tokens keyed by tokenID.
// Used in tests to inspect approval blocker state.
func (r *Runtime) ResumeTokens() map[string]ResumeToken {
	snap := r.memProvider.CommittedSnapshot()
	result := make(map[string]ResumeToken, len(snap.ResumeTokens))
	for k, v := range snap.ResumeTokens {
		result[k] = v
	}
	return result
}

func (r *Runtime) ListQueuedMessages(ctx context.Context) ([]UserMessage, error) {
	snap := r.memProvider.CommittedSnapshot()
	var out []UserMessage
	for _, msg := range snap.Messages {
		if msg.Status == UserMessageQueued {
			out = append(out, msg)
		}
	}
	return out, nil
}

func (r *Runtime) QueueEnvelope(ctx context.Context, env TaskEnvelope) error {
	uow, err := r.memProvider.BeginFull(ctx)
	if err != nil {
		return err
	}
	if err := uow.MailboxOutbox().QueueEnvelope(ctx, env); err != nil {
		_ = uow.Rollback(ctx)
		return err
	}
	return uow.Commit(ctx)
}

func (r *Runtime) LoadEnvelope(ctx context.Context, envelopeID string) (TaskEnvelope, error) {
	uow, done, err := r.beginReadUoW(ctx)
	if err != nil {
		return TaskEnvelope{}, err
	}
	defer done()
	return uow.MailboxOutbox().LoadEnvelope(ctx, envelopeID)
}

func (r *Runtime) UpdateEnvelope(ctx context.Context, env TaskEnvelope) error {
	uow, err := r.memProvider.BeginFull(ctx)
	if err != nil {
		return err
	}
	if err := uow.MailboxOutbox().UpdateEnvelope(ctx, env); err != nil {
		_ = uow.Rollback(ctx)
		return err
	}
	return uow.Commit(ctx)
}

func (r *Runtime) ListEnvelopes(ctx context.Context, runID string) ([]TaskEnvelope, error) {
	uow, done, err := r.beginReadUoW(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	return uow.MailboxOutbox().ListEnvelopes(ctx, runID)
}
