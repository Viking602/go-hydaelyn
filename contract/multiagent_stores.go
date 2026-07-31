package contract

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Viking602/venat/api"
)

// ─── Multi-agent store suite ────────────────────────────────────────────
//
// Conformance tests for the three v0.8.0 multi-agent stores. Required —
// no capability gate (spec 07 §"New store contracts"; ADR-016 §6).

func runMultiAgentStoreSuite(t *testing.T, factory ProviderFactory) {
	t.Helper()
	runSuite(t, factory, []suiteCase{
		{"TestSaveAndLoad_Handoff", testSaveAndLoadHandoff},
		{"TestSaveHandoff_DuplicateIDRejected", testSaveHandoffDuplicateRejected},
		{"TestListHandoffs_SelectorFilters", testListHandoffsSelector},
		{"TestTeamState_LatestSnapshotWins", testTeamStateLatestWins},
		{"TestTeamState_MissingRunReturnsNotFound", testTeamStateNotFound},
		{"TestAgentInstance_UpsertOnID", testAgentInstanceUpsert},
		{"TestListAgentInstances_SelectorFilters", testListAgentInstancesSelector},
	})
}

func sampleHandoff(runID, id, from, to string, at time.Time) api.HandoffRecord {
	return api.HandoffRecord{
		ID:                   id,
		RunID:                runID,
		From:                 from,
		To:                   to,
		Reason:               "verify claim",
		Payload:              json.RawMessage(`{"claim":"p<0.05"}`),
		EvidenceIDs:          []string{"ev-1"},
		RequiredOutputSchema: json.RawMessage(`{"type":"object"}`),
		CreatedAt:            at,
	}
}

func testSaveAndLoadHandoff(t *testing.T, factory ProviderFactory) {
	p := newProvider(t, factory)
	ctx := context.Background()
	want := sampleHandoff("run-h1", "h-1", "researcher", "verifier", time.Now().UTC().Truncate(time.Millisecond))

	withUoW(t, p, func(uow api.UnitOfWork) error {
		return uow.Handoffs().SaveHandoff(ctx, want)
	})

	withUoW(t, p, func(uow api.UnitOfWork) error {
		got, err := uow.Handoffs().LoadHandoff(ctx, "run-h1", "h-1")
		if err != nil {
			return err
		}
		if got.ID != want.ID || got.RunID != want.RunID || got.From != want.From || got.To != want.To {
			t.Fatalf("LoadHandoff = %+v, want %+v", got, want)
		}
		if string(got.Payload) != string(want.Payload) || string(got.RequiredOutputSchema) != string(want.RequiredOutputSchema) {
			t.Fatalf("handoff payload/schema not round-tripped: %+v", got)
		}
		return nil
	})
}

func testSaveHandoffDuplicateRejected(t *testing.T, factory ProviderFactory) {
	p := newProvider(t, factory)
	ctx := context.Background()
	record := sampleHandoff("run-h2", "h-dup", "a", "b", time.Now().UTC())

	withUoW(t, p, func(uow api.UnitOfWork) error {
		return uow.Handoffs().SaveHandoff(ctx, record)
	})

	uow, err := p.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	if err := uow.Handoffs().SaveHandoff(ctx, record); err == nil {
		t.Fatal("SaveHandoff accepted a duplicate (runID, handoffID); the store is append-only")
	}
}

func testListHandoffsSelector(t *testing.T, factory ProviderFactory) {
	p := newProvider(t, factory)
	ctx := context.Background()
	base := time.Now().UTC().Truncate(time.Millisecond)

	withUoW(t, p, func(uow api.UnitOfWork) error {
		// Persisted deliberately out of ID order: List must sort by ID
		// (spec 07 — ULID ascending), not echo persistence order.
		for _, record := range []api.HandoffRecord{
			sampleHandoff("run-sel", "h-2", "alpha", "gamma", base),
			sampleHandoff("run-sel", "h-3", "delta", "beta", base),
			sampleHandoff("run-sel", "h-1", "alpha", "beta", base.Add(-2*time.Hour)),
			sampleHandoff("run-other", "h-4", "alpha", "beta", base),
		} {
			if err := uow.Handoffs().SaveHandoff(ctx, record); err != nil {
				return err
			}
		}
		return nil
	})

	withUoW(t, p, func(uow api.UnitOfWork) error {
		byRun, err := uow.Handoffs().ListHandoffs(ctx, api.HandoffSelector{RunID: "run-sel"})
		if err != nil {
			return err
		}
		if len(byRun) != 3 {
			t.Fatalf("RunID filter returned %d records, want 3", len(byRun))
		}
		for i, want := range []string{"h-1", "h-2", "h-3"} {
			if byRun[i].ID != want {
				t.Fatalf("ListHandoffs order = [%s %s %s], want ID-ascending [h-1 h-2 h-3]", byRun[0].ID, byRun[1].ID, byRun[2].ID)
			}
		}
		fromAlpha, err := uow.Handoffs().ListHandoffs(ctx, api.HandoffSelector{RunID: "run-sel", From: "alpha"})
		if err != nil {
			return err
		}
		if len(fromAlpha) != 2 {
			t.Fatalf("From filter returned %d records, want 2", len(fromAlpha))
		}
		recent, err := uow.Handoffs().ListHandoffs(ctx, api.HandoffSelector{RunID: "run-sel", Since: base.Add(-time.Hour)})
		if err != nil {
			return err
		}
		if len(recent) != 2 {
			t.Fatalf("Since filter returned %d records, want 2", len(recent))
		}
		toBeta, err := uow.Handoffs().ListHandoffs(ctx, api.HandoffSelector{RunID: "run-sel", To: "beta", From: "delta"})
		if err != nil {
			return err
		}
		if len(toBeta) != 1 || toBeta[0].ID != "h-3" {
			t.Fatalf("AND-combined From+To filter = %+v, want exactly h-3", toBeta)
		}
		return nil
	})
}

