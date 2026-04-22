package blackboard

import "testing"

func TestDetectConflicts_NoConflictOnSingleWriter(t *testing.T) {
	state := &State{
		Exchanges: []Exchange{
			{ID: "ex-1", Key: "design.doc", TaskID: "impl-1", Version: 1, Text: "alpha"},
			{ID: "ex-2", Key: "design.doc", TaskID: "impl-1", Version: 2, Text: "alpha-v2"},
		},
	}
	if conflicts := state.DetectConflicts(); len(conflicts) != 0 {
		t.Fatalf("single-writer upgrades are not conflicts, got %#v", conflicts)
	}
}

func TestDetectConflicts_MatchingTextAcrossWritersIsNotConflict(t *testing.T) {
	// Two tasks both publish the same payload under the same key —
	// redundant but consistent. Flagging this as a conflict would be
	// noise, so the detector must look at text divergence, not just
	// writer count.
	state := &State{
		Exchanges: []Exchange{
			{ID: "ex-1", Key: "design.doc", TaskID: "impl-1", Text: "same"},
			{ID: "ex-2", Key: "design.doc", TaskID: "impl-2", Text: "same"},
		},
	}
	if conflicts := state.DetectConflicts(); len(conflicts) != 0 {
		t.Fatalf("matching text across writers should not conflict, got %#v", conflicts)
	}
}

func TestDetectConflicts_DivergentTextAcrossTasksFlags(t *testing.T) {
	state := &State{
		Exchanges: []Exchange{
			{ID: "ex-1", Key: "design.doc", TaskID: "impl-1", Namespace: "shared.design", Version: 1, Text: "alpha"},
			{ID: "ex-2", Key: "design.doc", TaskID: "impl-2", Namespace: "shared.design", Version: 1, Text: "beta"},
		},
	}
	conflicts := state.DetectConflicts()
	if len(conflicts) != 1 {
		t.Fatalf("expected one conflict, got %d: %#v", len(conflicts), conflicts)
	}
	c := conflicts[0]
	if c.Key != "design.doc" {
		t.Fatalf("expected design.doc, got %q", c.Key)
	}
	if got := c.TaskIDs; len(got) != 2 || got[0] != "impl-1" || got[1] != "impl-2" {
		t.Fatalf("expected sorted task ids [impl-1 impl-2], got %#v", got)
	}
	if got := c.Namespaces; len(got) != 1 || got[0] != "shared.design" {
		t.Fatalf("expected sorted namespaces, got %#v", got)
	}
	if len(c.Exchanges) != 2 {
		t.Fatalf("expected both exchanges surfaced, got %#v", c.Exchanges)
	}
}

func TestDetectConflicts_DifferentNamespacesDoNotConflict(t *testing.T) {
	state := &State{
		Exchanges: []Exchange{
			{ID: "ex-1", Key: "verify.gate", TaskID: "verify-1", Namespace: "verify.impl-1", Text: "alpha"},
			{ID: "ex-2", Key: "verify.gate", TaskID: "verify-2", Namespace: "verify.impl-2", Text: "beta"},
		},
	}
	if conflicts := state.DetectConflicts(); len(conflicts) != 0 {
		t.Fatalf("different namespaces should stay isolated, got %#v", conflicts)
	}
}

func TestDetectConflicts_DeterministicOrder(t *testing.T) {
	// Conflicts and their exchange lists must be sorted so replaying the
	// same blackboard produces identical digests — otherwise the
	// supervisor would see ordering churn that looks like progress.
	state := &State{
		Exchanges: []Exchange{
			{ID: "ex-z", Key: "beta.key", TaskID: "task-b", Text: "2"},
			{ID: "ex-y", Key: "beta.key", TaskID: "task-a", Text: "1"},
			{ID: "ex-x", Key: "alpha.key", TaskID: "task-b", Text: "B"},
			{ID: "ex-w", Key: "alpha.key", TaskID: "task-a", Text: "A"},
		},
	}
	conflicts := state.DetectConflicts()
	if len(conflicts) != 2 {
		t.Fatalf("expected two conflicts, got %#v", conflicts)
	}
	if conflicts[0].Key != "alpha.key" || conflicts[1].Key != "beta.key" {
		t.Fatalf("expected conflicts sorted by key, got %s then %s", conflicts[0].Key, conflicts[1].Key)
	}
	if conflicts[0].Exchanges[0].TaskID != "task-a" {
		t.Fatalf("expected exchanges sorted by task id, got %#v", conflicts[0].Exchanges)
	}
}

func TestDetectConflicts_IgnoresEmptyBoard(t *testing.T) {
	if conflicts := (&State{}).DetectConflicts(); len(conflicts) != 0 {
		t.Fatalf("empty board has no conflicts, got %#v", conflicts)
	}
	var nilState *State
	if conflicts := nilState.DetectConflicts(); len(conflicts) != 0 {
		t.Fatalf("nil state must not panic or fabricate conflicts, got %#v", conflicts)
	}
}

func TestDetectConflicts_ExcerptTruncation(t *testing.T) {
	longText := make([]rune, conflictExcerptMaxRunes+50)
	for i := range longText {
		longText[i] = 'x'
	}
	otherText := make([]rune, conflictExcerptMaxRunes+50)
	for i := range otherText {
		otherText[i] = 'y'
	}
	state := &State{
		Exchanges: []Exchange{
			{ID: "ex-1", Key: "notes", TaskID: "a", Text: string(longText)},
			{ID: "ex-2", Key: "notes", TaskID: "b", Text: string(otherText)},
		},
	}
	conflicts := state.DetectConflicts()
	if len(conflicts) != 1 {
		t.Fatalf("expected one conflict, got %#v", conflicts)
	}
	for _, ex := range conflicts[0].Exchanges {
		runes := []rune(ex.Excerpt)
		if len(runes) > conflictExcerptMaxRunes+1 { // +1 for ellipsis
			t.Fatalf("excerpt should be truncated, got %d runes", len(runes))
		}
	}
}
