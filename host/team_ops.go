package host

import (
	"context"
	"time"

	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
)

func (r *Runtime) GetTeam(ctx context.Context, teamID string) (team.RunState, error) {
	return r.storage.Teams().Load(ctx, teamID)
}

func (r *Runtime) ListTeams(ctx context.Context) ([]team.RunState, error) {
	return r.storage.Teams().List(ctx)
}

func (r *Runtime) TeamEvents(ctx context.Context, teamID string) ([]storage.Event, error) {
	return r.listEvents(ctx, teamID)
}

func (r *Runtime) RecoverQueueLeases(ctx context.Context, now time.Time) error {
	if r.queue == nil {
		return nil
	}
	return r.queue.RecoverExpired(ctx, now)
}

func (r *Runtime) QueueTeam(ctx context.Context, request StartTeamRequest) (team.RunState, error) {
	return r.startTeamPrepared(ctx, request, false)
}

func (r *Runtime) RunQueueWorker(ctx context.Context, maxTasks int) (int, error) {
	if r.queue == nil {
		return 0, nil
	}
	if maxTasks <= 0 {
		maxTasks = 1
	}
	processed := 0
	for processed < maxTasks {
		lease, ok, err := r.queue.Acquire(ctx, r.workerID, localQueueLeaseTTL)
		if err != nil {
			return processed, err
		}
		if !ok {
			return processed, nil
		}
		if err := r.runQueuedLease(ctx, lease); err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}

func (r *Runtime) resumeTeam(ctx context.Context, teamID string) (team.RunState, error) {
	state, err := r.storage.Teams().Load(ctx, teamID)
	if err != nil {
		return team.RunState{}, err
	}
	state.Normalize()
	if state.Status == team.StatusPaused {
		state.Status = team.StatusRunning
		state.UpdatedAt = time.Now().UTC()
		if isEmptyResult(state.Result) {
			state.Result = nil
		}
	}
	if err := r.validateTeamState(state); err != nil {
		return team.RunState{}, err
	}
	pattern, err := r.lookupPattern(state.Pattern)
	if err != nil {
		return team.RunState{}, err
	}
	teamCtx, cancel := context.WithCancel(ctx)
	r.mu.Lock()
	r.activeTeams[state.ID] = cancel
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.activeTeams, state.ID)
		r.mu.Unlock()
	}()
	return r.driveTeam(teamCtx, pattern, state)
}

func (r *Runtime) abortTeam(ctx context.Context, teamID string) error {
	r.mu.Lock()
	cancel, ok := r.activeTeams[teamID]
	if ok {
		cancel()
		delete(r.activeTeams, teamID)
	}
	r.mu.Unlock()
	state, err := r.storage.Teams().Load(ctx, teamID)
	if err != nil {
		return err
	}
	state.Normalize()
	now := time.Now().UTC()
	for i := range state.Tasks {
		task := state.Tasks[i]
		if task.HasAuthoritativeCompletion() {
			continue
		}
		next := task
		if task.Status == team.TaskStatusPending || task.Status == team.TaskStatusRunning {
			next.Status = team.TaskStatusAborted
			next.Error = "team aborted"
			next.Result = &team.Result{Error: "team aborted"}
			next.FinishedAt = now
			state.Tasks[i] = next
		}
		r.recordTaskCancelledEvent(ctx, state, next, eventReasonTeamAborted)
	}
	state.Status = team.StatusAborted
	state.UpdatedAt = now
	if state.Result == nil {
		state.Result = &team.Result{}
	}
	state.Result.Error = "team aborted"
	return r.saveTeam(ctx, &state)
}

func isEmptyResult(result *team.Result) bool {
	if result == nil {
		return false
	}
	return result.Summary == "" &&
		result.Error == "" &&
		len(result.Structured) == 0 &&
		len(result.ArtifactIDs) == 0 &&
		len(result.Findings) == 0 &&
		len(result.Evidence) == 0 &&
		result.Confidence == 0 &&
		result.Usage.InputTokens == 0 &&
		result.Usage.OutputTokens == 0 &&
		result.Usage.TotalTokens == 0 &&
		result.ToolCallCount == 0
}
