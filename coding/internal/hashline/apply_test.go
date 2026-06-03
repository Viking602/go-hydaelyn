package hashline

import (
	"errors"
	"reflect"
	"testing"
)

// sec is a small constructor for a Section with arbitrary ops.
func sec(ops ...Op) Section {
	return Section{Path: "x.go", Tag: "0000", Ops: ops}
}

func TestApply_ReplaceOneWithOne(t *testing.T) {
	res, err := Apply("a\nb\nc", sec(Op{Kind: OpReplace, Start: 2, End: 2, Body: []string{"B"}}))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Text != "a\nB\nc" {
		t.Errorf("Text = %q", res.Text)
	}
	if res.FirstChangedLine != 2 {
		t.Errorf("FirstChangedLine = %d, want 2", res.FirstChangedLine)
	}
}

func TestApply_ReplaceOneWithMany(t *testing.T) {
	res, err := Apply("a\nb\nc", sec(Op{Kind: OpReplace, Start: 2, End: 2, Body: []string{"x", "y", "z"}}))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Text != "a\nx\ny\nz\nc" {
		t.Errorf("Text = %q", res.Text)
	}
}

func TestApply_ReplaceManyWithOne(t *testing.T) {
	res, err := Apply("a\nb\nc\nd", sec(Op{Kind: OpReplace, Start: 2, End: 3, Body: []string{"M"}}))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Text != "a\nM\nd" {
		t.Errorf("Text = %q", res.Text)
	}
	if res.FirstChangedLine != 2 {
		t.Errorf("FirstChangedLine = %d, want 2", res.FirstChangedLine)
	}
}

func TestApply_DeleteOne(t *testing.T) {
	res, err := Apply("a\nb\nc", sec(Op{Kind: OpDelete, Start: 2, End: 2}))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Text != "a\nc" {
		t.Errorf("Text = %q", res.Text)
	}
}

func TestApply_DeleteRange(t *testing.T) {
	res, err := Apply("a\nb\nc\nd\ne", sec(Op{Kind: OpDelete, Start: 2, End: 4}))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Text != "a\ne" {
		t.Errorf("Text = %q", res.Text)
	}
}

func TestApply_DeleteAllLines(t *testing.T) {
	res, err := Apply("a\nb\nc", sec(Op{Kind: OpDelete, Start: 1, End: 3}))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Text != "" {
		t.Errorf("Text = %q, want empty", res.Text)
	}
}

func TestApply_InsertHead(t *testing.T) {
	res, err := Apply("a\nb", sec(Op{Kind: OpInsertHead, Body: []string{"// header"}}))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Text != "// header\na\nb" {
		t.Errorf("Text = %q", res.Text)
	}
	if res.FirstChangedLine != 1 {
		t.Errorf("FirstChangedLine = %d, want 1", res.FirstChangedLine)
	}
}

func TestApply_InsertTail(t *testing.T) {
	res, err := Apply("a\nb", sec(Op{Kind: OpInsertTail, Body: []string{"// footer"}}))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Text != "a\nb\n// footer" {
		t.Errorf("Text = %q", res.Text)
	}
	if res.FirstChangedLine != 3 {
		t.Errorf("FirstChangedLine = %d, want 3", res.FirstChangedLine)
	}
}

func TestApply_InsertBefore(t *testing.T) {
	res, err := Apply("a\nb\nc", sec(Op{Kind: OpInsertBefore, Start: 2, End: 2, Body: []string{"X"}}))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Text != "a\nX\nb\nc" {
		t.Errorf("Text = %q", res.Text)
	}
}

func TestApply_InsertBeforeFirst(t *testing.T) {
	res, err := Apply("a\nb", sec(Op{Kind: OpInsertBefore, Start: 1, End: 1, Body: []string{"X"}}))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Text != "X\na\nb" {
		t.Errorf("Text = %q", res.Text)
	}
}

func TestApply_InsertAfter(t *testing.T) {
	res, err := Apply("a\nb\nc", sec(Op{Kind: OpInsertAfter, Start: 2, End: 2, Body: []string{"X"}}))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Text != "a\nb\nX\nc" {
		t.Errorf("Text = %q", res.Text)
	}
}

