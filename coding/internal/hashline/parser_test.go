package hashline

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParse_SingleSectionReplace(t *testing.T) {
	in := "¶internal/foo.go#A1B2\nreplace 3..5:\n+func Add(a, b int) int {\n+\treturn a + b\n+}"
	patch, err := Parse(in)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := Patch{Sections: []Section{{
		Path: "internal/foo.go",
		Tag:  "A1B2",
		Ops: []Op{{
			Kind:  OpReplace,
			Start: 3,
			End:   5,
			Body:  []string{"func Add(a, b int) int {", "\treturn a + b", "}"},
		}},
	}}}
	if !reflect.DeepEqual(patch, want) {
		t.Errorf("Parse mismatch:\n got %#v\nwant %#v", patch, want)
	}
}

func TestParse_AllOperationVariants(t *testing.T) {
	in := "¶a.go#0000\n" +
		"replace 1..2:\n+x\n\n" +
		"delete 4..6\n\n" +
		"insert before 8:\n+b\n\n" +
		"insert after 9:\n+c\n\n" +
		"insert head:\n+h\n\n" +
		"insert tail:\n+t\n"
	patch, err := Parse(in)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(patch.Sections) != 1 {
		t.Fatalf("sections = %d, want 1", len(patch.Sections))
	}
	ops := patch.Sections[0].Ops
	want := []Op{
		{Kind: OpReplace, Start: 1, End: 2, Body: []string{"x"}},
		{Kind: OpDelete, Start: 4, End: 6},
		{Kind: OpInsertBefore, Start: 8, End: 8, Body: []string{"b"}},
		{Kind: OpInsertAfter, Start: 9, End: 9, Body: []string{"c"}},
		{Kind: OpInsertHead, Body: []string{"h"}},
		{Kind: OpInsertTail, Body: []string{"t"}},
	}
	if !reflect.DeepEqual(ops, want) {
		t.Errorf("ops mismatch:\n got %#v\nwant %#v", ops, want)
	}
}

func TestParse_BareLineNumberIsSingleLineRange(t *testing.T) {
	patch, err := Parse("¶a.go#0000\ndelete 4\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	op := patch.Sections[0].Ops[0]
	if op.Kind != OpDelete || op.Start != 4 || op.End != 4 {
		t.Errorf("got %#v, want delete 4..4", op)
	}
}

func TestParse_MultiSection(t *testing.T) {
	in := "¶a.go#0000\ndelete 1\n¶b.go#FFFF\ninsert head:\n+top\n"
	patch, err := Parse(in)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(patch.Sections) != 2 {
		t.Fatalf("sections = %d, want 2", len(patch.Sections))
	}
	if patch.Sections[0].Path != "a.go" || patch.Sections[1].Path != "b.go" {
		t.Errorf("paths = %q, %q", patch.Sections[0].Path, patch.Sections[1].Path)
	}
	if patch.Sections[1].Ops[0].Kind != OpInsertHead {
		t.Errorf("second section op = %v, want insert_head", patch.Sections[1].Ops[0].Kind)
	}
}

func TestParse_CRLFInput(t *testing.T) {
	in := "¶a.go#0000\r\nreplace 1..1:\r\n+x\r\n"
	patch, err := Parse(in)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	op := patch.Sections[0].Ops[0]
	if op.Kind != OpReplace || len(op.Body) != 1 || op.Body[0] != "x" {
		t.Errorf("CRLF parse got %#v", op)
	}
}

func TestParse_PathWithHashUsesLastHash(t *testing.T) {
	// A path containing '#' must split on the LAST '#'.
	patch, err := Parse("¶weird#name.go#1A2B\ndelete 1\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if patch.Sections[0].Path != "weird#name.go" || patch.Sections[0].Tag != "1A2B" {
		t.Errorf("got path=%q tag=%q", patch.Sections[0].Path, patch.Sections[0].Tag)
	}
}

func TestParse_RejectCases(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr error
		wantLn  int
	}{
		{
			name:    "missing header",
			in:      "replace 1..2:\n+x\n",
			wantErr: ErrMissingHeader,
			wantLn:  1,
		},
		{
			name:    "empty input",
			in:      "",
			wantErr: ErrMissingHeader,
		},
		{
			name:    "header without tag",
			in:      "¶a.go\ndelete 1\n",
			wantErr: ErrMissingTag,
			wantLn:  1,
		},
		{
			name:    "header empty tag",
			in:      "¶a.go#\ndelete 1\n",
			wantErr: ErrMissingTag,
			wantLn:  1,
		},
		{
			name:    "header empty path",
			in:      "¶#A1B2\ndelete 1\n",
			wantErr: ErrMissingHeader,
			wantLn:  1,
		},
		{
			name:    "tag too short",
			in:      "¶a.go#A1B\ndelete 1\n",
			wantErr: ErrInvalidTag,
			wantLn:  1,
		},
		{
			name:    "tag lowercase",
			in:      "¶a.go#a1b2\ndelete 1\n",
			wantErr: ErrInvalidTag,
			wantLn:  1,
		},
		{
			name:    "tag non-hex",
			in:      "¶a.go#A1BG\ndelete 1\n",
			wantErr: ErrInvalidTag,
			wantLn:  1,
		},
		{
			name:    "unknown operation",
			in:      "¶a.go#A1B2\nfrobnicate 1\n",
			wantErr: ErrInvalidOperation,
			wantLn:  2,
		},
		{
			name:    "replace body not plus",
			in:      "¶a.go#A1B2\nreplace 1..1:\nnoplus\n",
			wantErr: ErrInvalidOperation,
			wantLn:  3,
		},
		{
			name:    "delete with body",
			in:      "¶a.go#A1B2\ndelete 1\n+oops\n",
			wantErr: ErrInvalidBodyRow,
			wantLn:  3,
		},
		{
			name:    "line number zero",
			in:      "¶a.go#A1B2\ndelete 0\n",
			wantErr: ErrInvalidOperation,
			wantLn:  2,
		},
		{
			name:    "negative line number",
			in:      "¶a.go#A1B2\nreplace -1..2:\n+x\n",
			wantErr: ErrInvalidOperation,
			wantLn:  2,
		},
		{
			name:    "range start after end",
			in:      "¶a.go#A1B2\nreplace 5..2:\n+x\n",
			wantErr: ErrInvalidOperation,
			wantLn:  2,
		},
		{
			name:    "bare context line",
			in:      "¶a.go#A1B2\nreplace 1..1:\n+x\nstray context\n",
			wantErr: ErrInvalidOperation,
			wantLn:  4,
		},
		{
			name:    "old removal row",
			in:      "¶a.go#A1B2\nreplace 1..1:\n-old\n+new\n",
			wantErr: ErrInvalidBodyRow,
			wantLn:  3,
		},
		{
			name:    "body row before any op",
			in:      "¶a.go#A1B2\n+orphan\n",
			wantErr: ErrInvalidBodyRow,
			wantLn:  2,
		},
		{
			name:    "replace without body",
			in:      "¶a.go#A1B2\nreplace 1..1:\n",
			wantErr: ErrInvalidBodyRow,
			wantLn:  2,
		},
		{
			name:    "insert before without body",
			in:      "¶a.go#A1B2\ninsert before 2:\n",
			wantErr: ErrInvalidBodyRow,
			wantLn:  2,
		},
		{
			name:    "insert missing target",
			in:      "¶a.go#A1B2\ninsert\n+x\n",
			wantErr: ErrInvalidOperation,
			wantLn:  2,
		},
		{
			name:    "insert unknown target",
			in:      "¶a.go#A1B2\ninsert sideways 2:\n+x\n",
			wantErr: ErrInvalidOperation,
			wantLn:  2,
		},
		{
			name:    "insert head with arg",
			in:      "¶a.go#A1B2\ninsert head 2:\n+x\n",
			wantErr: ErrInvalidOperation,
			wantLn:  2,
		},
		{
			name:    "non-numeric line",
			in:      "¶a.go#A1B2\ndelete abc\n",
			wantErr: ErrInvalidOperation,
			wantLn:  2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.in)
			if err == nil {
				t.Fatalf("Parse(%q) = nil error, want %v", tt.in, tt.wantErr)
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("error = %v, want errors.Is %v", err, tt.wantErr)
			}
			if !errors.Is(err, ErrParse) {
				t.Errorf("error = %v, want it to also wrap ErrParse", err)
			}
			if tt.wantLn != 0 {
				var pe *ParseError
				if !errors.As(err, &pe) {
					t.Fatalf("error %v is not a *ParseError", err)
				}
				if pe.Line != tt.wantLn {
					t.Errorf("ParseError.Line = %d, want %d", pe.Line, tt.wantLn)
				}
			}
		})
	}
}

