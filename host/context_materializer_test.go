package host

import (
	"testing"

	"github.com/Viking602/go-hydaelyn/internal/blackboard"
	"github.com/Viking602/go-hydaelyn/team"
)

func TestSelectorsForTask_LegacyReadsFallBackToLegacySelector(t *testing.T) {
	task := team.Task{Reads: []string{"notes.alpha", "notes.beta"}}
	selectors := selectorsForTask(task)
	if len(selectors) != 2 {
		t.Fatalf("expected one selector per legacy read, got %d", len(selectors))
	}
	for _, sel := range selectors {
		if !sel.IncludeText || !sel.IncludeStructured || !sel.IncludeArtifacts {
			t.Fatalf("expected legacy selectors to include all payload kinds, got %#v", sel)
		}
		if !sel.Required {
			t.Fatalf("expected legacy selectors to be required by default, got %#v", sel)
		}
	}
}

func TestSelectorsForTask_ExplicitReadSelectorsWinOverReadsList(t *testing.T) {
	task := team.Task{
		Reads: []string{"notes.legacy"},
		ReadSelectors: []blackboard.ExchangeSelector{
			{Keys: []string{"notes.explicit"}, RequireVerified: true},
		},
	}
	selectors := selectorsForTask(task)
	if len(selectors) != 1 || selectors[0].Keys[0] != "notes.explicit" {
		t.Fatalf("expected explicit selectors to override legacy Reads list, got %#v", selectors)
	}
	if !selectors[0].RequireVerified {
		t.Fatalf("expected explicit selectors to preserve RequireVerified, got %#v", selectors[0])
	}
}

func TestSelectorsForTask_GuardedSynthesizeForcesRequireVerified(t *testing.T) {
	task := team.Task{
		Kind:             team.TaskKindSynthesize,
		VerifierRequired: true,
		Reads:            []string{"notes.alpha"},
	}
	selectors := selectorsForTask(task)
	if len(selectors) != 1 {
		t.Fatalf("expected one selector, got %d", len(selectors))
	}
	if !selectors[0].RequireVerified {
		t.Fatalf("expected guarded synthesize to lift RequireVerified on legacy selectors, got %#v", selectors[0])
	}
}

func TestMaterializeSelectors_RequireVerifiedFiltersUnlinkedExchanges(t *testing.T) {
	board := &blackboard.State{
		Claims: []blackboard.Claim{{ID: "claim-1"}},
		Verifications: []blackboard.VerificationResult{
			{ClaimID: "claim-1", Status: blackboard.VerificationStatusSupported, Confidence: 0.9, EvidenceIDs: []string{"ev-1"}},
		},
		Exchanges: []blackboard.Exchange{
			{ID: "ex-1", Key: "notes", Text: "unverified"},
			{ID: "ex-2", Key: "notes", Text: "verified", ClaimIDs: []string{"claim-1"}},
		},
	}
	ctx := MaterializeSelectors(board, []blackboard.ExchangeSelector{
		{Keys: []string{"notes"}, RequireVerified: true, IncludeText: true},
	})
	if len(ctx.Exchanges) != 1 || ctx.Exchanges[0].Text != "verified" {
		t.Fatalf("expected only verified exchange, got %#v", ctx.Exchanges)
	}
	if len(ctx.Misses) != 0 {
		t.Fatalf("expected selector hit to clear misses, got %#v", ctx.Misses)
	}
}

func TestMaterializeSelectors_MissReportedWhenSelectorEmpty(t *testing.T) {
	board := &blackboard.State{}
	ctx := MaterializeSelectors(board, []blackboard.ExchangeSelector{
		{Keys: []string{"missing.key"}, Required: true, Label: "missing.key"},
	})
	if len(ctx.Exchanges) != 0 {
		t.Fatalf("expected no exchanges, got %#v", ctx.Exchanges)
	}
	if len(ctx.Misses) != 1 || ctx.Misses[0].Selector.Label != "missing.key" {
		t.Fatalf("expected one miss for the required selector, got %#v", ctx.Misses)
	}
}

func TestMaterializeSelectors_DedupsAcrossSelectors(t *testing.T) {
	// Two selectors that both match the same exchange must not double-count
	// it; otherwise prompts could inflate the same evidence twice and
	// confidence weighting downstream would be fooled by apparent redundancy.
	board := &blackboard.State{
		Exchanges: []blackboard.Exchange{
			{ID: "ex-1", Key: "notes", Namespace: "research.notes", Text: "alpha"},
		},
	}
	selectors := []blackboard.ExchangeSelector{
		{Keys: []string{"notes"}, IncludeText: true},
		{Namespaces: []string{"research.notes"}, IncludeText: true},
	}
	ctx := MaterializeSelectors(board, selectors)
	if len(ctx.Exchanges) != 1 {
		t.Fatalf("expected a single materialized exchange after dedup, got %#v", ctx.Exchanges)
	}
}
