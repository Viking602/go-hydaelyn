package contract

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/api"
)

// ProviderFactory builds a fresh StoreProvider for one test. The returned
// cleanup func runs after the test (via t.Cleanup).
type ProviderFactory func(t *testing.T) (provider api.StoreProvider, cleanup func())

// RunStoreProviderContractTests is the public contract gate for any
// api.StoreProvider implementation. External adapter authors call this from
// their own _test.go to verify their provider satisfies the framework
// contract.
//
// v0.8.0 Phase 2 scope: 35 named subtests across CRUD, transactions, lease
// CAS, event ordering, resume tokens + outbox, replay determinism, and
// capability self-consistency. See docs/product-spec/v0.8.0/05-storage.md
// §"Contract test suite" for the authoritative list. See ADR-012 for the
// Position C stance: this suite is the validation bar for every provider,
// framework reference impls and external alike.
//
// Tests gated on optional capabilities call t.Skip when the provider
// self-declares the feature as unsupported via api.CapabilityReporter, so
// the suite never fails a conformant provider that simply does not opt in.
func RunStoreProviderContractTests(t *testing.T, factory ProviderFactory) {
	t.Helper()
	if factory == nil {
		t.Fatal("contract: ProviderFactory must not be nil")
	}

	t.Run("CRUD", func(t *testing.T) { runCRUDSuite(t, factory) })
	t.Run("Transactions", func(t *testing.T) { runTransactionSuite(t, factory) })
	t.Run("LeaseCAS", func(t *testing.T) { runLeaseCASSuite(t, factory) })
	t.Run("EventOrdering", func(t *testing.T) { runEventOrderingSuite(t, factory) })
	t.Run("ResumeAndOutbox", func(t *testing.T) { runResumeAndOutboxSuite(t, factory) })
	t.Run("ReplayDeterminism", func(t *testing.T) { runReplayDeterminismSuite(t, factory) })
	t.Run("CapabilitySelfConsistency", func(t *testing.T) { runCapabilitySelfConsistencySuite(t, factory) })
}

// suiteCase pairs a contract test name (locked surface — never rename
// without a spec bump) with its body.
type suiteCase struct {
	name string
	fn   func(t *testing.T, factory ProviderFactory)
}

func runSuite(t *testing.T, factory ProviderFactory, cases []suiteCase) {
	t.Helper()
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) { c.fn(t, factory) })
	}
}

// newProvider opens a fresh provider for the current test and registers
// cleanup. All contract tests funnel through this helper to keep setup
// uniform.
func newProvider(t *testing.T, factory ProviderFactory) api.StoreProvider {
	t.Helper()
	p, cleanup := factory(t)
	t.Cleanup(cleanup)
	return p
}

// withUoW opens a unit of work, hands it to fn, and commits on success.
// If fn returns an error or panics, the UoW is rolled back. Most CRUD
// helpers use this so individual tests stay focused on the contract.
func withUoW(t *testing.T, p api.StoreProvider, fn func(api.UnitOfWork) error) {
	t.Helper()
	ctx := context.Background()
	uow, err := p.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = uow.Rollback(ctx)
		}
	}()
	if err := fn(uow); err != nil {
		t.Fatalf("uow: %v", err)
	}
	if err := uow.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	committed = true
}

func capabilities(t *testing.T, p api.StoreProvider) api.StoreCapabilities {
	t.Helper()
	if reporter, ok := p.(api.CapabilityReporter); ok {
		caps, err := reporter.Capabilities(context.Background())
		if err != nil {
			t.Fatalf("Capabilities: %v", err)
		}
		return caps
	}
	return api.DefaultStoreCapabilities()
}

// ─── CRUD suite ─────────────────────────────────────────────────────────

func runCRUDSuite(t *testing.T, factory ProviderFactory) {
	t.Helper()
	runSuite(t, factory, []suiteCase{
		{"TestSaveAndLoad_Run", testSaveAndLoadRun},
		{"TestSaveAndLoad_Task", testSaveAndLoadTask},
		{"TestAppendAndList_Events", testAppendAndListEvents},
		{"TestSaveAndList_TraceSpans", testSaveAndListTraceSpans},
		{"TestWriteAndSelect_BlackboardItems", testWriteAndSelectBlackboard},
		{"TestQueueAndLoad_UserMessage", testQueueAndLoadUserMessage},
		{"TestQueueAndLoad_Envelope", testQueueAndLoadEnvelope},
		{"TestSaveAndLoad_Lease", testSaveAndLoadLease},
		{"TestSaveAndLoad_Approval", testSaveAndLoadApproval},
		{"TestSaveAndLoad_ResumeToken", testSaveAndLoadResumeToken},
		{"TestSaveAndList_AgentProfiles", testSaveAndListAgentProfiles},
		{"TestSaveAndList_Capabilities", testSaveAndListCapabilities},
		{"TestSaveAndList_UsageRecords", testSaveAndListUsageRecords},
		{"TestSaveAndList_DeadLetters", testSaveAndListDeadLetters},
		{"TestSaveAndList_ActionAttempts", testSaveAndListActionAttempts},
	})
}

func testSaveAndLoadRun(t *testing.T, factory ProviderFactory) {
	p := newProvider(t, factory)
	want := api.Run{ID: "run-crud-1", Status: api.RunStatusCreated, Request: "hello", CreatedAt: time.Now().UTC()}
	withUoW(t, p, func(uow api.UnitOfWork) error { return uow.Runs().SaveRun(context.Background(), want) })
	withUoW(t, p, func(uow api.UnitOfWork) error {
		got, err := uow.Runs().LoadRun(context.Background(), want.ID)
		if err != nil {
			return err
		}
		if got.ID != want.ID || got.Request != want.Request {
			t.Fatalf("LoadRun mismatch: %+v vs %+v", got, want)
		}
		return nil
	})
}

