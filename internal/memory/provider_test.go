package memory

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Viking602/venat/api"
)

func TestMemoryUnitOfWorkRollbackRunStore(t *testing.T) {
	ctx := context.Background()
	provider := NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if err := uow.Runs().SaveRun(ctx, api.Run{ID: "run-1", Status: api.RunStatusCreated}); err != nil {
		t.Fatalf("SaveRun() error = %v", err)
	}
	if err := uow.Rollback(ctx); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	reader, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() reader error = %v", err)
	}
	defer func() { _ = reader.Rollback(ctx) }()
	if _, err := reader.Runs().LoadRun(ctx, "run-1"); !errors.Is(err, api.ErrNotFound) {
		t.Fatalf("LoadRun() after rollback error = %v, want ErrNotFound", err)
	}
}

func TestMemoryUnitOfWorkCommitRunStore(t *testing.T) {
	ctx := context.Background()
	provider := NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if err := uow.Runs().SaveRun(ctx, api.Run{ID: "run-1", Status: api.RunStatusCreated}); err != nil {
		t.Fatalf("SaveRun() error = %v", err)
	}
	if err := uow.Commit(ctx); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	reader, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() reader error = %v", err)
	}
	defer func() { _ = reader.Rollback(ctx) }()
	if _, err := reader.Runs().LoadRun(ctx, "run-1"); err != nil {
		t.Fatalf("LoadRun() after commit error = %v", err)
	}
}

func TestMemoryUnitOfWorkRollbackLeaseStore(t *testing.T) {
	ctx := context.Background()
	provider := NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	lease := api.TaskExecutionLease{ID: "lease-1", RunID: "run-1", TaskID: "task-1", Status: api.LeaseStatusActive}
	if err := uow.Leases().SaveLease(ctx, lease); err != nil {
		t.Fatalf("SaveLease() error = %v", err)
	}
	if err := uow.Rollback(ctx); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	reader, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() reader error = %v", err)
	}
	defer func() { _ = reader.Rollback(ctx) }()
	if _, err := reader.Leases().LoadLease(ctx, "lease-1"); !errors.Is(err, api.ErrNotFound) {
		t.Fatalf("LoadLease() after rollback error = %v, want ErrNotFound", err)
	}
}

func TestBlackboardSubscriberNotNotifiedOnRollback(t *testing.T) {
	ctx := context.Background()
	provider := NewProvider()
	ch, cancel, err := provider.Subscribe(ctx, "run-1", api.BlackboardSelector{})
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer func() { _ = cancel() }()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if err := uow.Blackboard().WriteItem(ctx, api.BlackboardItem{RunID: "run-1", Source: api.SourceIdentity{Type: api.SourceSystem, ID: "test"}}); err != nil {
		t.Fatalf("WriteItem() error = %v", err)
	}
	if err := uow.Rollback(ctx); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	select {
	case item := <-ch:
		t.Fatalf("unexpected notification after rollback: %#v", item)
	case <-time.After(30 * time.Millisecond):
	}
}

