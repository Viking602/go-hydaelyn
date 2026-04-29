package runtime

import (
	"context"
	"slices"
	"strings"
	"time"
)

type DispatchTaskCommand struct {
	RunID           string
	TaskID          string
	TargetAgentID   string
	TargetComponent string
	Payload         map[string]any
}

// FanOutDispatchTaskCommand dispatches one task to multiple recipients
// resolved from an Address. The framework writes one envelope per resolved
// agent; per-task ownership remains the developer's responsibility.
type FanOutDispatchTaskCommand struct {
	RunID   string
	TaskID  string
	To      Address
	Payload map[string]any
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

func (r *Runtime) DispatchTask(ctx context.Context, cmd DispatchTaskCommand) (TaskEnvelope, error) {
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
	if len(task.DependsOn) > 0 {
		ready, fatal := r.dependencyGateLocked(cmd.RunID, task)
		if fatal {
			return TaskEnvelope{}, ErrDependencyFailed
		}
		if !ready {
			return TaskEnvelope{}, ErrDependencyUnmet
		}
	}
	if _, err := r.authorizeLocked(ctx, PolicyRequest{
		Operation: PolicyOperationDispatch,
		RunID:     cmd.RunID,
		TaskID:    cmd.TaskID,
		Actor:     SourceIdentity{Type: SourceComponent, ID: "dispatcher"},
		Metadata: map[string]string{
			"targetAgentId":   cmd.TargetAgentID,
			"targetComponent": cmd.TargetComponent,
		},
	}); err != nil {
		return TaskEnvelope{}, err
	}
	r.recordTraceLocked(cmd.RunID, cmd.TaskID, "mailbox.dispatch", "mailbox")
	task, err := r.transitionTaskPreserveVersionLocked(task, TaskStatusDispatched)
	if err != nil {
		return TaskEnvelope{}, err
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
	r.writeEnvelopeLocked(env)
	return env, nil
}

// DispatchTaskFanOut resolves cmd.To against the registered agent profiles
// and writes one envelope per recipient. The task transitions to Dispatched
// once (the receivers compete for the lease via AcquireTaskExecution).
func (r *Runtime) DispatchTaskFanOut(ctx context.Context, cmd FanOutDispatchTaskCommand) ([]TaskEnvelope, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, ok := r.runs[cmd.RunID]
	if !ok {
		return nil, ErrNotFound
	}
	if isTerminalRun(run.Status) {
		return nil, ErrTerminalState
	}
	task, ok := r.tasks[cmd.RunID][cmd.TaskID]
	if !ok {
		return nil, ErrNotFound
	}
	if isTerminalTask(task.Status) {
		return nil, ErrTerminalState
	}
	if len(task.DependsOn) > 0 {
		ready, fatal := r.dependencyGateLocked(cmd.RunID, task)
		if fatal {
			return nil, ErrDependencyFailed
		}
		if !ready {
			return nil, ErrDependencyUnmet
		}
	}
	recipients, err := ResolveRecipients(r.agentsLocked(), cmd.To)
	if err != nil {
		return nil, err
	}
	if _, err := r.authorizeLocked(ctx, PolicyRequest{
		Operation: PolicyOperationDispatch,
		RunID:     cmd.RunID,
		TaskID:    cmd.TaskID,
		Actor:     SourceIdentity{Type: SourceComponent, ID: "dispatcher"},
		Metadata: map[string]string{
			"addressKind": string(cmd.To.Kind),
			"recipients":  strings.Join(recipients, ","),
		},
	}); err != nil {
		return nil, err
	}
	r.recordTraceLocked(cmd.RunID, cmd.TaskID, "mailbox.dispatch_fanout", "mailbox")
	task, err = r.transitionTaskPreserveVersionLocked(task, TaskStatusDispatched)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	out := make([]TaskEnvelope, 0, len(recipients))
	for _, agentID := range recipients {
		env := TaskEnvelope{
			ID:            r.newID("env"),
			RunID:         cmd.RunID,
			TaskID:        cmd.TaskID,
			TargetAgentID: agentID,
			Payload:       cloneAnyMap(cmd.Payload),
			Status:        "pending",
			TaskVersion:   task.Version,
			ReadSelectors: slices.Clone(task.ReadSelectors),
			WriteTargets:  slices.Clone(task.WriteTargets),
			RetryPolicy:   task.RetryPolicy,
			CreatedAt:     now,
		}
		r.writeEnvelopeLocked(env)
		out = append(out, env)
	}
	return out, nil
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
	var env TaskEnvelope
	if cmd.EnvelopeID != "" {
		var err error
		env, err = r.validateEnvelopeForAcquireLocked(cmd, task)
		if err != nil {
			return TaskExecutionLease{}, false, err
		}
	} else if err := validateTaskHolder(task, cmd.HolderType, cmd.HolderID); err != nil {
		return TaskExecutionLease{}, false, err
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
	task, err := r.transitionTaskPreserveVersionLocked(task, TaskStatusRunning)
	if err != nil {
		return TaskExecutionLease{}, false, err
	}
	task.Attempts++
	r.saveTaskLocked(task)
	r.recordTraceLocked(cmd.RunID, cmd.TaskID, "lease.acquire", "lease")
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

func (r *Runtime) DeadLetter(ctx context.Context, cmd DeadLetterCommand) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	env, ok := r.envelopes[cmd.EnvelopeID]
	if !ok {
		return ErrNotFound
	}
	decision, err := r.pipeline.TaskMonitor.DecideDeadLetter(ctx, env, cmd.Reason)
	if err != nil {
		return err
	}
	r.recordTraceLocked(env.RunID, env.TaskID, "task_monitor.dead_letter", "task_monitor")
	if decision.Retry {
		env.Status = "pending"
		env.Attempts++
		backoff := env.RetryPolicy.Backoff
		if backoff <= 0 {
			backoff = time.Second
		}
		env.NextRetryAt = time.Now().UTC().Add(backoff)
		if task, ok := r.tasks[env.RunID][env.TaskID]; ok && !isTerminalTask(task.Status) {
			r.releaseActiveLeaseLocked(env.RunID, env.TaskID)
			if task.Status != TaskStatusDispatched {
				task, err = r.transitionTaskLocked(task, TaskStatusDispatched)
				if err != nil {
					return err
				}
			}
			env.TaskVersion = task.Version
			env.DeliveredAt = time.Time{}
		}
		r.envelopes[cmd.EnvelopeID] = env
		r.appendEventLocked(env.RunID, env.TaskID, EventMailboxRetryScheduled, map[string]any{
			"envelopeId":  cmd.EnvelopeID,
			"reason":      cmd.Reason,
			"nextRetryAt": env.NextRetryAt,
		})
		r.appendEventLocked(env.RunID, env.TaskID, EventTaskMonitorDecision, map[string]any{
			"decision": decision.Decision,
			"reason":   decision.Reason,
		})
		return nil
	}
	env.Status = "dead"
	r.envelopes[cmd.EnvelopeID] = env
	task, ok := r.tasks[env.RunID][env.TaskID]
	if !ok {
		return ErrNotFound
	}
	if !isTerminalTask(task.Status) {
		task, err = r.transitionTaskLocked(task, TaskStatusBlocked)
		if err != nil {
			return err
		}
		task.Error = cmd.Reason
		r.saveTaskLocked(task)
	}
	r.appendEventLocked(env.RunID, env.TaskID, EventEnvelopeDeadLettered, map[string]any{
		"envelopeId": cmd.EnvelopeID,
		"reason":     cmd.Reason,
	})
	r.appendEventLocked(env.RunID, env.TaskID, EventTaskMonitorDecision, map[string]any{
		"decision": decision.Decision,
		"reason":   decision.Reason,
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

func (r *Runtime) releaseActiveLeaseLocked(runID, taskID string) {
	key := activeLeaseKey(runID, taskID)
	leaseID := r.activeLeaseByTask[key]
	if leaseID == "" {
		return
	}
	lease, ok := r.leases[leaseID]
	if !ok || lease.Status != LeaseStatusActive {
		delete(r.activeLeaseByTask, key)
		return
	}
	lease.Status = LeaseStatusReleased
	r.leases[lease.ID] = lease
	delete(r.activeLeaseByTask, key)
	r.appendEventLocked(runID, taskID, EventTaskExecutionReleased, map[string]any{
		"leaseId": lease.ID,
	})
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
