package hashline

import (
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestThreeWayMerge_Table(t *testing.T) {
	tests := []struct {
		name              string
		base, ours, their string
		wantText          string
		wantConflict      bool
	}{
		{
			name:     "no change either side",
			base:     "a\nb\nc",
			ours:     "a\nb\nc",
			their:    "a\nb\nc",
			wantText: "a\nb\nc",
		},
		{
			name:     "only ours changed",
			base:     "a\nb\nc",
			ours:     "a\nB\nc",
			their:    "a\nb\nc",
			wantText: "a\nB\nc",
		},
		{
			name:     "only theirs changed",
			base:     "a\nb\nc",
			ours:     "a\nb\nc",
			their:    "a\nb\nC",
			wantText: "a\nb\nC",
		},
		{
			name:     "disjoint edits merge as union",
			base:     "a\nb\nc\nd\ne",
			ours:     "A\nb\nc\nd\ne", // changed line 1
			their:    "a\nb\nc\nd\nE", // changed line 5
			wantText: "A\nb\nc\nd\nE",
		},
		{
			name:     "both insert in different gaps",
			base:     "a\nb\nc",
			ours:     "a\nX\nb\nc", // insert X after a
			their:    "a\nb\nc\nY", // append Y
			wantText: "a\nX\nb\nc\nY",
		},
		{
			name:     "both made identical change",
			base:     "a\nb\nc",
			ours:     "a\nZ\nc",
			their:    "a\nZ\nc",
			wantText: "a\nZ\nc",
		},
		{
			name:         "conflicting change on same line",
			base:         "a\nb\nc",
			ours:         "a\nB1\nc",
			their:        "a\nB2\nc",
			wantConflict: true,
		},
		{
			name:         "ours edits a region theirs deletes differently",
			base:         "a\nb\nc\nd",
			ours:         "a\nB\nC\nd", // change b,c
			their:        "a\nd",       // delete b,c
			wantConflict: true,
		},
		{
			name:     "ours deletes, theirs untouched",
			base:     "a\nb\nc",
			ours:     "a\nc",
			their:    "a\nb\nc",
			wantText: "a\nc",
		},
		{
			name:     "empty base, only ours adds",
			base:     "",
			ours:     "x",
			their:    "",
			wantText: "x",
		},
		{
			// The block model resolves disjoint edits on ADJACENT base lines
			// (ours replaces line1, theirs replaces line2) — the case the
			// patcher recovery path relies on for tight edits next to a
			// concurrent change.
			name:     "adjacent replaced lines merge per-line",
			base:     "a\nb\nc",
			ours:     "A\nb\nc",
			their:    "a\nB\nc",
			wantText: "A\nB\nc",
		},
		{
			// ours replaces line1, theirs deletes line2: still disjoint per
			// line, merges to both effects.
			name:     "adjacent replace and delete merge",
			base:     "a\nb\nc",
			ours:     "A\nb\nc",
			their:    "a\nc",
			wantText: "A\nc",
		},
		{
			// REGRESSION: ours deletes the run b,c,d; theirs deletes only the
			// inner line c. The two deletions cover an OVERLAPPING multi-line
			// region differently (theirs keeps b and d, ours removes them), so
			// taking either side silently discards the other's intent. This must
			// conflict, not merge. A naive per-line model wrongly merged it to
			// "a\ne" because line c was deleted identically by both.
			name:         "overlapping multi-line deletions conflict",
			base:         "a\nb\nc\nd\ne",
			ours:         "a\ne",
			their:        "a\nb\nd\ne",
			wantConflict: true,
		},
		{
			// REGRESSION (symmetric): theirs deletes the run, ours deletes the
			// inner line. Still an overlapping incompatible region.
			name:         "overlapping multi-line deletions conflict (swapped)",
			base:         "a\nb\nc\nd\ne",
			ours:         "a\nb\nd\ne",
			their:        "a\ne",
			wantConflict: true,
		},
		{
			// REGRESSION: ours deletes b,c; theirs deletes c,d. The spans overlap
			// at c, and the union (drop b,c,d) was never intended by either side.
			name:         "partially overlapping deletions conflict",
			base:         "a\nb\nc\nd\ne",
			ours:         "a\nd\ne",
			their:        "a\nb\ne",
			wantConflict: true,
		},
		{
			// REGRESSION: ours replaces the whole run b,c,d with X; theirs deletes
			// only the inner line c. Overlapping, incompatible.
			name:         "replace-region vs inner delete conflict",
			base:         "a\nb\nc\nd\ne",
			ours:         "a\nX\ne",
			their:        "a\nb\nd\ne",
			wantConflict: true,
		},
		{
			// Both delete the exact same run: identical effect, mergeable.
			name:     "identical multi-line deletion merges",
			base:     "a\nb\nc\nd\ne",
			ours:     "a\ne",
			their:    "a\ne",
			wantText: "a\ne",
		},
		{
			// A deletion adjacent to (but not overlapping) an edit on a line the
			// other side left untouched still merges: ours deletes b,c,d; theirs
			// edits e, which ours kept.
			name:     "multi-line delete adjacent to disjoint edit merges",
			base:     "a\nb\nc\nd\ne",
			ours:     "a\ne",
			their:    "a\nb\nc\nd\nE",
			wantText: "a\nE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := threeWayMerge(tt.base, tt.ours, tt.their)
			if got.Conflict != tt.wantConflict {
				t.Fatalf("Conflict = %v, want %v (got text %q)", got.Conflict, tt.wantConflict, got.Text)
			}
			if !tt.wantConflict && got.Text != tt.wantText {
				t.Fatalf("Text = %q, want %q", got.Text, tt.wantText)
			}
		})
	}
}

