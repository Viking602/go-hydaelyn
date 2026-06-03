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
	missing        map[string]bool   // paths that don't exist (ReadText error)
	identity       map[string]string // canonical path -> resolved on-disk identity
	failWriteOn    string            // path whose WriteText returns an error
	failPreflight  string            // path whose PreflightWrite returns an error
	failCanonical  string            // path whose CanonicalPath returns an error
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

// ResolveIdentity maps a canonical path to its on-disk identity when one is
// configured (modeling symlink aliases), otherwise the path is its own
// identity. This makes fakeFS satisfy the patcher's identityResolver so the
// duplicate-section guard keys on resolved identity.
func (f *fakeFS) ResolveIdentity(path string) (string, error) {
	if id, ok := f.identity[path]; ok {
		return id, nil
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

func TestPatcher_DuplicateSectionKeysOnResolvedIdentity(t *testing.T) {
	// "a.go" and "link.go" are distinct lexical paths that resolve to the same
	// underlying file (link.go -> a.go). Keyed lexically they slip past the
	// duplicate-section guard and the second Commit clobbers the first's edit;
	// keyed by resolved identity the patch is rejected up front, untouched.
	const orig = "package p\n\nvar X = 1\n"
	fs := newFakeFS(map[string]string{"a.go": orig, "link.go": orig})
	fs.identity = map[string]string{"a.go": "id:a", "link.go": "id:a"}
	p := &Patcher{FS: fs}
	tag := tagFor(orig)
	patch := Patch{Sections: []Section{
		{Path: "a.go", Tag: tag, Ops: []Op{{Kind: OpReplace, Start: 3, End: 3, Body: []string{"var X = 2"}}}},
		{Path: "link.go", Tag: tag, Ops: []Op{{Kind: OpReplace, Start: 3, End: 3, Body: []string{"var X = 3"}}}},
	}}
	_, err := p.Apply(context.Background(), patch)
	if !errors.Is(err, ErrDuplicateSection) {
		t.Fatalf("want ErrDuplicateSection for symlink alias, got %v", err)
	}
	if fs.files["a.go"] != orig || fs.files["link.go"] != orig {
		t.Errorf("files mutated on alias reject: a=%q link=%q", fs.files["a.go"], fs.files["link.go"])
	}
	if len(fs.writeCallOrder) != 0 {
		t.Errorf("no writes should occur on a rejected patch: %v", fs.writeCallOrder)
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

// collisionStore reports, for any ByHash lookup, a snapshot whose content
// differs from the live file. It models a 16-bit tag collision: the live file
// and the recorded version the tag was minted from share the four-hex tag but
// are not the same content.
type collisionStore struct{ recorded string }

func (collisionStore) Head(string) (Snapshot, bool) { return Snapshot{}, false }
func (c collisionStore) ByHash(path, hash string) (Snapshot, bool) {
	return Snapshot{Path: path, Text: c.recorded, Hash: hash}, true
}

// UniqueByHash reports the single recorded content as the unambiguous base. The
// guard then rejects because that recorded content differs from the live file.
func (c collisionStore) UniqueByHash(path, hash string) (Snapshot, bool) {
	return Snapshot{Path: path, Text: c.recorded, Hash: hash}, true
}
func (collisionStore) Record(_, fullText string) string {
	return ComputeFileHash(Normalize(fullText).Text)
}
func (collisionStore) Invalidate(string) {}
func (collisionStore) Clear()            {}

// TestPatcher_RejectsTagCollisionAgainstSnapshot proves the fast path does not
// trust the 16-bit tag alone: when the recorded snapshot for the section's tag
// differs from the live content (a tag collision), Preflight rejects the edit as
// stale and writes nothing, instead of applying an edit built for a different
// file version.
func TestPatcher_RejectsTagCollisionAgainstSnapshot(t *testing.T) {
	const live = "package foo\n\nfunc Add(a, b int) int {\n\treturn a - b\n}\n"
	fs := newFakeFS(map[string]string{"foo.go": live})
	// The store reports a *different* recorded content for foo.go's tag.
	p := &Patcher{FS: fs, Snapshots: collisionStore{recorded: "package foo\n\nvar X = 1\n"}}

	patch := Patch{Sections: []Section{{
		Path: "foo.go",
		Tag:  tagFor(live), // equals the live tag, so we enter the fast path
		Ops:  []Op{{Kind: OpReplace, Start: 4, End: 4, Body: []string{"\treturn a + b"}}},
	}}}

	_, err := p.Apply(context.Background(), patch)
	if !errors.Is(err, ErrSnapshotMismatch) {
		t.Fatalf("Apply err = %v, want ErrSnapshotMismatch", err)
	}
	if fs.files["foo.go"] != live {
		t.Errorf("file modified despite collision rejection: %q", fs.files["foo.go"])
	}
}

// TestPatcher_RejectsAmbiguousCollidingTag proves the fast path refuses to
// apply when two distinct versions read this session collide on the 16-bit tag.
// Even though the live file IS a recorded version, the tag no longer identifies
// which version the edit's line numbers were built against, so applying it could
// misapply the edit. The patcher must reject as stale instead.
func TestPatcher_RejectsAmbiguousCollidingTag(t *testing.T) {
	a, b := findTagCollision(t)
	if Normalize(a).Text == Normalize(b).Text {
		t.Fatal("collision helper returned identical content")
	}
	// Live file is version b; the store has both a and b recorded under the same
	// tag (the agent read a, then later read/searched b).
	fs := newFakeFS(map[string]string{"f.go": b})
	store := NewMemorySnapshotStore()
	store.Record("f.go", a)
	store.Record("f.go", b)
	p := &Patcher{FS: fs, Snapshots: store}

	tag := tagFor(b) // equals tagFor(a); also the live tag
	patch := Patch{Sections: []Section{{
		Path: "f.go",
		Tag:  tag,
		Ops:  []Op{{Kind: OpReplace, Start: 1, End: 1, Body: []string{"// edited"}}},
	}}}

	_, err := p.Apply(context.Background(), patch)
	if !errors.Is(err, ErrSnapshotMismatch) {
		t.Fatalf("ambiguous colliding tag must be rejected, got %v", err)
	}
	if fs.files["f.go"] != b {
		t.Errorf("file mutated under an ambiguous tag: %q", fs.files["f.go"])
	}
}

// TestPatcher_FastPathAcceptsMatchingSnapshot is the companion: when the
// recorded snapshot for the tag equals the live content (the ordinary
// read-then-edit flow), the collision guard does not fire and the fast path
// applies the edit cleanly (not via recovery).
func TestPatcher_FastPathAcceptsMatchingSnapshot(t *testing.T) {
	const live = "package foo\n\nfunc Add(a, b int) int {\n\treturn a - b\n}\n"
	fs := newFakeFS(map[string]string{"foo.go": live})
	store := NewMemorySnapshotStore()
	store.Record("foo.go", live) // the model read exactly this content
	p := &Patcher{FS: fs, Snapshots: store}

	patch := Patch{Sections: []Section{{
		Path: "foo.go",
		Tag:  tagFor(live),
		Ops:  []Op{{Kind: OpReplace, Start: 4, End: 4, Body: []string{"\treturn a + b"}}},
	}}}

	res, err := p.Apply(context.Background(), patch)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Sections[0].Recovered {
		t.Errorf("matching snapshot should be a clean fast-path apply, not recovery")
	}
	want := "package foo\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n"
	if fs.files["foo.go"] != want {
		t.Errorf("file = %q, want %q", fs.files["foo.go"], want)
	}
}
