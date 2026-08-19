package hashline

import (
	"slices"
	"sort"
)

// maxRecoveryEditDistance bounds the Myers trace retained while mapping an old
// snapshot to the live file. Stale recovery is an optimization, not an
// authorization boundary: a file that diverged by more than this many line
// insertions/deletions is rejected and must be re-read instead of consuming
// unbounded memory.
const maxRecoveryEditDistance = 512

// remappedSection is a section whose line anchors now address the live file.
type remappedSection struct {
	section  Section
	offset   int
	anchored bool
}

// remapSectionToLive proves that every line touched by sec remains unchanged in
// liveText, maps those lines from baseText to their live positions, and returns
// a section that can be replayed directly on the live file.
//
// Safety rules mirror the OMP hashline recovery model:
//   - every replaced/deleted line and every before/after insertion anchor must
//     survive the base-to-live diff;
//   - all anchors must move by one consistent offset;
//   - each anchor needs unchanged non-target context, with stricter two-sided
//     validation for duplicated line values;
//   - changed, deleted, split, or ambiguously aligned targets fail closed.
//
// Head/tail-only sections have no line anchors and are safe to replay directly:
// they preserve all live content and only add text at a file edge.
func remapSectionToLive(baseText, liveText string, sec Section) (remappedSection, bool) {
	baseLines := splitLines(baseText)
	liveLines := splitLines(liveText)
	anchors, ok := collectAnchoredLines(sec.Ops, len(baseLines))
	if !ok {
		return remappedSection{}, false
	}
	if len(anchors) == 0 {
		return remappedSection{section: sec}, true
	}

	lineMap, ok := unchangedLineMap(baseLines, liveLines)
	if !ok {
		return remappedSection{}, false
	}
	if remapped, ok := remapSectionWithLineMap(
		baseLines, liveLines, sec, anchors, samePositionLineMap(baseLines, liveLines), false,
	); ok {
		return remapped, true
	}
	return remapSectionWithLineMap(baseLines, liveLines, sec, anchors, lineMap, true)
}

func remapSectionWithLineMap(
	baseLines, liveLines []string,
	sec Section,
	anchors []int,
	lineMap map[int]int,
	requireAllContext bool,
) (remappedSection, bool) {
	if !validateAnchorContext(baseLines, liveLines, anchors, lineMap, requireAllContext) {
		return remappedSection{}, false
	}

	firstMapped := lineMap[anchors[0]]
	offset := firstMapped - anchors[0]
	mappedOps := make([]Op, len(sec.Ops))
	copy(mappedOps, sec.Ops)
	for i := range mappedOps {
		op := &mappedOps[i]
		switch op.Kind {
		case OpReplace, OpDelete:
			op.Start = lineMap[op.Start]
			op.End = lineMap[op.End]
		case OpInsertBefore, OpInsertAfter:
			op.Start = lineMap[op.Start]
		case OpInsertHead, OpInsertTail:
			// File-edge operations are position-independent.
		default:
			// Block operations must be resolved against the recorded base before
			// reaching this function.
			return remappedSection{}, false
		}
	}

	return remappedSection{
		section:  Section{Path: sec.Path, Tag: sec.Tag, Ops: mappedOps},
		offset:   offset,
		anchored: true,
	}, true
}

// samePositionLineMap prefers the model's original coordinates whenever the
// anchored content is still present there. This prevents a longer copied block
// elsewhere from stealing an unchanged original target.
func samePositionLineMap(baseLines, liveLines []string) map[int]int {
	limit := min(len(baseLines), len(liveLines))
	lineMap := make(map[int]int, limit)
	for line := 1; line <= limit; line++ {
		if baseLines[line-1] == liveLines[line-1] {
			lineMap[line] = line
		}
	}
	return lineMap
}

// collectAnchoredLines returns the sorted, de-duplicated base line numbers that
// must remain unchanged for the operations to be replayed safely.
func collectAnchoredLines(ops []Op, baseLineCount int) ([]int, bool) {
	set := make(map[int]struct{})
	for _, op := range ops {
		switch op.Kind {
		case OpReplace, OpDelete:
			if op.Start < 1 || op.End < op.Start || op.End > baseLineCount {
				return nil, false
			}
			for line := op.Start; line <= op.End; line++ {
				set[line] = struct{}{}
			}
		case OpInsertBefore, OpInsertAfter:
			if op.Start < 1 || op.Start > baseLineCount {
				return nil, false
			}
			set[op.Start] = struct{}{}
		case OpInsertHead, OpInsertTail:
			// No line anchor.
		default:
			return nil, false
		}
	}

	lines := make([]int, 0, len(set))
	for line := range set {
		lines = append(lines, line)
	}
	sort.Ints(lines)
	return lines, true
}

// validateAnchorContext verifies every target and one uniform line offset. At
// the original coordinates, the unchanged line number itself identifies the
// target, so unrelated edits may consume either neighbor. A target moved by the
// diff must retain every available neighbor; duplicated moved values additionally
// require a unique complete local sequence.
func validateAnchorContext(
	baseLines, liveLines []string,
	anchors []int,
	lineMap map[int]int,
	requireAllContext bool,
) bool {
	baseCounts := lineCounts(baseLines)
	liveCounts := lineCounts(liveLines)

	var (
		offset     int
		haveOffset bool
	)
	for i := 0; i < len(anchors); {
		j := i
		for j+1 < len(anchors) && anchors[j+1] == anchors[j]+1 {
			j++
		}
		runStart, runEnd := anchors[i], anchors[j]
		before, after := runStart-1, runEnd+1
		if before < 1 {
			before = 0
		}
		if after > len(baseLines) {
			after = 0
		}
		runHasDuplicate := false

		if requireAllContext && (before == 0 || after == 0) {
			// A moved target needs two-sided context. At a file edge, a copied
			// old block is indistinguishable from the original target moving.
			return false
		}
		for k := i; k <= j; k++ {
			lineOffset, duplicate, ok := validateMappedAnchor(
				baseLines, liveLines, lineMap, baseCounts, liveCounts,
				anchors[k], before, after, requireAllContext,
			)
			if !ok {
				return false
			}
			if !haveOffset {
				offset = lineOffset
				haveOffset = true
			} else if lineOffset != offset {
				return false
			}
			runHasDuplicate = runHasDuplicate || duplicate
		}
		if runHasDuplicate && !duplicateRunContextUnique(baseLines, liveLines, runStart, runEnd, before, after, lineMap) {
			return false
		}
		i = j + 1
	}
	return haveOffset
}

