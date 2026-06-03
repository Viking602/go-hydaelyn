package hashline

import (
	"math/rand"
	"strconv"
	"strings"
	"testing"
)

// The oracles in this file are deliberately INDEPENDENT of the production merge:
// they never call decompose, lcsMatches, equalLines, or threeWayMerge's
// resolution helpers. The differential and overlapping fuzzers generate bases of
// DISTINCT lines, which makes alignment unambiguous, so the oracle can align
// each side to the base by exact line identity (identityBlocks) instead of LCS.
// A bug in the production LCS/decomposition therefore shows up as a disagreement
// with an oracle that shares none of that code. A separate duplicate-line fuzz
// (TestThreeWayMerge_DuplicateLineIdentitiesFuzz) stresses lcsMatches on
// ambiguous bases via alignment-independent identities.

// TestThreeWayMerge_OverlappingDeleteConflictFuzz hammers the case the naive
// per-line model got wrong: both sides delete or replace OVERLAPPING multi-line
// spans of the base with differing effects. Because the spans share at least
// one base line and the two sides disagree about the surrounding lines, the
// merge must report a conflict — never silently pick one side and drop the
// other's intent.
func TestThreeWayMerge_OverlappingDeleteConflictFuzz(t *testing.T) {
	rng := rand.New(rand.NewSource(0xD17FB17))
	const iters = 50000
	conflicts := 0

	for iter := 0; iter < iters; iter++ {
		n := 4 + rng.Intn(8)
		base := make([]string, n)
		for i := range base {
			base[i] = "L" + strconv.Itoa(iter) + "_" + strconv.Itoa(i)
		}

		// Pick two overlapping spans [oLo,oHi] and [tLo,tHi] that share at least
		// one base line.
		oLo := rng.Intn(n)
		oHi := oLo + rng.Intn(n-oLo)
		// theirs span must overlap ours: start somewhere within ours' span.
		tLo := oLo + rng.Intn(oHi-oLo+1)
		tHi := tLo + rng.Intn(n-tLo)

		// Each side either deletes its span or replaces it with distinct content.
		ours := editSpan(base, oLo, oHi, rng, "O")
		their := editSpan(base, tLo, tHi, rng, "T")

		// Make the two sides genuinely disagree over the overlapping region:
		// force ours and theirs to differ on the shared span so this is a real
		// incompatible overlap (identical edits are a separate, mergeable case).
		if strings.Join(ours, "\n") == strings.Join(their, "\n") {
			continue
		}
		// The two changed-line sets must actually overlap. (Disjoint random spans
		// can still merge cleanly; those are covered by the disjoint fuzz.)
		oSet := independentChangedSet(base, ours)
		tSet := independentChangedSet(base, their)
		if !setsOverlap(oSet, tSet) {
			continue
		}
		// If the independent oracle says these overlapping edits are still
		// mergeable (the sides happened to produce identical run text), skip —
		// this fuzz only asserts the must-conflict direction.
		if _, oracleConflict := independentMerge(base, ours, their); !oracleConflict {
			continue
		}

		got := threeWayMerge(strings.Join(base, "\n"), strings.Join(ours, "\n"), strings.Join(their, "\n"))
		if !got.Conflict {
			t.Fatalf("iter %d: overlapping incompatible edits not a conflict\nbase=%q\nours=%q\ntheir=%q\nmerged=%q",
				iter, base, ours, their, got.Text)
		}
		conflicts++
	}
	if conflicts == 0 {
		t.Fatal("expected to exercise overlapping-edit conflicts")
	}
}

// TestThreeWayMerge_NoSilentDataLossFuzz asserts the soundness invariant the
// run-grouping fix enforces: the merge agrees with an INDEPENDENT run-level
// oracle on both the conflict verdict and, when there is no conflict, the merged
// text. The oracle (independentMerge) aligns each side to the base by exact line
// identity — the bases here are distinct lines, so identity alignment is exact
// and shares no code with the production LCS/decomposition. It then walks
// maximal runs of consecutive changed base lines: a run touched by only one side
// takes that side, a co-edited run (any line changed by both) is taken only when
// the two sides produced identical text over the WHOLE run — otherwise conflict.
//
// This directly catches the historical bug: overlapping multi-line deletions
// (e.g. ours deletes b,c,d while theirs deletes only c) co-edit a run yet differ
// over it, so a clean merge there is unsound.
func TestThreeWayMerge_NoSilentDataLossFuzz(t *testing.T) {
	rng := rand.New(rand.NewSource(0x50FACE))
	const iters = 100000

	for iter := 0; iter < iters; iter++ {
		n := 1 + rng.Intn(10)
		base := make([]string, n)
		for i := range base {
			base[i] = "B" + strconv.Itoa(iter) + "_" + strconv.Itoa(i)
		}

		ours := randomSideEdit(base, rng, "o")
		their := randomSideEdit(base, rng, "t")

		baseS := strings.Join(base, "\n")
		oursS := strings.Join(ours, "\n")
		theirS := strings.Join(their, "\n")

		got := threeWayMerge(baseS, oursS, theirS)
		wantText, wantConflict := independentMerge(base, ours, their)

		if got.Conflict != wantConflict {
			t.Fatalf("iter %d: conflict=%v, oracle=%v\nbase=%q\nours=%q\ntheir=%q\nmerged=%q",
				iter, got.Conflict, wantConflict, base, ours, their, got.Text)
		}
		if !wantConflict && got.Text != wantText {
			t.Fatalf("iter %d: merge != run-level oracle\nbase=%q\nours=%q\ntheir=%q\n got=%q\nwant=%q",
				iter, base, ours, their, got.Text, wantText)
		}
	}
}