func TestApply_InsertAfterLast(t *testing.T) {
	res, err := Apply("a\nb", sec(Op{Kind: OpInsertAfter, Start: 2, End: 2, Body: []string{"X"}}))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Text != "a\nb\nX" {
		t.Errorf("Text = %q", res.Text)
	}
}

func TestApply_MultipleOpsUseOriginalLineNumbers(t *testing.T) {
	// Replacing line 1 (one->two lines) must NOT shift the delete of line 4.
	res, err := Apply("a\nb\nc\nd\ne", sec(
		Op{Kind: OpReplace, Start: 1, End: 1, Body: []string{"A1", "A2"}},
		Op{Kind: OpDelete, Start: 4, End: 4},
	))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	// original: a b c d e ; replace 1 -> A1,A2 ; delete original line 4 (d)
	if res.Text != "A1\nA2\nb\nc\ne" {
		t.Errorf("Text = %q", res.Text)
	}
	if res.FirstChangedLine != 1 {
		t.Errorf("FirstChangedLine = %d, want 1", res.FirstChangedLine)
	}
}

func TestApply_OpsAppliedRegardlessOfInputOrder(t *testing.T) {
	// Same ops in a different input order must produce the same file,
	// because indices reference the original.
	forward, err := Apply("a\nb\nc\nd", sec(
		Op{Kind: OpReplace, Start: 1, End: 1, Body: []string{"A"}},
		Op{Kind: OpReplace, Start: 4, End: 4, Body: []string{"D"}},
	))
	if err != nil {
		t.Fatalf("Apply forward: %v", err)
	}
	reverse, err := Apply("a\nb\nc\nd", sec(
		Op{Kind: OpReplace, Start: 4, End: 4, Body: []string{"D"}},
		Op{Kind: OpReplace, Start: 1, End: 1, Body: []string{"A"}},
	))
	if err != nil {
		t.Fatalf("Apply reverse: %v", err)
	}
	if forward.Text != reverse.Text || forward.Text != "A\nb\nc\nD" {
		t.Errorf("order dependence: forward=%q reverse=%q", forward.Text, reverse.Text)
	}
}

func TestApply_InsertBeforeAndReplaceAtBoundary(t *testing.T) {
	// insert before 2 sits at the top boundary of replace 2..3 — allowed.
	res, err := Apply("a\nb\nc\nd", sec(
		Op{Kind: OpInsertBefore, Start: 2, End: 2, Body: []string{"X"}},
		Op{Kind: OpReplace, Start: 2, End: 3, Body: []string{"M"}},
	))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Text != "a\nX\nM\nd" {
		t.Errorf("Text = %q", res.Text)
	}
}

