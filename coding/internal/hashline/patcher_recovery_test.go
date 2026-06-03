package hashline

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
)

// recoveryWarned reports whether any section carries the 3-way recovery
// warning surfaced on a successful stale recovery.
func recoveryWarned(res ApplyPatchResult) bool {
	for _, s := range res.Sections {
		for _, w := range s.Warnings {
			if strings.Contains(w, "3-way merge") {
				return true
			}
		}
	}
	return false
}

func TestPatcher_Recovery_NonConflicting(t *testing.T) {
	// Base the model read. We record it so the store can serve it by hash.
	const base = "line1\nline2\nline3\nline4\n"
	store := NewMemorySnapshotStore()
	baseTag := store.Record("f.go", base)

	// The live file diverged on an UNRELATED line (line4 changed out-of-band)
	// after the model read but before it edited.
	const live = "line1\nline2\nline3\nLINE4-CHANGED\n"
	fs := newFakeFS(map[string]string{"f.go": live})
	p := &Patcher{FS: fs, Snapshots: store}

	// The model edits line2 against the (now stale) base tag.
	patch := Patch{Sections: []Section{{
		Path: "f.go",
		Tag:  baseTag, // stale: live tag differs
		Ops:  []Op{{Kind: OpReplace, Start: 2, End: 2, Body: []string{"LINE2-EDITED"}}},
	}}}

	res, err := p.Apply(context.Background(), patch)
	if err != nil {
		t.Fatalf("expected recovery to succeed, got %v", err)
	}
	// Both the concurrent change and the model's edit must be present.
	want := "line1\nLINE2-EDITED\nline3\nLINE4-CHANGED\n"
	if fs.files["f.go"] != want {
		t.Errorf("merged file = %q, want %q", fs.files["f.go"], want)
	}
	if !recoveryWarned(res) {
		t.Errorf("recovered section must carry a 3-way merge warning: %#v", res.Sections)
	}
	// The result's NewTag must reflect the merged content.
	if res.Sections[0].NewTag != ComputeFileHash(want) {
		t.Errorf("NewTag = %s, want tag of merged text", res.Sections[0].NewTag)
	}
}

func TestPatcher_Recovery_Conflict(t *testing.T) {
	const base = "alpha\nbeta\ngamma\n"
	store := NewMemorySnapshotStore()
	baseTag := store.Record("f.go", base)

	// Live changed the SAME line the model is about to edit, differently.
	const live = "alpha\nBETA-FROM-DISK\ngamma\n"
	fs := newFakeFS(map[string]string{"f.go": live})
	p := &Patcher{FS: fs, Snapshots: store}

	patch := Patch{Sections: []Section{{
		Path: "f.go",
		Tag:  baseTag,
		Ops:  []Op{{Kind: OpReplace, Start: 2, End: 2, Body: []string{"BETA-FROM-MODEL"}}},
	}}}

	_, err := p.Apply(context.Background(), patch)
	if !errors.Is(err, ErrSnapshotMismatch) {
		t.Fatalf("conflict must reject with ErrSnapshotMismatch, got %v", err)
	}
	if !errors.Is(err, ErrRecoveryConflict) {
		t.Errorf("conflict should also match ErrRecoveryConflict, got %v", err)
	}
	if fs.files["f.go"] != live {
		t.Errorf("file must be untouched on conflict: %q", fs.files["f.go"])
	}
	if len(fs.writeCallOrder) != 0 {
		t.Errorf("no writes on conflict, got %v", fs.writeCallOrder)
	}
}

func TestPatcher_Recovery_NoHistoryStillRejects(t *testing.T) {
	// A store with no record of the stale tag must behave exactly like the
	// first release: stale-reject with ErrSnapshotMismatch (NOT a conflict).
	const live = "a\nb\nc\n"
	store := NewMemorySnapshotStore() // empty
	fs := newFakeFS(map[string]string{"f.go": live})
	p := &Patcher{FS: fs, Snapshots: store}

	patch := Patch{Sections: []Section{{
		Path: "f.go",
		Tag:  "DEAD", // never recorded
		Ops:  []Op{{Kind: OpReplace, Start: 1, End: 1, Body: []string{"X"}}},
	}}}

	_, err := p.Apply(context.Background(), patch)
	if !errors.Is(err, ErrSnapshotMismatch) {
		t.Fatalf("want ErrSnapshotMismatch, got %v", err)
	}
	if errors.Is(err, ErrRecoveryConflict) {
		t.Error("a missing-history reject must not be reported as a recovery conflict")
	}
	if !strings.Contains(err.Error(), "re-read") {
		t.Errorf("reject should instruct a re-read: %v", err)
	}
	if fs.files["f.go"] != live {
		t.Errorf("file mutated: %q", fs.files["f.go"])
	}
}

