package hashline

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeFS is an in-memory Filesystem for patcher tests. It records writes
// and can be configured to fail a specific path's write or preflight.
type fakeFS struct {
	files          map[string]string
	missing        map[string]bool // paths that don't exist (ReadText error)
	failWriteOn    string          // path whose WriteText returns an error
	failPreflight  string          // path whose PreflightWrite returns an error
	failCanonical  string          // path whose CanonicalPath returns an error
	writeCallOrder []string
}

func newFakeFS(files map[string]string) *fakeFS {
	cp := make(map[string]string, len(files))
	for k, v := range files {
		cp[k] = v
	}
	return &fakeFS{files: cp, missing: map[string]bool{}}
}

func (f *fakeFS) CanonicalPath(path string) (string, error) {
	if path == f.failCanonical {
		return "", errors.New("path escapes workspace")
	}
	return path, nil
}

func (f *fakeFS) ReadText(_ context.Context, path string) (string, error) {
	if f.missing[path] {
		return "", errors.New("no such file")
	}
	text, ok := f.files[path]
	if !ok {
		return "", errors.New("no such file: " + path)
	}
	return text, nil
}

func (f *fakeFS) PreflightWrite(_ context.Context, path string) error {
	if path == f.failPreflight {
		return errors.New("preflight denied")
	}
	return nil
}

func (f *fakeFS) WriteText(_ context.Context, path, text string) error {
	if path == f.failWriteOn {
		return errors.New("disk full")
	}
	f.writeCallOrder = append(f.writeCallOrder, path)
	f.files[path] = text
	return nil
}

// tagFor computes the live tag for a fake file's content.
func tagFor(text string) string {
	return ComputeFileHash(Normalize(text).Text)
}

func TestPatcher_ApplySingleSection(t *testing.T) {
	const orig = "package foo\n\nfunc Add(a, b int) int {\n\treturn a-b\n}\n"
	fs := newFakeFS(map[string]string{"foo.go": orig})
	p := &Patcher{FS: fs}

	tag := tagFor(orig)
	patch := Patch{Sections: []Section{{
		Path: "foo.go",
		Tag:  tag,
		Ops:  []Op{{Kind: OpReplace, Start: 4, End: 4, Body: []string{"\treturn a + b"}}},
	}}}

	res, err := p.Apply(context.Background(), patch)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(res.Sections) != 1 {
		t.Fatalf("sections = %d, want 1", len(res.Sections))
	}
	got := res.Sections[0]
	want := "package foo\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n"
	if fs.files["foo.go"] != want {
		t.Errorf("file = %q, want %q", fs.files["foo.go"], want)
	}
	if got.OldTag != tag {
		t.Errorf("OldTag = %q, want %q", got.OldTag, tag)
	}
	if got.NewTag == tag {
		t.Errorf("NewTag should differ from OldTag")
	}
	if got.Header != FormatHeader("foo.go", got.NewTag) {
		t.Errorf("Header = %q", got.Header)
	}
	if got.FirstChangedLine != 4 {
		t.Errorf("FirstChangedLine = %d, want 4", got.FirstChangedLine)
	}
	if !strings.Contains(got.Diff, "-\treturn a-b") || !strings.Contains(got.Diff, "+\treturn a + b") {
		t.Errorf("Diff = %q", got.Diff)
	}
}

func TestPatcher_StaleTagRejected(t *testing.T) {
	const orig = "a\nb\nc\n"
	fs := newFakeFS(map[string]string{"f.go": orig})
	p := &Patcher{FS: fs}

	patch := Patch{Sections: []Section{{
		Path: "f.go",
		Tag:  "DEAD", // not the live tag
		Ops:  []Op{{Kind: OpReplace, Start: 1, End: 1, Body: []string{"X"}}},
	}}}

	_, err := p.Apply(context.Background(), patch)
	if !errors.Is(err, ErrSnapshotMismatch) {
		t.Fatalf("want ErrSnapshotMismatch, got %v", err)
	}
	if fs.files["f.go"] != orig {
		t.Errorf("file mutated on stale-reject: %q", fs.files["f.go"])
	}
	if !strings.Contains(err.Error(), "re-read") {
		t.Errorf("mismatch error should instruct a re-read: %v", err)
	}
}