func testTeamStateLatestWins(t *testing.T, factory ProviderFactory) {
	p := newProvider(t, factory)
	ctx := context.Background()

	withUoW(t, p, func(uow api.UnitOfWork) error {
		return uow.TeamStates().SaveTeamState(ctx, api.TeamStateRecord{
			RunID: "run-ts", Tick: 1, State: json.RawMessage(`{"tick":1}`), UpdatedAt: time.Now().UTC(),
		})
	})
	withUoW(t, p, func(uow api.UnitOfWork) error {
		return uow.TeamStates().SaveTeamState(ctx, api.TeamStateRecord{
			RunID: "run-ts", Tick: 2, State: json.RawMessage(`{"tick":2}`), UpdatedAt: time.Now().UTC(),
		})
	})

	withUoW(t, p, func(uow api.UnitOfWork) error {
		got, err := uow.TeamStates().LoadTeamState(ctx, "run-ts")
		if err != nil {
			return err
		}
		if got.Tick != 2 || string(got.State) != `{"tick":2}` {
			t.Fatalf("LoadTeamState = %+v, want the tick-2 snapshot (latest overwrite wins)", got)
		}
		return nil
	})
}

func testTeamStateNotFound(t *testing.T, factory ProviderFactory) {
	p := newProvider(t, factory)
	ctx := context.Background()

	uow, err := p.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = uow.Rollback(ctx) }()
	if _, err := uow.TeamStates().LoadTeamState(ctx, "run-missing"); !errors.Is(err, api.ErrNotFound) {
		t.Fatalf("LoadTeamState(missing) error = %v, want api.ErrNotFound", err)
	}
}

func testAgentInstanceUpsert(t *testing.T, factory ProviderFactory) {
	p := newProvider(t, factory)
	ctx := context.Background()
	created := time.Now().UTC().Truncate(time.Millisecond)

	withUoW(t, p, func(uow api.UnitOfWork) error {
		return uow.AgentInstances().SaveAgentInstance(ctx, api.AgentInstanceRecord{
			ID: "ai-1", ClassName: "verifier", RunID: "run-ai", TaskID: "t-1", State: "running", CreatedAt: created,
		})
	})
	withUoW(t, p, func(uow api.UnitOfWork) error {
		return uow.AgentInstances().SaveAgentInstance(ctx, api.AgentInstanceRecord{
			ID: "ai-1", ClassName: "verifier", RunID: "run-ai", TaskID: "t-1", State: "finished", CreatedAt: created,
		})
	})

	withUoW(t, p, func(uow api.UnitOfWork) error {
		got, err := uow.AgentInstances().LoadAgentInstance(ctx, "ai-1")
		if err != nil {
			return err
		}
		if got.State != "finished" {
			t.Fatalf("LoadAgentInstance.State = %q, want finished (Save upserts on ID)", got.State)
		}
		return nil
	})
}

func testListAgentInstancesSelector(t *testing.T, factory ProviderFactory) {
	p := newProvider(t, factory)
	ctx := context.Background()
	now := time.Now().UTC()

	withUoW(t, p, func(uow api.UnitOfWork) error {
		for _, record := range []api.AgentInstanceRecord{
			{ID: "ai-a", ClassName: "research", RunID: "run-l", State: "finished", CreatedAt: now},
			{ID: "ai-b", ClassName: "research", RunID: "run-l", State: "failed", CreatedAt: now},
			{ID: "ai-c", ClassName: "write", RunID: "run-l", State: "finished", CreatedAt: now},
			{ID: "ai-d", ClassName: "research", RunID: "run-x", State: "finished", CreatedAt: now},
		} {
			if err := uow.AgentInstances().SaveAgentInstance(ctx, record); err != nil {
				return err
			}
		}
		return nil
	})

	withUoW(t, p, func(uow api.UnitOfWork) error {
		byRun, err := uow.AgentInstances().ListAgentInstances(ctx, api.AgentInstanceSelector{RunID: "run-l"})
		if err != nil {
			return err
		}
		if len(byRun) != 3 {
			t.Fatalf("RunID filter returned %d records, want 3", len(byRun))
		}
		finishedResearch, err := uow.AgentInstances().ListAgentInstances(ctx, api.AgentInstanceSelector{
			RunID: "run-l", ClassName: "research", State: "finished",
		})
		if err != nil {
			return err
		}
		if len(finishedResearch) != 1 || finishedResearch[0].ID != "ai-a" {
			t.Fatalf("AND-combined selector = %+v, want exactly ai-a", finishedResearch)
		}
		return nil
	})
}
