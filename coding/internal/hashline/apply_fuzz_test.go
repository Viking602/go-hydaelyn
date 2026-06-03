package hashline

import (
	"math/rand"
	"strings"
	"testing"
)

// These property-based tests check the original-line-anchored applier against
// two independent oracles: an all-pairs interval-overlap checker for conflict
// detection, and a separately written rebuilder for the apply result. They
// guard the two subtle invariants the protocol depends on — that an earlier op
// never shifts a later op's indices, and that overlapping/same-line edits are
// always rejected — across a large random sample rather than a handful of
// hand-picked cases.

// referenceClaims rebuilds the doubled-axis claims for a set of events using
// the same mapping production code uses, kept separate so a divergence in the
// production claim construction is itself caught.
func referenceClaims(events []editEvent, n int) []claim {
	claims := make([]claim, 0, len(events))
	for i, e := range events {
		switch e.op.Kind {
		case OpReplace, OpDelete:
			claims = append(claims, claim{lo: 2 * e.start, hi: 2 * e.end, op: e.op.Kind, pos: i})
		case OpInsertBefore:
			p := 2*e.start - 1
			claims = append(claims, claim{lo: p, hi: p, op: e.op.Kind, pos: i})
		case OpInsertAfter:
			p := 2*e.start + 1
			claims = append(claims, claim{lo: p, hi: p, op: e.op.Kind, pos: i})
		case OpInsertHead:
			claims = append(claims, claim{lo: 1, hi: 1, op: e.op.Kind, pos: i})
		case OpInsertTail:
			p := 2*n + 1
			if n == 0 {
				// Mirror production: on an empty file the tail gap is point 2 so
				// it stays distinct from the head gap (point 1); 2n+1 == 1 would
				// otherwise alias head and tail together.
				p = 2
			}
			claims = append(claims, claim{lo: p, hi: p, op: e.op.Kind, pos: i})
		}
	}
	return claims
}

// referenceConflict reports whether any two events overlap, by brute-force
// all-pairs comparison. It is the oracle for checkConflicts, which uses a
// sort-and-scan-adjacent strategy that must be equivalent.
func referenceConflict(events []editEvent, n int) bool {
	claims := referenceClaims(events, n)
	for i := 0; i < len(claims); i++ {
		for j := i + 1; j < len(claims); j++ {
			a, b := claims[i], claims[j]
			if a.lo <= b.hi && b.lo <= a.hi {
				return true
			}
		}
	}
	return false
}

func TestApply_FuzzConflictDetectorMatchesOracle(t *testing.T) {
	rng := rand.New(rand.NewSource(12345))
	kinds := []OpKind{OpReplace, OpDelete, OpInsertBefore, OpInsertAfter, OpInsertHead, OpInsertTail}
	const iters = 100000
	for iter := 0; iter < iters; iter++ {
		n := rng.Intn(6) // 0..5-line file
		numOps := 1 + rng.Intn(4)
		var events []editEvent
		for k := 0; k < numOps; k++ {
			kind := kinds[rng.Intn(len(kinds))]
			ev := editEvent{op: Op{Kind: kind}}
			switch kind {
			case OpReplace, OpDelete:
				if n == 0 {
					continue
				}
				s := 1 + rng.Intn(n)
				e := s + rng.Intn(n-s+1)
				ev.start, ev.end = s, e
				ev.op.Start, ev.op.End = s, e
			case OpInsertBefore, OpInsertAfter:
				if n == 0 {
					continue
				}
				s := 1 + rng.Intn(n)
				ev.start, ev.end = s, s
				ev.op.Start, ev.op.End = s, s
			}
			events = append(events, ev)
		}
		if len(events) == 0 {
			continue
		}
		got := checkConflicts(events, n) != nil
		if want := referenceConflict(events, n); got != want {
			t.Fatalf("conflict mismatch n=%d events=%+v: checkConflicts=%v oracle=%v", n, events, got, want)
		}
	}
}

