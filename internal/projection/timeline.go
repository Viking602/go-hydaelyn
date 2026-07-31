package projection

import (
	"fmt"

	"github.com/Viking602/venat/internal/core/model"
)

// Timeline converts a run's events into a human-readable timeline.
func Timeline(events []model.Event) []model.RunTimelineItem {
	items := make([]model.RunTimelineItem, 0, len(events))
	for _, event := range events {
		if item, ok := TimelineItem(event); ok {
			items = append(items, item)
		}
	}
	return items
}

// TimelineItem converts a single event into a RunTimelineItem if it is
// visible in the user-facing timeline.
func TimelineItem(event model.Event) (model.RunTimelineItem, bool) {
	item := model.RunTimelineItem{
		Sequence:   event.Sequence,
		RecordedAt: event.RecordedAt,
		RunID:      event.RunID,
		TaskID:     event.TaskID,
	}
	switch event.Type {
	case model.EventRunStatusChanged:
		item.Kind = model.RunTimelineKindControl
		item.Title = "Run status changed"
		item.Text = fmt.Sprintf("%s -> %s", stringFromPayload(event.Payload["from"]), stringFromPayload(event.Payload["to"]))
	case model.EventPlanCreated:
		item.Kind = model.RunTimelineKindControl
		item.Title = "Plan created"
		item.Text = fmt.Sprintf("%d task(s)", intFromPayload(event.Payload["taskCount"]))
	case model.EventTaskDispatched:
		item.Kind = model.RunTimelineKindWork
		item.Title = "Task dispatched"
		item.Text = event.TaskID
	case model.EventTaskCompleted:
		item.Kind = model.RunTimelineKindWork
		item.Title = "Task completed"
		item.Text = stringFromPayload(event.Payload["summary"])
	case model.EventUserMessageQueued:
		item.Kind = model.RunTimelineKindResponse
		item.Title = "User message queued"
		item.Text = stringFromPayload(mapFromPayload(event.Payload["message"])["payload"])
	case model.EventResponsePublished:
		item.Kind = model.RunTimelineKindResponse
		item.Title = "User message published"
		item.Text = stringFromPayload(mapFromPayload(event.Payload["message"])["payload"])
	default:
		return model.RunTimelineItem{}, false
	}
	return item, true
}