func TestBlackboardSubscriberNotifiedAfterCommit(t *testing.T) {
	ctx := context.Background()
	provider := NewProvider()
	ch, cancel, err := provider.Subscribe(ctx, "run-1", api.BlackboardSelector{})
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer func() { _ = cancel() }()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if err := uow.Blackboard().WriteItem(ctx, api.BlackboardItem{ID: "bb-1", RunID: "run-1", Source: api.SourceIdentity{Type: api.SourceSystem, ID: "test"}}); err != nil {
		t.Fatalf("WriteItem() error = %v", err)
	}
	if err := uow.Commit(ctx); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	select {
	case item := <-ch:
		if item.ID != "bb-1" {
			t.Fatalf("notification item ID = %q", item.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for notification")
	}
}

func TestMemoryProviderBeginRespectsContextWhileWaitingForTransaction(t *testing.T) {
	ctx := context.Background()
	provider := NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	waitCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	_, err = provider.Begin(waitCtx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Begin(waitCtx) error = %v, want DeadlineExceeded", err)
	}
}

func TestMemoryProviderCommittedReadDoesNotWaitForActiveWriteTransaction(t *testing.T) {
	ctx := context.Background()
	provider := NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	done := make(chan struct{})
	go func() {
		_ = provider.CommittedSnapshot()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("CommittedSnapshot blocked behind active write transaction")
	}
}

func TestMemoryProviderBeginReturnsUnifiedUnitOfWork(t *testing.T) {
	ctx := context.Background()
	provider := NewProvider()

	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}

	run := api.Run{ID: "run-unified", Status: api.RunStatusCreated}
	task := api.Task{ID: "task-unified", RunID: run.ID, Status: api.TaskStatusCreated, Version: 1}
	lease := api.TaskExecutionLease{ID: "lease-unified", RunID: run.ID, TaskID: task.ID, Status: api.LeaseStatusActive}
	approval := api.ApprovalRequest{ApprovalID: "approval-unified", RunID: run.ID, TaskID: task.ID, Status: "pending"}
	token := api.ResumeToken{TokenID: "token-unified", RunID: run.ID, TaskID: task.ID, ApprovalID: approval.ApprovalID}
	attempt := api.ActionAttempt{AttemptID: "attempt-unified", RunID: run.ID, TaskID: task.ID, Status: api.ActionAttemptRunning}

	if err := uow.Runs().SaveRun(ctx, run); err != nil {
		t.Fatalf("SaveRun() error = %v", err)
	}
	if err := uow.Tasks().SaveTask(ctx, task); err != nil {
		t.Fatalf("SaveTask() error = %v", err)
	}
	if err := uow.Events().AppendEvent(ctx, api.Event{RunID: run.ID, TaskID: task.ID, Type: api.EventTaskCreated}); err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}
	if err := uow.Blackboard().WriteItem(ctx, api.BlackboardItem{ID: "bb-unified", RunID: run.ID, TaskID: task.ID, Source: api.SourceIdentity{Type: api.SourceSystem, ID: "test"}}); err != nil {
		t.Fatalf("WriteItem() error = %v", err)
	}
	if err := uow.MailboxOutbox().QueueEnvelope(ctx, api.TaskEnvelope{ID: "env-unified", RunID: run.ID, TaskID: task.ID}); err != nil {
		t.Fatalf("QueueEnvelope() error = %v", err)
	}
	if err := uow.UserMessages().QueueMessage(ctx, api.UserMessage{ID: "msg-unified", RunID: run.ID, TaskID: task.ID}); err != nil {
		t.Fatalf("QueueMessage() error = %v", err)
	}
	if err := uow.Trace().SaveTraceSpan(ctx, api.TraceSpan{ID: "span-unified", RunID: run.ID, TaskID: task.ID, Name: "unified", Status: api.TraceSpanStarted}); err != nil {
		t.Fatalf("SaveTraceSpan() error = %v", err)
	}
	if err := uow.Leases().SaveLease(ctx, lease); err != nil {
		t.Fatalf("SaveLease() error = %v", err)
	}
	if err := uow.Approvals().SaveApproval(ctx, approval); err != nil {
		t.Fatalf("SaveApproval() error = %v", err)
	}
	if err := uow.ResumeTokens().SaveResumeToken(ctx, token); err != nil {
		t.Fatalf("SaveResumeToken() error = %v", err)
	}
	if err := uow.ActionAttempts().SaveActionAttempt(ctx, attempt); err != nil {
		t.Fatalf("SaveActionAttempt() error = %v", err)
	}
	if err := uow.Commit(ctx); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	reader, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() reader error = %v", err)
	}
	defer func() { _ = reader.Rollback(ctx) }()

	if _, err := reader.Runs().LoadRun(ctx, run.ID); err != nil {
		t.Fatalf("LoadRun() error = %v", err)
	}
	if _, err := reader.Tasks().LoadTask(ctx, run.ID, task.ID); err != nil {
		t.Fatalf("LoadTask() error = %v", err)
	}
	if events, err := reader.Events().ListEvents(ctx, run.ID); err != nil || len(events) != 1 {
		t.Fatalf("ListEvents() = %#v, %v", events, err)
	}
	if items, err := reader.Blackboard().SelectItems(ctx, run.ID, api.BlackboardSelector{}); err != nil || len(items) != 1 {
		t.Fatalf("SelectItems() = %#v, %v", items, err)
	}
	if _, err := reader.MailboxOutbox().LoadEnvelope(ctx, "env-unified"); err != nil {
		t.Fatalf("LoadEnvelope() error = %v", err)
	}
	if _, err := reader.UserMessages().LoadMessage(ctx, run.ID, "msg-unified"); err != nil {
		t.Fatalf("LoadMessage() error = %v", err)
	}
	if spans, err := reader.Trace().ListTraceSpans(ctx, run.ID); err != nil || len(spans) != 1 {
		t.Fatalf("ListTraceSpans() = %#v, %v", spans, err)
	}
	if _, err := reader.Leases().LoadLease(ctx, lease.ID); err != nil {
		t.Fatalf("LoadLease() error = %v", err)
	}
	if _, err := reader.Approvals().LoadApproval(ctx, approval.ApprovalID); err != nil {
		t.Fatalf("LoadApproval() error = %v", err)
	}
	if _, err := reader.ResumeTokens().LoadResumeToken(ctx, token.TokenID); err != nil {
		t.Fatalf("LoadResumeToken() error = %v", err)
	}
	if _, err := reader.ActionAttempts().LoadActionAttempt(ctx, attempt.AttemptID); err != nil {
		t.Fatalf("LoadActionAttempt() error = %v", err)
	}
}