// TestThreeWayMerge_DuplicateLineIdentitiesFuzz stresses the production LCS
// alignment (lcsMatches/decompose) on bases drawn from a TINY alphabet, so
// duplicate lines are frequent and identity alignment is NOT well defined — the
// regime the unique-line oracle cannot model. A full independent merge oracle is
// impossible here, so this asserts alignment-INDEPENDENT identities that must
// hold for ANY base, duplicates or not:
//
//   - a side equal to the base contributes no change, so the merge must
//     reproduce the OTHER side verbatim with no conflict;
//   - two identical sides merge to that shared text with no conflict.
//
// Each identity exercises decompose's partition and lcsMatches' monotonicity on
// ambiguous input; a non-monotonic or lossy alignment would corrupt the
// reconstructed text and trip the check.
func TestThreeWayMerge_DuplicateLineIdentitiesFuzz(t *testing.T) {
	rng := rand.New(rand.NewSource(0x0DDD0C))
	const iters = 30000
	alphabet := []string{"x", "y", "z"}

	for iter := 0; iter < iters; iter++ {
		n := 1 + rng.Intn(8)
		base := make([]string, n)
		for i := range base {
			base[i] = alphabet[rng.Intn(len(alphabet))]
		}
		side := randomSideEdit(base, rng, "s")
		baseS := strings.Join(base, "\n")
		sideS := strings.Join(side, "\n")

		// ours == base: the merge must equal theirs (= side) verbatim.
		if got := threeWayMerge(baseS, baseS, sideS); got.Conflict || got.Text != sideS {
			t.Fatalf("iter %d: ours==base must yield theirs verbatim; conflict=%v got=%q want=%q\nbase=%q",
				iter, got.Conflict, got.Text, sideS, base)
		}
		// theirs == base: the merge must equal ours (= side) verbatim.
		if got := threeWayMerge(baseS, sideS, baseS); got.Conflict || got.Text != sideS {
			t.Fatalf("iter %d: theirs==base must yield ours verbatim; conflict=%v got=%q want=%q\nbase=%q",
				iter, got.Conflict, got.Text, sideS, base)
		}
		// ours == theirs: identical edits merge to the shared text, no conflict.
		if got := threeWayMerge(baseS, sideS, sideS); got.Conflict || got.Text != sideS {
			t.Fatalf("iter %d: identical sides must merge to the shared text; conflict=%v got=%q want=%q\nbase=%q",
				iter, got.Conflict, got.Text, sideS, base)
		}
	}
}