func TestThreeWayMerge_Symmetry(t *testing.T) {
	// A non-conflicting merge of disjoint edits is order-independent: swapping
	// ours/theirs yields the same merged text.
	base := "1\n2\n3\n4\n5"
	ours := "1\nX\n3\n4\n5"
	their := "1\n2\n3\n4\nY"

	a := threeWayMerge(base, ours, their)
	b := threeWayMerge(base, their, ours)
	if a.Conflict || b.Conflict {
		t.Fatalf("unexpected conflict: a=%v b=%v", a.Conflict, b.Conflict)
	}
	if a.Text != b.Text {
		t.Fatalf("merge not symmetric: %q vs %q", a.Text, b.Text)
	}
}

// TestThreeWayMerge_DifferentialFuzz generates a base text and derives ours
// and theirs by INDEPENDENT, NON-OVERLAPPING line edits, then asserts the
// three-way merge equals the obvious union of those edits. It separately
// generates OVERLAPPING edits and asserts they are reported as conflicts.
//
// The two sides each pick a disjoint set of base line indices to mutate
// (replace in place, delete, or annotate with an inserted line). Because the
// touched index sets are disjoint, the correct merge is the deterministic
// per-line composition of both sides' edits — computed here by an independent
// oracle that walks the base once and applies whichever side (if any) touched
// each line.
func TestThreeWayMerge_DifferentialFuzz(t *testing.T) {
	rng := rand.New(rand.NewSource(0xC0DE))
	const iters = 50000

	for iter := 0; iter < iters; iter++ {
		n := 1 + rng.Intn(10)
		// Unique base lines so the LCS alignment of each side to the base is
		// unambiguous and equals identity on the untouched lines — that is the
		// precondition under which diff3's hunk grouping reduces to the obvious
		// per-line union.
		base := make([]string, n)
		for i := range base {
			base[i] = "b" + strconv.Itoa(iter) + "_" + strconv.Itoa(i)
		}

		// Partition base indices into three disjoint buckets: edited by ours,
		// edited by theirs, untouched. Two extra invariants make the union the
		// provably-correct merge under line-level diff3:
		//   - per-index disjoint (a line is edited by at most one side);
		//   - any line edited by one side is separated from the nearest line
		//     edited by the OTHER side by at least one untouched anchor line.
		// Adjacent edits by different sides collapse into a single diff3 hunk
		// that both sides "changed", which diff3 correctly reports as a
		// conflict; that case is exercised by the overlap fuzz, not here.
		oursIdx := map[int]lineEdit{}
		theirsIdx := map[int]lineEdit{}
		lastEditedIdx := -2
		lastSide := 0 // 1 = ours, 2 = theirs
		for i := 0; i < n; i++ {
			side := rng.Intn(4) // 0 ours, 1 theirs, else untouched
			if side > 1 {
				continue
			}
			s := side + 1
			// If the other side edited the immediately preceding line, skip to
			// preserve an untouched anchor between differently-owned edits.
			if lastSide != 0 && lastSide != s && i-lastEditedIdx < 2 {
				continue
			}
			if s == 1 {
				oursIdx[i] = randEdit(rng)
			} else {
				theirsIdx[i] = randEdit(rng)
			}
			lastEditedIdx = i
			lastSide = s
		}

		ours := applySideEdits(base, oursIdx)
		their := applySideEdits(base, theirsIdx)
		wantUnion := applySideEdits(base, mergeEdits(oursIdx, theirsIdx))

		got := threeWayMerge(strings.Join(base, "\n"), strings.Join(ours, "\n"), strings.Join(their, "\n"))
		if got.Conflict {
			t.Fatalf("iter %d: disjoint edits reported as conflict\nbase=%q\nours=%q\ntheir=%q",
				iter, base, ours, their)
		}
		if got.Text != strings.Join(wantUnion, "\n") {
			t.Fatalf("iter %d: merge != union\nbase=%q\nours=%q\ntheir=%q\n got=%q\nwant=%q",
				iter, base, ours, their, got.Text, strings.Join(wantUnion, "\n"))
		}
	}
}