func TestPatcher_MultiSectionAllOrNothing_AllSucceed(t *testing.T) {
	a := "a1\na2\n"
	b := "b1\nb2\n"
	fs := newFakeFS(map[string]string{"a.go": a, "b.go": b})
	p := &Patcher{FS: fs}

	patch := Patch{Sections: []Section{
		{Path: "a.go", Tag: tagFor(a), Ops: []Op{{Kind: OpReplace, Start: 1, End: 1, Body: []string{"A1"}}}},
		{Path: "b.go", Tag: tagFor(b), Ops: []Op{{Kind: OpReplace, Start: 1, End: 1, Body: []string{"B1"}}}},
	}}

	res, err := p.Apply(context.Background(), patch)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(res.Sections) != 2 {
		t.Fatalf("sections = %d, want 2", len(res.Sections))
	}
	if fs.files["a.go"] != "A1\na2\n" || fs.files["b.go"] != "B1\nb2\n" {
		t.Errorf("files = %q, %q", fs.files["a.go"], fs.files["b.go"])
	}
}

func TestPatcher_MultiSection_OneStaleNothingWritten(t *testing.T) {
	a := "a1\na2\n"
	b := "b1\nb2\n"
	fs := newFakeFS(map[string]string{"a.go": a, "b.go": b})
	p := &Patcher{FS: fs}

	patch := Patch{Sections: []Section{
		{Path: "a.go", Tag: tagFor(a), Ops: []Op{{Kind: OpReplace, Start: 1, End: 1, Body: []string{"A1"}}}},
		{Path: "b.go", Tag: "BEEF", Ops: []Op{{Kind: OpReplace, Start: 1, End: 1, Body: []string{"B1"}}}},
	}}

	_, err := p.Apply(context.Background(), patch)
	if !errors.Is(err, ErrSnapshotMismatch) {
		t.Fatalf("want ErrSnapshotMismatch, got %v", err)
	}
	if fs.files["a.go"] != a {
		t.Errorf("a.go must be untouched when b.go is stale: %q", fs.files["a.go"])
	}
	if len(fs.writeCallOrder) != 0 {
		t.Errorf("no writes should occur, got %v", fs.writeCallOrder)
	}
}

func TestPatcher_WriteFailureRollsBack(t *testing.T) {
	a := "a1\na2\n"
	b := "b1\nb2\n"
	fs := newFakeFS(map[string]string{"a.go": a, "b.go": b})
	fs.failWriteOn = "b.go" // second write fails
	p := &Patcher{FS: fs}

	patch := Patch{Sections: []Section{
		{Path: "a.go", Tag: tagFor(a), Ops: []Op{{Kind: OpReplace, Start: 1, End: 1, Body: []string{"A1"}}}},
		{Path: "b.go", Tag: tagFor(b), Ops: []Op{{Kind: OpReplace, Start: 1, End: 1, Body: []string{"B1"}}}},
	}}

	_, err := p.Apply(context.Background(), patch)
	if err == nil {
		t.Fatal("expected a write error")
	}
	// a.go was written then rolled back to its original content.
	if fs.files["a.go"] != a {
		t.Errorf("a.go not rolled back: %q, want %q", fs.files["a.go"], a)
	}
	// b.go never changed.
	if fs.files["b.go"] != b {
		t.Errorf("b.go changed unexpectedly: %q", fs.files["b.go"])
	}
}