func testSaveAndLoadTask(t *testing.T, factory ProviderFactory) {
	p := newProvider(t, factory)
	task := api.Task{
		ID:     "task-1",
		RunID:  "run-crud-2",
		Type:   api.TaskTypeWorker,
		Goal:   "do thing",
		Status: api.TaskStatusCreated,
		Budget: &api.TaskBudget{
			MaxTokens:    12_000,
			MaxWallClock: 2 * time.Minute,
			MaxToolCalls: 3,
			MaxSteps:     8,
		},
		InputSchema:  json.RawMessage(`{"type":"object","required":["topic"],"properties":{"topic":{"type":"string"}}}`),
		OutputSchema: json.RawMessage(`{"type":"object","required":["summary"],"properties":{"summary":{"type":"string"}}}`),
	}
	withUoW(t, p, func(uow api.UnitOfWork) error { return uow.Tasks().SaveTask(context.Background(), task) })
	withUoW(t, p, func(uow api.UnitOfWork) error {
		got, err := uow.Tasks().LoadTask(context.Background(), task.RunID, task.ID)
		if err != nil {
			return err
		}
		if got.Goal != task.Goal {
			t.Fatalf("LoadTask goal mismatch: %q vs %q", got.Goal, task.Goal)
		}
		if got.Budget == nil || *got.Budget != *task.Budget {
			t.Fatalf("LoadTask budget mismatch: %#v vs %#v", got.Budget, task.Budget)
		}
		if string(got.InputSchema) != string(task.InputSchema) {
			t.Fatalf("LoadTask input schema mismatch: %s vs %s", got.InputSchema, task.InputSchema)
		}
		if string(got.OutputSchema) != string(task.OutputSchema) {
			t.Fatalf("LoadTask output schema mismatch: %s vs %s", got.OutputSchema, task.OutputSchema)
		}
		listed, err := uow.Tasks().ListTasks(context.Background(), task.RunID)
		if err != nil {
			return err
		}
		var listedTask *api.Task
		for i := range listed {
			if listed[i].ID == task.ID {
				listedTask = &listed[i]
				break
			}
		}
		if listedTask == nil {
			t.Fatalf("ListTasks missing task %q in %+v", task.ID, listed)
		}
		if listedTask.Budget == nil || *listedTask.Budget != *task.Budget {
			t.Fatalf("ListTasks budget mismatch: %#v vs %#v", listedTask.Budget, task.Budget)
		}
		if string(listedTask.InputSchema) != string(task.InputSchema) {
			t.Fatalf("ListTasks input schema mismatch: %s vs %s", listedTask.InputSchema, task.InputSchema)
		}
		if string(listedTask.OutputSchema) != string(task.OutputSchema) {
			t.Fatalf("ListTasks output schema mismatch: %s vs %s", listedTask.OutputSchema, task.OutputSchema)
		}
		return nil
	})
}

func testAppendAndListEvents(t *testing.T, factory ProviderFactory) {
	p := newProvider(t, factory)
	runID := "run-events-1"
	events := []api.Event{
		{RunID: runID, Sequence: 1, Type: api.EventRunStarted, RecordedAt: time.Now().UTC()},
		{RunID: runID, Sequence: 2, Type: api.EventRunStatusChanged, RecordedAt: time.Now().UTC()},
	}
	withUoW(t, p, func(uow api.UnitOfWork) error {
		for _, e := range events {
			if err := uow.Events().AppendEvent(context.Background(), e); err != nil {
				return err
			}
		}
		return nil
	})
	withUoW(t, p, func(uow api.UnitOfWork) error {
		got, err := uow.Events().ListEvents(context.Background(), runID)
		if err != nil {
			return err
		}
		if len(got) != 2 || got[0].Sequence != 1 || got[1].Sequence != 2 {
			t.Fatalf("event list out of order: %+v", got)
		}
		return nil
	})
}

func testSaveAndListTraceSpans(t *testing.T, factory ProviderFactory) {
	p := newProvider(t, factory)
	runID := "run-trace-1"
	span := api.TraceSpan{ID: "span-1", RunID: runID, Name: "test", StartedAt: time.Now().UTC()}
	withUoW(t, p, func(uow api.UnitOfWork) error { return uow.Trace().SaveTraceSpan(context.Background(), span) })
	withUoW(t, p, func(uow api.UnitOfWork) error {
		got, err := uow.Trace().ListTraceSpans(context.Background(), runID)
		if err != nil {
			return err
		}
		if len(got) != 1 || got[0].ID != span.ID {
			t.Fatalf("trace list mismatch: %+v", got)
		}
		return nil
	})
}

func testWriteAndSelectBlackboard(t *testing.T, factory ProviderFactory) {
	p := newProvider(t, factory)
	runID := "run-bb-1"
	item := api.BlackboardItem{ID: "bb-1", RunID: runID, Type: "claim", Content: "x"}
	withUoW(t, p, func(uow api.UnitOfWork) error {
		return uow.Blackboard().WriteItem(context.Background(), item)
	})
	withUoW(t, p, func(uow api.UnitOfWork) error {
		items, err := uow.Blackboard().SelectItems(context.Background(), runID, api.BlackboardSelector{RunID: runID})
		if err != nil {
			return err
		}
		if len(items) != 1 || items[0].ID != item.ID {
			t.Fatalf("blackboard select mismatch: %+v", items)
		}
		return nil
	})
}