func TestThreeWayMerge_DifferentialFuzz_OverlapConflicts(t *testing.T) {
	rng := rand.New(rand.NewSource(0xBEEF))
	const iters = 20000
	conflictsSeen := 0

	for iter := 0; iter < iters; iter++ {
		n := 2 + rng.Intn(8)
		base := make([]string, n)
		for i := range base {
			base[i] = "c" + strconv.Itoa(iter) + "_" + strconv.Itoa(i)
		}

		// Pick one shared index both sides REPLACE with different content; this
		// is a genuine overlapping change and must conflict.
		shared := rng.Intn(n)
		ourVal := "OURS-" + randLine(rng)
		theirVal := "THEIRS-" + randLine(rng)
		// Guarantee the two replacements differ.
		if ourVal == theirVal {
			theirVal += "x"
		}

		ours := append([]string(nil), base...)
		their := append([]string(nil), base...)
		ours[shared] = ourVal
		their[shared] = theirVal

		// Add some extra disjoint edits so the conflict is embedded in real work.
		for i := 0; i < n; i++ {
			if i == shared {
				continue
			}
			switch rng.Intn(5) {
			case 0:
				ours[i] = "o-" + base[i]
			case 1:
				their[i] = "t-" + base[i]
			}
		}

		got := threeWayMerge(strings.Join(base, "\n"), strings.Join(ours, "\n"), strings.Join(their, "\n"))
		if !got.Conflict {
			t.Fatalf("iter %d: overlapping edit on line %d not reported as conflict\nbase=%q\nours=%q\ntheir=%q\nmerged=%q",
				iter, shared, base, ours, their, got.Text)
		}
		conflictsSeen++
	}
	if conflictsSeen == 0 {
		t.Fatal("expected to observe conflicts")
	}
}

// --- fuzz oracle helpers ---

// lineEdit describes a single side's edit to one base line.
type lineEdit struct {
	kind    editKind
	replace string // for replaceLine
	insert  string // for insertAfter
}

type editKind int

const (
	replaceLine editKind = iota
	deleteLine
	insertAfter
)

func randLine(rng *rand.Rand) string {
	// Short alphabetic tokens; the merge is line-granular so content variety
	// matters more than length.
	n := 1 + rng.Intn(4)
	b := make([]byte, n)
	for i := range b {
		b[i] = byte('a' + rng.Intn(20))
	}
	return string(b)
}

func randEdit(rng *rand.Rand) lineEdit {
	switch rng.Intn(3) {
	case 0:
		return lineEdit{kind: replaceLine, replace: "R-" + randLine(rng)}
	case 1:
		return lineEdit{kind: deleteLine}
	default:
		return lineEdit{kind: insertAfter, insert: "I-" + randLine(rng)}
	}
}

// applySideEdits walks the base once and applies the edit (if any) recorded
// for each base index, producing the side's text. This is the independent
// oracle: it never reorders and composes edits purely by original index.
func applySideEdits(base []string, edits map[int]lineEdit) []string {
	out := make([]string, 0, len(base)+len(edits))
	for i, line := range base {
		e, ok := edits[i]
		if !ok {
			out = append(out, line)
			continue
		}
		switch e.kind {
		case replaceLine:
			out = append(out, e.replace)
		case deleteLine:
			// drop the line
		case insertAfter:
			out = append(out, line, e.insert)
		}
	}
	return out
}

// mergeEdits unions two disjoint edit maps. Disjointness is an invariant of
// the caller; this asserts it to catch a buggy generator.
func mergeEdits(a, b map[int]lineEdit) map[int]lineEdit {
	out := make(map[int]lineEdit, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		if _, dup := out[k]; dup {
			panic("test generator produced overlapping edit indices")
		}
		out[k] = v
	}
	return out
}

func TestLCSMatches_Monotonic(t *testing.T) {
	// lcsMatches must return strictly increasing pairs (a valid subsequence
	// alignment): both the base indices and the paired indices increase.
	a := []string{"a", "b", "c", "d", "e"}
	b := []string{"x", "a", "c", "y", "e", "z"}
	m := lcsMatches(a, b)

	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	prevA, prevB := -1, -1
	for _, k := range keys {
		v := m[k]
		if k <= prevA || v <= prevB {
			t.Fatalf("non-monotonic match: a=%d->b=%d after a=%d->b=%d", k, v, prevA, prevB)
		}
		if a[k] != b[v] {
			t.Fatalf("matched non-equal lines: a[%d]=%q b[%d]=%q", k, a[k], v, b[v])
		}
		prevA, prevB = k, v
	}
}