func TestApply_RejectCases(t *testing.T) {
	tests := []struct {
		name string
		text string
		ops  []Op
	}{
		{
			name: "replace out of bounds high",
			text: "a\nb",
			ops:  []Op{{Kind: OpReplace, Start: 2, End: 5, Body: []string{"x"}}},
		},
		{
			name: "delete out of bounds",
			text: "a\nb",
			ops:  []Op{{Kind: OpDelete, Start: 3, End: 3}},
		},
		{
			name: "insert before out of bounds",
			text: "a\nb",
			ops:  []Op{{Kind: OpInsertBefore, Start: 3, End: 3, Body: []string{"x"}}},
		},
		{
			name: "insert after out of bounds",
			text: "a\nb",
			ops:  []Op{{Kind: OpInsertAfter, Start: 3, End: 3, Body: []string{"x"}}},
		},
		{
			name: "overlapping replace ranges",
			text: "a\nb\nc\nd",
			ops: []Op{
				{Kind: OpReplace, Start: 1, End: 2, Body: []string{"x"}},
				{Kind: OpReplace, Start: 2, End: 3, Body: []string{"y"}},
			},
		},
		{
			name: "same line replaced and deleted",
			text: "a\nb\nc",
			ops: []Op{
				{Kind: OpReplace, Start: 2, End: 2, Body: []string{"x"}},
				{Kind: OpDelete, Start: 2, End: 2},
			},
		},
		{
			name: "two inserts at same anchor before",
			text: "a\nb\nc",
			ops: []Op{
				{Kind: OpInsertBefore, Start: 2, End: 2, Body: []string{"x"}},
				{Kind: OpInsertBefore, Start: 2, End: 2, Body: []string{"y"}},
			},
		},
		{
			name: "insert after N equals insert before N+1",
			text: "a\nb\nc",
			ops: []Op{
				{Kind: OpInsertAfter, Start: 1, End: 1, Body: []string{"x"}},
				{Kind: OpInsertBefore, Start: 2, End: 2, Body: []string{"y"}},
			},
		},
		{
			name: "insert after lands inside replace range",
			text: "a\nb\nc\nd",
			ops: []Op{
				{Kind: OpReplace, Start: 2, End: 3, Body: []string{"m"}},
				{Kind: OpInsertAfter, Start: 2, End: 2, Body: []string{"x"}},
			},
		},
		{
			name: "two head inserts conflict",
			text: "a\nb",
			ops: []Op{
				{Kind: OpInsertHead, Body: []string{"x"}},
				{Kind: OpInsertHead, Body: []string{"y"}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Apply(tt.text, sec(tt.ops...))
			if err == nil {
				t.Fatalf("Apply(%q) = nil error, want rejection", tt.text)
			}
			var ae *ApplyError
			if !errors.As(err, &ae) {
				t.Errorf("error %v is not an *ApplyError", err)
			}
		})
	}
}

func TestApply_Noop(t *testing.T) {
	// Replacing a line with the same content is a no-op.
	_, err := Apply("a\nb\nc", sec(Op{Kind: OpReplace, Start: 2, End: 2, Body: []string{"b"}}))
	if !errors.Is(err, ErrNoop) {
		t.Errorf("want ErrNoop, got %v", err)
	}
}

func TestApply_NoopWhenOpsCancel(t *testing.T) {
	// delete line 2, then re-insert identical content before line 2: the file
	// is unchanged, so the whole patch is a no-op rather than two writes.
	_, err := Apply("a\nb\nc", sec(
		Op{Kind: OpDelete, Start: 2, End: 2},
		Op{Kind: OpInsertBefore, Start: 2, End: 2, Body: []string{"b"}},
	))
	if !errors.Is(err, ErrNoop) {
		t.Errorf("want ErrNoop for canceling ops, got %v", err)
	}
}

func TestApply_NoopWhenReplaceWithSameMultiline(t *testing.T) {
	_, err := Apply("a\nb\nc", sec(Op{Kind: OpReplace, Start: 1, End: 3, Body: []string{"a", "b", "c"}}))
	if !errors.Is(err, ErrNoop) {
		t.Errorf("want ErrNoop, got %v", err)
	}
}

// TestApply_InsertIntoDeletedOrReplacedRangeRejected covers the conflict cases
// where an insert anchor lands strictly inside a range that another op removes
// — inserting into vanished context is meaningless and must be rejected, while
// an insert at the boundary of the range is allowed (TestApply_InsertBeforeAndReplaceAtBoundary).
func TestApply_InsertIntoDeletedOrReplacedRangeRejected(t *testing.T) {
	tests := []struct {
		name string
		ops  []Op
	}{
		{
			name: "insert before last line of a deleted range",
			ops: []Op{
				{Kind: OpDelete, Start: 2, End: 3},
				{Kind: OpInsertBefore, Start: 3, End: 3, Body: []string{"X"}},
			},
		},
		{
			name: "insert after first line of a deleted range",
			ops: []Op{
				{Kind: OpDelete, Start: 2, End: 3},
				{Kind: OpInsertAfter, Start: 2, End: 2, Body: []string{"X"}},
			},
		},
		{
			name: "insert before last line of a replaced range",
			ops: []Op{
				{Kind: OpReplace, Start: 2, End: 3, Body: []string{"M"}},
				{Kind: OpInsertBefore, Start: 3, End: 3, Body: []string{"X"}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Apply("a\nb\nc\nd", sec(tt.ops...))
			var ae *ApplyError
			if !errors.As(err, &ae) {
				t.Fatalf("want *ApplyError, got %v", err)
			}
		})
	}
}

// TestApply_HeadCollidesWithInsertBeforeFirst documents that "insert head" and
// "insert before 1" address the same gap and therefore conflict, the same way
// "insert after N" and "insert before N+1" do.
func TestApply_HeadCollidesWithInsertBeforeFirst(t *testing.T) {
	_, err := Apply("a\nb", sec(
		Op{Kind: OpInsertHead, Body: []string{"H"}},
		Op{Kind: OpInsertBefore, Start: 1, End: 1, Body: []string{"X"}},
	))
	var ae *ApplyError
	if !errors.As(err, &ae) {
		t.Fatalf("want *ApplyError for head vs before-1, got %v", err)
	}
}

// TestApply_TailCollidesWithInsertAfterLast documents that "insert tail" and
// "insert after <last line>" address the same gap and conflict.
func TestApply_TailCollidesWithInsertAfterLast(t *testing.T) {
	_, err := Apply("a\nb", sec(
		Op{Kind: OpInsertTail, Body: []string{"T"}},
		Op{Kind: OpInsertAfter, Start: 2, End: 2, Body: []string{"X"}},
	))
	var ae *ApplyError
	if !errors.As(err, &ae) {
		t.Fatalf("want *ApplyError for tail vs after-last, got %v", err)
	}
}

// TestApply_WrapLineWithBeforeAndAfter shows that inserting before and after
// the SAME line are distinct gaps and compose, wrapping the line.
func TestApply_WrapLineWithBeforeAndAfter(t *testing.T) {
	res, err := Apply("a\nb\nc", sec(
		Op{Kind: OpInsertBefore, Start: 2, End: 2, Body: []string{"X"}},
		Op{Kind: OpInsertAfter, Start: 2, End: 2, Body: []string{"Y"}},
	))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Text != "a\nX\nb\nY\nc" {
		t.Errorf("Text = %q, want %q", res.Text, "a\nX\nb\nY\nc")
	}
}

// TestApply_ReplaceAndInsertAfterSameLine shows replace N composes with an
// insert after N (the insert lands after the replacement block).
func TestApply_ReplaceAndInsertAfterSameLine(t *testing.T) {
	res, err := Apply("a\nb\nc", sec(
		Op{Kind: OpReplace, Start: 2, End: 2, Body: []string{"B1", "B2"}},
		Op{Kind: OpInsertAfter, Start: 2, End: 2, Body: []string{"X"}},
	))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Text != "a\nB1\nB2\nX\nc" {
		t.Errorf("Text = %q, want %q", res.Text, "a\nB1\nB2\nX\nc")
	}
}

// TestApply_TrailingNewlineInsertSemantics pins down how head/tail inserts
// interact with the final-empty-line element a trailing newline produces. The
// file "a\nb\n" reads as three lines (a, b, <empty>); insert tail appends after
// that empty final line, and insert head goes above line 1.
func TestApply_TrailingNewlineInsertSemantics(t *testing.T) {
	tail, err := Apply("a\nb\n", sec(Op{Kind: OpInsertTail, Body: []string{"T"}}))
	if err != nil {
		t.Fatalf("tail Apply: %v", err)
	}
	if tail.Text != "a\nb\n\nT" {
		t.Errorf("insert tail = %q, want %q", tail.Text, "a\nb\n\nT")
	}
	head, err := Apply("a\nb\n", sec(Op{Kind: OpInsertHead, Body: []string{"H"}}))
	if err != nil {
		t.Fatalf("head Apply: %v", err)
	}
	if head.Text != "H\na\nb\n" {
		t.Errorf("insert head = %q, want %q", head.Text, "H\na\nb\n")
	}
}

// TestApply_ManyOpsAllOnOriginalIndices mixes replace (line-count-changing),
// insert, and delete in one section and confirms every op still references the
// ORIGINAL file, independent of input order.
func TestApply_ManyOpsAllOnOriginalIndices(t *testing.T) {
	ops := []Op{
		{Kind: OpInsertHead, Body: []string{"H"}},
		{Kind: OpReplace, Start: 1, End: 1, Body: []string{"A1", "A2"}}, // grows the file
		{Kind: OpDelete, Start: 3, End: 3},                              // original line 3
		{Kind: OpInsertAfter, Start: 4, End: 4, Body: []string{"X"}},    // after original line 4
		{Kind: OpInsertTail, Body: []string{"T"}},
	}
	// original: a b c d e
	// head H; line1 a -> A1,A2; line3 c deleted; after line4 d add X; tail T
	want := "H\nA1\nA2\nb\nd\nX\ne\nT"
	res, err := Apply("a\nb\nc\nd\ne", sec(ops...))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Text != want {
		t.Errorf("Text = %q, want %q", res.Text, want)
	}
	// Reversed input order must give the identical file.
	rev := make([]Op, len(ops))
	for i := range ops {
		rev[len(ops)-1-i] = ops[i]
	}
	res2, err := Apply("a\nb\nc\nd\ne", sec(rev...))
	if err != nil {
		t.Fatalf("Apply reversed: %v", err)
	}
	if res2.Text != want {
		t.Errorf("reversed order Text = %q, want %q", res2.Text, want)
	}
}

func TestApply_InsertHeadIntoEmptyFileWarns(t *testing.T) {
	res, err := Apply("", sec(Op{Kind: OpInsertHead, Body: []string{"x"}}))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Text != "x" {
		t.Errorf("Text = %q, want %q", res.Text, "x")
	}
	if len(res.Warnings) == 0 {
		t.Errorf("expected a warning for head insert into empty file")
	}
}

// TestApply_HeadAndTailIntoEmptyFile guards the empty-file conflict-axis fix:
// on an empty file the head gap (point 1) and the tail gap (point 2) must stay
// distinct, so a head insert and a tail insert into "" do NOT falsely collide
// the way "after the last line" (2n+1 == 1 when n == 0) would. Both bodies land,
// head before tail, each carrying an empty-file warning.
func TestApply_HeadAndTailIntoEmptyFile(t *testing.T) {
	res, err := Apply("", sec(
		Op{Kind: OpInsertHead, Body: []string{"H"}},
		Op{Kind: OpInsertTail, Body: []string{"T"}},
	))
	if err != nil {
		t.Fatalf("head+tail into empty file must not conflict, got: %v", err)
	}
	if res.Text != "H\nT" {
		t.Errorf("Text = %q, want %q", res.Text, "H\nT")
	}
	if len(res.Warnings) != 2 {
		t.Errorf("expected a warning for each empty-file insert, got %d: %v", len(res.Warnings), res.Warnings)
	}
}

func TestApply_HeadAndTailTogether(t *testing.T) {
	res, err := Apply("a\nb", sec(
		Op{Kind: OpInsertHead, Body: []string{"H"}},
		Op{Kind: OpInsertTail, Body: []string{"T"}},
	))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Text != "H\na\nb\nT" {
		t.Errorf("Text = %q", res.Text)
	}
}

func TestApply_PreservesTrailingNewlineSemantics(t *testing.T) {
	// "a\nb\n" splits to [a, b, ""]; replacing line 2 keeps the trailing
	// empty element (the final newline).
	res, err := Apply("a\nb\n", sec(Op{Kind: OpReplace, Start: 2, End: 2, Body: []string{"B"}}))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Text != "a\nB\n" {
		t.Errorf("Text = %q, want %q", res.Text, "a\nB\n")
	}
}

func TestFirstChangedLine(t *testing.T) {
	tests := []struct {
		old, new string
		want     int
	}{
		{"a\nb\nc", "a\nb\nc", 0},
		{"a\nb\nc", "a\nX\nc", 2},
		{"a\nb", "a\nb\nc", 3},
		{"a\nb\nc", "a\nb", 3},
		{"a", "X", 1},
	}
	for _, tt := range tests {
		if got := firstChangedLine(tt.old, tt.new); got != tt.want {
			t.Errorf("firstChangedLine(%q,%q) = %d, want %d", tt.old, tt.new, got, tt.want)
		}
	}
}

func TestSplitJoinLinesRoundTrip(t *testing.T) {
	for _, s := range []string{"", "a", "a\nb", "a\nb\n", "\n", "a\n\nb"} {
		if got := joinLines(splitLines(s)); got != s {
			t.Errorf("round-trip %q -> %q", s, got)
		}
	}
}

func TestApply_WarningsSliceShape(t *testing.T) {
	// A successful, non-empty-file edit carries no warnings.
	res, err := Apply("a\nb", sec(Op{Kind: OpReplace, Start: 1, End: 1, Body: []string{"A"}}))
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !reflect.DeepEqual(res.Warnings, []string(nil)) {
		t.Errorf("Warnings = %#v, want nil", res.Warnings)
	}
}
