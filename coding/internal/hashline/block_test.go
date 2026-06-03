package hashline

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// sampleGo is a small but representative Go file used across the block tests.
// Line numbers are annotated so the test cases read against a known layout.
const sampleGo = "package sample\n" + // 1
	"\n" + // 2
	"import \"fmt\"\n" + // 3
	"\n" + // 4
	"// Greet prints a friendly message.\n" + // 5
	"func Greet(name string) {\n" + // 6
	"\tif name == \"\" {\n" + // 7
	"\t\tname = \"world\"\n" + // 8
	"\t}\n" + // 9
	"\tfmt.Println(\"hello\", name)\n" + // 10
	"}\n" + // 11
	"\n" + // 12
	"func Bye() {\n" + // 13
	"\tfmt.Println(\"bye\")\n" + // 14
	"}\n" // 15

func TestResolveBlock(t *testing.T) {
	cases := []struct {
		name               string
		line               int
		wantStart, wantEnd int
	}{
		{"func with doc comment, keyword line", 6, 5, 11},
		{"func with doc comment, comment line", 5, 5, 11},
		{"nested if inside func", 7, 7, 9},
		{"single statement", 10, 10, 10},
		{"second func no doc", 13, 13, 15},
		{"import gen decl", 3, 3, 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			start, end, err := ResolveBlock(sampleGo, c.line)
			if err != nil {
				t.Fatalf("ResolveBlock(line=%d): unexpected error %v", c.line, err)
			}
			if start != c.wantStart || end != c.wantEnd {
				t.Errorf("ResolveBlock(line=%d) = [%d,%d], want [%d,%d]", c.line, start, end, c.wantStart, c.wantEnd)
			}
		})
	}
}

func TestResolveBlock_MidBlockAndBoundariesError(t *testing.T) {
	// START-line match only: pointing at a line that is not the start of a
	// syntactic block (a closing brace, a mid-doc-comment line, the package
	// clause, a build-tag comment) must error rather than silently selecting an
	// enclosing block.
	src := "//go:build linux\n" + // 1 build-tag comment
		"// +build linux\n" + // 2
		"\n" + // 3
		"package p\n" + // 4 package clause
		"\n" + // 5
		"// Foo does foo.\n" + // 6 doc line 1
		"// More docs.\n" + // 7 doc line 2 (mid-comment)
		"func Foo() {\n" + // 8 keyword
		"\tx := 1\n" + // 9 stmt
		"\t_ = x\n" + // 10 stmt
		"}\n" // 11 closing brace
	errLines := []int{1, 2, 3, 4, 5, 7, 11}
	for _, ln := range errLines {
		if _, _, err := ResolveBlock(src, ln); err == nil {
			t.Errorf("ResolveBlock(line=%d): expected an error (not a block start)", ln)
		} else if !errors.Is(err, ErrBlockResolve) {
			t.Errorf("ResolveBlock(line=%d) error = %v, want ErrBlockResolve", ln, err)
		}
	}
	// The doc-comment first line (6) and the keyword line (8) both resolve to
	// the whole func including the doc comment; mid-block statements resolve to
	// themselves.
	for _, c := range []struct{ line, ws, we int }{
		{6, 6, 11}, {8, 6, 11}, {9, 9, 9}, {10, 10, 10},
	} {
		s, e, err := ResolveBlock(src, c.line)
		if err != nil {
			t.Errorf("ResolveBlock(line=%d): unexpected error %v", c.line, err)
			continue
		}
		if s != c.ws || e != c.we {
			t.Errorf("ResolveBlock(line=%d) = [%d,%d], want [%d,%d]", c.line, s, e, c.ws, c.we)
		}
	}
}