// independentMerge is an INDEPENDENT re-derivation of the intended merge for a
// base of DISTINCT lines. It shares no code with the production merge: it aligns
// each side to the base by exact line identity (identityBlocks) and resolves the
// run-grouping contract with a local equality (eqLines). A logic error anywhere
// in the production path — LCS alignment, decomposition, or run resolution —
// surfaces as a disagreement.
func independentMerge(base, ours, theirs []string) (text string, conflict bool) {
	nb := len(base)
	oBlocks, oTrail := identityBlocks(base, ours)
	tBlocks, tTrail := identityBlocks(base, theirs)

	oc := make([]bool, nb)
	tc := make([]bool, nb)
	for k := 0; k < nb; k++ {
		nat := []string{base[k]}
		oc[k] = !eqLines(oBlocks[k], nat)
		tc[k] = !eqLines(tBlocks[k], nat)
	}

	var out []string
	for k := 0; k < nb; {
		if !oc[k] && !tc[k] {
			out = append(out, base[k])
			k++
			continue
		}
		start := k
		co := false
		for k < nb && (oc[k] || tc[k]) {
			if oc[k] && tc[k] {
				co = true
			}
			k++
		}
		if co {
			var oRun, tRun []string
			for i := start; i < k; i++ {
				oRun = append(oRun, oBlocks[i]...)
				tRun = append(tRun, tBlocks[i]...)
			}
			if !eqLines(oRun, tRun) {
				return "", true
			}
			out = append(out, oRun...)
		} else {
			for i := start; i < k; i++ {
				nat := []string{base[i]}
				switch {
				case !eqLines(oBlocks[i], nat):
					out = append(out, oBlocks[i]...)
				case !eqLines(tBlocks[i], nat):
					out = append(out, tBlocks[i]...)
				default:
					out = append(out, nat...)
				}
			}
		}
	}

	// Trailing.
	switch {
	case len(oTrail) != 0 && len(tTrail) != 0:
		if !eqLines(oTrail, tTrail) {
			return "", true
		}
		out = append(out, oTrail...)
	case len(oTrail) != 0:
		out = append(out, oTrail...)
	case len(tTrail) != 0:
		out = append(out, tTrail...)
	}

	return strings.Join(out, "\n"), false
}

// identityBlocks decomposes side against a DISTINCT-LINE base by exact line
// identity (no LCS). It mirrors decompose's attribution rule — inserted side
// lines before an anchor belong to that base line's block; a deleted base line
// claims the side lines up to the next surviving anchor — but derives the
// alignment independently, so it cannot inherit a decompose/lcsMatches bug.
// Valid only when base lines are distinct, which is exactly when greedy identity
// matching is unambiguous.
func identityBlocks(base, side []string) (blocks [][]string, trailing []string) {
	nb := len(base)
	blocks = make([][]string, nb)

	// kept[k] = the side index where base[k] survives (searched forward from the
	// previous match so the matches are monotonic), or -1 if the side dropped it.
	// Side-inserted lines never equal a base line (the generators tag them), so a
	// match is unambiguous on a distinct-line base.
	kept := make([]int, nb)
	cursor := 0
	for k := 0; k < nb; k++ {
		kept[k] = -1
		for j := cursor; j < len(side); j++ {
			if side[j] == base[k] {
				kept[k] = j
				cursor = j + 1
				break
			}
		}
	}

	blk := 0
	for k := 0; k < nb; k++ {
		var block []string
		if kept[k] >= 0 {
			block = append(block, side[blk:kept[k]]...)
			block = append(block, side[kept[k]])
			blk = kept[k] + 1
		} else {
			next := len(side)
			for m := k + 1; m < nb; m++ {
				if kept[m] >= 0 {
					next = kept[m]
					break
				}
			}
			block = append(block, side[blk:next]...)
			blk = next
		}
		blocks[k] = block
	}
	trailing = append(trailing, side[blk:]...)
	return blocks, trailing
}

// independentChangedSet returns the set of base indices the side changed,
// derived from the independent identity decomposition (not the production
// changedSet/decompose).
func independentChangedSet(base, side []string) map[int]bool {
	blocks, _ := identityBlocks(base, side)
	set := map[int]bool{}
	for k := range base {
		if !eqLines(blocks[k], []string{base[k]}) {
			set[k] = true
		}
	}
	return set
}

// eqLines is a test-local element-wise equality, kept independent of the
// production equalLines so the oracle shares no merge code.
func eqLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// editSpan returns base with [lo,hi] either deleted or replaced by a single
// tagged line, chosen at random.
func editSpan(base []string, lo, hi int, rng *rand.Rand, tag string) []string {
	out := make([]string, 0, len(base))
	out = append(out, base[:lo]...)
	if rng.Intn(2) == 0 {
		// replace with one distinct line
		out = append(out, tag+"-repl-"+strconv.Itoa(rng.Intn(1<<20)))
	} // else delete (emit nothing for the span)
	out = append(out, base[hi+1:]...)
	return out
}

func setsOverlap(a, b map[int]bool) bool {
	for k := range a {
		if b[k] {
			return true
		}
	}
	return false
}

// randomSideEdit applies a random set of per-line edits (replace/delete/insert)
// to base, used to generate arbitrary side versions. Inserted, replaced, and
// appended lines are tagged so they never collide with a base line.
func randomSideEdit(base []string, rng *rand.Rand, tag string) []string {
	out := make([]string, 0, len(base)+4)
	for _, line := range base {
		switch rng.Intn(5) {
		case 0:
			out = append(out, tag+"-r-"+strconv.Itoa(rng.Intn(1<<16))) // replace
		case 1:
			// delete: emit nothing
		case 2:
			out = append(out, line, tag+"-i-"+strconv.Itoa(rng.Intn(1<<16))) // insert after
		default:
			out = append(out, line) // keep
		}
	}
	if rng.Intn(3) == 0 {
		out = append(out, tag+"-append-"+strconv.Itoa(rng.Intn(1<<16)))
	}
	return out
}

