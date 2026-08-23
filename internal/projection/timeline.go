package projection

import (
	"fmt"

	"github.com/Viking602/venat/api"
)

// Timeline converts a run's events into a human-readable timeline.
func Timeline(events []api.Event) []api.RunTimelineItem {
	items := make([]api.RunTimelineItem, 0, len(events))
	for _, event := range events {
		if item, ok := TimelineItem(event); ok {
			items = append(items, item)
		}
	}
	return items
}

// TimelineItem converts a single event into a RunTimelineItem if it is
// visible in the user-facing timeline.
func TimelineItem(event api.Event) (api.RunTimelineItem, bool) {
	item := api.RunTimelineItem{
		Sequence:   event.Sequence,
		RecordedAt: event.RecordedAt,
		RunID:      event.RunID,
		TaskID:     event.TaskID,
	}
	switch event.Type {
	case api.EventRunStatusChanged:
		item.Kind = api.RunTimelineKindControl
		item.Title = "Run status changed"
		item.Text = fmt.Sprintf("%s -> %s", stringFromPayload(event.Payload["from"]), stringFromPayload(event.Payload["to"]))
	case api.EventPlanCreated:
		item.Kind = api.RunTimelineKindControl
		item.Title = "Plan created"
		item.Text = fmt.Sprintf("%d task(s)", intFromPayload(event.Payload["taskCount"]))
	case api.EventTaskDispatched:
		item.Kind = api.RunTimelineKindWork
		item.Title = "Task dispatched"
		item.Text = event.TaskID
	case api.EventTaskCompleted:
		item.Kind = api.RunTimelineKindWork
		item.Title = "Task completed"
		item.Text = stringFromPayload(event.Payload["summary"])
	case api.EventUserMessageQueued:
		item.Kind = api.RunTimelineKindResponse
		item.Title = "User message queued"
		item.Text = stringFromPayload(mapFromPayload(event.Payload["message"])["payload"])
	case api.EventResponsePublished:
		item.Kind = api.RunTimelineKindResponse
		item.Title = "User message published"
		item.Text = stringFromPayload(mapFromPayload(event.Payload["message"])["payload"])
	default:
		return api.RunTimelineItem{}, false
	}
	return item, true
}
