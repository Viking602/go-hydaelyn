package projection

import (
	"github.com/Viking602/go-hydaelyn/internal/core/model"
)

// Project replays a run's event log into a consistent Projection snapshot.
func Project(events []model.Event) (model.Projection, error) {
	if len(events) == 0 {
		return model.Projection{}, model.ErrNotFound
	}
	projection := model.Projection{
		Tasks: map[string]model.Task{},
		SideEffects: model.ReplaySideEffects{
			MailboxDeliveries:       0,
			UserMessagePublications: 0,
			ActionExecutions:        0,
		},
	}
	messages := map[string]model.UserMessage{}
	messageOrder := []string{}
	for _, event := range events {
		switch event.Type {
		case model.EventRunStarted:
			projection.Run = runFromPayload(event.Payload["run"])
		case model.EventRunStatusChanged:
			run := runFromPayload(event.Payload["run"])
			if run.ID != "" {
				projection.Run = run
			} else if to := stringFromPayload(event.Payload["to"]); to != "" {
				projection.Run.Status = model.RunStatus(to)
			}
		case model.EventTaskCreated, model.EventResponseTaskCreated:
			task := taskFromPayload(event.Payload)
			if task.ID != "" {
				projection.Tasks[task.ID] = task
			}
		case model.EventTaskDispatched:
			env := mapFromPayload(event.Payload["envelope"])
			taskID := stringFromPayload(env["taskId"])
			task := projection.Tasks[taskID]
			if task.ID == "" {
				task = model.Task{ID: taskID, RunID: stringFromPayload(env["runId"])}
			}
			task.Status = model.TaskStatusDispatched
			task.Version = intFromPayload(env["taskVersion"])
			projection.Tasks[task.ID] = task
		case model.EventTaskExecutionAcquired:
			taskID := event.TaskID
			task := projection.Tasks[taskID]
			if task.ID != "" {
				task.Status = model.TaskStatusRunning
				task.Attempts++
				projection.Tasks[task.ID] = task
			}
		case model.EventTaskCompleted, model.EventTaskFailed, model.EventTaskBlocked, model.EventTaskPaused, model.EventTaskOwnerChanged, model.EventUserMessageQueued:
			if taskPayload := mapFromPayload(event.Payload["task"]); len(taskPayload) > 0 {
				task := taskFromPayload(taskPayload)
				if task.ID != "" {
					projection.Tasks[task.ID] = task
				}
			}
			if messagePayload := mapFromPayload(event.Payload["message"]); len(messagePayload) > 0 {
				message := userMessageFromPayload(messagePayload)
				if message.ID != "" {
					if _, exists := messages[message.ID]; !exists {
						messageOrder = append(messageOrder, message.ID)
					}
					messages[message.ID] = message
				}
			}
		case model.EventResponsePublished:
			message := userMessageFromPayload(mapFromPayload(event.Payload["message"]))
			if message.ID != "" {
				if _, exists := messages[message.ID]; !exists {
					messageOrder = append(messageOrder, message.ID)
				}
				messages[message.ID] = message
			}
		}
	}
	if projection.Run.ID == "" {
		return model.Projection{}, model.ErrNotFound
	}
	for _, id := range messageOrder {
		projection.Messages = append(projection.Messages, messages[id])
	}
	return projection, nil
}