func TestMemoryProvider_CapabilityReporter(t *testing.T) {
	ctx := context.Background()
	provider := NewProvider()
	caps, err := provider.Capabilities(ctx)
	if err != nil {
		t.Fatalf("Capabilities() error = %v", err)
	}
	if !caps.SupportsTransactions {
		t.Fatalf("expected SupportsTransactions=true")
	}
	if !caps.SupportsBlackboardSubscribe {
		t.Fatalf("expected SupportsBlackboardSubscribe=true")
	}
	if !caps.SupportsListPending {
		t.Fatalf("expected SupportsListPending=true")
	}
	if caps.SupportsConcurrentWriters {
		t.Fatalf("expected SupportsConcurrentWriters=false (single-writer via gate)")
	}
}

func TestMemoryProvider_Close(t *testing.T) {
	provider := NewProvider()
	if err := provider.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestMemoryLeaseStore_AcquireWithExpectedVersion(t *testing.T) {
	ctx := context.Background()
	provider := NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()

	cas, ok := uow.Leases().(interface {
		AcquireWithExpectedVersion(context.Context, api.TaskExecutionLease, uint64) (bool, error)
	})
	if !ok {
		t.Fatalf("leaseStore does not satisfy LeaseCAS")
	}

	lease := api.TaskExecutionLease{
		ID:         "lease-cas-1",
		RunID:      "run-1",
		TaskID:     "task-1",
		HolderID:   "worker-A",
		HolderType: api.HolderAgent,
		Status:     api.LeaseStatusActive,
		Expiry:     time.Now().Add(time.Minute),
	}
	acquired, err := cas.AcquireWithExpectedVersion(ctx, lease, 0)
	if err != nil {
		t.Fatalf("Acquire(version=0) error = %v", err)
	}
	if !acquired {
		t.Fatalf("expected first Acquire to succeed")
	}

	stale, err := cas.AcquireWithExpectedVersion(ctx, lease, 0)
	if err != nil {
		t.Fatalf("Acquire(stale) error = %v", err)
	}
	if stale {
		t.Fatalf("expected stale Acquire to return false")
	}

	active, err := cas.AcquireWithExpectedVersion(ctx, lease, 1)
	if err != nil {
		t.Fatalf("Acquire(active version=1) error = %v", err)
	}
	if active {
		t.Fatalf("expected an unexpired active lease to reject takeover")
	}

	loaded, err := uow.Leases().LoadLease(ctx, lease.ID)
	if err != nil {
		t.Fatalf("LoadLease() error = %v", err)
	}
	loaded.Status = api.LeaseStatusReleased
	if err := uow.Leases().SaveLease(ctx, loaded); err != nil {
		t.Fatalf("SaveLease(released) error = %v", err)
	}
	replacement := lease
	replacement.ID = "lease-cas-2"
	replacement.HolderID = "worker-B"
	winning, err := cas.AcquireWithExpectedVersion(ctx, replacement, 2)
	if err != nil {
		t.Fatalf("Acquire(replacement version=2) error = %v", err)
	}
	if !winning {
		t.Fatalf("expected released lease takeover to succeed")
	}
}

func TestMemoryLeaseStore_ExtendLease(t *testing.T) {
	ctx := context.Background()
	provider := NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()

	cas := uow.Leases().(interface {
		AcquireWithExpectedVersion(context.Context, api.TaskExecutionLease, uint64) (bool, error)
		ExtendLease(context.Context, string, string, time.Time) (bool, error)
	})

	original := time.Now().Add(30 * time.Second)
	lease := api.TaskExecutionLease{
		ID:         "lease-ext-1",
		RunID:      "run-1",
		TaskID:     "task-1",
		HolderID:   "worker-A",
		HolderType: api.HolderAgent,
		Status:     api.LeaseStatusActive,
		Expiry:     original,
	}
	if _, err := cas.AcquireWithExpectedVersion(ctx, lease, 0); err != nil {
		t.Fatalf("Acquire error = %v", err)
	}

	newExpiry := original.Add(time.Minute)
	extended, err := cas.ExtendLease(ctx, lease.ID, "worker-A", newExpiry)
	if err != nil {
		t.Fatalf("ExtendLease(self) error = %v", err)
	}
	if !extended {
		t.Fatalf("expected ExtendLease(self) to succeed")
	}
	loaded, err := uow.Leases().LoadLease(ctx, lease.ID)
	if err != nil {
		t.Fatalf("LoadLease() error = %v", err)
	}
	if loaded.ExpiresAt.IsZero() || loaded.Expiry.IsZero() || !loaded.ExpiresAt.Equal(loaded.Expiry) || !loaded.ExpiresAt.Equal(newExpiry) {
		t.Fatalf("expected synchronized expiry fields after ExtendLease, got %+v", loaded)
	}

	rotated, err := cas.ExtendLease(ctx, lease.ID, "worker-B", newExpiry.Add(time.Minute))
	if err != nil {
		t.Fatalf("ExtendLease(other) error = %v", err)
	}
	if rotated {
		t.Fatalf("expected ExtendLease(other) to return false")
	}

	missing, err := cas.ExtendLease(ctx, "no-such-lease", "worker-A", newExpiry)
	if err != nil {
		t.Fatalf("ExtendLease(missing) error = %v", err)
	}
	if missing {
		t.Fatalf("expected ExtendLease(missing) to return false")
	}
}

func TestStateClone_NestedMutationDoesNotLeak(t *testing.T) {
	ctx := context.Background()
	provider := NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if err := uow.Runs().SaveRun(ctx, api.Run{
		ID:       "run-1",
		Status:   api.RunStatusCreated,
		Metadata: map[string]string{"k": "v"},
	}); err != nil {
		t.Fatalf("SaveRun() error = %v", err)
	}
	if err := uow.Events().AppendEvent(ctx, api.Event{
		RunID:   "run-1",
		Type:    api.EventRunStarted,
		Payload: map[string]any{"step": "start"},
	}); err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}
	if err := uow.Tasks().SaveTask(ctx, api.Task{
		ID:            "task-1",
		RunID:         "run-1",
		Status:        api.TaskStatusCreated,
		OwnerHistory:  []string{"agent-a"},
		ReadSelectors: []api.BlackboardSelector{{Keys: []string{"task-key"}}},
		Budget:        &api.TaskBudget{MaxTokens: 10},
		Result:        &api.TypedReport{Status: api.ReportStatusSuccess, Structured: map[string]any{"ok": true}},
	}); err != nil {
		t.Fatalf("SaveTask() error = %v", err)
	}
	if err := uow.MailboxOutbox().QueueEnvelope(ctx, api.TaskEnvelope{
		ID:            "env-1",
		RunID:         "run-1",
		TaskID:        "task-1",
		Status:        "pending",
		Payload:       map[string]any{"n": 1},
		ReadSelectors: []api.BlackboardSelector{{Keys: []string{"envelope-key"}}},
	}); err != nil {
		t.Fatalf("QueueEnvelope() error = %v", err)
	}
	if err := uow.Commit(ctx); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	writer, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin(writer) error = %v", err)
	}
	run, err := writer.Runs().LoadRun(ctx, "run-1")
	if err != nil {
		t.Fatalf("LoadRun() error = %v", err)
	}
	run.Metadata["k"] = "mutated"
	events, err := writer.Events().ListEvents(ctx, "run-1")
	if err != nil || len(events) != 1 {
		t.Fatalf("ListEvents() = %#v err=%v", events, err)
	}
	events[0].Payload["step"] = "mutated"
	task, err := writer.Tasks().LoadTask(ctx, "run-1", "task-1")
	if err != nil {
		t.Fatalf("LoadTask() error = %v", err)
	}
	task.OwnerHistory[0] = "mutated"
	task.Budget.MaxTokens = 99
	task.Result.Structured["ok"] = false
	task.ReadSelectors[0].Keys[0] = "mutated"
	envelope, err := writer.MailboxOutbox().LoadEnvelope(ctx, "env-1")
	if err != nil {
		t.Fatalf("LoadEnvelope() error = %v", err)
	}
	envelope.Payload["n"] = 99
	envelope.ReadSelectors[0].Keys[0] = "mutated"
	if err := writer.Rollback(ctx); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}

	reader, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin(reader) error = %v", err)
	}
	defer func() { _ = reader.Rollback(ctx) }()
	committed, err := reader.Runs().LoadRun(ctx, "run-1")
	if err != nil {
		t.Fatalf("LoadRun(committed) error = %v", err)
	}
	if committed.Metadata["k"] != "v" {
		t.Fatalf("committed run metadata leaked mutation: %#v", committed.Metadata)
	}
	committedEvents, err := reader.Events().ListEvents(ctx, "run-1")
	if err != nil || committedEvents[0].Payload["step"] != "start" {
		t.Fatalf("committed event payload leaked mutation: %#v", committedEvents)
	}
	committedTask, err := reader.Tasks().LoadTask(ctx, "run-1", "task-1")
	if err != nil {
		t.Fatalf("LoadTask(committed) error = %v", err)
	}
	if committedTask.OwnerHistory[0] != "agent-a" || committedTask.Budget.MaxTokens != 10 ||
		committedTask.Result.Structured["ok"] != true || committedTask.ReadSelectors[0].Keys[0] != "task-key" {
		t.Fatalf("committed task leaked mutation: %#v", committedTask)
	}
	committedEnv, err := reader.MailboxOutbox().LoadEnvelope(ctx, "env-1")
	if err != nil {
		t.Fatalf("LoadEnvelope(committed) error = %v", err)
	}
	if committedEnv.Payload["n"] != 1 || committedEnv.ReadSelectors[0].Keys[0] != "envelope-key" {
		t.Fatalf("committed envelope leaked mutation: %#v", committedEnv)
	}
}

