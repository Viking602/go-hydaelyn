package storage

import "github.com/Viking602/go-hydaelyn/team"

func ReplayTeam(events []Event) team.RunState {
	state := team.RunState{}
	tasks := map[string]int{}
	for _, event := range events {
		if state.ID == "" {
			state.ID = event.TeamID
		}
		switch event.Type {
		case EventTeamStarted:
			state.Pattern, _ = event.Payload["pattern"].(string)
			state.Status = team.StatusRunning
		case EventTaskScheduled:
			task := team.Task{
				ID:     event.TaskID,
				Title:  stringValue(event.Payload["title"]),
				Input:  stringValue(event.Payload["input"]),
				Status: team.TaskStatus(statusValue(event.Payload["status"], string(team.TaskStatusPending))),
			}
			tasks[event.TaskID] = len(state.Tasks)
			state.Tasks = append(state.Tasks, task)
		case EventTaskStarted:
			if idx, ok := tasks[event.TaskID]; ok {
				state.Tasks[idx].Status = team.TaskStatusRunning
			}
		case EventTaskCompleted:
			if idx, ok := tasks[event.TaskID]; ok {
				state.Tasks[idx].Status = team.TaskStatus(statusValue(event.Payload["status"], string(team.TaskStatusCompleted)))
				state.Tasks[idx].Result = &team.Result{Summary: stringValue(event.Payload["summary"])}
			}
		case EventTaskFailed:
			if idx, ok := tasks[event.TaskID]; ok {
				state.Tasks[idx].Status = team.TaskStatusFailed
			}
		case EventApprovalRequested:
			state.Status = team.StatusPaused
			state.Result = &team.Result{Error: stringValue(event.Payload["reason"])}
		case EventCheckpointSaved:
			state.Status = team.Status(statusValue(event.Payload["status"], string(state.Status)))
			if state.Result == nil {
				state.Result = &team.Result{}
			}
			state.Result.Summary = stringValue(event.Payload["summary"])
			state.Result.Error = stringValue(event.Payload["error"])
		case EventTeamCompleted:
			state.Status = team.StatusCompleted
			state.Result = &team.Result{Summary: stringValue(event.Payload["summary"])}
		}
	}
	return state
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func statusValue(value any, fallback string) string {
	if text, ok := value.(string); ok && text != "" {
		return text
	}
	return fallback
}
