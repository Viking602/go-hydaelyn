package host

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Viking602/go-hydaelyn/scheduler"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
)

const localQueueLeaseTTL = 40 * time.Millisecond

var errQueuedTaskMissing = errors.New("queued task missing")

type teamGuard interface {
	IsRunnable(state team.RunState, task team.Task) bool
	IsAborted(state team.RunState) bool
}

type defaultTeamGuard struct{}

func (g *defaultTeamGuard) IsRunnable(state team.RunState, task team.Task) bool {
	return !state.IsTerminal() && state.Status != team.StatusAborted && !task.IsTerminal()
}

func (g *defaultTeamGuard) IsAborted(state team.RunState) bool {
	return state.Status == team.StatusAborted
}

type LeaseReleaser interface {
	Release(ctx context.Context, lease scheduler.TaskLease) error
}

type defaultLeaseReleaser struct {
	queue scheduler.TaskQueue
}

func (r *defaultLeaseReleaser) Release(ctx context.Context, lease scheduler.TaskLease) error {
	return r.queue.Release(ctx, lease)
}

// syncLeaseReleaser lazy-initializes the releaser on first use. The pointer
// is replaced — never mutated in place — so a concurrent goroutine that has
// captured the previous releaser keeps using a stable struct.
func (r *Runtime) syncLeaseReleaser() {
	r.mu.RLock()
	releaser := r.leaseReleaser
	queue := r.queue
	r.mu.RUnlock()
	if releaser != nil {
		if existing, ok := releaser.(*defaultLeaseReleaser); !ok || existing.queue == queue {
			return
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.leaseReleaser.(*defaultLeaseReleaser); ok && existing.queue == r.queue {
		return
	}
	r.leaseReleaser = &defaultLeaseReleaser{queue: r.queue}
}

func (r *Runtime) executeViaQueue(ctx context.Context, current team.RunState, runnableSet map[string]struct{}, semByProfile map[string]chan struct{}) (<-chan taskOutcome, error) {
	if err := r.enqueueTaskSet(ctx, current, runnableSet); err != nil {
		return nil, err
	}
	results := make(chan taskOutcome, len(runnableSet))
	indexByTask := mapTaskIndexes(current.Tasks)
	var wg sync.WaitGroup
	for {
		prepared, immediate, ok := r.acquireQueuedLease(ctx, current, indexByTask)
		if !ok {
			break
		}
		if immediate != nil {
			results <- *immediate
			continue
		}
		wg.Add(1)
		go func(p preparedLease) {
			defer wg.Done()
			results <- r.runPreparedLease(ctx, current, p, semByProfile)
		}(prepared)
	}
	wg.Wait()
	close(results)
	return results, nil
}

func (r *Runtime) enqueueRunnableTasks(ctx context.Context, state team.RunState) error {
	_, runnableSet := runnableTaskSet(state)
	return r.enqueueTaskSet(ctx, state, runnableSet)
}

func (r *Runtime) enqueueTaskSet(ctx context.Context, state team.RunState, runnableSet map[string]struct{}) error {
	for _, task := range state.Tasks {
		if _, ok := runnableSet[task.ID]; !ok {
			continue
		}
		if err := r.queue.Enqueue(ctx, scheduler.TaskLease{
			TaskID:         task.ID,
			TeamID:         state.ID,
			TaskVersion:    task.Version,
			Attempt:        task.Attempts + 1,
			IdempotencyKey: task.IdempotencyKey,
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
	r.syncLeaseReleaser()
	r.recordLeaseAcquiredEvent(ctx, lease.TeamID, lease.TaskID, lease.OwnerID, localQueueLeaseTTL)
	released := false
	stopHeartbeat := func() {}
	releaseLease := func() error {
		// Always stop heartbeats before releasing so we don't extend a
		// lease that's about to be removed from the queue.
		stopHeartbeat()
		stopHeartbeat = func() {}
		released = true
		return r.leaseReleaser.Release(ctx, lease)
	}
	defer func() {
		if !released {
			stopHeartbeat()
			_ = r.leaseReleaser.Release(ctx, lease)
		}
	}()
	state, index, original, err := r.loadQueuedExecutionState(ctx, lease)
	if err != nil {
		if errors.Is(err, errQueuedTaskMissing) {
			return nil
		}
		_ = releaseLease()
		return err
	}
	if !r.teamGuard.IsRunnable(state, original) {
		return releaseLease()
	}
	item, stop, err := r.executeQueuedTask(ctx, state, original, lease)
	stopHeartbeat = stop
	if err != nil && item.ID == "" {
		_ = releaseLease()
		return err
	}
	state = r.applyQueuedTaskResult(ctx, state, index, item)
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
			return releaseLease()
		}
		_ = releaseLease()
		return err
	}
	return releaseLease()
}

func (r *Runtime) continueQueuedTeam(ctx context.Context, pattern team.Pattern, current team.RunState) (team.RunState, bool, error) {
	for range r.maxDriveSteps() {
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

// preparedLease carries the lease + execution targets resolved during the
// fast (serialized) acquisition phase, so the slow execution phase can run
// concurrently across workers.
type preparedLease struct {
	lease         scheduler.TaskLease
	index         int
	original      team.Task
	agentInstance team.AgentInstance
	profile       team.Profile
}

// acquireQueuedLease performs the synchronous acquire/validate steps. It
// returns one of:
//   - (_, _, false)            queue empty for this team — caller stops looping
//   - (_, &outcome, true)      non-execution outcome to forward (validation
//     failure, version conflict, queue error)
//   - (prepared, nil, true)    a lease ready for parallel execution
func (r *Runtime) acquireQueuedLease(ctx context.Context, current team.RunState, indexByTask map[string]int) (preparedLease, *taskOutcome, bool) {
	r.syncLeaseReleaser()
	lease, ok, err := r.queue.AcquireForTeam(ctx, r.workerID, current.ID, localQueueLeaseTTL)
	if err != nil {
		return preparedLease{}, &taskOutcome{err: err}, true
	}
	if !ok {
		return preparedLease{}, nil, false
	}
	r.recordLeaseAcquiredEvent(ctx, lease.TeamID, lease.TaskID, lease.OwnerID, localQueueLeaseTTL)
	if lease.TeamID != "" && lease.TeamID != current.ID {
		r.recordLeaseExpiredEvent(ctx, lease.TeamID, lease.TaskID, r.workerID, eventReasonTaskAlreadyTerminal)
		if err := r.nackQueuedLease(ctx, lease); err != nil {
			return preparedLease{}, &taskOutcome{err: err}, true
		}
		return preparedLease{}, &taskOutcome{}, true
	}
	index, ok := indexByTask[lease.TaskID]
	if !ok {
		if lease.TeamID != "" {
			r.recordLeaseExpiredEvent(ctx, lease.TeamID, lease.TaskID, r.workerID, eventReasonTaskAlreadyTerminal)
			if err := r.nackQueuedLease(ctx, lease); err != nil {
				return preparedLease{}, &taskOutcome{err: err}, true
			}
		} else if err := r.leaseReleaser.Release(ctx, lease); err != nil {
			return preparedLease{}, &taskOutcome{err: err}, true
		}
		return preparedLease{}, &taskOutcome{}, true
	}
	original := current.Tasks[index]
	if lease.TaskVersion > 0 && original.Version != lease.TaskVersion {
		r.recordLeaseExpiredEvent(ctx, current.ID, original.ID, r.workerID, eventReasonStateVersionConflict)
		_ = r.leaseReleaser.Release(ctx, lease)
		return preparedLease{}, &taskOutcome{}, true
	}
	agentInstance, profile, err := r.resolveTaskExecution(current, original)
	if err != nil {
		_ = r.leaseReleaser.Release(ctx, lease)
		failed, _ := finalizeTaskFailure(original, err)
		return preparedLease{}, &taskOutcome{index: index, task: failed, err: err, leaseID: lease.LeaseID, workerID: lease.WorkerID}, true
	}
	return preparedLease{
		lease:         lease,
		index:         index,
		original:      original,
		agentInstance: agentInstance,
		profile:       profile,
	}, nil, true
}

// processQueuedLease performs acquire + execute serially. Kept as a small
// helper for callers and tests that don't care about parallel dispatch.
func (r *Runtime) processQueuedLease(ctx context.Context, current team.RunState, indexByTask map[string]int, semByProfile map[string]chan struct{}) (taskOutcome, bool) {
	prepared, immediate, ok := r.acquireQueuedLease(ctx, current, indexByTask)
	if !ok {
		return taskOutcome{}, false
	}
	if immediate != nil {
		return *immediate, true
	}
	return r.runPreparedLease(ctx, current, prepared, semByProfile), true
}

// runPreparedLease executes a prepared lease. Safe to call concurrently from
// multiple goroutines; per-profile semaphores throttle real parallelism.
func (r *Runtime) runPreparedLease(ctx context.Context, current team.RunState, prepared preparedLease, semByProfile map[string]chan struct{}) taskOutcome {
	releaseSemaphore := func() {}
	if sem, ok := semByProfile[prepared.profile.Name]; ok {
		sem <- struct{}{}
		releaseSemaphore = func() { <-sem }
	}
	stopHeartbeat := startLeaseHeartbeat(ctx, r.queue, prepared.lease, localQueueLeaseTTL)
	leaseCtx := withTaskEventContext(ctx, taskEventContext{LeaseID: prepared.lease.LeaseID, WorkerID: prepared.lease.WorkerID})
	item, err := r.executeTask(leaseCtx, current, prepared.original, prepared.agentInstance, prepared.profile)
	// Stop heartbeat BEFORE releasing the lease so we don't issue a
	// Heartbeat against an already-released lease (the heartbeat goroutine
	// can otherwise be mid-tick when Release runs).
	stopHeartbeat()
	releaseSemaphore()
	_ = r.leaseReleaser.Release(ctx, prepared.lease)
	return taskOutcome{
		index:    prepared.index,
		task:     item,
		err:      err,
		leaseID:  prepared.lease.LeaseID,
		workerID: prepared.lease.WorkerID,
	}
}

func (r *Runtime) nackQueuedLease(ctx context.Context, lease scheduler.TaskLease) error {
	r.syncLeaseReleaser()
	if err := r.leaseReleaser.Release(ctx, lease); err != nil {
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
		terminal, err := r.persistTeamProgress(ctx, current, next)
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
	next, err = r.persistTeamProgress(ctx, current, advanced)
	if err != nil {
		return team.RunState{}, false, err
	}
	return next, next.IsTerminal(), nil
}

func (r *Runtime) loadQueuedExecutionState(ctx context.Context, lease scheduler.TaskLease) (team.RunState, int, team.Task, error) {
	r.syncLeaseReleaser()
	state, err := r.storage.Teams().Load(ctx, lease.TeamID)
	if err != nil {
		return team.RunState{}, 0, team.Task{}, err
	}
	state.Normalize()
	index, ok := mapTaskIndexes(state.Tasks)[lease.TaskID]
	if !ok {
		_ = r.leaseReleaser.Release(ctx, lease)
		return team.RunState{}, 0, team.Task{}, errQueuedTaskMissing
	}
	if lease.TaskVersion > 0 && state.Tasks[index].Version != lease.TaskVersion {
		_ = r.leaseReleaser.Release(ctx, lease)
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
	leaseCtx := withTaskEventContext(ctx, taskEventContext{LeaseID: lease.LeaseID, WorkerID: lease.WorkerID})
	item, err := r.executeTask(leaseCtx, state, original, agentInstance, profile)
	return item, stopHeartbeat, err
}

var errQueuedTaskAlreadyCommitted = errors.New("queued task already committed")

func (r *Runtime) applyQueuedTaskResult(ctx context.Context, state team.RunState, index int, item team.Task) team.RunState {
	state, _, _, _, _ = r.applyTaskOutcome(ctx, state, index, item)
	state.UpdatedAt = time.Now().UTC()
	return state
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
			return errQueuedTaskAlreadyCommitted
		}
	}
	return err
}

func startLeaseHeartbeat(ctx context.Context, queue scheduler.TaskQueue, lease scheduler.TaskLease, ttl time.Duration) func() {
	done := make(chan struct{})
	var once sync.Once
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
		once.Do(func() {
			close(done)
		})
	}
}