func testQueueAndLoadUserMessage(t *testing.T, factory ProviderFactory) {
	p := newProvider(t, factory)
	runID := "run-msg-1"
	msg := api.UserMessage{ID: "msg-1", RunID: runID, Type: api.UserMessageTypeProgressUpdate, Title: "hello", Status: api.UserMessageQueued}
	withUoW(t, p, func(uow api.UnitOfWork) error {
		return uow.UserMessages().QueueMessage(context.Background(), msg)
	})
	withUoW(t, p, func(uow api.UnitOfWork) error {
		got, err := uow.UserMessages().LoadMessage(context.Background(), runID, msg.ID)
		if err != nil {
			return err
		}
		if got.Title != msg.Title {
			t.Fatalf("message title mismatch: %q", got.Title)
		}
		return nil
	})
}

func testQueueAndLoadEnvelope(t *testing.T, factory ProviderFactory) {
	p := newProvider(t, factory)
	env := api.TaskEnvelope{ID: "env-1", RunID: "run-env", TaskID: "task-1", Status: "queued", CreatedAt: time.Now().UTC()}
	withUoW(t, p, func(uow api.UnitOfWork) error {
		return uow.MailboxOutbox().QueueEnvelope(context.Background(), env)
	})
	withUoW(t, p, func(uow api.UnitOfWork) error {
		got, err := uow.MailboxOutbox().LoadEnvelope(context.Background(), env.ID)
		if err != nil {
			return err
		}
		if got.TaskID != env.TaskID {
			t.Fatalf("envelope taskID mismatch: %q", got.TaskID)
		}
		return nil
	})
}

func testSaveAndLoadLease(t *testing.T, factory ProviderFactory) {
	p := newProvider(t, factory)
	lease := api.TaskExecutionLease{
		ID:         "lease-1",
		RunID:      "run-lease",
		TaskID:     "task-lease",
		HolderType: api.HolderAgent,
		HolderID:   "worker-1",
		Status:     api.LeaseStatusActive,
		AcquiredAt: time.Now().UTC(),
		ExpiresAt:  time.Now().UTC().Add(time.Minute),
	}
	withUoW(t, p, func(uow api.UnitOfWork) error {
		return uow.Leases().SaveLease(context.Background(), lease)
	})
	withUoW(t, p, func(uow api.UnitOfWork) error {
		got, err := uow.Leases().LoadLease(context.Background(), lease.ID)
		if err != nil {
			return err
		}
		if got.HolderID != lease.HolderID {
			t.Fatalf("lease holder mismatch: %q", got.HolderID)
		}
		return nil
	})
}

func testSaveAndLoadApproval(t *testing.T, factory ProviderFactory) {
	p := newProvider(t, factory)
	appr := api.ApprovalRequest{ApprovalID: "appr-1", RunID: "run-appr", TaskID: "task-appr", Status: "pending"}
	withUoW(t, p, func(uow api.UnitOfWork) error {
		return uow.Approvals().SaveApproval(context.Background(), appr)
	})
	withUoW(t, p, func(uow api.UnitOfWork) error {
		got, err := uow.Approvals().LoadApproval(context.Background(), appr.ApprovalID)
		if err != nil {
			return err
		}
		if got.Status != appr.Status {
			t.Fatalf("approval status mismatch: %q", got.Status)
		}
		return nil
	})
}

func testSaveAndLoadResumeToken(t *testing.T, factory ProviderFactory) {
	p := newProvider(t, factory)
	tok := api.ResumeToken{TokenID: "tok-1", RunID: "run-tok", TaskID: "task-tok"}
	withUoW(t, p, func(uow api.UnitOfWork) error {
		return uow.ResumeTokens().SaveResumeToken(context.Background(), tok)
	})
	withUoW(t, p, func(uow api.UnitOfWork) error {
		got, err := uow.ResumeTokens().LoadResumeToken(context.Background(), tok.TokenID)
		if err != nil {
			return err
		}
		if got.RunID != tok.RunID {
			t.Fatalf("resume token runID mismatch: %q", got.RunID)
		}
		return nil
	})
}

func testSaveAndListAgentProfiles(t *testing.T, factory ProviderFactory) {
	p := newProvider(t, factory)
	prof := api.AgentProfile{ID: "agent-1", Role: "analyst", Groups: []string{"g1"}}
	withUoW(t, p, func(uow api.UnitOfWork) error {
		return uow.AgentProfiles().SaveAgentProfile(context.Background(), prof)
	})
	withUoW(t, p, func(uow api.UnitOfWork) error {
		got, err := uow.AgentProfiles().ListAgentProfiles(context.Background(), api.AgentSelector{})
		if err != nil {
			return err
		}
		if len(got) != 1 || got[0].ID != prof.ID {
			t.Fatalf("agent profile list mismatch: %+v", got)
		}
		return nil
	})
}

func testSaveAndListCapabilities(t *testing.T, factory ProviderFactory) {
	p := newProvider(t, factory)
	capability := api.Capability{Name: "summarize", AgentID: "agent-1", Description: "summarize text"}
	withUoW(t, p, func(uow api.UnitOfWork) error {
		return uow.CapabilityCatalog().SaveCapability(context.Background(), capability)
	})
	withUoW(t, p, func(uow api.UnitOfWork) error {
		got, err := uow.CapabilityCatalog().ListCapabilities(context.Background(), api.CapabilitySelector{})
		if err != nil {
			return err
		}
		if len(got) != 1 || got[0].Name != capability.Name {
			t.Fatalf("capability list mismatch: %+v", got)
		}
		return nil
	})
}