func TestUnitOfWorkDoubleRollbackIsIdempotent(t *testing.T) {
	ctx := context.Background()
	provider := NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if err := uow.Rollback(ctx); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if err := uow.Rollback(ctx); err != nil {
		t.Fatalf("second Rollback() error = %v", err)
	}
	next, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() after double rollback error = %v", err)
	}
	if err := next.Rollback(ctx); err != nil {
		t.Fatalf("cleanup Rollback() error = %v", err)
	}
}

func TestBeginReadDoesNotTakeWriteGate(t *testing.T) {
	ctx := context.Background()
	provider := NewProvider()
	writer, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	reader, err := provider.BeginRead(ctx)
	if err != nil {
		t.Fatalf("BeginRead() while writer open error = %v", err)
	}
	if err := reader.Commit(ctx); !errors.Is(err, api.ErrInvalidCommand) {
		t.Fatalf("read-only Commit() error = %v, want ErrInvalidCommand", err)
	}
	if err := reader.Rollback(ctx); err != nil {
		t.Fatalf("read-only Rollback() error = %v", err)
	}
	if err := writer.Rollback(ctx); err != nil {
		t.Fatalf("writer Rollback() error = %v", err)
	}
}

func TestListRunsFiltersAgentMetadata(t *testing.T) {
	ctx := context.Background()
	provider := NewProvider()
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if err := uow.Runs().SaveRun(ctx, api.Run{
		ID: "run-a", Status: api.RunStatusCreated,
		Metadata: map[string]string{"agentId": "agent-1", "agentVersion": "v1"},
	}); err != nil {
		t.Fatalf("SaveRun(a) error = %v", err)
	}
	if err := uow.Runs().SaveRun(ctx, api.Run{
		ID: "run-b", Status: api.RunStatusCreated,
		Metadata: map[string]string{"agentId": "agent-2", "agentVersion": "v2"},
	}); err != nil {
		t.Fatalf("SaveRun(b) error = %v", err)
	}
	if err := uow.Commit(ctx); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	reader, err := provider.BeginRead(ctx)
	if err != nil {
		t.Fatalf("BeginRead() error = %v", err)
	}
	defer func() { _ = reader.Rollback(ctx) }()
	got, err := reader.Runs().ListRuns(ctx, api.RunSelector{AgentID: "agent-1", AgentVersion: "v1"})
	if err != nil {
		t.Fatalf("ListRuns() error = %v", err)
	}
	if len(got) != 1 || got[0].ID != "run-a" {
		t.Fatalf("ListRuns() = %#v, want run-a", got)
	}
}