func TestPatcher_PreflightWriteFailureWritesNothing(t *testing.T) {
	a := "a1\n"
	b := "b1\n"
	fs := newFakeFS(map[string]string{"a.go": a, "b.go": b})
	fs.failPreflight = "b.go"
	p := &Patcher{FS: fs}

	patch := Patch{Sections: []Section{
		{Path: "a.go", Tag: tagFor(a), Ops: []Op{{Kind: OpReplace, Start: 1, End: 1, Body: []string{"A"}}}},
		{Path: "b.go", Tag: tagFor(b), Ops: []Op{{Kind: OpReplace, Start: 1, End: 1, Body: []string{"B"}}}},
	}}

	_, err := p.Apply(context.Background(), patch)
	if err == nil {
		t.Fatal("expected a preflight error")
	}
	if len(fs.writeCallOrder) != 0 {
		t.Errorf("no file should be written when preflight fails: %v", fs.writeCallOrder)
	}
	if fs.files["a.go"] != a {
		t.Errorf("a.go mutated: %q", fs.files["a.go"])
	}
}

func TestPatcher_CanonicalPathFailure(t *testing.T) {
	fs := newFakeFS(map[string]string{"a.go": "x\n"})
	fs.failCanonical = "a.go"
	p := &Patcher{FS: fs}

	patch := Patch{Sections: []Section{
		{Path: "a.go", Tag: "0000", Ops: []Op{{Kind: OpDelete, Start: 1, End: 1}}},
	}}
	_, err := p.Apply(context.Background(), patch)
	if err == nil || !strings.Contains(err.Error(), "escapes workspace") {
		t.Fatalf("want canonical-path error, got %v", err)
	}
}

func TestPatcher_NoopSectionRejected(t *testing.T) {
	orig := "a\nb\n"
	fs := newFakeFS(map[string]string{"f.go": orig})
	p := &Patcher{FS: fs}
	patch := Patch{Sections: []Section{
		{Path: "f.go", Tag: tagFor(orig), Ops: []Op{{Kind: OpReplace, Start: 1, End: 1, Body: []string{"a"}}}},
	}}
	_, err := p.Apply(context.Background(), patch)
	if !errors.Is(err, ErrNoop) {
		t.Fatalf("want ErrNoop, got %v", err)
	}
	if fs.files["f.go"] != orig {
		t.Errorf("noop must not mutate the file")
	}
}

func TestPatcher_PreflightDryRunDoesNotWrite(t *testing.T) {
	orig := "a\nb\n"
	fs := newFakeFS(map[string]string{"f.go": orig})
	p := &Patcher{FS: fs}
	patch := Patch{Sections: []Section{
		{Path: "f.go", Tag: tagFor(orig), Ops: []Op{{Kind: OpReplace, Start: 1, End: 1, Body: []string{"A"}}}},
	}}
	prepared, err := p.Preflight(context.Background(), patch)
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	if len(prepared) != 1 {
		t.Fatalf("prepared = %d, want 1", len(prepared))
	}
	if len(fs.writeCallOrder) != 0 {
		t.Errorf("Preflight must not write: %v", fs.writeCallOrder)
	}
	if prepared[0].NewTag == "" || prepared[0].Diff == "" {
		t.Errorf("prepared section missing tag/diff: %#v", prepared[0])
	}
	// A subsequent Commit applies the same prepared work.
	res, err := p.Commit(context.Background(), prepared)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if fs.files["f.go"] != "A\nb\n" {
		t.Errorf("committed file = %q", fs.files["f.go"])
	}
	if res.Sections[0].Op != "replace 1" {
		t.Errorf("Op summary = %q, want %q", res.Sections[0].Op, "replace 1")
	}
}

func TestPatcher_PreservesCRLFAndBOM(t *testing.T) {
	bom := "\uFEFF"
	orig := bom + "a\r\nb\r\nc\r\n"
	fs := newFakeFS(map[string]string{"f.go": orig})
	p := &Patcher{FS: fs}

	patch := Patch{Sections: []Section{
		{Path: "f.go", Tag: tagFor(orig), Ops: []Op{{Kind: OpReplace, Start: 2, End: 2, Body: []string{"B"}}}},
	}}
	_, err := p.Apply(context.Background(), patch)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := bom + "a\r\nB\r\nc\r\n"
	if fs.files["f.go"] != want {
		t.Errorf("file = %q, want %q (BOM + CRLF must be preserved)", fs.files["f.go"], want)
	}
}

