package host

import (
	"context"

	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
)

func (r *Runtime) appendEvent(ctx context.Context, event storage.Event) error {
	existing, err := r.storage.Events().List(ctx, event.RunID)
	if err != nil {
		return err
	}
	if event.Sequence == 0 {
		event.Sequence = len(existing) + 1
	}
	return r.storage.Events().Append(ctx, event)
}

func (r *Runtime) recordInitialEvents(ctx context.Context, state team.RunState) error {
	if err := r.appendEvent(ctx, storage.Event{
		RunID:  state.ID,
		TeamID: state.ID,
		Type:   storage.EventTeamStarted,
		Payload: map[string]any{
			"pattern": state.Pattern,
		},
	}); err != nil {
		return err
	}
	if state.Planning != nil {
		if err := r.appendEvent(ctx, storage.Event{
			RunID:  state.ID,
			TeamID: state.ID,
			Type:   storage.EventPlanCreated,
			Payload: map[string]any{
				"planner": state.Planning.PlannerName,
				"goal":    state.Planning.Goal,
			},
		}); err != nil {
			return err
		}
	}
	for _, task := range state.Tasks {
		if err := r.appendEvent(ctx, storage.Event{
			RunID:  state.ID,
			TeamID: state.ID,
			TaskID: task.ID,
			Type:   storage.EventTaskScheduled,
			Payload: map[string]any{
				"title":  task.Title,
				"input":  task.Input,
				"status": string(task.Status),
			},
		}); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runtime) recordTaskLifecycleEvent(ctx context.Context, state team.RunState, task team.Task, eventType storage.EventType) {
	payload := map[string]any{
		"status": string(task.Status),
	}
	if task.Result != nil {
		payload["summary"] = task.Result.Summary
	}
	_ = r.appendEvent(ctx, storage.Event{
		RunID:   state.ID,
		TeamID:  state.ID,
		TaskID:  task.ID,
		Type:    eventType,
		Payload: payload,
	})
}

func (r *Runtime) recordTeamTerminalEvent(ctx context.Context, state team.RunState) {
	if !state.IsTerminal() {
		return
	}
	if state.Status == team.StatusPaused {
		_ = r.appendEvent(ctx, storage.Event{
			RunID:  state.ID,
			TeamID: state.ID,
			Type:   storage.EventApprovalRequested,
			Payload: map[string]any{
				"reason": state.Result.Error,
			},
		})
		return
	}
	eventType := storage.EventTeamCompleted
	if state.Status != team.StatusCompleted {
		eventType = storage.EventCheckpointSaved
	}
	payload := map[string]any{}
	if state.Result != nil {
		payload["summary"] = state.Result.Summary
		payload["error"] = state.Result.Error
	}
	_ = r.appendEvent(ctx, storage.Event{
		RunID:   state.ID,
		TeamID:  state.ID,
		Type:    eventType,
		Payload: payload,
	})
}

func (r *Runtime) ReplayTeamState(ctx context.Context, teamID string) (team.RunState, error) {
	events, err := r.storage.Events().List(ctx, teamID)
	if err != nil {
		return team.RunState{}, err
	}
	return storage.ReplayTeam(events), nil
}
