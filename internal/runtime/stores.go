package runtime

import (
	"context"
	"maps"
	"slices"
	"time"
)

type runtimeStoreProvider struct {
	runtime *Runtime
}

func (p runtimeStoreProvider) Begin(context.Context) (UnitOfWork, error) {
	return runtimeUnitOfWork(p), nil
}

type runtimeUnitOfWork struct {
	runtime *Runtime
}

func (u runtimeUnitOfWork) Runs() RunStore                    { return u.runtime }
func (u runtimeUnitOfWork) Tasks() TaskStore                  { return u.runtime }
func (u runtimeUnitOfWork) Events() EventStore                { return u.runtime }
func (u runtimeUnitOfWork) Blackboard() BlackboardStore       { return u.runtime }
func (u runtimeUnitOfWork) MailboxOutbox() MailboxOutboxStore { return u.runtime }
func (u runtimeUnitOfWork) UserMessages() UserMessageStore    { return u.runtime }
func (u runtimeUnitOfWork) Trace() TraceStore                 { return u.runtime }
func (u runtimeUnitOfWork) Commit(context.Context) error      { return nil }
func (u runtimeUnitOfWork) Rollback(context.Context) error    { return nil }

func (r *Runtime) StoreProvider() StoreProvider {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.storeProvider
}

func (r *Runtime) Begin(ctx context.Context) (UnitOfWork, error) {
	if r.storeProvider == nil {
		return runtimeStoreProvider{runtime: r}.Begin(ctx)
	}
	return r.storeProvider.Begin(ctx)
}

func (r *Runtime) SaveRun(_ context.Context, run Run) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if run.CreatedAt.IsZero() {
		run.CreatedAt = time.Now().UTC()
	}
	run.UpdatedAt = time.Now().UTC()
	r.runs[run.ID] = run
	return nil
}

func (r *Runtime) LoadRun(_ context.Context, runID string) (Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.runs[runID]
	if !ok {
		return Run{}, ErrNotFound
	}
	return run, nil
}

func (r *Runtime) SaveTask(_ context.Context, task Task) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tasks[task.RunID] == nil {
		r.tasks[task.RunID] = map[string]Task{}
	}
	task.UpdatedAt = time.Now().UTC()
	r.tasks[task.RunID][task.ID] = task
	return nil
}

func (r *Runtime) LoadTask(_ context.Context, runID, taskID string) (Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	task, ok := r.tasks[runID][taskID]
	if !ok {
		return Task{}, ErrNotFound
	}
	return task, nil
}

func (r *Runtime) ListTasks(_ context.Context, runID string) ([]Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	tasks := make([]Task, 0, len(r.tasks[runID]))
	for _, task := range r.tasks[runID] {
		tasks = append(tasks, task)
	}
	slices.SortFunc(tasks, func(a, b Task) int {
		return stringsCompare(a.ID, b.ID)
	})
	return tasks, nil
}

func (r *Runtime) AppendEvent(_ context.Context, event Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if event.Sequence == 0 {
		r.seq[event.RunID]++
		event.Sequence = r.seq[event.RunID]
	}
	if event.RecordedAt.IsZero() {
		event.RecordedAt = time.Now().UTC()
	}
	r.events[event.RunID] = append(r.events[event.RunID], event)
	return nil
}

func (r *Runtime) ListEvents(ctx context.Context, runID string) ([]Event, error) {
	return r.RunEvents(ctx, runID)
}

func (r *Runtime) WriteItem(ctx context.Context, item BlackboardItem) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := r.authorizeLocked(ctx, PolicyRequest{
		Operation: PolicyOperationBlackboardWrite,
		RunID:     item.RunID,
		TaskID:    item.TaskID,
		Actor:     item.Source,
		Item:      &item,
	}); err != nil {
		return err
	}
	r.writeBlackboardLocked(item)
	return nil
}

func (r *Runtime) SelectItems(ctx context.Context, runID string, selector BlackboardSelector) ([]BlackboardItem, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.selectItemsLocked(ctx, runID, selector)
}

