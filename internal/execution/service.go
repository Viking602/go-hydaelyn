package execution

import (
	"context"
	"fmt"
	"time"

	"github.com/Viking602/venat/internal/core/model"
	"github.com/Viking602/venat/internal/core/ports"
	corestate "github.com/Viking602/venat/internal/core/state"
)

type IDGenerator func(prefix string) string

type CapabilityCheck func(context.Context) (bool, error)

type AcquireInput struct {
	RunID                   string
	TaskID                  string
	EnvelopeID              string
	HolderType              model.HolderType
	HolderID                string
	TTL                     time.Duration
	ResourceClaimsAvailable CapabilityCheck
}

type AcquireResult struct {
	Lease          model.TaskExecutionLease
	Acquired       bool
	ResourceClaims model.ResourceClaimDecision
}

func Acquire(ctx context.Context, uow ports.UnitOfWork, newID IDGenerator, input AcquireInput) (AcquireResult, error) {
	task, env, err := loadAcquireTarget(ctx, uow, input)
	if err != nil {
		return AcquireResult{}, err
	}
	latest, hasLatest, err := uow.Leases().ActiveLeaseForTask(ctx, input.RunID, input.TaskID)
	if err != nil {
		return AcquireResult{}, err
	}
	now := time.Now().UTC()
	if hasLatest && latest.Status == model.LeaseStatusActive && model.LeaseExpiry(latest).After(now) {
		return AcquireResult{Lease: latest, Acquired: false}, nil
	}
	ttl := input.TTL
	if ttl <= 0 {
		ttl = time.Minute
	}
	lease := model.TaskExecutionLease{
		ID:          newID("lease"),
		RunID:       input.RunID,
		TaskID:      input.TaskID,
		EnvelopeID:  input.EnvelopeID,
		HolderType:  input.HolderType,
		HolderID:    input.HolderID,
		TaskVersion: task.Version,
		AcquiredAt:  now,
		ExpiresAt:   now.Add(ttl),
		HeartbeatAt: now,
		Status:      model.LeaseStatusActive,
	}
	model.SyncLeaseExpiry(&lease)
	claimDecision, err := acquireTaskResourceClaims(ctx, uow, newID, input.ResourceClaimsAvailable, task, lease, now)
	if err != nil {
		return AcquireResult{}, err
	}
	if len(task.ResourceClaims) > 0 && !claimDecision.Acquired {
		return AcquireResult{ResourceClaims: claimDecision}, nil
	}
	expectedVersion := uint64(0)
	if hasLatest {
		expectedVersion = latest.Version
	}
	acquired, err := uow.Leases().AcquireWithExpectedVersion(ctx, lease, expectedVersion)
	if err != nil {
		return AcquireResult{}, err
	}
	if !acquired {
		if len(task.ResourceClaims) > 0 {
			return AcquireResult{}, fmt.Errorf("execution: lease changed during atomic resource claim acquisition: %w", model.ErrStaleTaskVersion)
		}
		return currentLease(ctx, uow, input)
	}
	lease, err = uow.Leases().LoadLease(ctx, lease.ID)
	if err != nil {
		return AcquireResult{}, err
	}
	task, err = corestate.TransitionTask(task, model.TaskStatusRunning, false)
	if err != nil {
		return AcquireResult{}, err
	}
	task.Attempts++
	if err := uow.Tasks().SaveTask(ctx, task); err != nil {
		return AcquireResult{}, err
	}
	if input.EnvelopeID != "" {
		env.Status = "delivered"
		env.Attempts++
		env.DeliveredAt = now
		if err := uow.MailboxOutbox().UpdateEnvelope(ctx, env); err != nil {
			return AcquireResult{}, err
		}
	}
	if err := uow.Events().AppendEvent(ctx, model.Event{RunID: input.RunID, TaskID: input.TaskID, Type: model.EventTaskExecutionAcquired, Payload: map[string]any{"leaseId": lease.ID, "envelopeId": input.EnvelopeID, "holderType": string(input.HolderType), "holderId": input.HolderID, "taskVersion": task.Version, "resourceClaimIds": resourceClaimIDs(claimDecision.Claims)}, RecordedAt: now}); err != nil {
		return AcquireResult{}, err
	}
	return AcquireResult{Lease: lease, Acquired: true, ResourceClaims: claimDecision}, nil
}

