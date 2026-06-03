package hashline

import (
	"fmt"
	"sort"
	"strings"
)

// ApplyResult is the outcome of applying one section's ops to a file's
// LF-internal text.
type ApplyResult struct {
	// Text is the new LF-internal content.
	Text string
	// FirstChangedLine is the 1-based line number of the first line that
	// differs between the original and the result, or 0 if unchanged.
	FirstChangedLine int
	// Warnings carries non-fatal advisories (e.g. a head/tail insert into
	// an empty file). It is never nil-required; callers may ignore it.
	Warnings []string
}

// ApplyError reports an apply-time validation failure (out-of-bounds,
// overlap, or same-line double edit). It is not a parse error; the patch
// was well-formed but cannot be applied to this file.
type ApplyError struct {
	Op  OpKind
	Msg string
}

// Error renders the apply failure.
func (e *ApplyError) Error() string {
	return fmt.Sprintf("hashline: cannot apply %s: %s", e.Op, e.Msg)
}

// editEvent is a lowered op keyed by original line index. Replace and
// delete consume the inclusive original range [Start,End]; the three
// insert kinds consume no original lines and attach new lines at an anchor.
type editEvent struct {
	op    Op
	start int // 1-based original line, inclusive (0 for head/tail)
	end   int // 1-based original line, inclusive (0 for head/tail)
}

// Apply applies sec.Ops to LF-internal text. All op line numbers reference
// the ORIGINAL file; an earlier op never shifts a later op's indices.
// Overlapping ranges, two ops touching the same original line, and
// out-of-bounds references are rejected. If the result equals the input,
// Apply returns ErrNoop.
func Apply(text string, sec Section) (ApplyResult, error) {
	lines := splitLines(text)
	n := len(lines)

	events, warnings, err := lowerOps(sec.Ops, n)
	if err != nil {
		return ApplyResult{}, err
	}

	// Detect conflicts on original-line coverage. Inserts before/after
	// claim a zero-width point so two inserts at the same anchor, or an
	// insert landing inside a replaced/deleted range, are rejected.
	if err := checkConflicts(events, n); err != nil {
		return ApplyResult{}, err
	}

	out := rebuild(lines, events, n)
	newText := joinLines(out)

	if newText == text {
		return ApplyResult{}, ErrNoop
	}

	res := ApplyResult{
		Text:             newText,
		FirstChangedLine: firstChangedLine(text, newText),
		Warnings:         warnings,
	}
	return res, nil
}

// lowerOps validates each op against the file length and lowers it to an
// editEvent. n is the original line count.
func lowerOps(ops []Op, n int) ([]editEvent, []string, error) {
	events := make([]editEvent, 0, len(ops))
	var warnings []string
	for _, op := range ops {
		switch op.Kind {
		case OpReplace, OpDelete:
			if op.Start < 1 || op.End > n || op.Start > op.End {
				return nil, nil, &ApplyError{Op: op.Kind, Msg: fmt.Sprintf(
					"range %d..%d is out of bounds for a %d-line file", op.Start, op.End, n)}
			}
			events = append(events, editEvent{op: op, start: op.Start, end: op.End})

		case OpInsertBefore:
			// Valid anchors are lines 1..n. "before 1" is equivalent to head.
			if op.Start < 1 || op.Start > n {
				return nil, nil, &ApplyError{Op: op.Kind, Msg: fmt.Sprintf(
					"anchor line %d is out of bounds for a %d-line file", op.Start, n)}
			}
			events = append(events, editEvent{op: op, start: op.Start, end: op.Start})

		case OpInsertAfter:
			if op.Start < 1 || op.Start > n {
				return nil, nil, &ApplyError{Op: op.Kind, Msg: fmt.Sprintf(
					"anchor line %d is out of bounds for a %d-line file", op.Start, n)}
			}
			events = append(events, editEvent{op: op, start: op.Start, end: op.Start})

		case OpInsertHead, OpInsertTail:
			if n == 0 {
				warnings = append(warnings, fmt.Sprintf(
					"%s into an empty file", op.Kind))
			}
			events = append(events, editEvent{op: op, start: 0, end: 0})

		default:
			return nil, nil, &ApplyError{Op: op.Kind, Msg: "unknown operation"}
		}
	}
	return events, warnings, nil
}

// lineClaim describes the original-line region an event reserves for
// conflict detection. Replace/delete claim the inclusive span [lo,hi].
// insert-before N claims the gap just before N (modeled as point 2N-1 on a
// doubled axis); insert-after N claims the gap just after N (point 2N+1).
// Real lines occupy even points 2k. This lets a replace of [a,b] (points
// 2a..2b) collide with an insert that lands strictly inside, while
// allowing an insert-before the first replaced line at one boundary.
type claim struct {
	lo  int
	hi  int
	op  OpKind
	pos int // input order, for stable conflict reporting
}

