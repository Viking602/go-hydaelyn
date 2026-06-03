package hashline

// merge.go implements a line-level three-way merge used by the patcher's M6
// stale-recovery path. Given a common base and two derived versions (ours =
// the current live file, theirs = the edit re-applied to the base the tag
// referred to), it produces a merged result when the two sides changed
// disjoint regions, and reports a conflict when they changed the same region
// in incompatible ways.
//
// Model. Each side is aligned to the base by a longest-common-subsequence
// (LCS) match at line granularity. The aligned (matched) base lines are the
// stable anchors; everything else is a change. The merge is then decided
// per base line rather than per coarse hunk, so that an edit on one base line
// and a concurrent edit on the immediately adjacent base line — disjoint at
// line granularity — merge cleanly instead of colliding.
//
// Decomposition. For each side we derive, per base line k, the side's
// "block": the run of side lines this base line is responsible for, namely
// any inserted lines that precede the anchor for k followed by base line k
// itself when the side kept it (a deleted base line contributes an empty or
// insertion-only block). A trailing block captures lines appended after the
// last base line. The natural (unchanged) block for base line k is simply
// [base[k]].
//
// Resolution. For each base line's block (and the trailing block):
//   - if only one side's block differs from the base's natural block, take
//     that side;
//   - if both differ but are identical, take either;
//   - if both differ and disagree, it is a conflict.
//
// The merge is conservative: anything it cannot prove safe is a conflict, so
// the caller falls back to stale-reject (re-read). In particular, the per-line
// LCS alignment is only unambiguous on a base of DISTINCT lines; when the base
// has duplicate lines and both sides diverged from it, the alignment cannot be
// trusted (a duplicated base line can absorb a genuine insertion/deletion into
// a spurious match), so the merge declines rather than risk a silent
// mis-merge. See the soundness guard in threeWayMerge.

// mergeResult is the outcome of a three-way merge.
type mergeResult struct {
	// Text is the merged LF-internal content (valid only when !Conflict).
	Text string
	// Conflict is true when ours and theirs changed an overlapping region in
	// incompatible ways; Text is then meaningless.
	Conflict bool
}