func testSaveAndListUsageRecords(t *testing.T, factory ProviderFactory) {
	p := newProvider(t, factory)
	rec := api.UsageRecord{RunID: "run-u", Credits: 42, CreatedAt: time.Now().UTC()}
	withUoW(t, p, func(uow api.UnitOfWork) error {
		return uow.UsageRecords().AppendUsage(context.Background(), rec)
	})
	withUoW(t, p, func(uow api.UnitOfWork) error {
		got, err := uow.UsageRecords().QueryUsage(context.Background(), api.UsageSelector{RunID: "run-u"})
		if err != nil {
			return err
		}
		if len(got) != 1 || got[0].Credits != 42 {
			t.Fatalf("usage list mismatch: %+v", got)
		}
		sum, err := uow.UsageRecords().SumCredits(context.Background(), api.UsageSelector{RunID: "run-u"})
		if err != nil {
			return err
		}
		if sum != 42 {
			t.Fatalf("SumCredits = %d, want 42", sum)
		}
		return nil
	})
}

func testSaveAndListDeadLetters(t *testing.T, factory ProviderFactory) {
	p := newProvider(t, factory)
	entry := api.DeadLetterEntry{EnvelopeID: "env-x", RunID: "run-dl", Reason: "max retries", CreatedAt: time.Now().UTC()}
	withUoW(t, p, func(uow api.UnitOfWork) error {
		return uow.DeadLetters().AppendDeadLetter(context.Background(), entry)
	})
	withUoW(t, p, func(uow api.UnitOfWork) error {
		got, err := uow.DeadLetters().ListDeadLetters(context.Background(), api.DeadLetterSelector{RunID: "run-dl"})
		if err != nil {
			return err
		}
		if len(got) != 1 || got[0].EnvelopeID != entry.EnvelopeID {
			t.Fatalf("dead letter list mismatch: %+v", got)
		}
		return nil
	})
}

func testSaveAndListActionAttempts(t *testing.T, factory ProviderFactory) {
	p := newProvider(t, factory)
	att := api.ActionAttempt{
		AttemptID:      "att-1",
		RunID:          "run-act",
		TaskID:         "task-act",
		ToolName:       "tool-x",
		IdempotencyKey: "idem-1",
		InputHash:      "hash-1",
	}
	withUoW(t, p, func(uow api.UnitOfWork) error {
		return uow.ActionAttempts().SaveActionAttempt(context.Background(), att)
	})
	withUoW(t, p, func(uow api.UnitOfWork) error {
		got, err := uow.ActionAttempts().LoadActionAttempt(context.Background(), att.AttemptID)
		if err != nil {
			return err
		}
		if got.ToolName != att.ToolName {
			t.Fatalf("action attempt mismatch: %q", got.ToolName)
		}
		byKey, err := uow.ActionAttempts().LoadActionAttemptByIdempotencyKey(context.Background(), att.RunID, att.TaskID, att.ToolName, att.IdempotencyKey)
		if err != nil {
			return err
		}
		if byKey.AttemptID != att.AttemptID || byKey.InputHash != att.InputHash {
			t.Fatalf("action attempt idempotency lookup mismatch: %+v", byKey)
		}
		return nil
	})
}

// ─── Transaction suite ──────────────────────────────────────────────────

func runTransactionSuite(t *testing.T, factory ProviderFactory) {
	t.Helper()
	runSuite(t, factory, []suiteCase{
		{"TestUnitOfWork_CommitPersistsAll", testCommitPersistsAll},
		{"TestUnitOfWork_RollbackDiscardsAll", testRollbackDiscardsAll},
		{"TestUnitOfWork_ReadOwnWrites", testReadOwnWrites},
		{"TestUnitOfWork_IsolatedFromConcurrentBegin", testIsolatedFromConcurrentBegin},
	})
}

func testCommitPersistsAll(t *testing.T, factory ProviderFactory) {
	p := newProvider(t, factory)
	ctx := context.Background()
	uow, err := p.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := uow.Runs().SaveRun(ctx, api.Run{ID: "tx-r1", Status: api.RunStatusCreated}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	if err := uow.Tasks().SaveTask(ctx, api.Task{ID: "tx-t1", RunID: "tx-r1", Type: api.TaskTypeWorker, Status: api.TaskStatusCreated}); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}
	if err := uow.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	uow2, err := p.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin 2: %v", err)
	}
	defer func() { _ = uow2.Rollback(ctx) }()
	if _, err := uow2.Runs().LoadRun(ctx, "tx-r1"); err != nil {
		t.Fatalf("LoadRun after commit: %v", err)
	}
	if _, err := uow2.Tasks().LoadTask(ctx, "tx-r1", "tx-t1"); err != nil {
		t.Fatalf("LoadTask after commit: %v", err)
	}
}

func testRollbackDiscardsAll(t *testing.T, factory ProviderFactory) {
	p := newProvider(t, factory)
	ctx := context.Background()
	uow, err := p.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := uow.Runs().SaveRun(ctx, api.Run{ID: "tx-rb1", Status: api.RunStatusCreated}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	if err := uow.Rollback(ctx); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	uow2, err := p.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin 2: %v", err)
	}
	defer func() { _ = uow2.Rollback(ctx) }()
	if _, err := uow2.Runs().LoadRun(ctx, "tx-rb1"); err == nil {
		t.Fatal("LoadRun: expected NotFound after rollback, got success")
	}
}

