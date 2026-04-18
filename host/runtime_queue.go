package host

import (
	"context"
	"errors"
	"time"

	"github.com/Viking602/go-hydaelyn/scheduler"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
)

const localQueueLeaseTTL = 40 * time.Millisecond

var errQueuedTaskMissing = errors.New("queued task missing")

func (r *Runtime) executeViaQueue(ctx context.Context, current team.RunState, runnableSet map[string]struct{}, semByProfile map[string]chan struct{}) (<-chan taskOutcome, error) {
	if err := r.enqueueTaskSet(ctx, current.ID, runnableSet); err != nil {
		return nil, err
	}
	results := make(chan taskOutcome, len(runnableSet))
	indexByTask := mapTaskIndexes(current.Tasks)
	for {
		outcome, ok := r.processQueuedLease(ctx, current, indexByTask, semByProfile)
		if !ok {
			break
		}
		results <- outcome
	}
	close(results)
	return results, nil
}

func (r *Runtime) enqueueRunnableTasks(ctx context.Context, state team.RunState) error {
	_, runnableSet := runnableTaskSet(state)
	return r.enqueueTaskSet(ctx, state.ID, runnableSet)
}

func (r *Runtime) enqueueTaskSet(ctx context.Context, teamID string, runnableSet map[string]struct{}) error {
	for taskID := range runnableSet {
		if err := r.queue.Enqueue(ctx, scheduler.TaskLease{
			TaskID: taskID,
			TeamID: teamID,
		}); err != nil {
			return err
		}
	}
	return nil
}

func mapTaskIndexes(tasks []team.Task) map[string]int {
	indexes := make(map[string]int, len(tasks))
	for idx, task := range tasks {
		indexes[task.ID] = idx
	}
	return indexes
}

func (r *Runtime) runQueuedLease(ctx context.Context, lease scheduler.TaskLease) error {
	r.recordLeaseAcquiredEvent(ctx, lease.TeamID, lease.TaskID, lease.OwnerID, localQueueLeaseTTL)
	state, index, original, err := r.loadQueuedExecutionState(ctx, lease)
	if err != nil {
		if errors.Is(err, errQueuedTaskMissing) {
			return nil
		}
		return err
	}
	if state.IsTerminal() || state.Status == team.StatusAborted {
		return r.queue.Release(ctx, lease)
	}
	if original.IsTerminal() {
		return r.queue.Release(ctx, lease)
	}
	item, stopHeartbeat, err := r.executeQueuedTask(ctx, state, original, lease)
	defer stopHeartbeat()
	if err != nil && item.ID == "" {
		return err
	}
	state = r.applyQueuedTaskResult(state, index, item)
	applied := state.Tasks[index]
	if errors.Is(err, context.Canceled) {
		r.recordTaskCancelledEvent(ctx, state, applied, "cancellation_propagated")
	}
	if applied.Status == team.TaskStatusCompleted {
		if applied.Kind == team.TaskKindVerify || applied.Stage == team.TaskStageVerify {
			r.recordVerifierDecisionEvent(ctx, state, applied)
		}
		if applied.Kind == team.TaskKindSynthesize || applied.Stage == team.TaskStageSynthesize {
			r.recordSynthesisCommittedEvent(ctx, state, applied)
		}
	}
	if err := r.persistQueuedTaskState(ctx, state, item); err != nil {
		if errors.Is(err, errQueuedTaskAlreadyCommitted) {
			return r.queue.Release(ctx, lease)
		}
		return err
	}
	return r.queue.Release(ctx, lease)
}

func (r *Runtime) continueQueuedTeam(ctx context.Context, pattern team.Pattern, current team.RunState) (team.RunState, bool, error) {
	for range 24 {
		next, terminal, err := r.continueQueuedStep(ctx, pattern, current)
		if err != nil {
			return team.RunState{}, false, err
		}
		current = next
		if current.IsTerminal() {
			return current, true, nil
		}
		if terminal {
			return current, true, nil
		}
	}
	return current, false, nil
}