// threeWayMerge merges ours and theirs against their common base at line
// granularity. base/ours/theirs are LF-internal text.
//
// The merge is per base line, but base lines are first grouped into maximal
// runs of consecutive lines that at least one side changed (bounded by lines
// both sides left untouched). Within a run:
//
//   - if no line was changed by BOTH sides, the two sides edited disjoint base
//     lines, so the per-line union is well defined and sound — each line takes
//     whichever side changed it (or the base, if neither did);
//   - if any line was changed by both sides, the run is co-edited: a deletion
//     or replacement by one side couples the whole run with the other side's
//     changes (e.g. ours deletes b,c,d while theirs deletes only c — the lines
//     ours removed but theirs kept are not independent). The run is then
//     resolved as a unit: taken only if both sides produced identical text
//     across the entire run, otherwise it is a conflict.
//
// This keeps disjoint per-line edits mergeable (the design intent) while
// rejecting overlapping multi-line edits — especially deletions — that the
// naive per-line rule would silently mis-merge.
func threeWayMerge(base, ours, theirs string) mergeResult {
	// Trivial resolutions that need no alignment, and are therefore sound even
	// when the base contains duplicate lines: if one side is unchanged the merge
	// is the other side verbatim; if both sides made the identical change, that
	// change is the result. joinLines∘splitLines is the identity on LF text, so
	// returning the raw side string is byte-identical to the per-line path below.
	switch {
	case base == ours:
		return mergeResult{Text: theirs}
	case base == theirs:
		return mergeResult{Text: ours}
	case ours == theirs:
		return mergeResult{Text: ours}
	}

	b := splitLines(base)

	// Soundness guard for duplicate-line bases. The per-line merge below
	// localizes each side's edit by an LCS alignment to the base, which is only
	// unambiguous when the base lines are distinct. A duplicated base line can
	// absorb a genuine insertion or deletion into a spurious LCS match, so
	// decompose mis-attributes the change and the merge can silently DROP (or
	// duplicate) a line on a CLEAN, non-conflict result — e.g. base "b\nb\na\na"
	// with ours "b\na\na\na" and theirs "b\nb\na\na\na" loses one appended line.
	// The trivial cases above are already handled, so reaching here means both
	// sides diverged from the base; if it also has duplicate lines the alignment
	// is ambiguous, and per this module's conservative, no-silent-data-loss
	// contract we report a conflict and let the caller fall back to stale-reject.
	if hasDuplicateLine(b) {
		return mergeResult{Conflict: true}
	}

	o := splitLines(ours)
	t := splitLines(theirs)

	oBlocks, oTrail := decompose(b, o)
	tBlocks, tTrail := decompose(b, t)

	nb := len(b)
	oursChanged := make([]bool, nb)
	theirsChanged := make([]bool, nb)
	for k := 0; k < nb; k++ {
		natural := []string{b[k]}
		oursChanged[k] = !equalLines(oBlocks[k], natural)
		theirsChanged[k] = !equalLines(tBlocks[k], natural)
	}

	var out []string
	for k := 0; k < nb; {
		// A line untouched by both sides is a stable anchor: emit the base line.
		if !oursChanged[k] && !theirsChanged[k] {
			out = append(out, b[k])
			k++
			continue
		}

		// Extend to the maximal run of consecutive lines changed by either side.
		runStart := k
		coEdited := false
		for k < nb && (oursChanged[k] || theirsChanged[k]) {
			if oursChanged[k] && theirsChanged[k] {
				coEdited = true
			}
			k++
		}

		merged, ok := resolveRun(b[runStart:k], oBlocks[runStart:k], tBlocks[runStart:k], coEdited)
		if !ok {
			return mergeResult{Conflict: true}
		}
		out = append(out, merged...)
	}

	// Trailing block (lines after the last base line). Its natural form is
	// empty: the base has nothing past its last line. If only one side appended
	// there, take it; if both did, they must agree, else conflict.
	merged, ok := resolveTrailing(oTrail, tTrail)
	if !ok {
		return mergeResult{Conflict: true}
	}
	out = append(out, merged...)

	return mergeResult{Text: joinLines(out)}
}

// resolveRun merges one run of base lines. base holds the run's natural lines;
// oursBlocks/theirsBlocks hold each side's per-line block over the run.
// coEdited reports whether any single base line in the run was changed by both
// sides.
//
//   - coEdited: the run's changes are coupled, so it is resolved as a unit —
//     taken only if both sides produced identical text across the whole run,
//     otherwise a conflict.
//   - not coEdited: the sides changed disjoint lines, so each line takes
//     whichever side changed it (the per-line union).
func resolveRun(base []string, oursBlocks, theirsBlocks [][]string, coEdited bool) ([]string, bool) {
	if coEdited {
		ours := flatten(oursBlocks)
		theirs := flatten(theirsBlocks)
		if equalLines(ours, theirs) {
			return ours, true
		}
		return nil, false
	}

	// Disjoint within the run: per-line, take the side that changed each line.
	var out []string
	for i := range base {
		natural := []string{base[i]}
		switch {
		case !equalLines(oursBlocks[i], natural):
			out = append(out, oursBlocks[i]...)
		case !equalLines(theirsBlocks[i], natural):
			out = append(out, theirsBlocks[i]...)
		default:
			out = append(out, natural...)
		}
	}
	return out, true
}

// resolveTrailing merges the two sides' trailing appends (lines after the last
// base line). The natural trailing block is empty, so a non-empty side
// "changed" it. One-sided appends are taken; two differing appends conflict.
func resolveTrailing(ours, theirs []string) ([]string, bool) {
	oursChanged := len(ours) != 0
	theirsChanged := len(theirs) != 0
	switch {
	case !oursChanged && !theirsChanged:
		return nil, true
	case oursChanged && !theirsChanged:
		return ours, true
	case !oursChanged && theirsChanged:
		return theirs, true
	default:
		if equalLines(ours, theirs) {
			return ours, true
		}
		return nil, false
	}
}