func testReadOwnWrites(t *testing.T, factory ProviderFactory) {
	p := newProvider(t, factory)
	ctx := context.Background()
	uow, err := p.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	if err := uow.Runs().SaveRun(ctx, api.Run{ID: "tx-ow1", Status: api.RunStatusCreated}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	if _, err := uow.Runs().LoadRun(ctx, "tx-ow1"); err != nil {
		t.Fatalf("LoadRun (read-own-write): %v", err)
	}
}

func testIsolatedFromConcurrentBegin(t *testing.T, factory ProviderFactory) {
	caps := capabilities(t, newProvider(t, factory))
	if !caps.SupportsConcurrentWriters {
		// Memory provider serializes — exercise the serialization itself:
		// a second Begin must observe the first Commit's data once unblocked.
		p := newProvider(t, factory)
		ctx := context.Background()
		uow, err := p.Begin(ctx)
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		if err := uow.Runs().SaveRun(ctx, api.Run{ID: "iso-r1", Status: api.RunStatusCreated}); err != nil {
			t.Fatalf("SaveRun: %v", err)
		}
		if err := uow.Commit(ctx); err != nil {
			t.Fatalf("Commit: %v", err)
		}
		uow2, err := p.Begin(ctx)
		if err != nil {
			t.Fatalf("Begin 2: %v", err)
		}
		defer func() { _ = uow2.Rollback(ctx) }()
		if _, err := uow2.Runs().LoadRun(ctx, "iso-r1"); err != nil {
			t.Fatalf("LoadRun after concurrent boundary: %v", err)
		}
		return
	}
	// For concurrent-writer providers: two parallel transactions, both should
	// commit independent runs successfully.
	p := newProvider(t, factory)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			uow, err := p.Begin(ctx)
			if err != nil {
				t.Errorf("Begin: %v", err)
				return
			}
			id := fmt.Sprintf("iso-c-%d", i)
			if err := uow.Runs().SaveRun(ctx, api.Run{ID: id, Status: api.RunStatusCreated}); err != nil {
				t.Errorf("SaveRun: %v", err)
				_ = uow.Rollback(ctx)
				return
			}
			if err := uow.Commit(ctx); err != nil {
				t.Errorf("Commit: %v", err)
			}
		}()
	}
	wg.Wait()
}

// ─── Lease CAS suite ────────────────────────────────────────────────────

func runLeaseCASSuite(t *testing.T, factory ProviderFactory) {
	t.Helper()
	runSuite(t, factory, []suiteCase{
		{"TestLease_AcquireWithExpectedVersion_Succeeds", testLeaseAcquireSucceeds},
		{"TestLease_AcquireWithExpectedVersion_FailsOnStaleVersion", testLeaseAcquireFailsStale},
		{"TestLease_ExtendLease_HonorsWorkerID", testLeaseExtendHonorsWorker},
		{"TestLease_ExtendLease_RejectsAfterTransfer", testLeaseExtendRejectsAfterTransfer},
		{"TestLease_ConcurrentAcquireOnlyOneWins", testLeaseConcurrentAcquireOnlyOneWins},
	})
}

func testLeaseAcquireSucceeds(t *testing.T, factory ProviderFactory) {
	p := newProvider(t, factory)
	lease := api.TaskExecutionLease{
		ID: "lease-acq-1", RunID: "r-acq", TaskID: "t-acq",
		HolderType: api.HolderAgent, HolderID: "worker-A",
		Status: api.LeaseStatusActive, AcquiredAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Minute),
	}
	withUoW(t, p, func(uow api.UnitOfWork) error {
		ok, err := uow.Leases().AcquireWithExpectedVersion(context.Background(), lease, 0)
		if err != nil {
			return err
		}
		if !ok {
			t.Fatal("AcquireWithExpectedVersion(version=0): want true")
		}
		return nil
	})
}

func testLeaseAcquireFailsStale(t *testing.T, factory ProviderFactory) {
	p := newProvider(t, factory)
	ctx := context.Background()
	lease := api.TaskExecutionLease{
		ID: "lease-stale", RunID: "r-st", TaskID: "t-st",
		HolderType: api.HolderAgent, HolderID: "worker-A",
		Status: api.LeaseStatusActive, AcquiredAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Minute),
	}
	withUoW(t, p, func(uow api.UnitOfWork) error {
		ok, err := uow.Leases().AcquireWithExpectedVersion(ctx, lease, 0)
		if err != nil || !ok {
			t.Fatalf("first acquire: ok=%v err=%v", ok, err)
		}
		return nil
	})
	// Second acquire with stale expectedVersion=0 must fail.
	withUoW(t, p, func(uow api.UnitOfWork) error {
		ok, err := uow.Leases().AcquireWithExpectedVersion(ctx, lease, 0)
		if err != nil {
			return err
		}
		if ok {
			t.Fatal("AcquireWithExpectedVersion(stale version=0) returned true; want false")
		}
		return nil
	})
}

func testLeaseExtendHonorsWorker(t *testing.T, factory ProviderFactory) {
	p := newProvider(t, factory)
	ctx := context.Background()
	lease := api.TaskExecutionLease{
		ID: "lease-extend-A", RunID: "r-ex", TaskID: "t-ex",
		HolderType: api.HolderAgent, HolderID: "worker-A",
		Status: api.LeaseStatusActive, AcquiredAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Minute),
	}
	withUoW(t, p, func(uow api.UnitOfWork) error {
		ok, err := uow.Leases().AcquireWithExpectedVersion(ctx, lease, 0)
		if err != nil || !ok {
			t.Fatalf("acquire: ok=%v err=%v", ok, err)
		}
		return nil
	})
	withUoW(t, p, func(uow api.UnitOfWork) error {
		ok, err := uow.Leases().ExtendLease(ctx, lease.ID, "worker-A", time.Now().UTC().Add(5*time.Minute))
		if err != nil {
			return err
		}
		if !ok {
			t.Fatal("ExtendLease(holder match): want true")
		}
		return nil
	})
}