func TestResolveBlock_GroupedDeclInnerLinesError(t *testing.T) {
	// A grouped declaration resolves only at its opening keyword line; the
	// individual specs inside the parens are not block starts.
	src := "package p\n" + // 1
		"const (\n" + // 2
		"\tA = 1\n" + // 3
		"\tB = 2\n" + // 4
		")\n" // 5
	if s, e, err := ResolveBlock(src, 2); err != nil || s != 2 || e != 5 {
		t.Errorf("ResolveBlock(2) = [%d,%d],%v; want [2,5],nil", s, e, err)
	}
	for _, ln := range []int{3, 4, 5} {
		if _, _, err := ResolveBlock(src, ln); !errors.Is(err, ErrBlockResolve) {
			t.Errorf("ResolveBlock(%d) error = %v, want ErrBlockResolve", ln, err)
		}
	}
}

func TestResolveBlock_NotABlockStart(t *testing.T) {
	// Line 8 is inside the if body; no syntactic node *starts* there other
	// than the assignment statement on line 8 itself, so that resolves. Use a
	// blank line (line 2) which begins no block.
	if _, _, err := ResolveBlock(sampleGo, 2); err == nil {
		t.Fatal("expected an error for a line with no block start")
	} else if !errors.Is(err, ErrBlockResolve) {
		t.Errorf("error = %v, want ErrBlockResolve", err)
	} else if !errors.Is(err, ErrParse) {
		t.Errorf("error = %v, want it to wrap ErrParse", err)
	}
}

func TestResolveBlock_InvalidGo(t *testing.T) {
	const broken = "package p\nfunc Oops( {\n"
	_, _, err := ResolveBlock(broken, 2)
	if err == nil {
		t.Fatal("expected an error for invalid Go source")
	}
	if !errors.Is(err, ErrBlockResolve) {
		t.Errorf("error = %v, want ErrBlockResolve", err)
	}
	if !strings.Contains(err.Error(), "line-range") {
		t.Errorf("error %q should advise a line-range fallback", err.Error())
	}
}

func TestResolveBlock_LineLessThanOne(t *testing.T) {
	if _, _, err := ResolveBlock(sampleGo, 0); !errors.Is(err, ErrBlockResolve) {
		t.Errorf("ResolveBlock(0) error = %v, want ErrBlockResolve", err)
	}
}

func TestParse_ReplaceBlock(t *testing.T) {
	input := "¶x.go#ABCD\nreplace block 6:\n+func Greet(name string) {\n+\tfmt.Println(name)\n+}\n"
	patch, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(patch.Sections) != 1 || len(patch.Sections[0].Ops) != 1 {
		t.Fatalf("unexpected shape: %+v", patch)
	}
	op := patch.Sections[0].Ops[0]
	if op.Kind != OpReplaceBlock {
		t.Errorf("Kind = %q, want %q", op.Kind, OpReplaceBlock)
	}
	if op.Start != 6 || op.End != 6 {
		t.Errorf("Start/End = %d/%d, want 6/6", op.Start, op.End)
	}
	if len(op.Body) != 3 {
		t.Errorf("Body len = %d, want 3", len(op.Body))
	}
}

func TestParse_DeleteBlock(t *testing.T) {
	input := "¶x.go#ABCD\ndelete block 13\n"
	patch, err := Parse(input)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	op := patch.Sections[0].Ops[0]
	if op.Kind != OpDeleteBlock {
		t.Errorf("Kind = %q, want %q", op.Kind, OpDeleteBlock)
	}
	if op.Start != 13 || op.End != 13 {
		t.Errorf("Start/End = %d/%d, want 13/13", op.Start, op.End)
	}
}

