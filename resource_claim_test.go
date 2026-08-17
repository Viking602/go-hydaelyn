package venat

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/core/adapter"
	"github.com/Viking602/venat/internal/memory"
)

func TestAcquireTaskExecutionWithClaims_AtomicLeaseLifecycle(t *testing.T) {
	ctx := context.Background()
	runner := newTestRunner(t)
	firstTask, firstEnvelope := createClaimedExecutionTask(t, runner, "claims-first", "agent-first")
	secondTask, secondEnvelope := createClaimedExecutionTask(t, runner, "claims-second", "agent-second")

	first, err := runner.AcquireTaskExecutionWithClaims(ctx, api.AcquireTaskExecutionCommand{
		RunID: firstTask.RunID, TaskID: firstTask.ID, EnvelopeID: firstEnvelope.ID,
		HolderType: api.HolderAgent, HolderID: "agent-first", TTL: time.Minute,
	})
	if err != nil || !first.Acquired || !first.ResourceClaims.Acquired || len(first.ResourceClaims.Claims) != 1 {
		t.Fatalf("first acquire result=%#v error=%v", first, err)
	}
	firstClaim := first.ResourceClaims.Claims[0]
	if firstClaim.LeaseID != first.Lease.ID || firstClaim.Key != "repo:shared" || firstClaim.State != api.ResourceClaimActive {
		t.Fatalf("first claim not tied to lease: %#v", firstClaim)
	}

	blocked, err := runner.AcquireTaskExecutionWithClaims(ctx, api.AcquireTaskExecutionCommand{
		RunID: secondTask.RunID, TaskID: secondTask.ID, EnvelopeID: secondEnvelope.ID,
		HolderType: api.HolderAgent, HolderID: "agent-second", TTL: time.Minute,
	})
	if err != nil || blocked.Acquired || blocked.ResourceClaims.Reason != api.ResourceClaimDeniedConflict || len(blocked.ResourceClaims.Conflicts) != 1 {
		t.Fatalf("blocked acquire result=%#v error=%v", blocked, err)
	}
	blockedTask, err := runner.Task(ctx, secondTask.RunID, secondTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	active, activeErr := runner.ActiveLeaseCountContext(ctx, secondTask.RunID, secondTask.ID)
	if activeErr != nil {
		t.Fatalf("ActiveLeaseCountContext() error = %v", activeErr)
	}
	if blockedTask.Status != api.TaskStatusDispatched || blockedTask.Attempts != 0 || active != 0 {
		t.Fatalf("claim conflict partially acquired lease: task=%#v active=%d", blockedTask, active)
	}

	if err := runner.HeartbeatTaskExecution(ctx, api.HeartbeatTaskExecutionCommand{
		LeaseID: first.Lease.ID, HolderID: "agent-first", TTL: 2 * time.Minute,
	}); err != nil {
		t.Fatalf("HeartbeatTaskExecution() error = %v", err)
	}
	renewed, err := runner.LoadResourceClaim(ctx, firstClaim.ID)
	if err != nil {
		t.Fatal(err)
	}
	if renewed.Version != firstClaim.Version+1 || !renewed.ExpiresAt.After(firstClaim.ExpiresAt) {
		t.Fatalf("heartbeat did not renew claim: before=%#v after=%#v", firstClaim, renewed)
	}

	if err := runner.ReleaseTaskExecution(ctx, api.ReleaseTaskExecutionCommand{LeaseID: first.Lease.ID, HolderID: "agent-first"}); err != nil {
		t.Fatalf("ReleaseTaskExecution() error = %v", err)
	}
	released, err := runner.LoadResourceClaim(ctx, firstClaim.ID)
	if err != nil {
		t.Fatal(err)
	}
	if released.State != api.ResourceClaimReleased || released.Version != renewed.Version+1 {
		t.Fatalf("lease release did not release claim: %#v", released)
	}

	second, err := runner.AcquireTaskExecutionWithClaims(ctx, api.AcquireTaskExecutionCommand{
		RunID: secondTask.RunID, TaskID: secondTask.ID, EnvelopeID: secondEnvelope.ID,
		HolderType: api.HolderAgent, HolderID: "agent-second", TTL: time.Minute,
	})
	if err != nil || !second.Acquired || !second.ResourceClaims.Acquired {
		t.Fatalf("second acquire after release result=%#v error=%v", second, err)
	}
}

func TestAcquireResourceClaims_PublicTimestampCannotBypassLiveExclusiveClaim(t *testing.T) {
	ctx := context.Background()
	runner := newTestRunner(t)
	base := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	first, err := runner.AcquireResourceClaims(ctx, api.ResourceClaimRequest{
		RunID: "run-first", TaskID: "task-first", LeaseID: "lease-first", HolderID: "holder-first",
		Claims:      []api.ResourceClaimSpec{{ID: "claim-first", Key: "repo:protected", Mode: api.ResourceClaimExclusive}},
		RequestedAt: base, ExpiresAt: base.Add(time.Hour),
	})
	if err != nil || !first.Acquired {
		t.Fatalf("first acquire decision=%#v error=%v", first, err)
	}
	future := base.Add(24 * time.Hour)
	second, err := runner.AcquireResourceClaims(ctx, api.ResourceClaimRequest{
		RunID: "run-second", TaskID: "task-second", LeaseID: "lease-second", HolderID: "holder-second",
		Claims:      []api.ResourceClaimSpec{{ID: "claim-second", Key: "repo:protected", Mode: api.ResourceClaimExclusive}},
		RequestedAt: future, ExpiresAt: future.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("future acquire error=%v", err)
	}
	if second.Acquired || second.Reason != api.ResourceClaimDeniedConflict || len(second.Conflicts) != 1 {
		t.Fatalf("future acquire decision=%#v, want current conflict", second)
	}
}

func TestAcquireResourceClaims_PublicRetryIsIdempotentAcrossNormalizedTimes(t *testing.T) {
	ctx := context.Background()
	runner := newTestRunner(t)
	requestedAt := time.Date(2026, time.August, 5, 13, 0, 0, 0, time.UTC)
	request := api.ResourceClaimRequest{
		RunID: "run-retry", TaskID: "task-retry", LeaseID: "lease-retry", HolderID: "holder-retry",
		Claims:      []api.ResourceClaimSpec{{ID: "claim-retry", Key: "repo:retry", Mode: api.ResourceClaimExclusive}},
		RequestedAt: requestedAt, ExpiresAt: requestedAt.Add(time.Hour),
	}
	first, err := runner.AcquireResourceClaims(ctx, request)
	if err != nil || !first.Acquired || len(first.Claims) != 1 {
		t.Fatalf("first acquire decision=%#v error=%v", first, err)
	}
	second, err := runner.AcquireResourceClaims(ctx, request)
	if err != nil || !second.Acquired || len(second.Claims) != 1 || second.Claims[0] != first.Claims[0] {
		t.Fatalf("retry acquire decision=%#v error=%v, first=%#v", second, err, first)
	}
	listed, err := runner.ListResourceClaims(ctx, api.ResourceClaimSelector{IDs: []string{"claim-retry"}})
	if err != nil || len(listed) != 1 {
		t.Fatalf("retry persisted claims=%#v error=%v", listed, err)
	}
}

func TestAcquireTaskExecutionWithClaims_RejectsNonTransactionalProvider(t *testing.T) {
	ctx := context.Background()
	provider := &claimsNonTransactionalProvider{StoreProvider: adapter.StoreProviderFromCore(memory.NewProvider())}
	runner := NewDevelopment(api.Config{StoreProvider: provider})
	task, envelope := createClaimedExecutionTask(t, runner, "claims-nontransactional", "agent")
	_, err := runner.AcquireTaskExecutionWithClaims(ctx, api.AcquireTaskExecutionCommand{
		RunID: task.RunID, TaskID: task.ID, EnvelopeID: envelope.ID,
		HolderType: api.HolderAgent, HolderID: "agent", TTL: time.Minute,
	})
	if !errors.Is(err, api.ErrInvalidConfiguration) {
		t.Fatalf("AcquireTaskExecutionWithClaims() error=%v, want ErrInvalidConfiguration", err)
	}
	if loaded, loadErr := runner.Task(ctx, task.RunID, task.ID); loadErr != nil {
		t.Fatal(loadErr)
	} else if active, activeErr := runner.ActiveLeaseCountContext(ctx, task.RunID, task.ID); activeErr != nil {
		t.Fatalf("ActiveLeaseCountContext() error = %v", activeErr)
	} else if loaded.Status != api.TaskStatusDispatched || active != 0 {
		t.Fatalf("nontransactional store partially acquired execution: task=%#v active=%d", loaded, active)
	}
}

type claimsNonTransactionalProvider struct {
	api.StoreProvider
}

func (p *claimsNonTransactionalProvider) Begin(ctx context.Context) (api.UnitOfWork, error) {
	return p.StoreProvider.Begin(ctx)
}

func (p *claimsNonTransactionalProvider) Capabilities(ctx context.Context) (api.StoreCapabilities, error) {
	capabilities, err := p.StoreProvider.(api.CapabilityReporter).Capabilities(ctx)
	capabilities.SupportsResourceClaims = true
	capabilities.SupportsTransactions = false
	return capabilities, err
}

func TestCreateTask_RejectsInvalidResourceClaimDeclarations(t *testing.T) {
	ctx := context.Background()
	runner := newTestRunner(t)
	run, root, err := runner.StartRun(ctx, api.StartRunCommand{RunID: "invalid-claims", RootTaskID: "invalid-claims-root"})
	if err != nil {
		t.Fatal(err)
	}

	for name, claims := range map[string][]api.ResourceClaimSpec{
		"preassigned ID": {{ID: "claim", Key: "repo", Mode: api.ResourceClaimExclusive}},
		"missing key":    {{Mode: api.ResourceClaimShared}},
		"invalid mode":   {{Key: "repo", Mode: "write"}},
		"duplicate key":  {{Key: "repo", Mode: api.ResourceClaimShared}, {Key: "repo", Mode: api.ResourceClaimExclusive}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, createErr := runner.CreateTask(ctx, api.CreateTaskCommand{
				RunID: run.ID, ParentTaskID: root.ID, OwnerAgentID: "agent", ResourceClaims: claims,
			}); createErr == nil {
				t.Fatal("CreateTask() error = nil")
			}
		})
	}
}

func TestAcquireTaskExecutionWithClaims_FailsClosedWhenStorageLacksCapability(t *testing.T) {
	ctx := context.Background()
	provider := &claimsUnsupportedProvider{StoreProvider: adapter.StoreProviderFromCore(memory.NewProvider())}
	runner := NewDevelopment(api.Config{StoreProvider: provider})
	task, envelope := createClaimedExecutionTask(t, runner, "claims-unsupported", "agent")
	_, err := runner.AcquireTaskExecutionWithClaims(ctx, api.AcquireTaskExecutionCommand{
		RunID: task.RunID, TaskID: task.ID, EnvelopeID: envelope.ID,
		HolderType: api.HolderAgent, HolderID: "agent", TTL: time.Minute,
	})
	if !errors.Is(err, api.ErrInvalidConfiguration) {
		t.Fatalf("AcquireTaskExecutionWithClaims() error = %v, want ErrInvalidConfiguration", err)
	}
	loaded, loadErr := runner.Task(ctx, task.RunID, task.ID)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	active, activeErr := runner.ActiveLeaseCountContext(ctx, task.RunID, task.ID)
	if activeErr != nil {
		t.Fatalf("ActiveLeaseCountContext() error = %v", activeErr)
	}
	if loaded.Status != api.TaskStatusDispatched || active != 0 {
		t.Fatalf("unsupported store partially acquired execution: task=%#v active=%d", loaded, active)
	}
}

type claimsUnsupportedProvider struct {
	api.StoreProvider
}

func (p *claimsUnsupportedProvider) Begin(ctx context.Context) (api.UnitOfWork, error) {
	return p.StoreProvider.Begin(ctx)
}

func (p *claimsUnsupportedProvider) Capabilities(ctx context.Context) (api.StoreCapabilities, error) {
	capabilities, err := p.StoreProvider.(api.CapabilityReporter).Capabilities(ctx)
	capabilities.SupportsResourceClaims = false
	return capabilities, err
}

func createClaimedExecutionTask(t *testing.T, runner *Runner, runID, agentID string) (api.Task, api.TaskEnvelope) {
	t.Helper()
	ctx := context.Background()
	run, root, err := runner.StartRun(ctx, api.StartRunCommand{RunID: runID, RootTaskID: runID + "-root", Request: "claim resource"})
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []api.RunStatus{
		api.RunStatusPlanning, api.RunStatusValidating, api.RunStatusRouting,
		api.RunStatusDispatching, api.RunStatusRunning,
	} {
		if err := runner.TransitionRun(ctx, api.TransitionRunCommand{RunID: run.ID, To: status}); err != nil {
			t.Fatalf("TransitionRun(%s): %v", status, err)
		}
	}
	task, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID: run.ID, TaskID: runID + "-task", ParentTaskID: root.ID,
		OwnerAgentID:   agentID,
		ResourceClaims: []api.ResourceClaimSpec{{Key: "repo:shared", Mode: api.ResourceClaimExclusive}},
	})
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{RunID: run.ID, TaskID: task.ID, TargetAgentID: agentID})
	if err != nil {
		t.Fatal(err)
	}
	return task, envelope
}