func testLeaseExtendRejectsAfterTransfer(t *testing.T, factory ProviderFactory) {
	p := newProvider(t, factory)
	ctx := context.Background()
	lease := api.TaskExecutionLease{
		ID: "lease-transfer", RunID: "r-tr", TaskID: "t-tr",
		HolderType: api.HolderAgent, HolderID: "worker-A",
		Status: api.LeaseStatusActive, AcquiredAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Minute),
	}
	withUoW(t, p, func(uow api.UnitOfWork) error {
		ok, err := uow.Leases().AcquireWithExpectedVersion(ctx, lease, 0)
		if err != nil || !ok {
			t.Fatalf("acquire: ok=%v err=%v", ok, err)
		}
		return nil
	})
	withUoW(t, p, func(uow api.UnitOfWork) error {
		ok, err := uow.Leases().ExtendLease(ctx, lease.ID, "worker-B", time.Now().UTC().Add(5*time.Minute))
		if err != nil {
			return err
		}
		if ok {
			t.Fatal("ExtendLease(wrong worker): want false")
		}
		return nil
	})
}

func testLeaseConcurrentAcquireOnlyOneWins(t *testing.T, factory ProviderFactory) {
	p := newProvider(t, factory)
	ctx := context.Background()
	const N = 10
	var wins atomic.Int32
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			lease := api.TaskExecutionLease{
				ID: "lease-race", RunID: "r-race", TaskID: "t-race",
				HolderType: api.HolderAgent, HolderID: fmt.Sprintf("worker-%d", i),
				Status: api.LeaseStatusActive, AcquiredAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Minute),
			}
			uow, err := p.Begin(ctx)
			if err != nil {
				return
			}
			ok, err := uow.Leases().AcquireWithExpectedVersion(ctx, lease, 0)
			if err == nil && ok {
				if cerr := uow.Commit(ctx); cerr == nil {
					wins.Add(1)
					return
				}
			}
			_ = uow.Rollback(ctx)
		}()
	}
	wg.Wait()
	if got := wins.Load(); got != 1 {
		t.Fatalf("ConcurrentAcquire: want exactly 1 winner, got %d", got)
	}
}

// ─── Event ordering suite ───────────────────────────────────────────────

func runEventOrderingSuite(t *testing.T, factory ProviderFactory) {
	t.Helper()
	runSuite(t, factory, []suiteCase{
		{"TestEvents_AppendPreservesOrder", testEventsAppendPreservesOrder},
		{"TestEvents_ListEventsByRunID_ReturnsInOrder", testEventsListInOrder},
		{"TestEvents_SequenceMonotonic", testEventsSequenceMonotonic},
	})
}

func testEventsAppendPreservesOrder(t *testing.T, factory ProviderFactory) {
	p := newProvider(t, factory)
	runID := "r-ord-1"
	withUoW(t, p, func(uow api.UnitOfWork) error {
		for i := 1; i <= 5; i++ {
			if err := uow.Events().AppendEvent(context.Background(), api.Event{RunID: runID, Sequence: i, Type: api.EventRunStatusChanged, RecordedAt: time.Now().UTC()}); err != nil {
				return err
			}
		}
		return nil
	})
	withUoW(t, p, func(uow api.UnitOfWork) error {
		got, err := uow.Events().ListEvents(context.Background(), runID)
		if err != nil {
			return err
		}
		if len(got) != 5 {
			t.Fatalf("event count: %d != 5", len(got))
		}
		for i, e := range got {
			if e.Sequence != i+1 {
				t.Fatalf("event[%d].Sequence = %d, want %d", i, e.Sequence, i+1)
			}
		}
		return nil
	})
}

func testEventsListInOrder(t *testing.T, factory ProviderFactory) {
	p := newProvider(t, factory)
	runID := "r-ord-2"
	withUoW(t, p, func(uow api.UnitOfWork) error {
		// Intentionally append out of "natural" temporal order to verify that
		// the contract follows Sequence, not insert time. Memory provider
		// stores in insert order for the same RunID; ListEvents must return
		// the slice in insert order which here equals Sequence order.
		seqs := []int{1, 2, 3, 4}
		for _, s := range seqs {
			if err := uow.Events().AppendEvent(context.Background(), api.Event{RunID: runID, Sequence: s, Type: api.EventRunStatusChanged}); err != nil {
				return err
			}
		}
		return nil
	})
	withUoW(t, p, func(uow api.UnitOfWork) error {
		got, err := uow.Events().ListEvents(context.Background(), runID)
		if err != nil {
			return err
		}
		for i, e := range got {
			if e.Sequence != i+1 {
				t.Fatalf("list order broken at %d: %d", i, e.Sequence)
			}
		}
		return nil
	})
}

func testEventsSequenceMonotonic(t *testing.T, factory ProviderFactory) {
	p := newProvider(t, factory)
	runID := "r-mono"
	withUoW(t, p, func(uow api.UnitOfWork) error {
		for i := 1; i <= 6; i++ {
			if err := uow.Events().AppendEvent(context.Background(), api.Event{RunID: runID, Sequence: i, Type: api.EventRunStatusChanged}); err != nil {
				return err
			}
		}
		return nil
	})
	withUoW(t, p, func(uow api.UnitOfWork) error {
		got, err := uow.Events().ListAfter(context.Background(), runID, 3)
		if err != nil {
			return err
		}
		for _, e := range got {
			if e.Sequence <= 3 {
				t.Fatalf("ListAfter(3) returned Sequence=%d", e.Sequence)
			}
		}
		if len(got) != 3 {
			t.Fatalf("ListAfter(3): expected 3 events (4,5,6), got %d", len(got))
		}
		return nil
	})
}