func (r *Runtime) processQueuedLease(ctx context.Context, current team.RunState, indexByTask map[string]int, semByProfile map[string]chan struct{}) (taskOutcome, bool) {
	lease, ok, err := r.queue.Acquire(ctx, r.workerID, localQueueLeaseTTL)
	if err != nil {
		return taskOutcome{err: err}, true
	}
	if !ok {
		return taskOutcome{}, false
	}
	r.recordLeaseAcquiredEvent(ctx, lease.TeamID, lease.TaskID, lease.OwnerID, localQueueLeaseTTL)
	if lease.TeamID != "" && lease.TeamID != current.ID {
		r.recordLeaseExpiredEvent(ctx, lease.TeamID, lease.TaskID, r.workerID, eventReasonTaskAlreadyTerminal)
		if err := r.nackQueuedLease(ctx, lease); err != nil {
			return taskOutcome{err: err}, true
		}
		return taskOutcome{}, true
	}
	index, ok := indexByTask[lease.TaskID]
	if !ok {
		if lease.TeamID != "" {
			r.recordLeaseExpiredEvent(ctx, lease.TeamID, lease.TaskID, r.workerID, eventReasonTaskAlreadyTerminal)
			if err := r.nackQueuedLease(ctx, lease); err != nil {
				return taskOutcome{err: err}, true
			}
		} else if err := r.queue.Release(ctx, lease); err != nil {
			return taskOutcome{err: err}, true
		}
		return taskOutcome{}, true
	}
	original := current.Tasks[index]
	agentInstance, profile, err := r.resolveTaskExecution(current, original)
	if err != nil {
		_ = r.queue.Release(ctx, lease)
		failed, _ := finalizeTaskFailure(original, err)
		return taskOutcome{index: index, task: failed, err: err}, true
	}
	releaseSemaphore := func() {}
	if sem, ok := semByProfile[profile.Name]; ok {
		sem <- struct{}{}
		releaseSemaphore = func() { <-sem }
	}
	stopHeartbeat := startLeaseHeartbeat(ctx, r.queue, lease, localQueueLeaseTTL)
	item, err := r.executeTask(ctx, current, original, agentInstance, profile)
	stopHeartbeat()
	releaseSemaphore()
	_ = r.queue.Release(ctx, lease)
	return taskOutcome{index: index, task: item, err: err}, true
}

func (r *Runtime) nackQueuedLease(ctx context.Context, lease scheduler.TaskLease) error {
	if err := r.queue.Release(ctx, lease); err != nil {
		return err
	}
	lease.OwnerID = ""
	lease.ExpiresAt = time.Time{}
	return r.queue.Enqueue(ctx, lease)
}

func (r *Runtime) resolveQueuedState(ctx context.Context, current team.RunState) (team.RunState, bool, bool, error) {
	if current.IsTerminal() {
		if err := r.saveTeam(ctx, &current); err != nil {
			return team.RunState{}, false, false, err
		}
		return current, true, true, nil
	}
	if next, changed := current.ResolveBlockedTasks(); changed {
		terminal, err := r.persistTeamProgress(ctx, next)
		if err != nil {
			return team.RunState{}, false, false, err
		}
		return terminal, true, terminal.IsTerminal(), nil
	}
	if len(current.RunnableTasks()) > 0 {
		if err := r.enqueueRunnableTasks(ctx, current); err != nil {
			return team.RunState{}, false, false, err
		}
		return current, true, false, nil
	}
	if next, progressed, terminal, err := r.reviewPlannedTeam(ctx, current); progressed || err != nil || terminal {
		return next, progressed, terminal, err
	}
	return current, false, false, nil
}

func (r *Runtime) continueQueuedStep(ctx context.Context, pattern team.Pattern, current team.RunState) (team.RunState, bool, error) {
	next, progressed, terminal, err := r.resolveQueuedState(ctx, current)
	if err != nil || terminal {
		return next, terminal, err
	}
	if progressed {
		return next, false, nil
	}
	advanced, err := r.advancePatternState(ctx, pattern, current)
	if err != nil {
		return team.RunState{}, false, err
	}
	next, err = r.persistTeamProgress(ctx, advanced)
	if err != nil {
		return team.RunState{}, false, err
	}
	return next, next.IsTerminal(), nil
}