// checkConflicts rejects overlapping ranges and same-line double edits by
// mapping every event onto a doubled integer axis and looking for
// overlapping claims.
func checkConflicts(events []editEvent, n int) error {
	claims := make([]claim, 0, len(events))
	for i, e := range events {
		switch e.op.Kind {
		case OpReplace, OpDelete:
			claims = append(claims, claim{lo: 2 * e.start, hi: 2 * e.end, op: e.op.Kind, pos: i})
		case OpInsertBefore:
			// Gap immediately before line Start.
			p := 2*e.start - 1
			claims = append(claims, claim{lo: p, hi: p, op: e.op.Kind, pos: i})
		case OpInsertAfter:
			// Gap immediately after line Start.
			p := 2*e.start + 1
			claims = append(claims, claim{lo: p, hi: p, op: e.op.Kind, pos: i})
		case OpInsertHead:
			// Gap before line 1.
			claims = append(claims, claim{lo: 1, hi: 1, op: e.op.Kind, pos: i})
		case OpInsertTail:
			// Gap after the last line. On a non-empty file that is point 2n+1,
			// which is already strictly greater than the head gap (point 1). On
			// an empty file there is no line n (2*0+1 == 1 would collide with
			// the head gap), so use point 2 to keep head and tail distinct —
			// spec §4.6 allows head and tail together, and rebuild orders head
			// before tail regardless.
			p := 2*n + 1
			if n == 0 {
				p = 2
			}
			claims = append(claims, claim{lo: p, hi: p, op: e.op.Kind, pos: i})
		}
	}

	sort.Slice(claims, func(i, j int) bool {
		if claims[i].lo != claims[j].lo {
			return claims[i].lo < claims[j].lo
		}
		return claims[i].hi < claims[j].hi
	})

	for i := 1; i < len(claims); i++ {
		prev := claims[i-1]
		curr := claims[i]
		if curr.lo <= prev.hi {
			return &ApplyError{Op: curr.op, Msg: fmt.Sprintf(
				"operation conflicts with an earlier %s on the same original line(s)", prev.op)}
		}
	}
	return nil
}

// rebuild constructs the new line slice from the original lines and the
// lowered events. Events are bucketed by original line so that ordering is
// driven entirely by original indices, never by the order of application.
func rebuild(lines []string, events []editEvent, n int) []string {
	var head []string // insert head bodies, in input order
	var tail []string // insert tail bodies, in input order

	// before[k] / after[k] hold inserts anchored at original line k.
	before := make(map[int][]string)
	after := make(map[int][]string)
	// replacement[k] holds the body that replaces original line k (only
	// the first line of a replaced range carries the body; the rest are
	// dropped). deleted[k] marks an original line as removed.
	replacement := make(map[int][]string)
	deleted := make(map[int]bool)
	hasReplacement := make(map[int]bool)

	for _, e := range events {
		switch e.op.Kind {
		case OpInsertHead:
			head = append(head, e.op.Body...)
		case OpInsertTail:
			tail = append(tail, e.op.Body...)
		case OpInsertBefore:
			before[e.start] = append(before[e.start], e.op.Body...)
		case OpInsertAfter:
			after[e.start] = append(after[e.start], e.op.Body...)
		case OpDelete:
			for k := e.start; k <= e.end; k++ {
				deleted[k] = true
			}
		case OpReplace:
			// Emit the body at the first line of the range; mark the whole
			// range deleted so its original lines are dropped.
			replacement[e.start] = append(replacement[e.start], e.op.Body...)
			hasReplacement[e.start] = true
			for k := e.start; k <= e.end; k++ {
				deleted[k] = true
			}
		}
	}

	out := make([]string, 0, len(lines)+len(head)+len(tail)+8)
	out = append(out, head...)
	for idx := 1; idx <= n; idx++ {
		out = append(out, before[idx]...)
		if hasReplacement[idx] {
			out = append(out, replacement[idx]...)
		} else if !deleted[idx] {
			out = append(out, lines[idx-1])
		}
		out = append(out, after[idx]...)
	}
	out = append(out, tail...)
	return out
}

// firstChangedLine returns the 1-based number of the first line that
// differs between original and updated text, or 0 if they are identical.
func firstChangedLine(original, updated string) int {
	o := splitLines(original)
	u := splitLines(updated)
	limit := len(o)
	if len(u) < limit {
		limit = len(u)
	}
	for i := 0; i < limit; i++ {
		if o[i] != u[i] {
			return i + 1
		}
	}
	if len(o) != len(u) {
		return limit + 1
	}
	return 0
}

// splitLines splits LF-internal text into lines. Empty text yields an
// empty slice (a zero-line file), so head/tail inserts into "" behave
// predictably. A trailing newline produces a final empty element, matching
// strings.Split semantics, which round-trips through joinLines.
func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(text, lf)
}

// joinLines is the inverse of splitLines.
func joinLines(lines []string) string {
	return strings.Join(lines, lf)
}