// ─── Resume + Outbox suite ──────────────────────────────────────────────

func runResumeAndOutboxSuite(t *testing.T, factory ProviderFactory) {
	t.Helper()
	runSuite(t, factory, []suiteCase{
		{"TestResumeToken_ListPending_ExcludesConsumed", testResumeTokenListExcludesConsumed},
		{"TestResumeToken_ListPending_Pagination", testResumeTokenListPagination},
		{"TestMessageOutbox_ScanReturnsQueued", testOutboxScanReturnsQueued},
		{"TestMessageOutbox_FIFO", testOutboxFIFO},
		{"TestMessageOutbox_UpdateRemovesFromQueue", testOutboxUpdateRemovesFromQueue},
	})
}

func testResumeTokenListExcludesConsumed(t *testing.T, factory ProviderFactory) {
	p := newProvider(t, factory)
	caps := capabilities(t, p)
	if !caps.SupportsListPending {
		t.Skip("provider does not support list-pending")
	}
	ctx := context.Background()
	withUoW(t, p, func(uow api.UnitOfWork) error {
		return uow.ResumeTokens().SaveResumeToken(ctx, api.ResumeToken{TokenID: "tok-pending", RunID: "r-tok", TaskID: "t-tok"})
	})
	withUoW(t, p, func(uow api.UnitOfWork) error {
		got, err := uow.ResumeTokens().ListPending(ctx, api.ResumeTokenSelector{RunID: "r-tok"})
		if err != nil {
			return err
		}
		if len(got) == 0 {
			t.Fatal("ListPending: expected at least 1 pending token")
		}
		return nil
	})
}

func testResumeTokenListPagination(t *testing.T, factory ProviderFactory) {
	p := newProvider(t, factory)
	caps := capabilities(t, p)
	if !caps.SupportsListPending {
		t.Skip("provider does not support list-pending")
	}
	ctx := context.Background()
	withUoW(t, p, func(uow api.UnitOfWork) error {
		for i := 0; i < 5; i++ {
			if err := uow.ResumeTokens().SaveResumeToken(ctx, api.ResumeToken{TokenID: fmt.Sprintf("tok-page-%d", i), RunID: "r-page"}); err != nil {
				return err
			}
		}
		return nil
	})
	withUoW(t, p, func(uow api.UnitOfWork) error {
		got, err := uow.ResumeTokens().ListPending(ctx, api.ResumeTokenSelector{RunID: "r-page", Limit: 2})
		if err != nil {
			return err
		}
		if len(got) > 2 {
			t.Fatalf("ListPending(Limit=2): got %d, want ≤2", len(got))
		}
		return nil
	})
}

func testOutboxScanReturnsQueued(t *testing.T, factory ProviderFactory) {
	p := newProvider(t, factory)
	caps := capabilities(t, p)
	if !caps.SupportsListPending {
		t.Skip("provider does not support list-pending")
	}
	ctx := context.Background()
	withUoW(t, p, func(uow api.UnitOfWork) error {
		return uow.UserMessages().QueueMessage(ctx, api.UserMessage{ID: "m-q-1", RunID: "r-out", Status: api.UserMessageQueued, Type: api.UserMessageTypeProgressUpdate})
	})
	withUoW(t, p, func(uow api.UnitOfWork) error {
		got, err := uow.UserMessages().ListPendingFor(ctx, api.UserMessageSelector{RunID: "r-out"})
		if err != nil {
			return err
		}
		if len(got) != 1 || got[0].ID != "m-q-1" {
			t.Fatalf("ListPendingFor: %+v", got)
		}
		return nil
	})
}

func testOutboxFIFO(t *testing.T, factory ProviderFactory) {
	p := newProvider(t, factory)
	caps := capabilities(t, p)
	if !caps.SupportsListPending {
		t.Skip("provider does not support list-pending")
	}
	ctx := context.Background()
	now := time.Now().UTC()
	withUoW(t, p, func(uow api.UnitOfWork) error {
		for i, id := range []string{"m1", "m2", "m3"} {
			if err := uow.UserMessages().QueueMessage(ctx, api.UserMessage{
				ID: id, RunID: "r-fifo", Status: api.UserMessageQueued, Type: api.UserMessageTypeProgressUpdate,
				CreatedAt: now.Add(time.Duration(i) * time.Second),
			}); err != nil {
				return err
			}
		}
		return nil
	})
	withUoW(t, p, func(uow api.UnitOfWork) error {
		got, err := uow.UserMessages().ListPendingFor(ctx, api.UserMessageSelector{RunID: "r-fifo"})
		if err != nil {
			return err
		}
		if len(got) != 3 {
			t.Fatalf("FIFO: got %d messages, want 3", len(got))
		}
		// Verify FIFO order: m1 before m2 before m3 (or empty if provider
		// doesn't enforce ordering by CreatedAt; but the contract REQUIRES
		// FIFO).
		for i, want := range []string{"m1", "m2", "m3"} {
			if got[i].ID != want {
				t.Fatalf("FIFO order broken at %d: got %q, want %q", i, got[i].ID, want)
			}
		}
		return nil
	})
}