func (r *Runtime) loadQueuedExecutionState(ctx context.Context, lease scheduler.TaskLease) (team.RunState, int, team.Task, error) {
	state, err := r.storage.Teams().Load(ctx, lease.TeamID)
	if err != nil {
		return team.RunState{}, 0, team.Task{}, err
	}
	state.Normalize()
	index, ok := mapTaskIndexes(state.Tasks)[lease.TaskID]
	if !ok {
		_ = r.queue.Release(ctx, lease)
		return team.RunState{}, 0, team.Task{}, errQueuedTaskMissing
	}
	return state, index, state.Tasks[index], nil
}

func (r *Runtime) executeQueuedTask(ctx context.Context, state team.RunState, original team.Task, lease scheduler.TaskLease) (team.Task, func(), error) {
	agentInstance, profile, err := r.resolveTaskExecution(state, original)
	if err != nil {
		return team.Task{}, func() {}, err
	}
	stopHeartbeat := startLeaseHeartbeat(ctx, r.queue, lease, localQueueLeaseTTL)
	item, err := r.executeTask(ctx, state, original, agentInstance, profile)
	return item, stopHeartbeat, err
}

var errQueuedTaskAlreadyCommitted = errors.New("queued task already committed")

var _ = (*Runtime).persistQueuedState

func (r *Runtime) applyQueuedTaskResult(state team.RunState, index int, item team.Task) team.RunState {
	state, _, _ = r.applyTaskOutcome(state, index, item)
	state.UpdatedAt = time.Now().UTC()
	return state
}

func (r *Runtime) persistQueuedState(ctx context.Context, state team.RunState, taskID string) error {
	return r.persistQueuedTaskState(ctx, state, team.Task{ID: taskID})
}

func (r *Runtime) persistQueuedTaskState(ctx context.Context, state team.RunState, task team.Task) error {
	state = r.ensureCommittedTaskOutputs(state)
	pattern, err := r.lookupPattern(state.Pattern)
	if err != nil {
		return err
	}
	next, terminal, err := r.continueQueuedTeam(ctx, pattern, state)
	if err != nil {
		return r.resolveQueuedCommitConflict(ctx, state.ID, task, err)
	}
	next = r.ensureCommittedTaskOutputs(next)
	if terminal {
		return nil
	}
	_, err = r.storage.Teams().SaveCAS(ctx, next, next.Version)
	return r.resolveQueuedCommitConflict(ctx, state.ID, task, err)
}

func (r *Runtime) resolveQueuedCommitConflict(ctx context.Context, teamID string, task team.Task, err error) error {
	if !errors.Is(err, storage.ErrStaleState) {
		return err
	}
	r.recordStaleWriteRejectedEvent(ctx, teamID, task.ID, r.workerID, eventReasonStateVersionConflict)
	current, loadErr := r.storage.Teams().Load(ctx, teamID)
	if loadErr != nil {
		return err
	}
	current.Normalize()
	for _, currentTask := range current.Tasks {
		if currentTask.ID != task.ID {
			continue
		}
		if currentTask.Version > task.Version || currentTask.IsTerminal() {
			r.recordLeaseExpiredEvent(ctx, teamID, task.ID, r.workerID, eventReasonStateVersionConflict)
			return errQueuedTaskAlreadyCommitted
		}
	}
	return err
}

func startLeaseHeartbeat(ctx context.Context, queue scheduler.TaskQueue, lease scheduler.TaskLease, ttl time.Duration) func() {
	done := make(chan struct{})
	interval := ttl / 2
	if interval <= 0 {
		interval = ttl
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = queue.Heartbeat(ctx, lease, ttl)
			case <-done:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	return func() {
		close(done)
	}
}
