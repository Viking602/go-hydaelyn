package projection

import (
	"github.com/Viking602/venat/api"
)

// Project replays a run's event log into a consistent Projection snapshot.
func Project(events []api.Event) (api.Projection, error) {
	if len(events) == 0 {
		return api.Projection{}, api.ErrNotFound
	}

	state := newReplayState()
	for _, event := range events {
		state.applyEvent(event)
	}
	if state.projection.Run.ID == "" {
		return api.Projection{}, api.ErrNotFound
	}
	return state.projectionWithMessages(), nil
}

type replayState struct {
	projection   api.Projection
	messages     map[string]api.UserMessage
	messageOrder []string
}

func newReplayState() replayState {
	return replayState{
		projection: api.Projection{
			Tasks: map[string]api.Task{},
			SideEffects: api.ReplaySideEffects{
				MailboxDeliveries:       0,
				UserMessagePublications: 0,
				ActionExecutions:        0,
			},
		},
		messages: map[string]api.UserMessage{},
	}
}

func (state *replayState) applyEvent(event api.Event) {
	switch event.Type {
	case api.EventRunStarted:
		state.projection.Run = runFromPayload(event.Payload["run"])
	case api.EventRunStatusChanged:
		state.applyRunStatusChanged(event)
	case api.EventTaskCreated, api.EventResponseTaskCreated:
		state.upsertTask(taskFromPayload(event.Payload))
	case api.EventTaskDispatched:
		state.applyTaskDispatched(event)
	case api.EventMailboxRetryScheduled:
		state.applyTaskAndMessagePayload(event)
		state.applyTaskDispatched(event)
	case api.EventTaskExecutionAcquired:
		state.applyTaskExecutionAcquired(event)
	case api.EventTaskCompleted, api.EventTaskFailed, api.EventTaskBlocked, api.EventTaskPaused, api.EventTaskOwnerChanged, api.EventUserMessageQueued, api.EventEnvelopeDeadLettered, api.EventActionReconcileRequired:
		state.applyTaskAndMessagePayload(event)
	case api.EventResponsePublished:
		state.upsertMessage(userMessageFromPayload(mapFromPayload(event.Payload["message"])))
	}
}

func (state *replayState) applyRunStatusChanged(event api.Event) {
	run := runFromPayload(event.Payload["run"])
	if run.ID != "" {
		state.projection.Run = run
		return
	}
	if to := stringFromPayload(event.Payload["to"]); to != "" {
		state.projection.Run.Status = api.RunStatus(to)
	}
}

func (state *replayState) applyTaskDispatched(event api.Event) {
	env := mapFromPayload(event.Payload["envelope"])
	taskID := stringFromPayload(env["taskId"])
	task := state.projection.Tasks[taskID]
	if task.ID == "" {
		task = api.Task{ID: taskID, RunID: stringFromPayload(env["runId"])}
	}
	task.Status = api.TaskStatusDispatched
	task.Version = intFromPayload(env["taskVersion"])
	state.projection.Tasks[task.ID] = task
}

func (state *replayState) applyTaskExecutionAcquired(event api.Event) {
	task := state.projection.Tasks[event.TaskID]
	if task.ID == "" {
		return
	}
	task.Status = api.TaskStatusRunning
	task.Attempts++
	state.projection.Tasks[task.ID] = task
}

func (state *replayState) applyTaskAndMessagePayload(event api.Event) {
	state.upsertTask(taskFromPayload(mapFromPayload(event.Payload["task"])))
	state.upsertMessage(userMessageFromPayload(mapFromPayload(event.Payload["message"])))
}

func (state *replayState) upsertTask(task api.Task) {
	if task.ID == "" {
		return
	}
	state.projection.Tasks[task.ID] = task
}

func (state *replayState) upsertMessage(message api.UserMessage) {
	if message.ID == "" {
		return
	}
	if _, exists := state.messages[message.ID]; !exists {
		state.messageOrder = append(state.messageOrder, message.ID)
	}
	state.messages[message.ID] = message
}

func (state *replayState) projectionWithMessages() api.Projection {
	for _, id := range state.messageOrder {
		state.projection.Messages = append(state.projection.Messages, state.messages[id])
	}
	return state.projection
}