func TestParse_BlockStrictness(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want error
	}{
		{
			name: "delete block with body rejected",
			in:   "¶x.go#ABCD\ndelete block 6\n+oops\n",
			want: ErrInvalidBodyRow,
		},
		{
			name: "replace block without body rejected",
			in:   "¶x.go#ABCD\nreplace block 6:\n",
			want: ErrInvalidBodyRow,
		},
		{
			name: "block with a range argument rejected",
			in:   "¶x.go#ABCD\ndelete block 6..8\n",
			want: ErrInvalidOperation,
		},
		{
			name: "block line < 1 rejected",
			in:   "¶x.go#ABCD\ndelete block 0\n",
			want: ErrInvalidOperation,
		},
		{
			name: "replace block missing line rejected",
			in:   "¶x.go#ABCD\nreplace block:\n+x\n",
			want: ErrInvalidOperation,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse(c.in)
			if err == nil {
				t.Fatalf("Parse(%q): expected error", c.in)
			}
			if !errors.Is(err, c.want) {
				t.Errorf("Parse(%q) error = %v, want %v", c.in, err, c.want)
			}
			if !errors.Is(err, ErrParse) {
				t.Errorf("Parse(%q) error = %v, want it to wrap ErrParse", c.in, err)
			}
		})
	}
}

func TestPatcher_ReplaceFuncBlock(t *testing.T) {
	fs := newFakeFS(map[string]string{"sample.go": sampleGo})
	p := &Patcher{FS: fs}
	tag := tagFor(sampleGo)

	// Replace the whole Bye function (begins on line 13) with a new body.
	patch := Patch{Sections: []Section{{
		Path: "sample.go",
		Tag:  tag,
		Ops: []Op{{
			Kind:  OpReplaceBlock,
			Start: 13,
			End:   13,
			Body:  []string{"func Bye() {", "\tfmt.Println(\"farewell\")", "}"},
		}},
	}}}

	res, err := p.Apply(context.Background(), patch)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got := fs.files["sample.go"]
	if !strings.Contains(got, "farewell") {
		t.Errorf("file not updated:\n%s", got)
	}
	if strings.Contains(got, "\"bye\"") {
		t.Errorf("old body still present:\n%s", got)
	}
	// The result header summarizes the original block op.
	if op := res.Sections[0].Op; !strings.Contains(op, "replace block 13") {
		t.Errorf("Op summary = %q, want it to mention replace block 13", op)
	}
}

func TestPatcher_DeleteFuncBlock(t *testing.T) {
	fs := newFakeFS(map[string]string{"sample.go": sampleGo})
	p := &Patcher{FS: fs}
	tag := tagFor(sampleGo)

	// Delete the Bye function (lines 13..15).
	patch := Patch{Sections: []Section{{
		Path: "sample.go",
		Tag:  tag,
		Ops:  []Op{{Kind: OpDeleteBlock, Start: 13, End: 13}},
	}}}

	if _, err := p.Apply(context.Background(), patch); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got := fs.files["sample.go"]
	if strings.Contains(got, "func Bye") {
		t.Errorf("Bye function should be gone:\n%s", got)
	}
	if !strings.Contains(got, "func Greet") {
		t.Errorf("Greet function should remain:\n%s", got)
	}
}

func TestPatcher_DeleteFuncBlockWithDocComment(t *testing.T) {
	fs := newFakeFS(map[string]string{"sample.go": sampleGo})
	p := &Patcher{FS: fs}
	tag := tagFor(sampleGo)

	// Delete the Greet function by pointing at its func keyword line (6).
	// The block resolves back to the doc comment on line 5, so the comment
	// is removed along with the function.
	patch := Patch{Sections: []Section{{
		Path: "sample.go",
		Tag:  tag,
		Ops:  []Op{{Kind: OpDeleteBlock, Start: 6, End: 6}},
	}}}

	if _, err := p.Apply(context.Background(), patch); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got := fs.files["sample.go"]
	if strings.Contains(got, "func Greet") {
		t.Errorf("Greet function should be gone:\n%s", got)
	}
	if strings.Contains(got, "Greet prints") {
		t.Errorf("doc comment should be gone too:\n%s", got)
	}
	if !strings.Contains(got, "func Bye") {
		t.Errorf("Bye function should remain:\n%s", got)
	}
}

