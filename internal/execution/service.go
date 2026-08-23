package execution

import (
	"context"
	"fmt"
	"time"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core/ports"
	corestate "github.com/Viking602/venat/internal/core/state"
)

type IDGenerator func(prefix string) string

type CapabilityCheck func(context.Context) (bool, error)

type AcquireInput struct {
	RunID                   string
	TaskID                  string
	EnvelopeID              string
	HolderType              api.HolderType
	HolderID                string
	TTL                     time.Duration
	ResourceClaimsAvailable CapabilityCheck
}

type AcquireResult struct {
	Lease          api.TaskExecutionLease
	Acquired       bool
	ResourceClaims api.ResourceClaimDecision
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
	if hasLatest && latest.Status == api.LeaseStatusActive && api.LeaseExpiry(latest).After(now) {
		return AcquireResult{Lease: latest, Acquired: false}, nil
	}
	ttl := input.TTL
	if ttl <= 0 {
		ttl = time.Minute
	}
	lease := api.TaskExecutionLease{
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
		Status:      api.LeaseStatusActive,
	}
	api.SyncLeaseExpiry(&lease)
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
			return AcquireResult{}, fmt.Errorf("execution: lease changed during atomic resource claim acquisition: %w", api.ErrStaleTaskVersion)
		}
		return currentLease(ctx, uow, input)
	}
	lease, err = uow.Leases().LoadLease(ctx, lease.ID)
	if err != nil {
		return AcquireResult{}, err
	}
	task, err = corestate.TransitionTask(task, api.TaskStatusRunning, false)
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
	if err := uow.Events().AppendEvent(ctx, api.Event{RunID: input.RunID, TaskID: input.TaskID, Type: api.EventTaskExecutionAcquired, Payload: map[string]any{"leaseId": lease.ID, "envelopeId": input.EnvelopeID, "holderType": string(input.HolderType), "holderId": input.HolderID, "taskVersion": task.Version, "resourceClaimIds": resourceClaimIDs(claimDecision.Claims)}, RecordedAt: now}); err != nil {
		return AcquireResult{}, err
	}
	return AcquireResult{Lease: lease, Acquired: true, ResourceClaims: claimDecision}, nil
}

func acquireTaskResourceClaims(
	ctx context.Context,
	uow ports.UnitOfWork,
	newID IDGenerator,
	available CapabilityCheck,
	task api.Task,
	lease api.TaskExecutionLease,
	now time.Time,
) (api.ResourceClaimDecision, error) {
	if len(task.ResourceClaims) == 0 {
		return api.ResourceClaimDecision{}, nil
	}
	if available == nil {
		return api.ResourceClaimDecision{}, fmt.Errorf("execution: task %q requires advertised resource claim storage: %w", task.ID, api.ErrInvalidConfiguration)
	}
	supported, err := available(ctx)
	if err != nil {
		return api.ResourceClaimDecision{}, fmt.Errorf("execution: inspect resource claim storage capability: %w", err)
	}
	if !supported {
		return api.ResourceClaimDecision{}, fmt.Errorf("execution: task %q requires advertised resource claim storage: %w", task.ID, api.ErrInvalidConfiguration)
	}
	extension, ok := uow.(ports.ResourceClaimUnitOfWork)
	if !ok || extension.ResourceClaims() == nil {
		return api.ResourceClaimDecision{}, fmt.Errorf("execution: task %q requires resource claim storage: %w", task.ID, api.ErrInvalidConfiguration)
	}
	specs := make([]api.ResourceClaimSpec, len(task.ResourceClaims))
	for index, spec := range task.ResourceClaims {
		spec.ID = newID("claim")
		specs[index] = spec
	}
	return extension.ResourceClaims().AcquireResourceClaims(ctx, api.ResourceClaimRequest{
		RunID: task.RunID, TaskID: task.ID, LeaseID: lease.ID, HolderID: lease.HolderID,
		Claims: specs, RequestedAt: now, ExpiresAt: lease.ExpiresAt,
	})
}

func resourceClaimIDs(claims []api.ResourceClaim) []string {
	if len(claims) == 0 {
		return nil
	}
	ids := make([]string, len(claims))
	for index, claim := range claims {
		ids[index] = claim.ID
	}
	return ids
}