// TestParse_BareContextLineMessage pins the diagnostic quality of the
// bare-context rejection: a stray line that is NOT an operation header is
// reported as a bare context line with the fix-it hint, whereas a stray line
// that DOES start with an op keyword (but is malformed) keeps its specific
// header error rather than being masked as "bare context".
func TestParse_BareContextLineMessage(t *testing.T) {
	t.Run("after a body row", func(t *testing.T) {
		_, err := Parse("¶a.go#A1B2\nreplace 1..1:\n+x\nstray context\n")
		if !errors.Is(err, ErrInvalidOperation) {
			t.Fatalf("want ErrInvalidOperation, got %v", err)
		}
		if !strings.Contains(err.Error(), "bare context line") ||
			!strings.Contains(err.Error(), "must start with '+'") {
			t.Errorf("message lacks the bare-context hint: %v", err)
		}
	})

	t.Run("empty body then stray", func(t *testing.T) {
		// A body-taking op with no rows yet, followed by a non-+ line, is also a
		// bare context line (reported at the stray line, here line 3).
		_, err := Parse("¶a.go#A1B2\nreplace 1..1:\nstray\n")
		if !errors.Is(err, ErrInvalidOperation) {
			t.Fatalf("want ErrInvalidOperation, got %v", err)
		}
		if !strings.Contains(err.Error(), "bare context line") {
			t.Errorf("message lacks the bare-context hint: %v", err)
		}
		var pe *ParseError
		if errors.As(err, &pe) && pe.Line != 3 {
			t.Errorf("ParseError.Line = %d, want 3", pe.Line)
		}
	})

	t.Run("malformed op keyword is not masked", func(t *testing.T) {
		// "delete abc" looks like an op header (starts with a keyword) but has a
		// non-numeric line, so it must keep its specific header error, NOT be
		// rewritten as a bare context line.
		_, err := Parse("¶a.go#A1B2\nreplace 1..1:\n+x\ndelete abc\n")
		if !errors.Is(err, ErrInvalidOperation) {
			t.Fatalf("want ErrInvalidOperation, got %v", err)
		}
		if strings.Contains(err.Error(), "bare context line") {
			t.Errorf("malformed op header was masked as bare context: %v", err)
		}
	})
}

func TestParseError_WrapsBothSentinelAndParse(t *testing.T) {
	_, err := Parse("¶a.go#zzzz\ndelete 1\n")
	if !errors.Is(err, ErrInvalidTag) {
		t.Errorf("want ErrInvalidTag, got %v", err)
	}
	if !errors.Is(err, ErrParse) {
		t.Errorf("want ErrParse, got %v", err)
	}
	if errors.Is(err, ErrInvalidOperation) {
		t.Errorf("should not match an unrelated sentinel")
	}
}