func TestPatcher_Recovery_NilStoreIdenticalToStaleReject(t *testing.T) {
	// CRITICAL: with a nil store, the stale path must be byte-for-byte the
	// first-release behavior — a plain ErrSnapshotMismatch, no recovery.
	const live = "a\nb\nc\n"
	fs := newFakeFS(map[string]string{"f.go": live})
	p := &Patcher{FS: fs, Snapshots: nil}

	patch := Patch{Sections: []Section{{
		Path: "f.go",
		Tag:  "BEEF",
		Ops:  []Op{{Kind: OpReplace, Start: 1, End: 1, Body: []string{"X"}}},
	}}}

	_, err := p.Apply(context.Background(), patch)
	if !errors.Is(err, ErrSnapshotMismatch) {
		t.Fatalf("nil store stale edit must reject with ErrSnapshotMismatch, got %v", err)
	}
	if errors.Is(err, ErrRecoveryConflict) {
		t.Error("nil store must never enter the recovery path")
	}
	if fs.files["f.go"] != live {
		t.Errorf("file mutated under nil store: %q", fs.files["f.go"])
	}
}

func TestPatcher_Recovery_LazyStoreIdenticalToStaleReject(t *testing.T) {
	// The LazySnapshotStore's ByHash always returns false, so it too must
	// fall straight through to stale-reject.
	const live = "a\nb\nc\n"
	fs := newFakeFS(map[string]string{"f.go": live})
	p := &Patcher{FS: fs, Snapshots: LazySnapshotStore{}}

	patch := Patch{Sections: []Section{{
		Path: "f.go",
		Tag:  "BEEF",
		Ops:  []Op{{Kind: OpReplace, Start: 1, End: 1, Body: []string{"X"}}},
	}}}

	_, err := p.Apply(context.Background(), patch)
	if !errors.Is(err, ErrSnapshotMismatch) {
		t.Fatalf("lazy store stale edit must reject with ErrSnapshotMismatch, got %v", err)
	}
	if errors.Is(err, ErrRecoveryConflict) {
		t.Error("lazy store must never enter the recovery path")
	}
}

func TestPatcher_Recovery_AfterCommitRecordsHistory(t *testing.T) {
	// End-to-end: a clean commit records the new version, so a SUBSEQUENT
	// edit submitted against the now-old tag (because the file changed again
	// out-of-band) can be recovered against the committed version.
	const v0 = "p\nq\nr\n"
	store := NewMemorySnapshotStore()
	tag0 := store.Record("f.go", v0)
	fs := newFakeFS(map[string]string{"f.go": v0})
	p := &Patcher{FS: fs, Snapshots: store}

	// First edit: change line1 with the current tag. This commits and records
	// the new version (v1).
	patch1 := Patch{Sections: []Section{{
		Path: "f.go", Tag: tag0,
		Ops: []Op{{Kind: OpReplace, Start: 1, End: 1, Body: []string{"P"}}},
	}}}
	res1, err := p.Apply(context.Background(), patch1)
	if err != nil {
		t.Fatalf("first apply: %v", err)
	}
	v1Tag := res1.Sections[0].NewTag
	if _, ok := store.ByHash("f.go", v1Tag); !ok {
		t.Fatal("commit must record the new version in history")
	}

	// The file changes out-of-band on an unrelated line (line3 -> R).
	const v2 = "P\nq\nR\n"
	fs.files["f.go"] = v2

	// Second edit references v1's tag (stale now), editing line2. Recover.
	patch2 := Patch{Sections: []Section{{
		Path: "f.go", Tag: v1Tag,
		Ops: []Op{{Kind: OpReplace, Start: 2, End: 2, Body: []string{"Q"}}},
	}}}
	res2, err := p.Apply(context.Background(), patch2)
	if err != nil {
		t.Fatalf("second apply (recovery) failed: %v", err)
	}
	want := "P\nQ\nR\n"
	if fs.files["f.go"] != want {
		t.Errorf("recovered file = %q, want %q", fs.files["f.go"], want)
	}
	if !recoveryWarned(res2) {
		t.Error("second edit should be a recovered stale edit")
	}
}