// referenceApply is an independent rebuilder used as the oracle for Apply on
// conflict-free patches. It composes ops by ORIGINAL line index, matching the
// documented head/before/replace-or-keep/after/tail walk, but is written apart
// from production rebuild() so a real divergence surfaces.
func referenceApply(ops []Op, n int, lines []string) string {
	var head, tail []string
	before := map[int][]string{}
	after := map[int][]string{}
	repl := map[int][]string{}
	hasRepl := map[int]bool{}
	del := map[int]bool{}
	for _, op := range ops {
		switch op.Kind {
		case OpInsertHead:
			head = append(head, op.Body...)
		case OpInsertTail:
			tail = append(tail, op.Body...)
		case OpInsertBefore:
			before[op.Start] = append(before[op.Start], op.Body...)
		case OpInsertAfter:
			after[op.Start] = append(after[op.Start], op.Body...)
		case OpDelete:
			for k := op.Start; k <= op.End; k++ {
				del[k] = true
			}
		case OpReplace:
			repl[op.Start] = append(repl[op.Start], op.Body...)
			hasRepl[op.Start] = true
			for k := op.Start; k <= op.End; k++ {
				del[k] = true
			}
		}
	}
	var out []string
	out = append(out, head...)
	for i := 1; i <= n; i++ {
		out = append(out, before[i]...)
		if hasRepl[i] {
			out = append(out, repl[i]...)
		} else if !del[i] {
			out = append(out, lines[i-1])
		}
		out = append(out, after[i]...)
	}
	out = append(out, tail...)
	return strings.Join(out, "\n")
}

func TestApply_FuzzMatchesReference(t *testing.T) {
	rng := rand.New(rand.NewSource(987654))
	kinds := []OpKind{OpReplace, OpDelete, OpInsertBefore, OpInsertAfter, OpInsertHead, OpInsertTail}
	body := func() []string {
		m := rng.Intn(3)
		if m == 0 {
			return nil
		}
		b := make([]string, m)
		for i := range b {
			b[i] = string(rune('P' + rng.Intn(8)))
		}
		return b
	}
	nonEmptyBody := func() []string {
		b := body()
		if len(b) == 0 {
			return []string{"I"}
		}
		return b
	}

	const iters = 150000
	for iter := 0; iter < iters; iter++ {
		n := 1 + rng.Intn(6)
		lines := make([]string, n)
		for i := range lines {
			lines[i] = string(rune('a' + i))
		}
		text := strings.Join(lines, "\n")

		numOps := 1 + rng.Intn(4)
		var ops []Op
		for k := 0; k < numOps; k++ {
			kind := kinds[rng.Intn(len(kinds))]
			op := Op{Kind: kind}
			switch kind {
			case OpReplace, OpDelete:
				s := 1 + rng.Intn(n)
				e := s + rng.Intn(n-s+1)
				op.Start, op.End = s, e
				if kind == OpReplace {
					op.Body = body()
				}
			case OpInsertBefore, OpInsertAfter:
				s := 1 + rng.Intn(n)
				op.Start, op.End = s, s
				op.Body = nonEmptyBody()
			case OpInsertHead, OpInsertTail:
				op.Body = nonEmptyBody()
			}
			ops = append(ops, op)
		}

		res, err := Apply(text, sec(ops...))

		events := make([]editEvent, len(ops))
		for i, op := range ops {
			events[i] = editEvent{op: op, start: op.Start, end: op.End}
			if op.Kind == OpInsertHead || op.Kind == OpInsertTail {
				events[i].start, events[i].end = 0, 0
			}
		}
		if referenceConflict(events, n) {
			if err == nil {
				t.Fatalf("expected conflict rejection: ops=%+v text=%q got=%q", ops, text, res.Text)
			}
			continue
		}

		want := referenceApply(ops, n, lines)
		if err != nil {
			if err == ErrNoop && want == text {
				continue
			}
			t.Fatalf("unexpected error: ops=%+v text=%q err=%v (want %q)", ops, text, err, want)
		}
		if res.Text != want {
			t.Fatalf("rebuild mismatch: ops=%+v text=%q\n got=%q\nwant=%q", ops, text, res.Text, want)
		}
		if wantFCL := referenceFirstChanged(text, want); res.FirstChangedLine != wantFCL {
			t.Fatalf("FirstChangedLine mismatch: ops=%+v got=%d want=%d", ops, res.FirstChangedLine, wantFCL)
		}
	}
}

// referenceFirstChanged is an independent first-changed-line oracle.
func referenceFirstChanged(a, b string) int {
	la := splitLines(a)
	lb := splitLines(b)
	lim := len(la)
	if len(lb) < lim {
		lim = len(lb)
	}
	for i := 0; i < lim; i++ {
		if la[i] != lb[i] {
			return i + 1
		}
	}
	if len(la) != len(lb) {
		return lim + 1
	}
	return 0
}
