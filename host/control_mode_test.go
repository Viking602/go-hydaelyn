package host

import (
	"context"
	"testing"

	"github.com/Viking602/go-hydaelyn/internal/blackboard"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
)

// memorySink is a minimal EventSink used by control-mode tests so we can
// assert exact event ordering without reaching into a storage driver.
type memorySink struct {
	events []storage.Event
}

func (m *memorySink) AppendEvent(_ context.Context, ev storage.Event) error {
	m.events = append(m.events, ev)
	return nil
}

func (m *memorySink) ListEvents(_ context.Context, _ string) ([]storage.Event, error) {
	out := make([]storage.Event, len(m.events))
	copy(out, m.events)
	return out, nil
}

func (m *memorySink) types() []storage.EventType {
	out := make([]storage.EventType, 0, len(m.events))
	for _, ev := range m.events {
		out = append(out, ev.Type)
	}
	return out
}

func (m *memorySink) contains(t storage.EventType) bool {
	for _, ev := range m.events {
		if ev.Type == t {
			return true
		}
	}
	return false
}

// decisionRecorder captures decisions to assert the control loop passed the
// right digest into the decider. Used instead of mocking Runtime directly.
type decisionRecorder struct {
	digests   []SupervisorDigest
	decisions []team.SupervisorDecision
}

func (d *decisionRecorder) Decide(_ context.Context, digest SupervisorDigest, _ *blackboard.State) ([]team.SupervisorDecision, error) {
	d.digests = append(d.digests, digest)
	return d.decisions, nil
}

func TestAutoGrantReadyTasks_OnlyGrantsPendingReady(t *testing.T) {
	digest := SupervisorDigest{
		Sequence: 3,
		Tasks: []TaskDigest{
			{ID: "ready", Status: team.TaskStatusPending, Ready: true},
			{ID: "blocked", Status: team.TaskStatusPending, Ready: false},
			{ID: "running", Status: team.TaskStatusRunning, Ready: true},
			{ID: "done", Status: team.TaskStatusCompleted},
		},
	}
	decisions := autoGrantReadyTasks(digest)
	if len(decisions) != 1 {
		t.Fatalf("expected 1 grant_run decision, got %d", len(decisions))
	}
	d := decisions[0]
	if d.Kind != team.DecisionGrantRun {
		t.Fatalf("expected grant_run, got %s", d.Kind)
	}
	if d.DigestSequence != 3 {
		t.Fatalf("expected anchor to digest 3, got %d", d.DigestSequence)
	}
	if len(d.Grants) != 1 || d.Grants[0].TaskID != "ready" {
		t.Fatalf("expected single grant for 'ready', got %#v", d.Grants)
	}
}

func TestAutoGrantReadyTasks_EmptyWhenNoneReady(t *testing.T) {
	digest := SupervisorDigest{
		Sequence: 1,
		Tasks: []TaskDigest{
			{ID: "blocked", Status: team.TaskStatusPending, Ready: false},
		},
	}
	if got := autoGrantReadyTasks(digest); len(got) != 0 {
		t.Fatalf("expected no decisions, got %#v", got)
	}
}

func TestEnsureFreshTaskContext_AllowsWhenBoardHasEnoughExchanges(t *testing.T) {
	board := &blackboard.State{
		Exchanges: []blackboard.Exchange{{ID: "ex1"}, {ID: "ex2"}, {ID: "ex3"}},
	}
	grant := team.TaskRunGrant{TaskID: "t", ContextPolicy: team.TaskContextPolicy{MinExchangeIndex: 2}}
	if err := ensureFreshTaskContext(board, grant); err != nil {
		t.Fatalf("fresh context must pass, got %v", err)
	}
}

func TestEnsureFreshTaskContext_RejectsStale(t *testing.T) {
	board := &blackboard.State{Exchanges: []blackboard.Exchange{{ID: "ex1"}}}
	grant := team.TaskRunGrant{TaskID: "t", ContextPolicy: team.TaskContextPolicy{MinExchangeIndex: 3}}
	err := ensureFreshTaskContext(board, grant)
	if err == nil {
		t.Fatalf("expected stale-context rejection")
	}
}

func TestEnsureFreshTaskContext_NoPolicyIsNoOp(t *testing.T) {
	if err := ensureFreshTaskContext(nil, team.TaskRunGrant{}); err != nil {
		t.Fatalf("zero policy must not block dispatch, got %v", err)
	}
}