func TestPatcher_Recovery_MergeReproducesLiveIsNoop(t *testing.T) {
	// If the model's edit, re-applied to the recorded base and merged with the
	// live file, reproduces the live file exactly (e.g. the same change was
	// already made on disk), the recovery is a no-op.
	const base = "a\nb\nc\n"
	store := NewMemorySnapshotStore()
	baseTag := store.Record("f.go", base)

	// Live already contains the exact change the model is about to make.
	const live = "a\nB\nc\n"
	fs := newFakeFS(map[string]string{"f.go": live})
	p := &Patcher{FS: fs, Snapshots: store}

	patch := Patch{Sections: []Section{{
		Path: "f.go", Tag: baseTag,
		Ops: []Op{{Kind: OpReplace, Start: 2, End: 2, Body: []string{"B"}}},
	}}}
	_, err := p.Apply(context.Background(), patch)
	if !errors.Is(err, ErrNoop) {
		t.Fatalf("merge reproducing the live file should be ErrNoop, got %v", err)
	}
	if fs.files["f.go"] != live {
		t.Errorf("no-op must not mutate: %q", fs.files["f.go"])
	}
}

func TestPatcher_Recovery_MultiSectionOneRecovers(t *testing.T) {
	// A patch with one clean section and one recoverable stale section must
	// apply both (all-or-nothing still holds: both succeed together).
	store := NewMemorySnapshotStore()

	const aBase = "a1\na2\n"
	aTag := store.Record("a.go", aBase)
	const aLive = "a1\nA2-DISK\n" // a.go changed out-of-band on line2
	const bLive = "b1\nb2\n"
	bTag := ComputeFileHash(bLive)

	fs := newFakeFS(map[string]string{"a.go": aLive, "b.go": bLive})
	p := &Patcher{FS: fs, Snapshots: store}

	patch := Patch{Sections: []Section{
		// a.go: stale (edits line1, recoverable against the line2 disk change).
		{Path: "a.go", Tag: aTag, Ops: []Op{{Kind: OpReplace, Start: 1, End: 1, Body: []string{"A1"}}}},
		// b.go: clean (current tag).
		{Path: "b.go", Tag: bTag, Ops: []Op{{Kind: OpReplace, Start: 1, End: 1, Body: []string{"B1"}}}},
	}}

	res, err := p.Apply(context.Background(), patch)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if fs.files["a.go"] != "A1\nA2-DISK\n" {
		t.Errorf("a.go = %q, want recovered merge", fs.files["a.go"])
	}
	if fs.files["b.go"] != "B1\nb2\n" {
		t.Errorf("b.go = %q, want clean edit", fs.files["b.go"])
	}
	if !recoveryWarned(res) {
		t.Error("the stale section should report a recovery warning")
	}
}

func TestPatcher_Recovery_CRLFBOMRoundTrip(t *testing.T) {
	// A recovered merge must restore the live file's BOM and CRLF line ending.
	// The store records the LF-normalized base (as Record always normalizes);
	// the live file on disk is BOM+CRLF and diverged out-of-band on line 3.
	const bom = "\uFEFF" // UTF-8 BOM written as an escape (no literal BOM in source)
	baseLF := "line1\nline2\nline3\n"
	store := NewMemorySnapshotStore()
	baseTag := store.Record("w.go", baseLF)

	liveDisk := bom + "line1\r\nline2\r\nLINE3-DISK\r\n"
	fs := newFakeFS(map[string]string{"w.go": liveDisk})
	p := &Patcher{FS: fs, Snapshots: store}

	// Model edits line2 against the stale base tag.
	patch := Patch{Sections: []Section{{
		Path: "w.go", Tag: baseTag,
		Ops: []Op{{Kind: OpReplace, Start: 2, End: 2, Body: []string{"LINE2-EDIT"}}},
	}}}

	if _, err := p.Apply(context.Background(), patch); err != nil {
		t.Fatalf("recovery should succeed: %v", err)
	}
	got := fs.files["w.go"]
	// The merged content carries both edits AND the on-disk BOM + CRLF shape.
	want := bom + "line1\r\nLINE2-EDIT\r\nLINE3-DISK\r\n"
	if got != want {
		t.Errorf("recovered file = %q, want %q", got, want)
	}
}