func loadAcquireTarget(ctx context.Context, uow ports.UnitOfWork, input AcquireInput) (api.Task, api.TaskEnvelope, error) {
	run, err := uow.Runs().LoadRun(ctx, input.RunID)
	if err != nil {
		return api.Task{}, api.TaskEnvelope{}, err
	}
	if corestate.IsTerminalRun(run.Status) {
		return api.Task{}, api.TaskEnvelope{}, api.ErrTerminalState
	}
	task, err := uow.Tasks().LoadTask(ctx, input.RunID, input.TaskID)
	if err != nil {
		return api.Task{}, api.TaskEnvelope{}, err
	}
	if corestate.IsTerminalTask(task.Status) {
		return api.Task{}, api.TaskEnvelope{}, api.ErrTerminalState
	}
	var env api.TaskEnvelope
	if input.EnvelopeID != "" {
		env, err = uow.MailboxOutbox().LoadEnvelope(ctx, input.EnvelopeID)
		if err != nil {
			return api.Task{}, api.TaskEnvelope{}, err
		}
		if err := validateEnvelopeForAcquire(input, task, env); err != nil {
			return api.Task{}, api.TaskEnvelope{}, err
		}
	} else if err := validateTaskHolder(task, input.HolderType, input.HolderID); err != nil {
		return api.Task{}, api.TaskEnvelope{}, err
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

func Heartbeat(ctx context.Context, uow ports.UnitOfWork, leaseID, holderID string, ttl time.Duration) (api.TaskExecutionLease, error) {
	lease, err := uow.Leases().LoadLease(ctx, leaseID)
	if err != nil {
		return api.TaskExecutionLease{}, err
	}
	if lease.HolderID != holderID {
		return api.TaskExecutionLease{}, api.ErrLeaseHolderMismatch
	}
	if ttl <= 0 {
		ttl = time.Minute
	}
	now := time.Now().UTC()
	extended, err := uow.Leases().ExtendLease(ctx, leaseID, holderID, now.Add(ttl))
	if err != nil {
		return api.TaskExecutionLease{}, err
	}
	if !extended {
		return api.TaskExecutionLease{}, api.ErrLeaseNotActive
	}
	lease, err = uow.Leases().LoadLease(ctx, leaseID)
	if err != nil {
		return api.TaskExecutionLease{}, err
	}
	if err := transitionLeaseResourceClaims(ctx, uow, lease.ID, api.ResourceClaimActive, now, lease.ExpiresAt); err != nil {
		return api.TaskExecutionLease{}, err
	}
	if err := uow.Events().AppendEvent(ctx, api.Event{RunID: lease.RunID, TaskID: lease.TaskID, Type: api.EventTaskExecutionHeartbeat, Payload: map[string]any{"leaseId": lease.ID}, RecordedAt: now}); err != nil {
		return api.TaskExecutionLease{}, err
	}
	return lease, nil
}

func Release(ctx context.Context, uow ports.UnitOfWork, leaseID, holderID string) (api.TaskExecutionLease, error) {
	lease, err := uow.Leases().LoadLease(ctx, leaseID)
	if err != nil {
		return api.TaskExecutionLease{}, err
	}
	if holderID != "" && lease.HolderID != holderID {
		return api.TaskExecutionLease{}, api.ErrLeaseHolderMismatch
	}
	if lease.Status == api.LeaseStatusReleased {
		return lease, nil
	}
	now := time.Now().UTC()
	lease.Status = api.LeaseStatusReleased
	if err := uow.Leases().SaveLease(ctx, lease); err != nil {
		return api.TaskExecutionLease{}, err
	}
	if err := transitionLeaseResourceClaims(ctx, uow, lease.ID, api.ResourceClaimReleased, now, time.Time{}); err != nil {
		return api.TaskExecutionLease{}, err
	}
	if err := uow.Events().AppendEvent(ctx, api.Event{RunID: lease.RunID, TaskID: lease.TaskID, Type: api.EventTaskExecutionReleased, Payload: map[string]any{"leaseId": lease.ID}, RecordedAt: now}); err != nil {
		return api.TaskExecutionLease{}, err
	}
	return lease, nil
}

func transitionLeaseResourceClaims(ctx context.Context, uow ports.UnitOfWork, leaseID string, to api.ResourceClaimState, at, expiresAt time.Time) error {
	extension, ok := uow.(ports.ResourceClaimUnitOfWork)
	if !ok || extension.ResourceClaims() == nil {
		return nil
	}
	store := extension.ResourceClaims()
	claims, err := store.ListResourceClaims(ctx, api.ResourceClaimSelector{
		LeaseIDs: []string{leaseID}, States: []api.ResourceClaimState{api.ResourceClaimActive},
	})
	if err != nil {
		return err
	}
	if len(claims) == 0 {
		return nil
	}
	transitions := make([]api.ResourceClaimTransition, len(claims))
	for index, claim := range claims {
		transitions[index] = api.ResourceClaimTransition{
			ClaimID: claim.ID, ExpectedVersion: claim.Version, To: to, At: at, ExpiresAt: expiresAt,
		}
	}
	decision, err := store.TransitionResourceClaims(ctx, api.ResourceClaimTransitionRequest{Transitions: transitions})
	if err != nil {
		return err
	}
	if !decision.Acquired {
		return fmt.Errorf("execution: resource claims changed during lease transition: %w", api.ErrStaleTaskVersion)
	}
	return nil
}

// ReleaseResourceClaims releases every active claim tied to a task lease in
// the caller's transaction.
func ReleaseResourceClaims(ctx context.Context, uow ports.UnitOfWork, leaseID string, at time.Time) error {
	return transitionLeaseResourceClaims(ctx, uow, leaseID, api.ResourceClaimReleased, at, time.Time{})
}

// ExpireResourceClaims expires every active claim tied to an expired task
// lease in the caller's transaction.
func ExpireResourceClaims(ctx context.Context, uow ports.UnitOfWork, leaseID string, at time.Time) error {
	return transitionLeaseResourceClaims(ctx, uow, leaseID, api.ResourceClaimExpired, at, time.Time{})
}

func validateEnvelopeForAcquire(input AcquireInput, task api.Task, env api.TaskEnvelope) error {
	if env.RunID != input.RunID || env.TaskID != input.TaskID {
		return api.ErrLeaseHolderMismatch
	}
	if env.TaskVersion != 0 && env.TaskVersion != task.Version {
		return api.ErrStaleTaskVersion
	}
	switch input.HolderType {
	case api.HolderAgent:
		if env.TargetAgentID != "" && env.TargetAgentID != input.HolderID {
			return api.ErrLeaseHolderMismatch
		}
	case api.HolderComponent:
		if env.TargetComponent != "" && env.TargetComponent != input.HolderID {
			return api.ErrLeaseHolderMismatch
		}
	}
	return nil
}

func validateTaskHolder(task api.Task, holderType api.HolderType, holderID string) error {
	switch holderType {
	case api.HolderAgent:
		if task.OwnerAgentID != holderID {
			return api.ErrLeaseHolderMismatch
		}
	case api.HolderComponent:
		if task.OwnerComponent != holderID {
			return api.ErrLeaseHolderMismatch
		}
	default:
		return api.ErrInvalidCommand
	}
	return nil
}