func acquireTaskResourceClaims(
	ctx context.Context,
	uow ports.UnitOfWork,
	newID IDGenerator,
	available CapabilityCheck,
	task model.Task,
	lease model.TaskExecutionLease,
	now time.Time,
) (model.ResourceClaimDecision, error) {
	if len(task.ResourceClaims) == 0 {
		return model.ResourceClaimDecision{}, nil
	}
	if available == nil {
		return model.ResourceClaimDecision{}, fmt.Errorf("execution: task %q requires advertised resource claim storage: %w", task.ID, model.ErrInvalidConfiguration)
	}
	supported, err := available(ctx)
	if err != nil {
		return model.ResourceClaimDecision{}, fmt.Errorf("execution: inspect resource claim storage capability: %w", err)
	}
	if !supported {
		return model.ResourceClaimDecision{}, fmt.Errorf("execution: task %q requires advertised resource claim storage: %w", task.ID, model.ErrInvalidConfiguration)
	}
	extension, ok := uow.(ports.ResourceClaimUnitOfWork)
	if !ok || extension.ResourceClaims() == nil {
		return model.ResourceClaimDecision{}, fmt.Errorf("execution: task %q requires resource claim storage: %w", task.ID, model.ErrInvalidConfiguration)
	}
	specs := make([]model.ResourceClaimSpec, len(task.ResourceClaims))
	for index, spec := range task.ResourceClaims {
		spec.ID = newID("claim")
		specs[index] = spec
	}
	return extension.ResourceClaims().AcquireResourceClaims(ctx, model.ResourceClaimRequest{
		RunID: task.RunID, TaskID: task.ID, LeaseID: lease.ID, HolderID: lease.HolderID,
		Claims: specs, RequestedAt: now, ExpiresAt: lease.ExpiresAt,
	})
}

func resourceClaimIDs(claims []model.ResourceClaim) []string {
	if len(claims) == 0 {
		return nil
	}
	ids := make([]string, len(claims))
	for index, claim := range claims {
		ids[index] = claim.ID
	}
	return ids
}

func loadAcquireTarget(ctx context.Context, uow ports.UnitOfWork, input AcquireInput) (model.Task, model.TaskEnvelope, error) {
	run, err := uow.Runs().LoadRun(ctx, input.RunID)
	if err != nil {
		return model.Task{}, model.TaskEnvelope{}, err
	}
	if corestate.IsTerminalRun(run.Status) {
		return model.Task{}, model.TaskEnvelope{}, model.ErrTerminalState
	}
	task, err := uow.Tasks().LoadTask(ctx, input.RunID, input.TaskID)
	if err != nil {
		return model.Task{}, model.TaskEnvelope{}, err
	}
	if corestate.IsTerminalTask(task.Status) {
		return model.Task{}, model.TaskEnvelope{}, model.ErrTerminalState
	}
	var env model.TaskEnvelope
	if input.EnvelopeID != "" {
		env, err = uow.MailboxOutbox().LoadEnvelope(ctx, input.EnvelopeID)
		if err != nil {
			return model.Task{}, model.TaskEnvelope{}, err
		}
		if err := validateEnvelopeForAcquire(input, task, env); err != nil {
			return model.Task{}, model.TaskEnvelope{}, err
		}
	} else if err := validateTaskHolder(task, input.HolderType, input.HolderID); err != nil {
		return model.Task{}, model.TaskEnvelope{}, err
	}
	return task, env, nil
}

func currentLease(ctx context.Context, uow ports.UnitOfWork, input AcquireInput) (AcquireResult, error) {
	current, ok, err := uow.Leases().ActiveLeaseForTask(ctx, input.RunID, input.TaskID)
	if err != nil {
		return AcquireResult{}, err
	}
	if ok {
		return AcquireResult{Lease: current, Acquired: false}, nil
	}
	return AcquireResult{Acquired: false}, nil
}

func Heartbeat(ctx context.Context, uow ports.UnitOfWork, leaseID, holderID string, ttl time.Duration) (model.TaskExecutionLease, error) {
	lease, err := uow.Leases().LoadLease(ctx, leaseID)
	if err != nil {
		return model.TaskExecutionLease{}, err
	}
	if lease.HolderID != holderID {
		return model.TaskExecutionLease{}, model.ErrLeaseHolderMismatch
	}
	if ttl <= 0 {
		ttl = time.Minute
	}
	now := time.Now().UTC()
	extended, err := uow.Leases().ExtendLease(ctx, leaseID, holderID, now.Add(ttl))
	if err != nil {
		return model.TaskExecutionLease{}, err
	}
	if !extended {
		return model.TaskExecutionLease{}, model.ErrLeaseNotActive
	}
	lease, err = uow.Leases().LoadLease(ctx, leaseID)
	if err != nil {
		return model.TaskExecutionLease{}, err
	}
	if err := transitionLeaseResourceClaims(ctx, uow, lease.ID, model.ResourceClaimActive, now, lease.ExpiresAt); err != nil {
		return model.TaskExecutionLease{}, err
	}
	if err := uow.Events().AppendEvent(ctx, model.Event{RunID: lease.RunID, TaskID: lease.TaskID, Type: model.EventTaskExecutionHeartbeat, Payload: map[string]any{"leaseId": lease.ID}, RecordedAt: now}); err != nil {
		return model.TaskExecutionLease{}, err
	}
	return lease, nil
}

