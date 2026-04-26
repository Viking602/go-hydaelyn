package orchestrator

import (
	"context"
	"slices"
	"time"
)

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

func (r *Runtime) WriteItem(_ context.Context, item BlackboardItem) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.writeBlackboardLocked(item)
	return nil
}

func (r *Runtime) SelectItems(_ context.Context, runID string, selector BlackboardSelector) ([]BlackboardItem, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
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

func (r *Runtime) ListMessages(_ context.Context, runID string) ([]UserMessage, error) {
	return r.ResponseOutbox(runID), nil
}