func TestPatcher_ReplaceNestedBlock(t *testing.T) {
	fs := newFakeFS(map[string]string{"sample.go": sampleGo})
	p := &Patcher{FS: fs}
	tag := tagFor(sampleGo)

	// Replace just the nested if statement (begins on line 7).
	patch := Patch{Sections: []Section{{
		Path: "sample.go",
		Tag:  tag,
		Ops: []Op{{
			Kind:  OpReplaceBlock,
			Start: 7,
			End:   7,
			Body:  []string{"\tif name == \"\" {", "\t\tname = \"nobody\"", "\t}"},
		}},
	}}}

	if _, err := p.Apply(context.Background(), patch); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got := fs.files["sample.go"]
	if !strings.Contains(got, "nobody") {
		t.Errorf("nested block not updated:\n%s", got)
	}
	// The surrounding function structure is preserved.
	if !strings.Contains(got, "func Greet(name string) {") || !strings.Contains(got, "fmt.Println(\"hello\", name)") {
		t.Errorf("surrounding code disturbed:\n%s", got)
	}
}

func TestPatcher_BlockOpOverlapsRangeOpRejected(t *testing.T) {
	fs := newFakeFS(map[string]string{"sample.go": sampleGo})
	p := &Patcher{FS: fs}
	tag := tagFor(sampleGo)

	// The block op deletes lines 13..15 (func Bye); the range op replaces
	// line 14, which lies inside the resolved block. After resolution these
	// compose like two range ops and must be rejected as overlapping.
	patch := Patch{Sections: []Section{{
		Path: "sample.go",
		Tag:  tag,
		Ops: []Op{
			{Kind: OpDeleteBlock, Start: 13, End: 13},
			{Kind: OpReplace, Start: 14, End: 14, Body: []string{"\tfmt.Println(\"x\")"}},
		},
	}}}

	_, err := p.Apply(context.Background(), patch)
	if err == nil {
		t.Fatal("expected an overlap/conflict error")
	}
	var ae *ApplyError
	if !errors.As(err, &ae) {
		t.Errorf("error = %v, want an *ApplyError (overlap)", err)
	}
	// Nothing should have been written.
	if fs.files["sample.go"] != sampleGo {
		t.Errorf("workspace mutated on a rejected patch")
	}
}

func TestPatcher_BlockOpOnNonGoFileRejected(t *testing.T) {
	const notGo = "this is not go source\nat all\n"
	fs := newFakeFS(map[string]string{"readme.txt": notGo})
	p := &Patcher{FS: fs}
	tag := tagFor(notGo)

	patch := Patch{Sections: []Section{{
		Path: "readme.txt",
		Tag:  tag,
		Ops:  []Op{{Kind: OpDeleteBlock, Start: 1, End: 1}},
	}}}

	_, err := p.Apply(context.Background(), patch)
	if err == nil {
		t.Fatal("expected an error for a block op on a non-Go file")
	}
	if !errors.Is(err, ErrBlockResolve) {
		t.Errorf("error = %v, want ErrBlockResolve", err)
	}
	if fs.files["readme.txt"] != notGo {
		t.Errorf("workspace mutated on a rejected patch")
	}
}

func TestPatcher_BlockOpNoBlockStartRejected(t *testing.T) {
	fs := newFakeFS(map[string]string{"sample.go": sampleGo})
	p := &Patcher{FS: fs}
	tag := tagFor(sampleGo)

	// Line 2 is blank; no block starts there.
	patch := Patch{Sections: []Section{{
		Path: "sample.go",
		Tag:  tag,
		Ops:  []Op{{Kind: OpReplaceBlock, Start: 2, End: 2, Body: []string{"x := 1"}}},
	}}}

	_, err := p.Apply(context.Background(), patch)
	if err == nil {
		t.Fatal("expected an error for a line that is not a block start")
	}
	if !errors.Is(err, ErrBlockResolve) {
		t.Errorf("error = %v, want ErrBlockResolve", err)
	}
}
