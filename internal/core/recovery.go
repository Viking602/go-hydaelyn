package core

import (
	"context"
	"sort"
	"time"

	"github.com/Viking602/venat/internal/core/model"
	"github.com/Viking602/venat/internal/core/ports"
	corestate "github.com/Viking602/venat/internal/core/state"
	"github.com/Viking602/venat/internal/eventpayload"
)

func (r *Runtime) recoverExpiredTaskExecutions(ctx context.Context, runID string) error {
	uow, err := r.beginWriteUoW(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = uow.Rollback(ctx)
		}
	}()
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
	events, err := uow.Events().ListEvents(ctx, runID)
	if err != nil {
		return err
	}
	recovery := executionRecovery{
		runtime:    r,
		uow:        uow,
		runID:      runID,
		now:        time.Now().UTC(),
		envelopes:  recoverableEnvelopes(envelopes),
		unresolved: unresolvedActionAttempts(events),
	}
	changed := false
	for _, task := range tasks {
		if task.Status != model.TaskStatusRunning {
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
	envelopes     map[string]model.TaskEnvelope
	unresolved    map[string][]string
	runReconciled bool
}

func (recovery *executionRecovery) recoverTask(ctx context.Context, task model.Task) (bool, error) {
	lease, hasLease, err := recovery.uow.Leases().ActiveLeaseForTask(ctx, recovery.runID, task.ID)
	if err != nil {
		return false, err
	}
	if hasLease && lease.Status == model.LeaseStatusActive {
		if model.LeaseExpiry(lease).After(recovery.now) {
			return false, nil
		}
		released, err := recovery.uow.Leases().ReleaseExpiredLease(ctx, lease.ID, lease.Version, recovery.now)
		if err != nil {
			return false, err
		}
		if !released {
			return false, nil
		}
		if err := recovery.uow.Events().AppendEvent(ctx, model.Event{
			RunID:      recovery.runID,
			TaskID:     task.ID,
			Type:       model.EventTaskExecutionReleased,
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

func (recovery *executionRecovery) quarantine(ctx context.Context, task model.Task, attemptIDs []string) error {
	sort.Strings(attemptIDs)
	for _, attemptID := range attemptIDs {
		attempt, err := recovery.uow.ActionAttempts().LoadActionAttempt(ctx, attemptID)
		if err != nil {
			return err
		}
		if attempt.Status == model.ActionAttemptUnknown && attempt.RequiresReconcile {
			continue
		}
		if attempt.Status != model.ActionAttemptRunning {
			continue
		}
		attempt.Status = model.ActionAttemptUnknown
		attempt.RequiresReconcile = true
		if err := recovery.uow.ActionAttempts().SaveActionAttempt(ctx, attempt); err != nil {
			return err
		}
		if err := recovery.uow.Events().AppendEvent(ctx, model.Event{
			RunID:      recovery.runID,
			TaskID:     task.ID,
			Type:       model.EventActionAttemptUpdated,
			Payload:    map[string]any{"attemptId": attempt.AttemptID, "status": string(attempt.Status), "requiresReconcile": true},
			RecordedAt: recovery.now,
		}); err != nil {
			return err
		}
	}
	next, err := corestate.TransitionTask(task, model.TaskStatusReconcileRequired, true)
	if err != nil {
		return err
	}
	next.Error = "action attempt outcome unresolved after lease expiry"
	if err := recovery.uow.Tasks().SaveTask(ctx, next); err != nil {
		return err
	}
	if err := recovery.uow.Events().AppendEvent(ctx, model.Event{
		RunID:      recovery.runID,
		TaskID:     task.ID,
		Type:       model.EventActionReconcileRequired,
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
	if run.Status != model.RunStatusReconcileRequired {
		next, err := corestate.TransitionRun(run, model.RunStatusReconcileRequired)
		if err != nil {
			return err
		}
		if err := recovery.uow.Runs().SaveRun(ctx, next); err != nil {
			return err
		}
		if err := recovery.uow.Events().AppendEvent(ctx, model.Event{
			RunID:      next.ID,
			TaskID:     next.RootTaskID,
			Type:       model.EventRunStatusChanged,
			Payload:    map[string]any{"from": string(run.Status), "to": string(next.Status), "run": eventpayload.Run(next)},
			RecordedAt: recovery.now,
		}); err != nil {
			return err
		}
	}
	recovery.runReconciled = true
	return nil
}

func (recovery *executionRecovery) redispatch(ctx context.Context, task model.Task) error {
	next, err := corestate.TransitionTask(task, model.TaskStatusDispatched, true)
	if err != nil {
		return err
	}
	if err := recovery.uow.Tasks().SaveTask(ctx, next); err != nil {
		return err
	}
	envelope, found := recovery.envelopes[task.ID]
	if !found {
		envelope = model.TaskEnvelope{
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
	return recovery.uow.Events().AppendEvent(ctx, model.Event{
		RunID:      recovery.runID,
		TaskID:     task.ID,
		Type:       model.EventTaskDispatched,
		Payload:    map[string]any{"envelope": eventpayload.Envelope(envelope), "reason": "recovery"},
		RecordedAt: recovery.now,
	})
}

func hasRunningTask(tasks []model.Task) bool {
	for _, task := range tasks {
		if task.Status == model.TaskStatusRunning {
			return true
		}
	}
	return false
}

func recoverableEnvelopes(envelopes []model.TaskEnvelope) map[string]model.TaskEnvelope {
	byTask := make(map[string]model.TaskEnvelope)
	for _, envelope := range envelopes {
		if envelope.Status != "dead" {
			byTask[envelope.TaskID] = envelope
		}
	}
	return byTask
}

func unresolvedActionAttempts(events []model.Event) map[string][]string {
	type attempt struct {
		taskID string
	}
	running := make(map[string]attempt)
	for _, event := range events {
		attemptID, _ := event.Payload["attemptId"].(string)
		if attemptID == "" {
			continue
		}
		switch event.Type {
		case model.EventActionAttemptStarted:
			running[attemptID] = attempt{taskID: event.TaskID}
		case model.EventActionAttemptUpdated:
			delete(running, attemptID)
		}
	}
	byTask := make(map[string][]string)
	for attemptID, entry := range running {
		byTask[entry.taskID] = append(byTask[entry.taskID], attemptID)
	}
	return byTask
}