func TestPatcher_PreservesCRLFAndBOMOnInsertHeadTail(t *testing.T) {
	// Inserting at head/tail must keep the file's CRLF endings and leading BOM,
	// including on the newly inserted lines.
	bom := "\uFEFF"
	orig := bom + "a\r\nb\r\n"
	fs := newFakeFS(map[string]string{"f.go": orig})
	p := &Patcher{FS: fs}

	patch := Patch{Sections: []Section{{
		Path: "f.go",
		Tag:  tagFor(orig),
		Ops: []Op{
			{Kind: OpInsertHead, Body: []string{"// top"}},
			{Kind: OpInsertTail, Body: []string{"// bottom"}},
		},
	}}}
	_, err := p.Apply(context.Background(), patch)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	// LF-internal lines are [<top>, a, b, "", <bottom>]; restored as CRLF + BOM.
	want := bom + "// top\r\na\r\nb\r\n\r\n// bottom"
	if fs.files["f.go"] != want {
		t.Errorf("file = %q, want %q", fs.files["f.go"], want)
	}
}

func TestPatcher_LoneCRPreservedAsContent(t *testing.T) {
	// A bare CR (not part of CRLF) is ordinary content; an LF-only file with a
	// lone CR must round-trip unchanged on an unrelated-line edit.
	orig := "a\rb\nc\n" // LF file; "a\rb" is one line containing a CR
	fs := newFakeFS(map[string]string{"f.go": orig})
	p := &Patcher{FS: fs}
	patch := Patch{Sections: []Section{{
		Path: "f.go",
		Tag:  tagFor(orig),
		Ops:  []Op{{Kind: OpReplace, Start: 2, End: 2, Body: []string{"C"}}},
	}}}
	_, err := p.Apply(context.Background(), patch)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := "a\rb\nC\n"
	if fs.files["f.go"] != want {
		t.Errorf("file = %q, want %q (lone CR must survive)", fs.files["f.go"], want)
	}
}

func TestPatcher_ReadFailure(t *testing.T) {
	fs := newFakeFS(map[string]string{})
	fs.missing["gone.go"] = true
	p := &Patcher{FS: fs}
	patch := Patch{Sections: []Section{
		{Path: "gone.go", Tag: "0000", Ops: []Op{{Kind: OpDelete, Start: 1, End: 1}}},
	}}
	_, err := p.Apply(context.Background(), patch)
	if err == nil || !strings.Contains(err.Error(), "read") {
		t.Fatalf("want a read error, got %v", err)
	}
}

// canonFS canonicalizes a leading "./" so two spellings of the same path map
// to one canonical key. It otherwise behaves like fakeFS, and is used to prove
// the duplicate-section guard keys on the canonical (not the raw) path.
type canonFS struct {
	*fakeFS
}

func (f canonFS) CanonicalPath(path string) (string, error) {
	return strings.TrimPrefix(path, "./"), nil
}

func TestPatcher_DuplicateSectionSamePathRejected(t *testing.T) {
	// Two sections for the same file would each validate against (and write)
	// the live original, so the second Commit would silently clobber the
	// first's edit. The patcher must reject the patch up front and leave the
	// file untouched. Before this guard, the first edit (line 1 -> "A") was
	// lost and the file became "a\nb\nC\n" with no error.
	orig := "a\nb\nc\n"
	fs := newFakeFS(map[string]string{"f.go": orig})
	p := &Patcher{FS: fs}
	tag := tagFor(orig)
	patch := Patch{Sections: []Section{
		{Path: "f.go", Tag: tag, Ops: []Op{{Kind: OpReplace, Start: 1, End: 1, Body: []string{"A"}}}},
		{Path: "f.go", Tag: tag, Ops: []Op{{Kind: OpReplace, Start: 3, End: 3, Body: []string{"C"}}}},
	}}

	_, err := p.Apply(context.Background(), patch)
	if !errors.Is(err, ErrDuplicateSection) {
		t.Fatalf("want ErrDuplicateSection, got %v", err)
	}
	if fs.files["f.go"] != orig {
		t.Errorf("file mutated on duplicate-section reject: %q, want %q", fs.files["f.go"], orig)
	}
	if len(fs.writeCallOrder) != 0 {
		t.Errorf("no writes should occur on a rejected patch: %v", fs.writeCallOrder)
	}
}