func TestPatcher_Recovery_MultiVersionBase(t *testing.T) {
	// The recorded history holds several versions; the model's stale tag refers
	// to an INTERMEDIATE (non-head) version. Recovery must merge against exactly
	// that version, not the head.
	store := NewMemorySnapshotStore()
	const v0 = "a\nb\nc\nd\n"
	const v1 = "a\nb1\nc\nd\n"  // line2 changed
	const v2 = "a\nb1\nc2\nd\n" // line3 changed
	store.Record("m.go", v0)
	v1Tag := store.Record("m.go", v1)
	store.Record("m.go", v2)

	// Live diverged further on line1 (out-of-band) from v2; the model edits
	// line4, which neither out-of-band change touched.
	const live = "A\nb1\nc2\nd\n"
	fs := newFakeFS(map[string]string{"m.go": live})
	p := &Patcher{FS: fs, Snapshots: store}

	// Model read v1 (tag v1Tag, now stale twice over) and edits line4.
	patch := Patch{Sections: []Section{{
		Path: "m.go", Tag: v1Tag,
		Ops: []Op{{Kind: OpReplace, Start: 4, End: 4, Body: []string{"D-EDIT"}}},
	}}}

	res, err := p.Apply(context.Background(), patch)
	if err != nil {
		t.Fatalf("multi-version recovery should succeed: %v", err)
	}
	// base=v1 (a,b1,c,d), ours=live (A,b1,c2,d), theirs=desired (a,b1,c,D-EDIT).
	// Merge against base=v1: line1 only-ours (A), line3 only-ours (c2),
	// line4 only-theirs (D-EDIT). line2 unchanged.
	want := "A\nb1\nc2\nD-EDIT\n"
	if fs.files["m.go"] != want {
		t.Errorf("recovered file = %q, want %q", fs.files["m.go"], want)
	}
	if !recoveryWarned(res) {
		t.Error("multi-version recovery must carry the recovery warning")
	}
}

func TestPatcher_Recovery_FallsBackToRejectAfterByHashEviction(t *testing.T) {
	// If the model's base version has aged out of the per-path version history,
	// ByHash no longer finds it and recovery must fall back to stale-reject
	// (never a conflict, never a silent apply).
	store := NewMemorySnapshotStore()
	const v0 = "v0-content\n"
	v0Tag := store.Record("e.go", v0)
	// Push enough distinct versions to evict v0 from the per-path ring.
	for i := 0; i < maxVersionsPerPath+1; i++ {
		store.Record("e.go", "ver"+strconv.Itoa(i)+"\n")
	}
	if _, ok := store.ByHash("e.go", v0Tag); ok {
		t.Fatal("precondition: v0 should have been evicted from the version ring")
	}

	const live = "live-now\n"
	fs := newFakeFS(map[string]string{"e.go": live})
	p := &Patcher{FS: fs, Snapshots: store}
	patch := Patch{Sections: []Section{{
		Path: "e.go", Tag: v0Tag, // base no longer retained
		Ops: []Op{{Kind: OpReplace, Start: 1, End: 1, Body: []string{"X"}}},
	}}}

	_, err := p.Apply(context.Background(), patch)
	if !errors.Is(err, ErrSnapshotMismatch) {
		t.Fatalf("evicted base must stale-reject, got %v", err)
	}
	if errors.Is(err, ErrRecoveryConflict) {
		t.Error("an evicted base is a plain reject, not a recovery conflict")
	}
	if fs.files["e.go"] != live {
		t.Errorf("file mutated after eviction reject: %q", fs.files["e.go"])
	}
}

func TestPatcher_Recovery_EditNoLongerAppliesToBase(t *testing.T) {
	// If the recorded base is present but the edit's line range is out of
	// bounds for that base (e.g. the base was shorter), recovery cannot
	// re-apply the edit and must fall back to stale-reject.
	const base = "only-one-line\n"
	store := NewMemorySnapshotStore()
	baseTag := store.Record("f.go", base)

	const live = "only-one-line\nplus-a-second\n"
	fs := newFakeFS(map[string]string{"f.go": live})
	p := &Patcher{FS: fs, Snapshots: store}

	patch := Patch{Sections: []Section{{
		Path: "f.go", Tag: baseTag,
		// Line 5 does not exist in the 1-line recorded base.
		Ops: []Op{{Kind: OpReplace, Start: 5, End: 5, Body: []string{"X"}}},
	}}}
	_, err := p.Apply(context.Background(), patch)
	if !errors.Is(err, ErrSnapshotMismatch) {
		t.Fatalf("want ErrSnapshotMismatch when the edit no longer applies to the base, got %v", err)
	}
	if fs.files["f.go"] != live {
		t.Errorf("file mutated: %q", fs.files["f.go"])
	}
}