// TestThreeWayMerge_DuplicateBaseCounterexample pins the exact ambiguous-base
// case the pre-release audit found: on a base with duplicate lines where BOTH
// sides diverge, the LCS alignment can absorb a genuine append into a spurious
// duplicate-line match and emit a CLEAN merge that silently drops a line
// (historically base "b\nb\na\na", ours "b\na\na\na", theirs "b\nb\na\na\na"
// merged to 4 lines, losing one appended line). The conservative contract
// requires a conflict here so the caller falls back to stale-reject (re-read).
func TestThreeWayMerge_DuplicateBaseCounterexample(t *testing.T) {
	got := threeWayMerge("b\nb\na\na", "b\na\na\na", "b\nb\na\na\na")
	if !got.Conflict {
		t.Fatalf("ambiguous duplicate-line base with both sides diverging must conflict, got clean merge %q", got.Text)
	}
}

// TestThreeWayMerge_DuplicateBaseTrivialStillMerges confirms the duplicate-line
// guard does not over-reject: the alignment-free trivial resolutions (one side
// unchanged, both sides identical) stay clean and correct even on a base full
// of duplicate lines.
func TestThreeWayMerge_DuplicateBaseTrivialStillMerges(t *testing.T) {
	const base = "x\nx\ny\nx" // duplicate lines: x appears three times
	tests := []struct {
		name, ours, theirs, want string
	}{
		{"only theirs changed", base, "x\nx\ny\nz", "x\nx\ny\nz"},
		{"only ours changed", "x\nx\nY\nx", base, "x\nx\nY\nx"},
		{"both identical change", "x\nx\nY\nx", "x\nx\nY\nx", "x\nx\nY\nx"},
		{"all three equal", base, base, base},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := threeWayMerge(base, tt.ours, tt.theirs)
			if got.Conflict {
				t.Fatalf("trivial resolution must not conflict on a duplicate base: %#v", got)
			}
			if got.Text != tt.want {
				t.Fatalf("Text = %q, want %q", got.Text, tt.want)
			}
		})
	}
}

// TestThreeWayMerge_DuplicateBaseNoUnsoundCleanMerge closes the coverage hole
// the audit flagged: the distinct-line oracles above can never exercise the
// duplicate-line regime where the bug lived. Over a tiny alphabet (so duplicate
// base lines are frequent) it asserts the SOUNDNESS CONTRACT directly, without
// an oracle: a CLEAN (non-conflict) merge is permitted ONLY in a provably-safe
// shape — one side unchanged, both sides identical, or a distinct-line base
// where the LCS alignment is unambiguous. A clean merge outside those shapes is
// exactly the silent mis-merge this guards against; it would have fired on the
// historical counterexample and now never occurs.
func TestThreeWayMerge_DuplicateBaseNoUnsoundCleanMerge(t *testing.T) {
	rng := rand.New(rand.NewSource(0xA11B1A5))
	const iters = 100000
	distinctClean := 0
	for iter := 0; iter < iters; iter++ {
		n := 1 + rng.Intn(6)
		base := make([]string, n)
		for i := range base {
			base[i] = string(rune('a' + rng.Intn(3))) // alphabet {a,b,c}: frequent duplicates
		}
		ours := randomSideEdit(base, rng, "o")
		their := randomSideEdit(base, rng, "t")

		baseS := strings.Join(base, "\n")
		oursS := strings.Join(ours, "\n")
		theirS := strings.Join(their, "\n")

		got := threeWayMerge(baseS, oursS, theirS)
		if got.Conflict {
			continue
		}
		safeShape := oursS == baseS || theirS == baseS || oursS == theirS || !hasDup(base)
		if !safeShape {
			t.Fatalf("iter %d: CLEAN merge on an ambiguous duplicate-line base (unsound)\nbase=%q\nours=%q\ntheir=%q\nmerged=%q",
				iter, base, ours, their, got.Text)
		}
		if !hasDup(base) {
			distinctClean++
		}
	}
	if distinctClean == 0 {
		t.Fatal("expected some distinct-line clean merges to exercise the safe path")
	}
}

// hasDup is a test-local duplicate detector kept independent of the production
// hasDuplicateLine so the soundness check above shares no code with the merge.
func hasDup(lines []string) bool {
	seen := map[string]bool{}
	for _, ln := range lines {
		if seen[ln] {
			return true
		}
		seen[ln] = true
	}
	return false
}