func TestPatcher_DuplicateSectionDetectedAtPreflight(t *testing.T) {
	// The guard lives in Preflight so dry_run also rejects (no writes either).
	orig := "a\nb\n"
	fs := newFakeFS(map[string]string{"f.go": orig})
	p := &Patcher{FS: fs}
	tag := tagFor(orig)
	patch := Patch{Sections: []Section{
		{Path: "f.go", Tag: tag, Ops: []Op{{Kind: OpReplace, Start: 1, End: 1, Body: []string{"A"}}}},
		{Path: "f.go", Tag: tag, Ops: []Op{{Kind: OpReplace, Start: 2, End: 2, Body: []string{"B"}}}},
	}}
	_, err := p.Preflight(context.Background(), patch)
	if !errors.Is(err, ErrDuplicateSection) {
		t.Fatalf("Preflight want ErrDuplicateSection, got %v", err)
	}
}

func TestPatcher_DuplicateSectionKeysOnCanonicalPath(t *testing.T) {
	// "f.go" and "./f.go" canonicalize to the same file, so the second must be
	// rejected as a duplicate even though the raw paths differ.
	orig := "a\nb\n"
	base := newFakeFS(map[string]string{"f.go": orig})
	fs := canonFS{fakeFS: base}
	p := &Patcher{FS: fs}
	tag := tagFor(orig)
	patch := Patch{Sections: []Section{
		{Path: "f.go", Tag: tag, Ops: []Op{{Kind: OpReplace, Start: 1, End: 1, Body: []string{"A"}}}},
		{Path: "./f.go", Tag: tag, Ops: []Op{{Kind: OpReplace, Start: 2, End: 2, Body: []string{"B"}}}},
	}}
	_, err := p.Apply(context.Background(), patch)
	if !errors.Is(err, ErrDuplicateSection) {
		t.Fatalf("want ErrDuplicateSection for ./ alias, got %v", err)
	}
	if base.files["f.go"] != orig {
		t.Errorf("file mutated: %q", base.files["f.go"])
	}
}

func TestPatcher_DistinctPathsNotFlaggedAsDuplicate(t *testing.T) {
	// Sanity: the guard must not false-positive on genuinely distinct files.
	a := "a1\n"
	b := "b1\n"
	fs := newFakeFS(map[string]string{"a.go": a, "b.go": b})
	p := &Patcher{FS: fs}
	patch := Patch{Sections: []Section{
		{Path: "a.go", Tag: tagFor(a), Ops: []Op{{Kind: OpReplace, Start: 1, End: 1, Body: []string{"A"}}}},
		{Path: "b.go", Tag: tagFor(b), Ops: []Op{{Kind: OpReplace, Start: 1, End: 1, Body: []string{"B"}}}},
	}}
	if _, err := p.Apply(context.Background(), patch); err != nil {
		t.Fatalf("distinct paths should apply cleanly: %v", err)
	}
}

func TestPatcher_NilSnapshotStoreIsSafe(t *testing.T) {
	orig := "a\n"
	fs := newFakeFS(map[string]string{"f.go": orig})
	p := &Patcher{FS: fs, Snapshots: nil}
	patch := Patch{Sections: []Section{
		{Path: "f.go", Tag: tagFor(orig), Ops: []Op{{Kind: OpReplace, Start: 1, End: 1, Body: []string{"B"}}}},
	}}
	if _, err := p.Apply(context.Background(), patch); err != nil {
		t.Fatalf("nil snapshot store should be safe: %v", err)
	}
}
