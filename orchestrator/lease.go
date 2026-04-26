package orchestrator

import (
	"context"
	"slices"
	"time"
)

type DispatchTaskCommand struct {
	RunID           string
	TaskID          string
	TargetAgentID   string
	TargetComponent string
	Payload         map[string]any
}

type AcquireTaskExecutionCommand struct {
	RunID      string
	TaskID     string
	EnvelopeID string
	HolderType HolderType
	HolderID   string
	TTL        time.Duration
}

type HeartbeatTaskExecutionCommand struct {
	LeaseID string
	TTL     time.Duration
}

type ReleaseTaskExecutionCommand struct {
	LeaseID  string
	HolderID string
}

type AckEnvelopeCommand struct {
	EnvelopeID string
	HolderID   string
}

type DeadLetterCommand struct {
	EnvelopeID string
	Reason     string
}

func (r *Runtime) DispatchTask(_ context.Context, cmd DispatchTaskCommand) (TaskEnvelope, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.runs[cmd.RunID]
	if !ok {
		return TaskEnvelope{}, ErrNotFound
	}
	if isTerminalRun(run.Status) {
		return TaskEnvelope{}, ErrTerminalState
	}
	task, ok := r.tasks[cmd.RunID][cmd.TaskID]
	if !ok {
		return TaskEnvelope{}, ErrNotFound
	}
	if isTerminalTask(task.Status) {
		return TaskEnvelope{}, ErrTerminalState
	}
	if len(task.DependsOn) > 0 && !r.dependenciesCompletedLocked(cmd.RunID, task.DependsOn) {
		return TaskEnvelope{}, ErrDependencyUnmet
	}
	now := time.Now().UTC()
	env := TaskEnvelope{
		ID:              r.newID("env"),
		RunID:           cmd.RunID,
		TaskID:          cmd.TaskID,
		TargetAgentID:   cmd.TargetAgentID,
		TargetComponent: cmd.TargetComponent,
		Payload:         cloneAnyMap(cmd.Payload),
		Status:          "pending",
		TaskVersion:     task.Version,
		ReadSelectors:   slices.Clone(task.ReadSelectors),
		WriteTargets:    slices.Clone(task.WriteTargets),
		RetryPolicy:     task.RetryPolicy,
		CreatedAt:       now,
	}
	task.Status = TaskStatusDispatched
	r.saveTaskLocked(task)
	r.writeEnvelopeLocked(env)
	return env, nil
}

func (r *Runtime) AcquireTaskExecution(_ context.Context, cmd AcquireTaskExecutionCommand) (TaskExecutionLease, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.runs[cmd.RunID]
	if !ok {
		return TaskExecutionLease{}, false, ErrNotFound
	}
	if isTerminalRun(run.Status) {
		return TaskExecutionLease{}, false, ErrTerminalState
	}
	task, ok := r.tasks[cmd.RunID][cmd.TaskID]
	if !ok {
		return TaskExecutionLease{}, false, ErrNotFound
	}
	if isTerminalTask(task.Status) {
		return TaskExecutionLease{}, false, ErrTerminalState
	}
	if err := validateTaskHolder(task, cmd.HolderType, cmd.HolderID); err != nil {
		return TaskExecutionLease{}, false, err
	}
	var env TaskEnvelope
	if cmd.EnvelopeID != "" {
		var err error
		env, err = r.validateEnvelopeForAcquireLocked(cmd, task)
		if err != nil {
			return TaskExecutionLease{}, false, err
		}
	}
	key := activeLeaseKey(cmd.RunID, cmd.TaskID)
	if leaseID := r.activeLeaseByTask[key]; leaseID != "" {
		lease := r.leases[leaseID]
		if lease.Status == LeaseStatusActive && lease.ExpiresAt.After(time.Now().UTC()) {
			return lease, false, nil
		}
	}
	ttl := cmd.TTL
	if ttl <= 0 {
		ttl = time.Minute
	}
	now := time.Now().UTC()
	lease := TaskExecutionLease{
		ID:          r.newID("lease"),
		RunID:       cmd.RunID,
		TaskID:      cmd.TaskID,
		EnvelopeID:  cmd.EnvelopeID,
		HolderType:  cmd.HolderType,
		HolderID:    cmd.HolderID,
		TaskVersion: task.Version,
		AcquiredAt:  now,
		ExpiresAt:   now.Add(ttl),
		HeartbeatAt: now,
		Status:      LeaseStatusActive,
	}
	task.Status = TaskStatusRunning
	task.Attempts++
	r.saveTaskLocked(task)
	if cmd.EnvelopeID != "" {
		env.Status = "delivered"
		env.Attempts++
		env.DeliveredAt = now
		r.envelopes[cmd.EnvelopeID] = env
	}
	r.leases[lease.ID] = lease
	r.activeLeaseByTask[key] = lease.ID
	r.appendEventLocked(cmd.RunID, cmd.TaskID, EventTaskExecutionAcquired, map[string]any{
		"leaseId":     lease.ID,
		"envelopeId":  cmd.EnvelopeID,
		"holderType":  string(cmd.HolderType),
		"holderId":    cmd.HolderID,
		"taskVersion": task.Version,
	})
	return lease, true, nil
}