func (r *Runtime) selectItemsLocked(ctx context.Context, runID string, selector BlackboardSelector) ([]BlackboardItem, error) {
	selector = normalizeBlackboardSelector(selector)
	if _, err := r.authorizeLocked(ctx, PolicyRequest{
		Operation: PolicyOperationBlackboardRead,
		RunID:     runID,
		Selector:  &selector,
	}); err != nil {
		return nil, err
	}
	items := make([]BlackboardItem, 0, len(r.blackboard[runID]))
	for _, item := range r.blackboard[runID] {
		if selector.RunID != "" && item.RunID != selector.RunID {
			continue
		}
		if selector.TaskID != "" && item.TaskID != selector.TaskID {
			continue
		}
		if selector.Visibility != "" && item.Visibility != selector.Visibility {
			continue
		}
		if len(selector.ItemTypes) > 0 && !slices.Contains(selector.ItemTypes, item.Type) {
			continue
		}
		if len(selector.SourceIDs) > 0 && !slices.Contains(selector.SourceIDs, item.Source.ID) {
			continue
		}
		if len(selector.SourceTypes) > 0 && !slices.Contains(selector.SourceTypes, item.Source.Type) {
			continue
		}
		if len(selector.SourceAgentIDs) > 0 && (item.Source.Type != SourceAgent || !slices.Contains(selector.SourceAgentIDs, item.Source.ID)) {
			continue
		}
		if selector.SinceVersion > 0 && item.Version <= selector.SinceVersion {
			continue
		}
		if len(selector.Keys) > 0 && !slices.Contains(selector.Keys, item.Key) {
			continue
		}
		items = append(items, item)
		if selector.Limit > 0 && len(items) >= selector.Limit {
			break
		}
	}
	return items, nil
}

func (r *Runtime) QueueEnvelope(_ context.Context, env TaskEnvelope) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.writeEnvelopeLocked(env)
	return nil
}

func (r *Runtime) LoadEnvelope(_ context.Context, envelopeID string) (TaskEnvelope, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	env, ok := r.envelopes[envelopeID]
	if !ok {
		return TaskEnvelope{}, ErrNotFound
	}
	return env, nil
}

func (r *Runtime) UpdateEnvelope(_ context.Context, env TaskEnvelope) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.envelopes[env.ID]; !ok {
		return ErrNotFound
	}
	r.envelopes[env.ID] = env
	return nil
}

func (r *Runtime) ListEnvelopes(_ context.Context, runID string) ([]TaskEnvelope, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := slices.Clone(r.envelopesByRun[runID])
	out := make([]TaskEnvelope, 0, len(ids))
	for _, id := range ids {
		out = append(out, r.envelopes[id])
	}
	return out, nil
}

func (r *Runtime) QueueMessage(_ context.Context, message UserMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if message.ID == "" {
		message.ID = r.newID("msg")
	}
	if message.CreatedAt.IsZero() {
		message.CreatedAt = time.Now().UTC()
	}
	message.Status = UserMessageQueued
	message.UpdatedAt = time.Now().UTC()
	if _, exists := r.messages[message.ID]; !exists {
		r.messagesByRun[message.RunID] = append(r.messagesByRun[message.RunID], message.ID)
	}
	r.messages[message.ID] = message
	return nil
}

func (r *Runtime) PublishMessage(ctx context.Context, messageID string) error {
	r.mu.Lock()
	message, ok := r.messages[messageID]
	r.mu.Unlock()
	if !ok {
		return ErrNotFound
	}
	return r.PublishResponse(ctx, PublishResponseCommand{RunID: message.RunID, MessageID: messageID})
}

func (r *Runtime) LoadMessage(_ context.Context, runID, messageID string) (UserMessage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	message, ok := r.messages[messageID]
	if !ok || message.RunID != runID {
		return UserMessage{}, ErrNotFound
	}
	return message, nil
}

func (r *Runtime) UpdateMessage(_ context.Context, message UserMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.messages[message.ID]; !ok {
		return ErrNotFound
	}
	message.UpdatedAt = time.Now().UTC()
	r.messages[message.ID] = message
	return nil
}

func (r *Runtime) ListMessages(_ context.Context, runID string) ([]UserMessage, error) {
	return r.ResponseOutbox(runID), nil
}

func (r *Runtime) SaveTraceSpan(_ context.Context, span TraceSpan) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if span.ID == "" {
		span.ID = r.newID("span")
	}
	if span.StartedAt.IsZero() {
		span.StartedAt = time.Now().UTC()
	}
	if span.Status == "" {
		span.Status = TraceSpanStarted
	}
	span.Metadata = maps.Clone(span.Metadata)
	r.traceSpans[span.RunID] = append(r.traceSpans[span.RunID], span)
	return nil
}

func (r *Runtime) ListTraceSpans(_ context.Context, runID string) ([]TraceSpan, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]TraceSpan{}, r.traceSpans[runID]...), nil
}

func normalizeBlackboardSelector(selector BlackboardSelector) BlackboardSelector {
	if len(selector.SourceAgentIDs) == 0 {
		return selector
	}
	if !slices.Contains(selector.SourceTypes, SourceAgent) {
		selector.SourceTypes = append(selector.SourceTypes, SourceAgent)
	}
	for _, id := range selector.SourceAgentIDs {
		if !slices.Contains(selector.SourceIDs, id) {
			selector.SourceIDs = append(selector.SourceIDs, id)
		}
	}
	return selector
}
