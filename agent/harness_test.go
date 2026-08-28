package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/session"
)

type blockingProvider struct {
	started chan struct{}
	once    sync.Once
}

func (*blockingProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "blocking"}
}

func (b *blockingProvider) Stream(ctx context.Context, _ provider.Request) (provider.Stream, error) {
	b.once.Do(func() { close(b.started) })
	<-ctx.Done()
	return nil, ctx.Err()
}

type countingStorage struct {
	session.Storage
	scan   int
	getReg int
	getEnt int
}

func (c *countingStorage) ScanBranch(ctx context.Context, startID string) ([]session.Entry, error) {
	c.scan++
	return c.Storage.ScanBranch(ctx, startID)
}

func (c *countingStorage) GetRegister(ctx context.Context, namespace, key string) (session.Register, bool, error) {
	c.getReg++
	return c.Storage.GetRegister(ctx, namespace, key)
}

func (c *countingStorage) GetEntries(ctx context.Context, ids []string) (map[string]session.Entry, error) {
	c.getEnt++
	return c.Storage.GetEntries(ctx, ids)
}

func helloTurns() [][]provider.Event {
	return [][]provider.Event{{
		{Kind: provider.EventTextDelta, Text: "hello"},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}}
}

func TestHarness_HappyPath(t *testing.T) {
	store := session.NewMemory()
	driver := &scriptedProvider{turns: helloTurns()}
	h, err := OpenHarness(context.Background(), store, HarnessOptions{Provider: driver, Model: "test-model"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := h.Prompt(context.Background(), message.NewText(message.RoleUser, "hi"))
	if err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}
	if out.Kind != harnessOutcomeCompleted || out.FinalMessage == nil || out.FinalMessage.Text != "hello" {
		t.Fatalf("outcome = %#v", out)
	}
	last, ok, err := h.LastResult(context.Background())
	if err != nil || !ok || last.Outcome != harnessOutcomeCompleted {
		t.Fatalf("LastResult ok=%v err=%v last=%#v", ok, err, last)
	}
	ops, err := store.ListRegisters(context.Background(), session.NSOpState, "")
	if err != nil || len(ops) != 0 {
		t.Fatalf("op.state leftovers = %#v err=%v", ops, err)
	}
	branch, err := store.ScanBranch(context.Background(), out.LeafID)
	if err != nil || len(branch) != 2 {
		t.Fatalf("ScanBranch = %#v err=%v", branch, err)
	}
	if branch[0].Message.Role != message.RoleUser || branch[1].Message.Text != "hello" {
		t.Fatalf("branch messages = %#v", branch)
	}
}

func TestHarness_CloseMidStreamResumeExhausts(t *testing.T) {
	m := session.NewMemory()
	blocker := &blockingProvider{started: make(chan struct{})}
	a, err := OpenHarness(context.Background(), m, HarnessOptions{
		Provider: blocker,
		Model:    "test-model",
		Retry:    HarnessRetry{MaxAttempts: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	errc := make(chan error, 1)
	go func() {
		_, err := a.Prompt(context.Background(), message.NewText(message.RoleUser, "hi"))
		errc <- err
	}()
	waitStarted(t, blocker.started)
	if err := a.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := waitErr(t, errc); !errors.Is(err, ErrHarnessClosed) {
		t.Fatalf("Prompt after Close = %v, want ErrHarnessClosed", err)
	}

	pending, reserved, usageID := pendingGeneration(t, m)
	if pending.Status != harnessGenEffectPending {
		t.Fatalf("status = %q, want effect_pending", pending.Status)
	}

	bDriver := &scriptedProvider{turns: helloTurns()}
	b, err := OpenHarness(context.Background(), m, HarnessOptions{
		Provider: bDriver,
		Model:    "test-model",
		Retry:    HarnessRetry{MaxAttempts: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := b.Resume(context.Background())
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if out.Kind != harnessOutcomeFailed || out.Error == nil || out.Error.Code != harnessCodeInterrupted {
		t.Fatalf("resume outcome = %#v", out)
	}
	entries, err := m.GetEntries(context.Background(), []string{reserved})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := entries[reserved]; !ok {
		t.Fatal("synthetic response entry missing")
	}
	rows, err := m.GetUsage(context.Background(), []string{usageID})
	if err != nil {
		t.Fatal(err)
	}
	row, ok := rows[usageID]
	if !ok || row.Usage != (session.Usage{}) {
		t.Fatalf("usage = %#v ok=%v, want zero row", row, ok)
	}
	assertNoOpRegisters(t, m)
	if bDriver.callIndex != 0 {
		t.Fatalf("scripted Stream calls = %d, want 0", bDriver.callIndex)
	}
}

func TestHarness_CloseMidStreamRetrySucceeds(t *testing.T) {
	m := session.NewMemory()
	blocker := &blockingProvider{started: make(chan struct{})}
	a, err := OpenHarness(context.Background(), m, HarnessOptions{
		Provider: blocker,
		Model:    "test-model",
		Retry:    HarnessRetry{MaxAttempts: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	errc := make(chan error, 1)
	go func() {
		_, err := a.Prompt(context.Background(), message.NewText(message.RoleUser, "hi"))
		errc <- err
	}()
	waitStarted(t, blocker.started)
	if err := a.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := waitErr(t, errc); !errors.Is(err, ErrHarnessClosed) {
		t.Fatalf("Prompt after Close = %v, want ErrHarnessClosed", err)
	}
	_, abandoned, _ := pendingGeneration(t, m)

	bDriver := &scriptedProvider{turns: [][]provider.Event{{
		{Kind: provider.EventTextDelta, Text: "ok"},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}}}
	b, err := OpenHarness(context.Background(), m, HarnessOptions{
		Provider: bDriver,
		Model:    "test-model",
		Retry:    HarnessRetry{MaxAttempts: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := b.Resume(context.Background())
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if out.Kind != harnessOutcomeCompleted || out.FinalMessage == nil || out.FinalMessage.Text != "ok" {
		t.Fatalf("resume outcome = %#v", out)
	}
	if bDriver.callIndex != 1 {
		t.Fatalf("Stream calls = %d, want 1", bDriver.callIndex)
	}
	entries, err := m.GetEntries(context.Background(), []string{abandoned})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := entries[abandoned]; ok {
		t.Fatal("abandoned reserved response id should have no entry")
	}
}

func TestHarness_LaneBusy(t *testing.T) {
	blocker := &blockingProvider{started: make(chan struct{})}
	h, err := OpenHarness(context.Background(), session.NewMemory(), HarnessOptions{Provider: blocker, Model: "test-model"})
	if err != nil {
		t.Fatal(err)
	}
	errc := make(chan error, 1)
	go func() {
		_, err := h.Prompt(context.Background(), message.NewText(message.RoleUser, "hi"))
		errc <- err
	}()
	waitStarted(t, blocker.started)
	if _, err := h.Prompt(context.Background(), message.NewText(message.RoleUser, "again")); !errors.Is(err, ErrLaneBusy) {
		t.Fatalf("second Prompt = %v, want ErrLaneBusy", err)
	}
	if _, err := h.Resume(context.Background()); !errors.Is(err, ErrLaneBusy) {
		t.Fatalf("Resume = %v, want ErrLaneBusy", err)
	}
	if err := h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := waitErr(t, errc); !errors.Is(err, ErrHarnessClosed) {
		t.Fatalf("blocked Prompt = %v, want ErrHarnessClosed", err)
	}
}

func TestHarness_DurableLaneLeaseBlocksSecondInstance(t *testing.T) {
	const leaseTTL = 30 * time.Millisecond
	store := session.NewMemory()
	blocker := &blockingProvider{started: make(chan struct{})}
	first, err := OpenHarness(context.Background(), store, HarnessOptions{
		Provider: blocker,
		Model:    "test-model",
		LeaseTTL: leaseTTL,
	})
	if err != nil {
		t.Fatal(err)
	}
	firstErr := make(chan error, 1)
	go func() {
		_, err := first.Prompt(context.Background(), message.NewText(message.RoleUser, "hi"))
		firstErr <- err
	}()
	waitStarted(t, blocker.started)

	second, err := OpenHarness(context.Background(), store, HarnessOptions{
		Provider: echoProvider{},
		Model:    "test-model",
		LeaseTTL: leaseTTL,
	})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(3 * leaseTTL)
	if _, err := second.Resume(context.Background()); !errors.Is(err, ErrLaneBusy) {
		t.Fatalf("second Resume while first lease is renewed = %v, want ErrLaneBusy", err)
	}

	if err := first.Close(context.Background()); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := waitErr(t, firstErr); !errors.Is(err, ErrHarnessClosed) {
		t.Fatalf("first Prompt after Close = %v, want ErrHarnessClosed", err)
	}
	out, err := second.Resume(context.Background())
	if err != nil {
		t.Fatalf("second Resume after handoff error = %v", err)
	}
	if out.Kind != harnessOutcomeFailed || out.Error == nil || out.Error.Code != harnessCodeInterrupted {
		t.Fatalf("resumed outcome = %#v, want interrupted", out)
	}
}

func TestHarness_RestoreRejectsMismatchedOperationIdentity(t *testing.T) {
	store := session.NewMemory()
	blocker := &blockingProvider{started: make(chan struct{})}
	first, err := OpenHarness(context.Background(), store, HarnessOptions{
		Provider: blocker,
		Model:    "test-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	promptErr := make(chan error, 1)
	go func() {
		_, err := first.Prompt(context.Background(), message.NewText(message.RoleUser, "hi"))
		promptErr <- err
	}()
	waitStarted(t, blocker.started)
	if err := first.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := waitErr(t, promptErr); !errors.Is(err, ErrHarnessClosed) {
		t.Fatalf("Prompt after Close = %v, want ErrHarnessClosed", err)
	}
	lane, ok := loadReg[session.LaneState](t, store, session.NSLaneState, harnessLaneMain)
	if !ok || lane.CurrentOperationID == "" {
		t.Fatalf("pending lane = %#v, ok=%t", lane, ok)
	}
	op, ok := loadReg[Operation](t, store, session.NSOpMeta, lane.CurrentOperationID)
	if !ok {
		t.Fatal("pending operation metadata is missing")
	}
	op.OperationID = "different-operation"
	write, err := setReg(session.NSOpMeta, lane.CurrentOperationID, op)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Commit(context.Background(), []session.Write{write}); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenHarness(context.Background(), store, HarnessOptions{
		Provider: echoProvider{},
		Model:    "test-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Resume(context.Background()); !errors.Is(err, ErrHarnessFault) {
		t.Fatalf("Resume with mismatched operation id = %v, want ErrHarnessFault", err)
	}
}

func TestHarness_IdleResume(t *testing.T) {
	h, err := OpenHarness(context.Background(), session.NewMemory(), HarnessOptions{
		Provider: &scriptedProvider{turns: helloTurns()},
		Model:    "test-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Resume(context.Background()); !errors.Is(err, ErrNothingToResume) {
		t.Fatalf("Resume = %v, want ErrNothingToResume", err)
	}
}

func TestHarness_EmptyPrompt(t *testing.T) {
	store := session.NewMemory()
	h, err := OpenHarness(context.Background(), store, HarnessOptions{
		Provider: &scriptedProvider{turns: helloTurns()},
		Model:    "test-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Prompt(context.Background()); !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("empty Prompt = %v, want ErrInvalidMessage", err)
	}
	var n int
	for _, ns := range []string{
		session.NSLaneLeaf, session.NSLaneConfig, session.NSLaneState,
		session.NSLaneLastResult, session.NSOpMeta, session.NSOpState,
	} {
		regs, err := store.ListRegisters(context.Background(), ns, "")
		if err != nil {
			t.Fatal(err)
		}
		n += len(regs)
	}
	if n != 3 {
		t.Fatalf("registers after empty Prompt = %d, want 3", n)
	}
}

func TestHarness_RestoreBudget(t *testing.T) {
	m := session.NewMemory()
	h, err := OpenHarness(context.Background(), m, HarnessOptions{
		Provider: &scriptedProvider{turns: helloTurns()},
		Model:    "test-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Prompt(context.Background(), message.NewText(message.RoleUser, "hi")); err != nil {
		t.Fatal(err)
	}
	counter := &countingStorage{Storage: m}
	reopened, err := OpenHarness(context.Background(), counter, HarnessOptions{
		Provider: &scriptedProvider{turns: helloTurns()},
		Model:    "test-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Resume(context.Background()); !errors.Is(err, ErrNothingToResume) {
		t.Fatalf("idle Resume = %v, want ErrNothingToResume", err)
	}
	if counter.scan != 0 {
		t.Fatalf("ScanBranch count = %d, want 0", counter.scan)
	}
	if counter.getReg > 6 {
		t.Fatalf("GetRegister count = %d, want <= 6", counter.getReg)
	}
	if counter.getEnt > 1 {
		t.Fatalf("GetEntries count = %d, want <= 1", counter.getEnt)
	}
}

func TestHarness_ToolUseUnsupported(t *testing.T) {
	store := session.NewMemory()
	driver := &scriptedProvider{turns: [][]provider.Event{{
		{Kind: provider.EventToolCall, ToolCall: &message.ToolCall{ID: "call-1", Name: "lookup"}},
		{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
	}}}
	h, err := OpenHarness(context.Background(), store, HarnessOptions{Provider: driver, Model: "test-model"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := h.Prompt(context.Background(), message.NewText(message.RoleUser, "hi"))
	if err != nil {
		t.Fatal(err)
	}
	if out.Kind != harnessOutcomeFailed || out.Error == nil || out.Error.Code != harnessCodeTools {
		t.Fatalf("outcome = %#v", out)
	}
	entries, err := store.GetEntries(context.Background(), []string{out.LeafID})
	if err != nil {
		t.Fatal(err)
	}
	entry := entries[out.LeafID]
	if len(entry.Message.ToolCalls) != 0 || entry.StopReason != string(provider.StopReasonError) {
		t.Fatalf("assistant entry = %#v", entry)
	}
}

func TestHarness_ContextOmitsErrorAssistant(t *testing.T) {
	driver := &scriptedProvider{turns: [][]provider.Event{
		{
			{Kind: provider.EventToolCall, ToolCall: &message.ToolCall{ID: "call-1", Name: "lookup"}},
			{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
		},
		{
			{Kind: provider.EventTextDelta, Text: "ok"},
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
		},
	}}
	h, err := OpenHarness(context.Background(), session.NewMemory(), HarnessOptions{Provider: driver, Model: "test-model"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Prompt(context.Background(), message.NewText(message.RoleUser, "hi")); err != nil {
		t.Fatal(err)
	}
	out, err := h.Prompt(context.Background(), message.NewText(message.RoleUser, "again"))
	if err != nil {
		t.Fatal(err)
	}
	if out.Kind != harnessOutcomeCompleted {
		t.Fatalf("second prompt = %#v", out)
	}
	if len(driver.requests) < 2 {
		t.Fatalf("expected 2 stream requests, got %d", len(driver.requests))
	}
	msgs := driver.requests[len(driver.requests)-1].Messages
	var users int
	for _, msg := range msgs {
		if msg.Role == message.RoleAssistant {
			t.Fatalf("provider saw assistant message %#v", msg)
		}
		if msg.Role == message.RoleUser {
			users++
		}
	}
	if users != 2 {
		t.Fatalf("user messages = %d, want 2 (got %#v)", users, msgs)
	}
}

func TestHarness_RetryableErrorWithToolEventsTerminatesUnsupported(t *testing.T) {
	store := session.NewMemory()
	driver := &scriptedProvider{turns: [][]provider.Event{
		{
			{Kind: provider.EventToolCall, ToolCall: &message.ToolCall{ID: "call-1", Name: "lookup"}},
			{Kind: provider.EventError, Err: &provider.Error{Provider: "scripted", Kind: provider.ErrorRateLimit}},
		},
		{
			{Kind: provider.EventTextDelta, Text: "ok"},
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
		},
	}}
	h, err := OpenHarness(context.Background(), store, HarnessOptions{
		Provider: driver,
		Model:    "test-model",
		Retry:    HarnessRetry{MaxAttempts: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := h.Prompt(context.Background(), message.NewText(message.RoleUser, "hi"))
	if err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}
	if out.Kind != harnessOutcomeFailed || out.Error == nil || out.Error.Code != harnessCodeTools {
		t.Fatalf("outcome = %#v, want tools_unsupported", out)
	}
	if driver.callIndex != 1 {
		t.Fatalf("Stream calls = %d, want 1", driver.callIndex)
	}
	branch, err := store.ScanBranch(context.Background(), out.LeafID)
	if err != nil || len(branch) != 2 {
		t.Fatalf("ScanBranch = %#v err=%v", branch, err)
	}
}

func waitStarted(t *testing.T, started <-chan struct{}) {
	t.Helper()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not start")
	}
}

func waitErr(t *testing.T, errc <-chan error) error {
	t.Helper()
	select {
	case err := <-errc:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("Prompt goroutine did not return")
		return nil
	}
}

func loadReg[T any](t *testing.T, store session.Storage, namespace, key string) (T, bool) {
	t.Helper()
	var zero T
	reg, ok, err := store.GetRegister(context.Background(), namespace, key)
	if err != nil {
		t.Fatalf("GetRegister(%s/%s) error = %v", namespace, key, err)
	}
	if !ok {
		return zero, false
	}
	value, err := session.UnmarshalRegister[T](reg.Value)
	if err != nil {
		t.Fatalf("decode %s/%s: %v", namespace, key, err)
	}
	return value, true
}

func pendingGeneration(t *testing.T, store *session.Memory) (Generation, string, string) {
	t.Helper()
	state, ok := loadReg[session.LaneState](t, store, session.NSLaneState, harnessLaneMain)
	if !ok || state.CurrentOperationID == "" {
		t.Fatalf("lane.state missing op: ok=%v state=%#v", ok, state)
	}
	st, ok := loadReg[RunState](t, store, session.NSOpState, state.CurrentOperationID)
	if !ok || st.Phase.Generation == nil {
		t.Fatalf("op.state missing generation: ok=%v st=%#v", ok, st)
	}
	return *st.Phase.Generation, st.Phase.Generation.ResponseEntryID, st.Phase.Generation.UsageID
}

func assertNoOpRegisters(t *testing.T, store session.Storage) {
	t.Helper()
	for _, ns := range []string{session.NSOpMeta, session.NSOpState} {
		regs, err := store.ListRegisters(context.Background(), ns, "")
		if err != nil || len(regs) != 0 {
			t.Fatalf("%s leftovers = %#v err=%v", ns, regs, err)
		}
	}
}

// stuckProvider ignores cancellation until the test releases it, so a drive can
// be parked past the point where Close would like to give up.
type stuckProvider struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (*stuckProvider) Metadata() provider.Metadata { return provider.Metadata{Name: "stuck"} }

func (p *stuckProvider) Stream(context.Context, provider.Request) (provider.Stream, error) {
	p.once.Do(func() { close(p.started) })
	<-p.release
	return provider.NewSliceStream(helloTurns()[0]), nil
}

// echoProvider answers every call with the same finished turn and holds no
// state, so concurrent drives can share one instance.
type echoProvider struct{}

func (echoProvider) Metadata() provider.Metadata { return provider.Metadata{Name: "echo"} }

func (echoProvider) Stream(context.Context, provider.Request) (provider.Stream, error) {
	return provider.NewSliceStream(helloTurns()[0]), nil
}

// floodProvider streams one event forever so the per-turn ceilings are the only
// thing that can end the turn.
type floodProvider struct{ event provider.Event }

func (*floodProvider) Metadata() provider.Metadata { return provider.Metadata{Name: "flood"} }

func (p *floodProvider) Stream(context.Context, provider.Request) (provider.Stream, error) {
	return floodStream{event: p.event}, nil
}

type floodStream struct{ event provider.Event }

func (s floodStream) Recv() (provider.Event, error) { return s.event, nil }

func (floodStream) Close() error { return nil }

// injectStorage fails reads the test asks it to fail.
type injectStorage struct {
	session.Storage
	mu     sync.Mutex
	onRead func(namespace, key string) error
}

func (s *injectStorage) GetRegister(ctx context.Context, namespace, key string) (session.Register, bool, error) {
	s.mu.Lock()
	hook := s.onRead
	s.mu.Unlock()
	if hook != nil {
		if err := hook(namespace, key); err != nil {
			return session.Register{}, false, err
		}
	}
	return s.Storage.GetRegister(ctx, namespace, key)
}

func (s *injectStorage) setHook(hook func(namespace, key string) error) {
	s.mu.Lock()
	s.onRead = hook
	s.mu.Unlock()
}

// gatedStorage parks ScanBranch until the gate opens. runStream calls it
// without holding laneMu, so a drive stalls there while Close is free to run.
type gatedStorage struct {
	session.Storage
	parked chan struct{}
	gate   chan struct{}
	once   sync.Once
}

func (s *gatedStorage) ScanBranch(ctx context.Context, startID string) ([]session.Entry, error) {
	s.once.Do(func() { close(s.parked) })
	<-s.gate
	return s.Storage.ScanBranch(ctx, startID)
}

type gatedCommitStorage struct {
	session.Storage
	block  bool
	parked chan struct{}
	gate   chan struct{}
	once   sync.Once
}

func (s *gatedCommitStorage) Commit(ctx context.Context, writes []session.Write) (session.CommitResult, error) {
	if s.block {
		s.once.Do(func() { close(s.parked) })
		<-s.gate
	}
	return s.Storage.Commit(ctx, writes)
}

func TestHarness_CancelledPromptPersistsInterrupt(t *testing.T) {
	store := session.NewMemory()
	blocker := &blockingProvider{started: make(chan struct{})}
	h, err := OpenHarness(context.Background(), store, HarnessOptions{
		Provider: blocker,
		Model:    "test-model",
		Retry:    HarnessRetry{MaxAttempts: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	outc := make(chan RunOutcome, 1)
	errc := make(chan error, 1)
	go func() {
		out, err := h.Prompt(ctx, message.NewText(message.RoleUser, "hi"))
		outc <- out
		errc <- err
	}()
	waitStarted(t, blocker.started)
	cancel()
	if err := waitErr(t, errc); err != nil {
		t.Fatalf("Prompt after cancel = %v, want the interrupted outcome", err)
	}
	out := <-outc
	if out.Kind != harnessOutcomeFailed || out.Error == nil || out.Error.Code != harnessCodeInterrupted {
		t.Fatalf("outcome = %#v, want an interrupted failure", out)
	}
	// The store rejects cancelled calls, so reaching a terminal record at all
	// proves the finalization writes ran on a detached context.
	state, ok := loadReg[session.LaneState](t, store, session.NSLaneState, harnessLaneMain)
	if !ok || state.CurrentOperationID != "" {
		t.Fatalf("lane.state = %#v ok=%v, want an idle lane", state, ok)
	}
	assertNoOpRegisters(t, store)
	last, ok := loadReg[session.LaneLastResult](t, store, session.NSLaneLastResult, harnessLaneMain)
	if !ok || last.Outcome != harnessOutcomeFailed || last.Error == nil || last.Error.Code != harnessCodeInterrupted {
		t.Fatalf("lane.lastResult = %#v ok=%v", last, ok)
	}
	entries, err := store.GetEntries(context.Background(), []string{out.LeafID})
	if err != nil {
		t.Fatal(err)
	}
	if entry, found := entries[out.LeafID]; !found || entry.ErrorMessage != "interrupted" {
		t.Fatalf("leaf entry = %#v found=%v, want the interrupted marker", entry, found)
	}
}

func TestHarness_StorageFailureClassification(t *testing.T) {
	transient := errors.New("storage unavailable")
	tests := []struct {
		name     string
		injected error
		retires  bool
	}{
		{name: "unavailable store stays retryable", injected: transient},
		{name: "closed store stays retryable", injected: session.ErrClosed},
		{name: "cancelled read stays retryable", injected: context.Canceled},
		{name: "corrupt state retires the harness", injected: session.ErrCorrupt, retires: true},
		{name: "invalid write retires the harness", injected: session.ErrInvalidWrite, retires: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &injectStorage{Storage: session.NewMemory()}
			h, err := OpenHarness(context.Background(), store, HarnessOptions{
				Provider: echoProvider{},
				Model:    "test-model",
			})
			if err != nil {
				t.Fatal(err)
			}
			var fired sync.Once
			store.setHook(func(namespace, _ string) error {
				if namespace != session.NSLaneState {
					return nil
				}
				var hit error
				fired.Do(func() { hit = tt.injected })
				return hit
			})
			_, err = h.Prompt(context.Background(), message.NewText(message.RoleUser, "hi"))
			if !errors.Is(err, tt.injected) {
				t.Fatalf("first Prompt = %v, want %v", err, tt.injected)
			}
			if got := errors.Is(err, ErrHarnessFault); got != tt.retires {
				t.Fatalf("first Prompt fault = %v, want %v (err = %v)", got, tt.retires, err)
			}
			out, err := h.Prompt(context.Background(), message.NewText(message.RoleUser, "again"))
			if tt.retires {
				if !errors.Is(err, ErrHarnessFault) {
					t.Fatalf("Prompt after a fault = %v, want ErrHarnessFault", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Prompt after a transient failure = %v, want success", err)
			}
			if out.Kind != harnessOutcomeCompleted || out.FinalMessage == nil || out.FinalMessage.Text != "hello" {
				t.Fatalf("retry outcome = %#v", out)
			}
		})
	}
}

func TestHarness_MissingRegisterFaults(t *testing.T) {
	store := session.NewMemory()
	h, err := OpenHarness(context.Background(), store, HarnessOptions{
		Provider: echoProvider{},
		Model:    "test-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Commit(context.Background(), []session.Write{
		session.DeleteRegister{Namespace: session.NSLaneState, Key: harnessLaneMain},
	}); err != nil {
		t.Fatal(err)
	}
	_, err = h.Prompt(context.Background(), message.NewText(message.RoleUser, "hi"))
	if !errors.Is(err, ErrHarnessFault) || !errors.Is(err, ErrRegisterMissing) {
		t.Fatalf("Prompt with no lane.state = %v, want a fault naming the missing register", err)
	}
	if _, _, err := h.LastResult(context.Background()); !errors.Is(err, ErrHarnessFault) {
		t.Fatalf("LastResult after fault = %v, want ErrHarnessFault", err)
	}
}

func TestHarness_CloseDeadlineDoesNotWaitForLaneMutex(t *testing.T) {
	store := &gatedCommitStorage{
		Storage: session.NewMemory(),
		parked:  make(chan struct{}),
		gate:    make(chan struct{}),
	}
	h, err := OpenHarness(context.Background(), store, HarnessOptions{
		Provider: echoProvider{},
		Model:    "test-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	store.block = true
	promptErr := make(chan error, 1)
	go func() {
		_, err := h.Prompt(context.Background(), message.NewText(message.RoleUser, "hi"))
		promptErr <- err
	}()
	waitStarted(t, store.parked)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := h.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close() while storage holds laneMu = %v, want DeadlineExceeded", err)
	}
	close(store.gate)
	if err := waitErr(t, promptErr); !errors.Is(err, context.Canceled) {
		t.Fatalf("Prompt after blocked commit = %v, want context.Canceled", err)
	}
	if err := h.Close(context.Background()); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func TestHarness_CloseWaitsForParkedDrive(t *testing.T) {
	store := &gatedStorage{
		Storage: session.NewMemory(),
		parked:  make(chan struct{}),
		gate:    make(chan struct{}),
	}
	h, err := OpenHarness(context.Background(), store, HarnessOptions{
		Provider: echoProvider{},
		Model:    "test-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	errc := make(chan error, 1)
	go func() {
		_, err := h.Prompt(context.Background(), message.NewText(message.RoleUser, "hi"))
		errc <- err
	}()
	// The drive parks inside ScanBranch, which runs without laneMu, so Close
	// can start while a commit is still possible.
	waitStarted(t, store.parked)
	const park = 100 * time.Millisecond
	go func() {
		time.Sleep(park)
		close(store.gate)
	}()
	start := time.Now()
	if err := h.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if waited := time.Since(start); waited < park {
		t.Fatalf("Close returned after %v while a drive was parked in storage; it must wait", waited)
	}
	if err := waitErr(t, errc); !errors.Is(err, ErrHarnessClosed) {
		t.Fatalf("parked Prompt = %v, want ErrHarnessClosed", err)
	}
}

func TestHarness_CloseHonorsDeadline(t *testing.T) {
	stuck := &stuckProvider{started: make(chan struct{}), release: make(chan struct{})}
	release := sync.OnceFunc(func() { close(stuck.release) })
	t.Cleanup(release)
	h, err := OpenHarness(context.Background(), session.NewMemory(), HarnessOptions{
		Provider: stuck,
		Model:    "test-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	errc := make(chan error, 1)
	go func() {
		_, err := h.Prompt(context.Background(), message.NewText(message.RoleUser, "hi"))
		errc <- err
	}()
	waitStarted(t, stuck.started)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := h.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close() on an unresponsive drive = %v, want DeadlineExceeded", err)
	}
	release()
	if err := waitErr(t, errc); !errors.Is(err, ErrHarnessClosed) {
		t.Fatalf("released Prompt = %v, want ErrHarnessClosed", err)
	}
	if err := h.Close(context.Background()); err != nil {
		t.Fatalf("second Close() after drive exit = %v", err)
	}
}

func TestHarness_ConcurrentPromptAndClose(t *testing.T) {
	h, err := OpenHarness(context.Background(), session.NewMemory(), HarnessOptions{
		Provider: echoProvider{},
		Model:    "test-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 10 {
				_, err := h.Prompt(context.Background(), message.NewText(message.RoleUser, "hi"))
				switch {
				case err == nil,
					errors.Is(err, ErrHarnessClosed),
					errors.Is(err, ErrLaneBusy),
					errors.Is(err, context.Canceled):
				default:
					t.Errorf("Prompt racing Close = %v", err)
					return
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := h.Close(context.Background()); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	}()
	wg.Wait()
	if _, err := h.Prompt(context.Background(), message.NewText(message.RoleUser, "after")); !errors.Is(err, ErrHarnessClosed) {
		t.Fatalf("Prompt after Close = %v, want ErrHarnessClosed", err)
	}
	if _, err := h.Prompt(context.Background()); !errors.Is(err, ErrHarnessClosed) {
		t.Fatalf("empty Prompt after Close = %v, want ErrHarnessClosed", err)
	}
	if _, _, err := h.LastResult(context.Background()); !errors.Is(err, ErrHarnessClosed) {
		t.Fatalf("LastResult after Close = %v, want ErrHarnessClosed", err)
	}
}

func TestHarness_ProviderTurnLimits(t *testing.T) {
	tests := []struct {
		name  string
		event provider.Event
		want  string
	}{
		{
			name:  "event ceiling",
			event: provider.Event{Kind: provider.EventTextDelta},
			want:  "events",
		},
		{
			name:  "byte ceiling",
			event: provider.Event{Kind: provider.EventTextDelta, ProviderState: make([]byte, 8<<20)},
			want:  "bytes",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, err := OpenHarness(context.Background(), session.NewMemory(), HarnessOptions{
				Provider: &floodProvider{event: tt.event},
				Model:    "test-model",
			})
			if err != nil {
				t.Fatal(err)
			}
			out, err := h.Prompt(context.Background(), message.NewText(message.RoleUser, "hi"))
			if err != nil {
				t.Fatalf("Prompt() error = %v", err)
			}
			if out.Kind != harnessOutcomeFailed || out.Error == nil || out.Error.Code != harnessCodeProvider {
				t.Fatalf("outcome = %#v, want a provider failure", out)
			}
			if !strings.Contains(out.Error.Message, ErrProviderTurnLimit.Error()) ||
				!strings.Contains(out.Error.Message, tt.want) {
				t.Fatalf("error message = %q, want the %s ceiling", out.Error.Message, tt.want)
			}
		})
	}
}

func TestRetryDelay_ExponentialWithJitter(t *testing.T) {
	tests := []struct {
		name    string
		baseMs  int
		attempt int
		low     time.Duration
		high    time.Duration
	}{
		{name: "no base means no wait", baseMs: 0, attempt: 3},
		{name: "first retry uses the base", baseMs: 100, attempt: 1, low: 50 * time.Millisecond, high: 100 * time.Millisecond},
		{name: "fourth retry doubles three times", baseMs: 100, attempt: 4, low: 400 * time.Millisecond, high: 800 * time.Millisecond},
		{name: "growth saturates at the cap", baseMs: 100, attempt: 40, low: harnessRetryMaxDelay / 2, high: harnessRetryMaxDelay},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var spread int
			first := retryDelay(tt.baseMs, tt.attempt)
			for range 200 {
				got := retryDelay(tt.baseMs, tt.attempt)
				if got < tt.low || got > tt.high {
					t.Fatalf("retryDelay(%d, %d) = %v, want within [%v, %v]", tt.baseMs, tt.attempt, got, tt.low, tt.high)
				}
				if got != first {
					spread++
				}
			}
			if tt.high > 0 && spread == 0 {
				t.Fatalf("retryDelay(%d, %d) never varied; the jitter is not applied", tt.baseMs, tt.attempt)
			}
		})
	}
}

// branchFailStorage fails the branch scan that runStream performs before the
// provider is called. Injection is one-shot so a retry can succeed.
type branchFailStorage struct {
	session.Storage
	mu     sync.Mutex
	failed bool
	err    error
}

func (s *branchFailStorage) ScanBranch(ctx context.Context, startID string) ([]session.Entry, error) {
	s.mu.Lock()
	fail := !s.failed && s.err != nil
	s.failed = s.failed || fail
	s.mu.Unlock()
	if fail {
		return nil, s.err
	}
	return s.Storage.ScanBranch(ctx, startID)
}

func TestHarness_BranchReadFailureIsNotAProviderVerdict(t *testing.T) {
	transient := errors.New("storage unavailable")
	tests := []struct {
		name     string
		injected error
		retires  bool
	}{
		{name: "corrupt branch retires the harness", injected: session.ErrCorrupt, retires: true},
		{name: "unavailable store stays retryable", injected: transient},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &branchFailStorage{Storage: session.NewMemory(), err: tt.injected}
			driver := &scriptedProvider{turns: helloTurns()}
			h, err := OpenHarness(context.Background(), store, HarnessOptions{
				Provider: driver,
				Model:    "test-model",
				Retry:    HarnessRetry{MaxAttempts: 2},
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = h.Prompt(context.Background(), message.NewText(message.RoleUser, "hi"))
			if !errors.Is(err, tt.injected) {
				t.Fatalf("Prompt = %v, want %v", err, tt.injected)
			}
			if got := errors.Is(err, ErrHarnessFault); got != tt.retires {
				t.Fatalf("Prompt fault = %v, want %v (err = %v)", got, tt.retires, err)
			}
			if driver.callIndex != 0 {
				t.Fatalf("provider Stream calls = %d, want 0; the branch read failed first", driver.callIndex)
			}
			// A failed branch read must never be recorded as a model verdict.
			if last, ok := loadReg[session.LaneLastResult](t, store, session.NSLaneLastResult, harnessLaneMain); ok {
				t.Fatalf("lane.lastResult = %#v, want none; no turn was settled", last)
			}
			if tt.retires {
				if _, err := h.Resume(context.Background()); !errors.Is(err, ErrHarnessFault) {
					t.Fatalf("Resume after a fault = %v, want ErrHarnessFault", err)
				}
				return
			}
			// The reservation is still pending, so the retry goes through Resume.
			out, err := h.Resume(context.Background())
			if err != nil {
				t.Fatalf("Resume after a transient branch failure = %v", err)
			}
			if out.Kind != harnessOutcomeCompleted || out.FinalMessage == nil || out.FinalMessage.Text != "hello" {
				t.Fatalf("resume outcome = %#v", out)
			}
		})
	}
}