func validateMappedAnchor(
	baseLines, liveLines []string,
	lineMap map[int]int,
	baseCounts, liveCounts map[string]int,
	line, before, after int,
	requireAllContext bool,
) (lineOffset int, duplicate bool, ok bool) {
	mapped, exists := lineMap[line]
	if !exists || mapped < 1 || mapped > len(liveLines) || baseLines[line-1] != liveLines[mapped-1] {
		return 0, false, false
	}
	if !requireAllContext {
		return mapped - line, false, true
	}
	if !validateAllAnchorContext(line, mapped, before, after, lineMap) {
		return 0, false, false
	}
	value := baseLines[line-1]
	duplicate = baseCounts[value] > 1 || liveCounts[value] > 1
	return mapped - line, duplicate, true
}

func validateAllAnchorContext(line, mapped, before, after int, lineMap map[int]int) bool {
	checked := false
	if before != 0 {
		checked = true
		contextMapped, ok := lineMap[before]
		if !ok || contextMapped != mapped-(line-before) {
			return false
		}
	}
	if after != 0 {
		checked = true
		contextMapped, ok := lineMap[after]
		if !ok || contextMapped != mapped+(after-line) {
			return false
		}
	}
	return checked
}

func duplicateRunContextUnique(baseLines, liveLines []string, runStart, runEnd, before, after int, lineMap map[int]int) bool {
	contextStart, contextEnd := runStart, runEnd
	if before != 0 {
		contextStart = before
	}
	if after != 0 {
		contextEnd = after
	}

	liveStart, ok := lineMap[contextStart]
	if !ok {
		return false
	}
	liveEnd := liveStart + contextEnd - contextStart
	if liveStart < 1 || liveEnd > len(liveLines) {
		return false
	}
	sequence := baseLines[contextStart-1 : contextEnd]
	if !slices.Equal(sequence, liveLines[liveStart-1:liveEnd]) {
		return false
	}
	return countLineSequence(baseLines, sequence) == 1 && countLineSequence(liveLines, sequence) == 1
}

func countLineSequence(lines, sequence []string) int {
	if len(sequence) == 0 || len(sequence) > len(lines) {
		return 0
	}
	count := 0
	for start := 0; start+len(sequence) <= len(lines); start++ {
		if slices.Equal(lines[start:start+len(sequence)], sequence) {
			count++
			if count > 1 {
				return count
			}
		}
	}
	return count
}

func lineCounts(lines []string) map[string]int {
	counts := make(map[string]int, len(lines))
	for _, line := range lines {
		counts[line]++
	}
	return counts
}

// unchangedLineMap returns the 1-based base-to-live mapping for unchanged lines
// selected by a Myers shortest-edit script. Recovery normally sees small drift,
// so retaining the trace is cheap; maxRecoveryEditDistance makes large rewrites
// fail closed rather than allocate in proportion to the whole file product.
func unchangedLineMap(base, live []string) (map[int]int, bool) {
	limit := len(base) + len(live)
	if limit > maxRecoveryEditDistance {
		limit = maxRecoveryEditDistance
	}
	offset := limit + 1
	v := make([]int, 2*limit+3)
	for i := range v {
		v[i] = -1
	}
	v[offset+1] = 0
	trace := make([][]int, 0, limit+1)

	for distance := 0; distance <= limit; distance++ {
		trace = append(trace, append([]int(nil), v...))
		for diagonal := -distance; diagonal <= distance; diagonal += 2 {
			index := offset + diagonal
			var x int
			if diagonal == -distance || (diagonal != distance && v[index-1] < v[index+1]) {
				x = v[index+1]
			} else {
				x = v[index-1] + 1
			}
			if x < 0 {
				continue
			}
			y := x - diagonal
			if y < 0 {
				continue
			}
			for x < len(base) && y < len(live) && base[x] == live[y] {
				x++
				y++
			}
			v[index] = x
			if x == len(base) && y == len(live) {
				return backtrackLineMap(trace, distance, base, live, offset), true
			}
		}
	}
	return nil, false
}

func backtrackLineMap(trace [][]int, distance int, base, live []string, offset int) map[int]int {
	mapped := make(map[int]int, min(len(base), len(live)))
	x, y := len(base), len(live)
	for depth := distance; depth > 0; depth-- {
		v := trace[depth]
		diagonal := x - y
		index := offset + diagonal
		var previousDiagonal int
		if diagonal == -depth || (diagonal != depth && v[index-1] < v[index+1]) {
			previousDiagonal = diagonal + 1
		} else {
			previousDiagonal = diagonal - 1
		}
		previousX := v[offset+previousDiagonal]
		previousY := previousX - previousDiagonal
		for x > previousX && y > previousY {
			mapped[x] = y
			x--
			y--
		}
		x, y = previousX, previousY
	}
	for x > 0 && y > 0 && base[x-1] == live[y-1] {
		mapped[x] = y
		x--
		y--
	}
	return mapped
}