func (r *Runtime) HeartbeatTaskExecution(_ context.Context, cmd HeartbeatTaskExecutionCommand) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	lease, ok := r.leases[cmd.LeaseID]
	if !ok {
		return ErrNotFound
	}
	if lease.Status != LeaseStatusActive {
		return ErrLeaseNotActive
	}
	ttl := cmd.TTL
	if ttl <= 0 {
		ttl = time.Minute
	}
	now := time.Now().UTC()
	lease.HeartbeatAt = now
	lease.ExpiresAt = now.Add(ttl)
	r.leases[lease.ID] = lease
	r.appendEventLocked(lease.RunID, lease.TaskID, EventTaskExecutionHeartbeat, map[string]any{
		"leaseId": lease.ID,
	})
	return nil
}

func (r *Runtime) ReleaseTaskExecution(_ context.Context, cmd ReleaseTaskExecutionCommand) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	lease, ok := r.leases[cmd.LeaseID]
	if !ok {
		return ErrNotFound
	}
	if cmd.HolderID != "" && lease.HolderID != cmd.HolderID {
		return ErrLeaseHolderMismatch
	}
	lease.Status = LeaseStatusReleased
	r.leases[lease.ID] = lease
	delete(r.activeLeaseByTask, activeLeaseKey(lease.RunID, lease.TaskID))
	r.appendEventLocked(lease.RunID, lease.TaskID, EventTaskExecutionReleased, map[string]any{
		"leaseId": lease.ID,
	})
	return nil
}

func (r *Runtime) AckEnvelope(_ context.Context, cmd AckEnvelopeCommand) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	env, ok := r.envelopes[cmd.EnvelopeID]
	if !ok {
		return ErrNotFound
	}
	if cmd.HolderID != "" && env.TargetAgentID != "" && env.TargetAgentID != cmd.HolderID {
		return ErrLeaseHolderMismatch
	}
	env.Status = "acked"
	r.envelopes[cmd.EnvelopeID] = env
	r.appendEventLocked(env.RunID, env.TaskID, EventEnvelopeAcked, map[string]any{
		"envelopeId": cmd.EnvelopeID,
		"holderId":   cmd.HolderID,
	})
	return nil
}

func (r *Runtime) DeadLetter(_ context.Context, cmd DeadLetterCommand) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	env, ok := r.envelopes[cmd.EnvelopeID]
	if !ok {
		return ErrNotFound
	}
	env.Status = "dead"
	r.envelopes[cmd.EnvelopeID] = env
	task, ok := r.tasks[env.RunID][env.TaskID]
	if !ok {
		return ErrNotFound
	}
	if !isTerminalTask(task.Status) {
		task.Status = TaskStatusBlocked
		task.Error = cmd.Reason
		task.Version++
		r.saveTaskLocked(task)
	}
	r.appendEventLocked(env.RunID, env.TaskID, EventEnvelopeDeadLettered, map[string]any{
		"envelopeId": cmd.EnvelopeID,
		"reason":     cmd.Reason,
	})
	r.appendEventLocked(env.RunID, env.TaskID, EventTaskMonitorDecision, map[string]any{
		"decision": "blocked",
		"reason":   cmd.Reason,
	})
	return nil
}

func validateTaskHolder(task Task, holderType HolderType, holderID string) error {
	switch holderType {
	case HolderAgent:
		if task.OwnerAgentID != holderID {
			return ErrLeaseHolderMismatch
		}
	case HolderComponent:
		if task.OwnerComponent != holderID {
			return ErrLeaseHolderMismatch
		}
	default:
		return ErrInvalidCommand
	}
	return nil
}

func (r *Runtime) validateEnvelopeForAcquireLocked(cmd AcquireTaskExecutionCommand, task Task) (TaskEnvelope, error) {
	env, ok := r.envelopes[cmd.EnvelopeID]
	if !ok {
		return TaskEnvelope{}, ErrNotFound
	}
	if env.RunID != cmd.RunID || env.TaskID != cmd.TaskID {
		return TaskEnvelope{}, ErrLeaseHolderMismatch
	}
	if env.TaskVersion != 0 && env.TaskVersion != task.Version {
		return TaskEnvelope{}, ErrStaleTaskVersion
	}
	switch cmd.HolderType {
	case HolderAgent:
		if env.TargetAgentID != "" && env.TargetAgentID != cmd.HolderID {
			return TaskEnvelope{}, ErrLeaseHolderMismatch
		}
	case HolderComponent:
		if env.TargetComponent != "" && env.TargetComponent != cmd.HolderID {
			return TaskEnvelope{}, ErrLeaseHolderMismatch
		}
	}
	return env, nil
}

func activeLeaseKey(runID, taskID string) string {
	return runID + "\x00" + taskID
}

func cloneAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
