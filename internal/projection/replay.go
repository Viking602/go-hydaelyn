package projection

import (
	"github.com/Viking602/venat/internal/core/model"
)

// Project replays a run's event log into a consistent Projection snapshot.
func Project(events []model.Event) (model.Projection, error) {
	if len(events) == 0 {
		return model.Projection{}, model.ErrNotFound
	}

	state := newReplayState()
	for _, event := range events {
		state.applyEvent(event)
	}
	if state.projection.Run.ID == "" {
		return model.Projection{}, model.ErrNotFound
	}
	return state.projectionWithMessages(), nil
}

type replayState struct {
	projection   model.Projection
	messages     map[string]model.UserMessage
	messageOrder []string
}

func newReplayState() replayState {
	return replayState{
		projection: model.Projection{
			Tasks: map[string]model.Task{},
			SideEffects: model.ReplaySideEffects{
				MailboxDeliveries:       0,
				UserMessagePublications: 0,
				ActionExecutions:        0,
			},
		},
		messages: map[string]model.UserMessage{},
	}
}

func (state *replayState) applyEvent(event model.Event) {
	switch event.Type {
	case model.EventRunStarted:
		state.projection.Run = runFromPayload(event.Payload["run"])
	case model.EventRunStatusChanged:
		state.applyRunStatusChanged(event)
	case model.EventTaskCreated, model.EventResponseTaskCreated:
		state.upsertTask(taskFromPayload(event.Payload))
	case model.EventTaskDispatched:
		state.applyTaskDispatched(event)
	case model.EventMailboxRetryScheduled:
		state.applyTaskAndMessagePayload(event)
		state.applyTaskDispatched(event)
	case model.EventTaskExecutionAcquired:
		state.applyTaskExecutionAcquired(event)
	case model.EventTaskCompleted, model.EventTaskFailed, model.EventTaskBlocked, model.EventTaskPaused, model.EventTaskOwnerChanged, model.EventUserMessageQueued, model.EventEnvelopeDeadLettered, model.EventActionReconcileRequired:
		state.applyTaskAndMessagePayload(event)
	case model.EventResponsePublished:
		state.upsertMessage(userMessageFromPayload(mapFromPayload(event.Payload["message"])))
	}
}

func (state *replayState) applyRunStatusChanged(event model.Event) {
	run := runFromPayload(event.Payload["run"])
	if run.ID != "" {
		state.projection.Run = run
		return
	}
	if to := stringFromPayload(event.Payload["to"]); to != "" {
		state.projection.Run.Status = model.RunStatus(to)
	}
}

func (state *replayState) applyTaskDispatched(event model.Event) {
	env := mapFromPayload(event.Payload["envelope"])
	taskID := stringFromPayload(env["taskId"])
	task := state.projection.Tasks[taskID]
	if task.ID == "" {
		task = model.Task{ID: taskID, RunID: stringFromPayload(env["runId"])}
	}
	task.Status = model.TaskStatusDispatched
	task.Version = intFromPayload(env["taskVersion"])
	state.projection.Tasks[task.ID] = task
}

func (state *replayState) applyTaskExecutionAcquired(event model.Event) {
	task := state.projection.Tasks[event.TaskID]
	if task.ID == "" {
		return
	}
	task.Status = model.TaskStatusRunning
	task.Attempts++
	state.projection.Tasks[task.ID] = task
}

func (state *replayState) applyTaskAndMessagePayload(event model.Event) {
	state.upsertTask(taskFromPayload(mapFromPayload(event.Payload["task"])))
	state.upsertMessage(userMessageFromPayload(mapFromPayload(event.Payload["message"])))
}

func (state *replayState) upsertTask(task model.Task) {
	if task.ID == "" {
		return
	}
	state.projection.Tasks[task.ID] = task
}

func (state *replayState) upsertMessage(message model.UserMessage) {
	if message.ID == "" {
		return
	}
	if _, exists := state.messages[message.ID]; !exists {
		state.messageOrder = append(state.messageOrder, message.ID)
	}
	state.messages[message.ID] = message
}

func (state *replayState) projectionWithMessages() model.Projection {
	for _, id := range state.messageOrder {
		state.projection.Messages = append(state.projection.Messages, state.messages[id])
	}
	return state.projection
}