func TestProviderEnforcesEventLimit(t *testing.T) {
	ctx := context.Background()
	provider := NewProviderWithLimits(Limits{MaxEventsPerRun: 1})
	uow, err := provider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if err := uow.Events().AppendEvent(ctx, api.Event{RunID: "run-1", Type: api.EventRunStarted}); err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}
	if err := uow.Events().AppendEvent(ctx, api.Event{RunID: "run-1", Type: api.EventRunStatusChanged}); !errors.Is(err, api.ErrInvalidCommand) {
		t.Fatalf("second AppendEvent() error = %v, want limit", err)
	}
	_ = uow.Rollback(ctx)
}

func TestSubscribeRespectsContextAndCountsDrops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	provider := NewProvider()
	ch, stop, err := provider.Subscribe(ctx, "run-1", api.BlackboardSelector{})
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	cancel()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected subscription channel to close after cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("subscription did not close after cancel")
	}
	if err := stop(); err != nil {
		t.Fatalf("stop() after cancel error = %v", err)
	}

	live, liveStop, err := provider.Subscribe(context.Background(), "run-1", api.BlackboardSelector{})
	if err != nil {
		t.Fatalf("Subscribe(live) error = %v", err)
	}
	defer func() { _ = liveStop() }()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 64; j++ {
				provider.Notify([]api.BlackboardItem{{
					ID:    "item",
					RunID: "run-1",
					Type:  api.BlackboardItemEvidence,
				}})
			}
		}()
	}
	wg.Wait()
	if provider.DroppedCount() == 0 {
		t.Fatal("expected overflowed subscriber to increment DroppedCount")
	}
	// Drain so the test does not leak a blocked goroutine.
	for {
		select {
		case <-live:
		default:
			return
		}
	}
}
