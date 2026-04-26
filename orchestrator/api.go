package orchestrator

import (
	"context"
	"fmt"
	"time"
)

type RunTimelineKind string

const (
	RunTimelineKindControl  RunTimelineKind = "control"
	RunTimelineKindWork     RunTimelineKind = "work"
	RunTimelineKindResponse RunTimelineKind = "response"
)

type RunTimelineItem struct {
	Sequence   int             `json:"sequence"`
	RecordedAt time.Time       `json:"recordedAt,omitempty"`
	Kind       RunTimelineKind `json:"kind"`
	RunID      string          `json:"runId"`
	TaskID     string          `json:"taskId,omitempty"`
	Title      string          `json:"title,omitempty"`
	Text       string          `json:"text,omitempty"`
}

func (r *Runtime) QueueRun(ctx context.Context, cmd StartRunCommand) (Run, error) {
	run, _, err := r.StartRun(ctx, cmd)
	if err != nil {
		return Run{}, err
	}
	return r.AdvanceRun(ctx, AdvanceRunCommand{RunID: run.ID})
}

func (r *Runtime) RunTimeline(ctx context.Context, runID string) ([]RunTimelineItem, error) {
	events, err := r.RunEvents(ctx, runID)
	if err != nil {
		return nil, err
	}
	items := make([]RunTimelineItem, 0, len(events))
	for _, event := range events {
		if item, ok := projectRunTimelineEvent(event); ok {
			items = append(items, item)
		}
	}
	return items, nil
}

func projectRunTimelineEvent(event Event) (RunTimelineItem, bool) {
	item := RunTimelineItem{
		Sequence:   event.Sequence,
		RecordedAt: event.RecordedAt,
		RunID:      event.RunID,
		TaskID:     event.TaskID,
	}
	switch event.Type {
	case EventRunStatusChanged:
		item.Kind = RunTimelineKindControl
		item.Title = "Run status changed"
		item.Text = fmt.Sprintf("%s -> %s", stringFromPayload(event.Payload["from"]), stringFromPayload(event.Payload["to"]))
	case EventPlanCreated:
		item.Kind = RunTimelineKindControl
		item.Title = "Plan created"
		item.Text = fmt.Sprintf("%d task(s)", intFromPayload(event.Payload["taskCount"]))
	case EventTaskDispatched:
		item.Kind = RunTimelineKindWork
		item.Title = "Task dispatched"
		item.Text = event.TaskID
	case EventTaskCompleted:
		item.Kind = RunTimelineKindWork
		item.Title = "Task completed"
		item.Text = stringFromPayload(event.Payload["summary"])
	case EventUserMessageQueued:
		item.Kind = RunTimelineKindResponse
		item.Title = "User message queued"
		item.Text = stringFromPayload(mapFromPayload(event.Payload["message"])["payload"])
	case EventResponsePublished:
		item.Kind = RunTimelineKindResponse
		item.Title = "User message published"
		item.Text = stringFromPayload(mapFromPayload(event.Payload["message"])["payload"])
	default:
		return RunTimelineItem{}, false
	}
	return item, true
}
