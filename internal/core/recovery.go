package core

import (
	"context"
	"sort"
	"time"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core/ports"
	corestate "github.com/Viking602/venat/internal/core/state"
	"github.com/Viking602/venat/internal/eventpayload"
	"github.com/Viking602/venat/internal/execution"
)

func (r *Runtime) recoverExpiredTaskExecutions(ctx context.Context, runID string) (err error) {
	uow, err := r.beginWriteUoW(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer rollbackIfNotCommitted(ctx, uow, &committed, &err)
	tasks, err := uow.Tasks().ListTasks(ctx, runID)
	if err != nil {
		return err
	}
	if !hasRunningTask(tasks) {
		if err := uow.Rollback(ctx); err != nil {
			return err
		}
		committed = true
		return nil
	}
	envelopes, err := uow.MailboxOutbox().ListEnvelopes(ctx, runID)
	if err != nil {
		return err
	}
	attempts, err := uow.ActionAttempts().ListActionAttempts(ctx, api.ActionAttemptSelector{RunID: runID})
	if err != nil {
		return err
	}
	recovery := executionRecovery{
		runtime:    r,
		uow:        uow,
		runID:      runID,
		now:        time.Now().UTC(),
		envelopes:  recoverableEnvelopes(envelopes),
		unresolved: unresolvedActionAttempts(attempts),
	}
	changed := false
	for _, task := range tasks {
		if task.Status != api.TaskStatusRunning {
			continue
		}
		taskChanged, err := recovery.recoverTask(ctx, task)
		if err != nil {
			return err
		}
		changed = changed || taskChanged
	}
	if !changed {
		if err := uow.Rollback(ctx); err != nil {
			return err
		}
		committed = true
		return nil
	}
	if err := uow.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}

type executionRecovery struct {
	runtime       *Runtime
	uow           ports.UnitOfWork
	runID         string
	now           time.Time
	envelopes     map[string]api.TaskEnvelope
	unresolved    map[string][]string
	runReconciled bool
}

func (recovery *executionRecovery) recoverTask(ctx context.Context, task api.Task) (bool, error) {
	lease, hasLease, err := recovery.uow.Leases().ActiveLeaseForTask(ctx, recovery.runID, task.ID)
	if err != nil {
		return false, err
	}
	if hasLease && lease.Status == api.LeaseStatusActive {
		if api.LeaseExpiry(lease).After(recovery.now) {
			return false, nil
		}
		released, err := recovery.uow.Leases().ReleaseExpiredLease(ctx, lease.ID, lease.Version, recovery.now)
		if err != nil {
			return false, err
		}
		if !released {
			return false, nil
		}
		if err := execution.ExpireResourceClaims(ctx, recovery.uow, lease.ID, recovery.now); err != nil {
			return false, err
		}
		if err := recovery.uow.Events().AppendEvent(ctx, api.Event{
			RunID:      recovery.runID,
			TaskID:     task.ID,
			Type:       api.EventTaskExecutionReleased,
			Payload:    map[string]any{"leaseId": lease.ID, "reason": "expired", "version": lease.Version},
			RecordedAt: recovery.now,
		}); err != nil {
			return false, err
		}
	}
	if attemptIDs := recovery.unresolved[task.ID]; len(attemptIDs) > 0 {
		return true, recovery.quarantine(ctx, task, attemptIDs)
	}
	return true, recovery.redispatch(ctx, task)
}

func (recovery *executionRecovery) quarantine(ctx context.Context, task api.Task, attemptIDs []string) error {
	sort.Strings(attemptIDs)
	for _, attemptID := range attemptIDs {
		attempt, err := recovery.uow.ActionAttempts().LoadActionAttempt(ctx, attemptID)
		if err != nil {
			return err
		}
		if attempt.Status == api.ActionAttemptUnknown && attempt.RequiresReconcile {
			continue
		}
		if attempt.Status != api.ActionAttemptRunning {
			continue
		}
		attempt.Status = api.ActionAttemptUnknown
		attempt.RequiresReconcile = true
		if err := recovery.uow.ActionAttempts().SaveActionAttempt(ctx, attempt); err != nil {
			return err
		}
		if err := recovery.uow.Events().AppendEvent(ctx, api.Event{
			RunID:      recovery.runID,
			TaskID:     task.ID,
			Type:       api.EventActionAttemptUpdated,
			Payload:    map[string]any{"attemptId": attempt.AttemptID, "status": string(attempt.Status), "requiresReconcile": true},
			RecordedAt: recovery.now,
		}); err != nil {
			return err
		}
	}
	next, err := corestate.TransitionTask(task, api.TaskStatusReconcileRequired, true)
	if err != nil {
		return err
	}
	next.Error = "action attempt outcome unresolved after lease expiry"
	if err := recovery.uow.Tasks().SaveTask(ctx, next); err != nil {
		return err
	}
	if err := recovery.uow.Events().AppendEvent(ctx, api.Event{
		RunID:      recovery.runID,
		TaskID:     task.ID,
		Type:       api.EventActionReconcileRequired,
		Payload:    map[string]any{"attemptIds": attemptIDs, "reason": next.Error, "task": eventpayload.Task(next)},
		RecordedAt: recovery.now,
	}); err != nil {
		return err
	}
	return recovery.reconcileRun(ctx)
}

func (recovery *executionRecovery) reconcileRun(ctx context.Context) error {
	if recovery.runReconciled {
		return nil
	}
	run, err := recovery.uow.Runs().LoadRun(ctx, recovery.runID)
	if err != nil {
		return err
	}
	if run.Status != api.RunStatusReconcileRequired {
		next, err := corestate.TransitionRun(run, api.RunStatusReconcileRequired)
		if err != nil {
			return err
		}
		if err := recovery.uow.Runs().SaveRun(ctx, next); err != nil {
			return err
		}
		if err := recovery.uow.Events().AppendEvent(ctx, api.Event{
			RunID:      next.ID,
			TaskID:     next.RootTaskID,
			Type:       api.EventRunStatusChanged,
			Payload:    map[string]any{"from": string(run.Status), "to": string(next.Status), "run": eventpayload.Run(next)},
			RecordedAt: recovery.now,
		}); err != nil {
			return err
		}
	}
	recovery.runReconciled = true
	return nil
}

func (recovery *executionRecovery) redispatch(ctx context.Context, task api.Task) error {
	next, err := corestate.TransitionTask(task, api.TaskStatusDispatched, true)
	if err != nil {
		return err
	}
	if err := recovery.uow.Tasks().SaveTask(ctx, next); err != nil {
		return err
	}
	envelope, found := recovery.envelopes[task.ID]
	if !found {
		envelope = api.TaskEnvelope{
			ID:              recovery.runtime.newID("env"),
			RunID:           recovery.runID,
			TaskID:          task.ID,
			TargetAgentID:   task.OwnerAgentID,
			TargetComponent: task.OwnerComponent,
			Type:            "TaskEnvelope",
			RetryPolicy:     task.RetryPolicy,
			CreatedAt:       recovery.now,
		}
	}
	envelope.Status = "pending"
	envelope.TaskVersion = next.Version
	envelope.DeliveredAt = time.Time{}
	envelope.NextRetryAt = time.Time{}
	if found {
		if err := recovery.uow.MailboxOutbox().UpdateEnvelope(ctx, envelope); err != nil {
			return err
		}
	} else if err := recovery.uow.MailboxOutbox().QueueEnvelope(ctx, envelope); err != nil {
		return err
	}
	recovery.envelopes[task.ID] = envelope
	return recovery.uow.Events().AppendEvent(ctx, api.Event{
		RunID:      recovery.runID,
		TaskID:     task.ID,
		Type:       api.EventTaskDispatched,
		Payload:    map[string]any{"envelope": eventpayload.Envelope(envelope), "reason": "recovery"},
		RecordedAt: recovery.now,
	})
}

func hasRunningTask(tasks []api.Task) bool {
	for _, task := range tasks {
		if task.Status == api.TaskStatusRunning {
			return true
		}
	}
	return false
}

func recoverableEnvelopes(envelopes []api.TaskEnvelope) map[string]api.TaskEnvelope {
	byTask := make(map[string]api.TaskEnvelope)
	for _, envelope := range envelopes {
		if envelope.Status != "dead" {
			byTask[envelope.TaskID] = envelope
		}
	}
	return byTask
}

func unresolvedActionAttempts(attempts []api.ActionAttempt) map[string][]string {
	byTask := make(map[string][]string)
	for _, attempt := range attempts {
		if !isUnresolvedActionAttempt(attempt) {
			continue
		}
		byTask[attempt.TaskID] = append(byTask[attempt.TaskID], attempt.AttemptID)
	}
	return byTask
}

func isUnresolvedActionAttempt(attempt api.ActionAttempt) bool {
	if attempt.RequiresReconcile {
		return true
	}
	switch attempt.Status {
	case api.ActionAttemptRunning, api.ActionAttemptCreated, api.ActionAttemptUnknown:
		return true
	default:
		return false
	}
}