// flatten concatenates a slice of line blocks into a single line slice.
func flatten(blocks [][]string) []string {
	var out []string
	for _, blk := range blocks {
		out = append(out, blk...)
	}
	return out
}

// decompose aligns side against base via LCS and returns, for each base line
// k, the side's block (insertions that precede base line k's anchor, then
// base line k itself if the side kept it), plus a trailing block of side
// lines that follow the last base line.
//
// Attribution rule: an inserted (unmatched) side line is attributed to the
// block of the next base line it precedes. Lines following the last matched
// base line go to the trailing block. This is consistent for both sides, so
// a pure insertion in the same gap by both sides compares equal, and a
// replacement (delete base line + insert) localizes to that base line's
// block.
func decompose(base, side []string) (blocks [][]string, trailing []string) {
	nb := len(base)
	blocks = make([][]string, nb)

	match := lcsMatches(base, side) // base index -> side index, monotonic

	// Walk base lines and side lines together using the match map.
	sideCursor := 0
	for k := 0; k < nb; k++ {
		j, kept := match[k]
		var block []string
		if kept {
			// Inserted side lines before this anchor belong to this block.
			block = append(block, side[sideCursor:j]...)
			block = append(block, side[j]) // the kept base line itself
			sideCursor = j + 1
		}
		// If this base line was not kept (deleted/replaced), its block is the
		// not-yet-emitted side lines up to the NEXT kept base line. Defer those
		// to the next kept anchor (or trailing) so the leading insertions land
		// on the first deleted line of a run. We handle that by collecting the
		// pending side lines lazily: when a base line is deleted, attribute the
		// pending insertions up to the next anchor to THIS line.
		if !kept {
			next := nextMatchedSideIndex(match, base, k, len(side))
			block = append(block, side[sideCursor:next]...)
			sideCursor = next
		}
		blocks[k] = block
	}

	// Anything left over follows the last base line.
	trailing = append(trailing, side[sideCursor:]...)
	return blocks, trailing
}

// nextMatchedSideIndex returns the side index of the next kept base line at
// or after k+1, or sideLen if none remain. Used to bound the side lines a
// deleted base line claims.
func nextMatchedSideIndex(match map[int]int, base []string, k, sideLen int) int {
	for m := k + 1; m < len(base); m++ {
		if j, ok := match[m]; ok {
			return j
		}
	}
	return sideLen
}

// lcsMatches computes a longest common subsequence of a and b at line
// granularity and returns the mapping from each matched index in a to its
// paired index in b. Lines not present in the LCS have no entry. The returned
// matches are strictly increasing in both a and b indices.
func lcsMatches(a, b []string) map[int]int {
	na, nb := len(a), len(b)
	match := make(map[int]int)
	if na == 0 || nb == 0 {
		return match
	}

	// Classic DP LCS table over lines. File sizes on the recovery path are
	// small, so the quadratic table is acceptable.
	dp := make([][]int, na+1)
	for i := range dp {
		dp[i] = make([]int, nb+1)
	}
	for i := na - 1; i >= 0; i-- {
		for j := nb - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}

	// Backtrack to recover the matched pairs in increasing order.
	i, j := 0, 0
	for i < na && j < nb {
		if a[i] == b[j] {
			match[i] = j
			i++
			j++
		} else if dp[i+1][j] >= dp[i][j+1] {
			i++
		} else {
			j++
		}
	}
	return match
}

// hasDuplicateLine reports whether any line value occurs more than once. A base
// with duplicate lines makes the LCS alignment in decompose ambiguous, so
// threeWayMerge declines to merge such a base when both sides diverged from it
// (see the soundness guard there).
func hasDuplicateLine(lines []string) bool {
	seen := make(map[string]struct{}, len(lines))
	for _, ln := range lines {
		if _, ok := seen[ln]; ok {
			return true
		}
		seen[ln] = struct{}{}
	}
	return false
}

// equalLines reports whether two line slices are element-wise equal.
func equalLines(a, b []string) bool {
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