func Release(ctx context.Context, uow ports.UnitOfWork, leaseID, holderID string) (model.TaskExecutionLease, error) {
	lease, err := uow.Leases().LoadLease(ctx, leaseID)
	if err != nil {
		return model.TaskExecutionLease{}, err
	}
	if holderID != "" && lease.HolderID != holderID {
		return model.TaskExecutionLease{}, model.ErrLeaseHolderMismatch
	}
	if lease.Status == model.LeaseStatusReleased {
		return lease, nil
	}
	now := time.Now().UTC()
	lease.Status = model.LeaseStatusReleased
	if err := uow.Leases().SaveLease(ctx, lease); err != nil {
		return model.TaskExecutionLease{}, err
	}
	if err := transitionLeaseResourceClaims(ctx, uow, lease.ID, model.ResourceClaimReleased, now, time.Time{}); err != nil {
		return model.TaskExecutionLease{}, err
	}
	if err := uow.Events().AppendEvent(ctx, model.Event{RunID: lease.RunID, TaskID: lease.TaskID, Type: model.EventTaskExecutionReleased, Payload: map[string]any{"leaseId": lease.ID}, RecordedAt: now}); err != nil {
		return model.TaskExecutionLease{}, err
	}
	return lease, nil
}

func transitionLeaseResourceClaims(ctx context.Context, uow ports.UnitOfWork, leaseID string, to model.ResourceClaimState, at, expiresAt time.Time) error {
	extension, ok := uow.(ports.ResourceClaimUnitOfWork)
	if !ok || extension.ResourceClaims() == nil {
		return nil
	}
	store := extension.ResourceClaims()
	claims, err := store.ListResourceClaims(ctx, model.ResourceClaimSelector{
		LeaseIDs: []string{leaseID}, States: []model.ResourceClaimState{model.ResourceClaimActive},
	})
	if err != nil {
		return err
	}
	if len(claims) == 0 {
		return nil
	}
	transitions := make([]model.ResourceClaimTransition, len(claims))
	for index, claim := range claims {
		transitions[index] = model.ResourceClaimTransition{
			ClaimID: claim.ID, ExpectedVersion: claim.Version, To: to, At: at, ExpiresAt: expiresAt,
		}
	}
	decision, err := store.TransitionResourceClaims(ctx, model.ResourceClaimTransitionRequest{Transitions: transitions})
	if err != nil {
		return err
	}
	if !decision.Acquired {
		return fmt.Errorf("execution: resource claims changed during lease transition: %w", model.ErrStaleTaskVersion)
	}
	return nil
}

// ReleaseResourceClaims releases every active claim tied to a task lease in
// the caller's transaction.
func ReleaseResourceClaims(ctx context.Context, uow ports.UnitOfWork, leaseID string, at time.Time) error {
	return transitionLeaseResourceClaims(ctx, uow, leaseID, model.ResourceClaimReleased, at, time.Time{})
}

// ExpireResourceClaims expires every active claim tied to an expired task
// lease in the caller's transaction.
func ExpireResourceClaims(ctx context.Context, uow ports.UnitOfWork, leaseID string, at time.Time) error {
	return transitionLeaseResourceClaims(ctx, uow, leaseID, model.ResourceClaimExpired, at, time.Time{})
}

func validateEnvelopeForAcquire(input AcquireInput, task model.Task, env model.TaskEnvelope) error {
	if env.RunID != input.RunID || env.TaskID != input.TaskID {
		return model.ErrLeaseHolderMismatch
	}
	if env.TaskVersion != 0 && env.TaskVersion != task.Version {
		return model.ErrStaleTaskVersion
	}
	switch input.HolderType {
	case model.HolderAgent:
		if env.TargetAgentID != "" && env.TargetAgentID != input.HolderID {
			return model.ErrLeaseHolderMismatch
		}
	case model.HolderComponent:
		if env.TargetComponent != "" && env.TargetComponent != input.HolderID {
			return model.ErrLeaseHolderMismatch
		}
	}
	return nil
}

func validateTaskHolder(task model.Task, holderType model.HolderType, holderID string) error {
	switch holderType {
	case model.HolderAgent:
		if task.OwnerAgentID != holderID {
			return model.ErrLeaseHolderMismatch
		}
	case model.HolderComponent:
		if task.OwnerComponent != holderID {
			return model.ErrLeaseHolderMismatch
		}
	default:
		return model.ErrInvalidCommand
	}
	return nil
}
