package hashline

import (
	"errors"
	"strings"
	"testing"
)

func TestSummarizeOps(t *testing.T) {
	tests := []struct {
		name string
		ops  []Op
		want string
	}{
		{"empty", nil, ""},
		{"replace single line", []Op{{Kind: OpReplace, Start: 3, End: 3}}, "replace 3"},
		{"replace range", []Op{{Kind: OpReplace, Start: 3, End: 5}}, "replace 3..5"},
		{"delete single", []Op{{Kind: OpDelete, Start: 7, End: 7}}, "delete 7"},
		{"delete range", []Op{{Kind: OpDelete, Start: 7, End: 9}}, "delete 7..9"},
		{"insert before", []Op{{Kind: OpInsertBefore, Start: 2}}, "insert before 2"},
		{"insert after", []Op{{Kind: OpInsertAfter, Start: 4}}, "insert after 4"},
		{"insert head", []Op{{Kind: OpInsertHead}}, "insert head"},
		{"insert tail", []Op{{Kind: OpInsertTail}}, "insert tail"},
		{
			name: "multiple joined",
			ops: []Op{
				{Kind: OpReplace, Start: 1, End: 2},
				{Kind: OpInsertTail},
			},
			want: "replace 1..2, insert tail",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := summarizeOps(tt.ops); got != tt.want {
				t.Errorf("summarizeOps = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseError_Message(t *testing.T) {
	pe := &ParseError{Line: 7, Msg: "boom", Err: ErrParse}
	if got := pe.Error(); !strings.Contains(got, "line 7") || !strings.Contains(got, "boom") {
		t.Errorf("ParseError.Error() = %q", got)
	}
	if !errors.Is(pe, ErrParse) {
		t.Errorf("ParseError should wrap ErrParse")
	}
}

func TestApplyError_Message(t *testing.T) {
	ae := &ApplyError{Op: OpReplace, Msg: "out of bounds"}
	if got := ae.Error(); !strings.Contains(got, "replace") || !strings.Contains(got, "out of bounds") {
		t.Errorf("ApplyError.Error() = %q", got)
	}
}

func TestPatcher_StoreFallsBackToLazy(t *testing.T) {
	// A nil-store patcher uses LazySnapshotStore.
	p := &Patcher{}
	if _, ok := p.store().(LazySnapshotStore); !ok {
		t.Errorf("store() = %T, want LazySnapshotStore", p.store())
	}
	// An explicit store is returned as-is.
	var s LazySnapshotStore
	p2 := &Patcher{Snapshots: s}
	if p2.store() != SnapshotStore(s) {
		t.Errorf("store() did not return the configured store")
	}
}

func TestLazySnapshotStore_InvalidateAndClearNoPanic(t *testing.T) {
	var s LazySnapshotStore
	s.Invalidate("anything")
	s.Clear()
}

func TestParseRange_TooManyFields(t *testing.T) {
	_, err := Parse("¶a.go#0000\nreplace 1 2:\n+x\n")
	if !errors.Is(err, ErrInvalidOperation) {
		t.Errorf("want ErrInvalidOperation for multi-field range, got %v", err)
	}
}

func TestParseInsert_BeforeMissingNumber(t *testing.T) {
	_, err := Parse("¶a.go#0000\ninsert before:\n+x\n")
	if !errors.Is(err, ErrInvalidOperation) {
		t.Errorf("want ErrInvalidOperation, got %v", err)
	}
}

func TestWrapParse_ErrParseIdentity(t *testing.T) {
	// Wrapping ErrParse itself returns ErrParse unchanged.
	if wrapParse(ErrParse) != ErrParse {
		t.Error("wrapParse(ErrParse) should be ErrParse")
	}
}