func TestProduceDecisions_LegacyReturnsNothing(t *testing.T) {
	r := &Runtime{controlMode: ControlModeLegacy}
	decisions, err := r.produceDecisions(context.Background(), ControlModeLegacy, SupervisorDigest{Sequence: 1}, nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(decisions) != 0 {
		t.Fatalf("Legacy mode must not produce decisions, got %#v", decisions)
	}
}

func TestProduceDecisions_StrictAutoGrantsWithoutDecider(t *testing.T) {
	r := &Runtime{controlMode: ControlModeStrict}
	digest := SupervisorDigest{
		Sequence: 1,
		Tasks:    []TaskDigest{{ID: "t1", Status: team.TaskStatusPending, Ready: true}},
	}
	decisions, err := r.produceDecisions(context.Background(), ControlModeStrict, digest, nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(decisions) != 1 || decisions[0].Kind != team.DecisionGrantRun {
		t.Fatalf("expected auto-grant fallback, got %#v", decisions)
	}
}

func TestProduceDecisions_StrictPrefersRegisteredDecider(t *testing.T) {
	recorder := &decisionRecorder{
		decisions: []team.SupervisorDecision{{
			Kind:           team.DecisionGrantRun,
			DigestSequence: 1,
			Grants:         []team.TaskRunGrant{{TaskID: "explicit"}},
			Rationale:      "decider chose",
		}},
	}
	r := &Runtime{controlMode: ControlModeStrict, supervisorDecider: recorder}
	digest := SupervisorDigest{
		Sequence: 1,
		Tasks:    []TaskDigest{{ID: "t1", Status: team.TaskStatusPending, Ready: true}},
	}
	decisions, err := r.produceDecisions(context.Background(), ControlModeStrict, digest, nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(decisions) != 1 || decisions[0].Grants[0].TaskID != "explicit" {
		t.Fatalf("expected decider output, got %#v", decisions)
	}
	if len(recorder.digests) != 1 {
		t.Fatalf("expected decider invoked once, got %d", len(recorder.digests))
	}
}

func TestProduceDecisions_HybridWithoutDeciderReturnsNothing(t *testing.T) {
	r := &Runtime{controlMode: ControlModeHybrid}
	digest := SupervisorDigest{
		Sequence: 1,
		Tasks:    []TaskDigest{{ID: "t1", Status: team.TaskStatusPending, Ready: true}},
	}
	decisions, err := r.produceDecisions(context.Background(), ControlModeHybrid, digest, nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(decisions) != 0 {
		t.Fatalf("Hybrid with no decider must stay advisory, got %#v", decisions)
	}
}

func TestFilterRunnableByGrants_LegacyIsPassthrough(t *testing.T) {
	r := &Runtime{controlMode: ControlModeLegacy}
	state := &team.RunState{}
	runnable := []team.Task{{ID: "t1"}, {ID: "t2"}}
	set := map[string]struct{}{"t1": {}, "t2": {}}
	outTasks, outSet := r.filterRunnableByGrants(state, runnable, set)
	if len(outTasks) != 2 || len(outSet) != 2 {
		t.Fatalf("Legacy must pass runnable through unchanged, got %d/%d", len(outTasks), len(outSet))
	}
}

func TestFilterRunnableByGrants_StrictConsumesMatchingGrants(t *testing.T) {
	r := &Runtime{controlMode: ControlModeStrict}
	state := &team.RunState{Control: &team.ControlState{
		PendingGrants: []team.TaskRunGrant{{TaskID: "t1"}, {TaskID: "t3-not-runnable"}},
	}}
	runnable := []team.Task{{ID: "t1"}, {ID: "t2"}}
	set := map[string]struct{}{"t1": {}, "t2": {}}
	outTasks, outSet := r.filterRunnableByGrants(state, runnable, set)
	if len(outTasks) != 1 || outTasks[0].ID != "t1" {
		t.Fatalf("expected only t1 to survive grant filter, got %#v", outTasks)
	}
	if _, ok := outSet["t2"]; ok {
		t.Fatalf("t2 has no grant and must not dispatch")
	}
	if state.Control.HasPendingGrant("t1") {
		t.Fatalf("expected t1 grant to be consumed on dispatch")
	}
	if !state.Control.HasPendingGrant("t3-not-runnable") {
		t.Fatalf("non-dispatched grants must not be consumed")
	}
}

func TestFilterRunnableByGrants_StrictWithoutControlBlocksEverything(t *testing.T) {
	r := &Runtime{controlMode: ControlModeStrict}
	state := &team.RunState{} // no Control
	runnable := []team.Task{{ID: "t1"}}
	set := map[string]struct{}{"t1": {}}
	outTasks, outSet := r.filterRunnableByGrants(state, runnable, set)
	if len(outTasks) != 0 || len(outSet) != 0 {
		t.Fatalf("Strict without Control must dispatch nothing, got %#v / %#v", outTasks, outSet)
	}
}

func TestObserveAndDecide_LegacyShortCircuits(t *testing.T) {
	r := &Runtime{controlMode: ControlModeLegacy, eventSink: &memorySink{}}
	before := team.RunState{ID: "team-1", Status: team.StatusRunning}
	after, err := r.observeAndDecide(context.Background(), before)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if after.Control != nil {
		t.Fatalf("Legacy must not create ControlState, got %#v", after.Control)
	}
}

func TestObserveAndDecide_HybridRunsObservationButClearsGrants(t *testing.T) {
	events := &memorySink{}
	r := &Runtime{controlMode: ControlModeHybrid, eventSink: events}
	state := team.RunState{
		ID:         "team-1",
		Status:     team.StatusRunning,
		Blackboard: &blackboard.State{},
		Tasks:      []team.Task{{ID: "t1", Status: team.TaskStatusPending, Version: 1}},
	}
	r.supervisorDecider = SupervisorDeciderFunc(func(_ context.Context, d SupervisorDigest, _ *blackboard.State) ([]team.SupervisorDecision, error) {
		return []team.SupervisorDecision{{
			Kind:           team.DecisionGrantRun,
			DigestSequence: d.Sequence,
			Grants:         []team.TaskRunGrant{{TaskID: "t1", Reason: "decider wanted"}},
		}}, nil
	})
	after, err := r.observeAndDecide(context.Background(), state)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if after.Control == nil {
		t.Fatalf("Hybrid must initialize ControlState")
	}
	if len(after.Control.PendingGrants) != 0 {
		t.Fatalf("Hybrid must clear grants so execution stays legacy-gated, got %#v", after.Control.PendingGrants)
	}
	if !events.contains(storage.EventSupervisorObserved) {
		t.Fatalf("Hybrid must emit SupervisorObserved, got %v", events.types())
	}
	if !events.contains(storage.EventSupervisorDecision) {
		t.Fatalf("Hybrid must emit SupervisorDecision even when gating is off, got %v", events.types())
	}
}

func TestObserveAndDecide_StrictRetainsGrantsForDispatch(t *testing.T) {
	events := &memorySink{}
	r := &Runtime{controlMode: ControlModeStrict, eventSink: events}
	state := team.RunState{
		ID:         "team-1",
		Status:     team.StatusRunning,
		Blackboard: &blackboard.State{},
		Tasks:      []team.Task{{ID: "t1", Status: team.TaskStatusPending, Version: 1}},
	}
	after, err := r.observeAndDecide(context.Background(), state)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if after.Control == nil || len(after.Control.PendingGrants) != 1 {
		t.Fatalf("Strict auto-grant must leave 1 pending grant, got %#v", after.Control)
	}
	if after.Control.PendingGrants[0].TaskID != "t1" {
		t.Fatalf("expected grant for t1, got %q", after.Control.PendingGrants[0].TaskID)
	}
	want := []storage.EventType{
		storage.EventSupervisorObserved,
		storage.EventSupervisorDecision,
		storage.EventTaskRunGranted,
	}
	got := events.types()
	if len(got) != len(want) {
		t.Fatalf("expected events %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event[%d]: expected %s, got %s (full=%v)", i, want[i], got[i], got)
		}
	}
}

func TestObserveAndDecide_AdvancesCursorAndSequence(t *testing.T) {
	r := &Runtime{controlMode: ControlModeHybrid, eventSink: &memorySink{}}
	state := team.RunState{
		ID:         "team-1",
		Status:     team.StatusRunning,
		Blackboard: &blackboard.State{},
		Tasks:      []team.Task{{ID: "t1", Status: team.TaskStatusPending, Version: 1}},
	}
	first, err := r.observeAndDecide(context.Background(), state)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if first.Control.DigestCount != 1 {
		t.Fatalf("expected DigestCount=1 after first tick, got %d", first.Control.DigestCount)
	}
	if first.Control.LastObserved.IsZero() {
		t.Fatalf("expected LastObserved to be populated")
	}
	second, err := r.observeAndDecide(context.Background(), first)
	if err != nil {
		t.Fatalf("unexpected err on tick 2: %v", err)
	}
	if second.Control.DigestCount != 2 {
		t.Fatalf("expected DigestCount=2 after second tick, got %d", second.Control.DigestCount)
	}
}