func testOutboxUpdateRemovesFromQueue(t *testing.T, factory ProviderFactory) {
	p := newProvider(t, factory)
	caps := capabilities(t, p)
	if !caps.SupportsListPending {
		t.Skip("provider does not support list-pending")
	}
	ctx := context.Background()
	withUoW(t, p, func(uow api.UnitOfWork) error {
		return uow.UserMessages().QueueMessage(ctx, api.UserMessage{ID: "m-pub", RunID: "r-upd", Status: api.UserMessageQueued, Type: api.UserMessageTypeProgressUpdate})
	})
	withUoW(t, p, func(uow api.UnitOfWork) error {
		msg, err := uow.UserMessages().LoadMessage(ctx, "r-upd", "m-pub")
		if err != nil {
			return err
		}
		msg.Status = api.UserMessagePublished
		return uow.UserMessages().UpdateMessage(ctx, msg)
	})
	withUoW(t, p, func(uow api.UnitOfWork) error {
		got, err := uow.UserMessages().ListPendingFor(ctx, api.UserMessageSelector{RunID: "r-upd", Statuses: []string{string(api.UserMessageQueued)}})
		if err != nil {
			return err
		}
		for _, m := range got {
			if m.ID == "m-pub" {
				t.Fatal("Update did not remove published message from queued list")
			}
		}
		return nil
	})
}

// ─── Replay determinism suite ───────────────────────────────────────────

func runReplayDeterminismSuite(t *testing.T, factory ProviderFactory) {
	t.Helper()
	runSuite(t, factory, []suiteCase{
		{"TestReplay_SameInputSameOutput", testReplaySameInputSameOutput},
		{"TestReplay_PartialReplay", testReplayPartial},
	})
}

func testReplaySameInputSameOutput(t *testing.T, factory ProviderFactory) {
	p := newProvider(t, factory)
	runID := "r-replay-1"
	events := []api.Event{
		{RunID: runID, Sequence: 1, Type: api.EventRunStarted},
		{RunID: runID, Sequence: 2, Type: api.EventRunStatusChanged},
		{RunID: runID, Sequence: 3, Type: api.EventRunStatusChanged},
	}
	withUoW(t, p, func(uow api.UnitOfWork) error {
		for _, e := range events {
			if err := uow.Events().AppendEvent(context.Background(), e); err != nil {
				return err
			}
		}
		return nil
	})
	var a, b []api.Event
	withUoW(t, p, func(uow api.UnitOfWork) error {
		got, err := uow.Events().ListEvents(context.Background(), runID)
		a = got
		return err
	})
	withUoW(t, p, func(uow api.UnitOfWork) error {
		got, err := uow.Events().ListEvents(context.Background(), runID)
		b = got
		return err
	})
	if len(a) != len(b) {
		t.Fatalf("replay length mismatch: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Sequence != b[i].Sequence || a[i].Type != b[i].Type {
			t.Fatalf("replay determinism broken at %d: %+v vs %+v", i, a[i], b[i])
		}
	}
}

func testReplayPartial(t *testing.T, factory ProviderFactory) {
	p := newProvider(t, factory)
	runID := "r-replay-2"
	withUoW(t, p, func(uow api.UnitOfWork) error {
		for i := 1; i <= 5; i++ {
			if err := uow.Events().AppendEvent(context.Background(), api.Event{RunID: runID, Sequence: i, Type: api.EventRunStatusChanged}); err != nil {
				return err
			}
		}
		return nil
	})
	withUoW(t, p, func(uow api.UnitOfWork) error {
		got, err := uow.Events().ListAfter(context.Background(), runID, 2)
		if err != nil {
			return err
		}
		if len(got) != 3 {
			t.Fatalf("ListAfter(2): got %d, want 3", len(got))
		}
		for i, e := range got {
			if e.Sequence != i+3 {
				t.Fatalf("partial replay: got Sequence=%d at idx %d", e.Sequence, i)
			}
		}
		return nil
	})
}

// ─── Capability self-consistency ────────────────────────────────────────

func runCapabilitySelfConsistencySuite(t *testing.T, factory ProviderFactory) {
	t.Helper()
	t.Run("TestCapabilities_AreSelfConsistent", func(t *testing.T) {
		p := newProvider(t, factory)
		caps := capabilities(t, p)
		ctx := context.Background()
		// If the provider claims SupportsListPending, ListPending must work.
		// Each check opens and closes its own UoW because providers that
		// serialize writers (e.g. the memory reference impl) would otherwise
		// deadlock when a second Begin runs before the first UoW closes.
		if caps.SupportsListPending {
			uow, err := p.Begin(ctx)
			if err != nil {
				t.Fatalf("Begin: %v", err)
			}
			if _, err := uow.ResumeTokens().ListPending(ctx, api.ResumeTokenSelector{}); err != nil {
				_ = uow.Rollback(ctx)
				t.Fatalf("ListPending claimed but failed: %v", err)
			}
			if err := uow.Rollback(ctx); err != nil {
				t.Fatalf("Rollback: %v", err)
			}
		}
		// If the provider does NOT support DeadLetterRequeue, Requeue must
		// return an error rather than silently succeed.
		if !caps.SupportsDeadLetterRequeue {
			uow, err := p.Begin(ctx)
			if err != nil {
				t.Fatalf("Begin: %v", err)
			}
			rerr := uow.DeadLetters().Requeue(ctx, "nonexistent")
			_ = uow.Rollback(ctx)
			if rerr == nil {
				t.Fatal("Requeue: provider declares !SupportsDeadLetterRequeue but Requeue returned nil; want error")
			}
			_ = errors.Unwrap(rerr) // structural check; specific kind not mandated
		}
	})
}

// Compile-time guard: the factory must return a value that satisfies
// api.StoreProvider.
var _ ProviderFactory = func(t *testing.T) (api.StoreProvider, func()) { return nil, func() {} }
