package coding

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Viking602/venat/coding/internal/hashline"
	"github.com/Viking602/venat/tool"
)

// driverByName finds a driver in the toolset by its tool name.
func driverByName(t *testing.T, set []tool.Driver, name string) tool.Driver {
	t.Helper()
	for _, d := range set {
		if d.Definition().Name == name {
			return d
		}
	}
	t.Fatalf("driver %q not found in toolset", name)
	return nil
}

// callJSON marshals args to JSON and runs the driver, capturing any sink
// updates. It returns the result and the collected updates.
func callJSON(t *testing.T, d tool.Driver, args any) (tool.Result, []tool.Update) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	var updates []tool.Update
	sink := func(u tool.Update) error {
		updates = append(updates, u)
		return nil
	}
	res, err := d.Execute(context.Background(), tool.Call{
		ID:        "call-1",
		Name:      d.Definition().Name,
		Arguments: raw,
	}, sink)
	if err != nil {
		t.Fatalf("%s execute returned error: %v", d.Definition().Name, err)
	}
	return res, updates
}

func TestReadFile_ReturnsHashlineHeader(t *testing.T) {
	const content = "package a\n\nfunc Add(a, b int) int {\n\treturn a - b\n}\n"
	ws, _ := newTestWorkspace(t, map[string]string{"foo.go": content})
	set := NewToolSet(ws)

	res, _ := callJSON(t, driverByName(t, set, ToolReadFile), readFileInput{Path: "foo.go"})
	if res.IsError {
		t.Fatalf("read_file errored: %s", res.Content)
	}
	tag := hashline.ComputeFileHash(hashline.Normalize(content).Text)
	wantHeader := "¶foo.go#" + tag
	if !strings.HasPrefix(res.Content, wantHeader) {
		t.Errorf("content does not start with %q:\n%s", wantHeader, res.Content)
	}
	if !strings.Contains(res.Content, "1:package a") {
		t.Errorf("content missing numbered line:\n%s", res.Content)
	}
	var structured ReadFileToolResult
	if err := json.Unmarshal(res.Structured, &structured); err != nil {
		t.Fatalf("unmarshal structured: %v", err)
	}
	if structured.Tag != tag || structured.Header != wantHeader {
		t.Errorf("structured tag/header = %q/%q", structured.Tag, structured.Header)
	}
}

func TestSearch_ReturnsHashlineSections(t *testing.T) {
	ws, _ := newTestWorkspace(t, map[string]string{
		"a.go": "package a\nvar Target = 1\n",
		"b.go": "package b\n// nothing here\n",
	})
	set := NewToolSet(ws)
	res, _ := callJSON(t, driverByName(t, set, ToolSearch), searchInput{Query: "Target"})
	if res.IsError {
		t.Fatalf("search errored: %s", res.Content)
	}
	if !strings.Contains(res.Content, "¶a.go#") {
		t.Errorf("search content missing ¶a.go header:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "2:var Target = 1") {
		t.Errorf("search content missing numbered match:\n%s", res.Content)
	}
	if strings.Contains(res.Content, "¶b.go#") {
		t.Errorf("search should not report non-matching b.go:\n%s", res.Content)
	}
}

// editWith reads a file's current tag and runs edit_hashline with a patch body
// built from it, returning the result.
func editWith(t *testing.T, set []tool.Driver, header, body string, dryRun bool) (tool.Result, []tool.Update) {
	t.Helper()
	patch := header + "\n" + body
	return callJSON(t, driverByName(t, set, ToolEditHashline), editHashlineInput{Input: patch, DryRun: dryRun})
}

func TestEditHashline_CurrentTagSucceeds(t *testing.T) {
	const content = "package a\n\nfunc Add(a, b int) int {\n\treturn a - b\n}\n"
	ws, root := newTestWorkspace(t, map[string]string{"foo.go": content})
	set := NewToolSet(ws)

	read, _ := callJSON(t, driverByName(t, set, ToolReadFile), readFileInput{Path: "foo.go"})
	var rr ReadFileToolResult
	_ = json.Unmarshal(read.Structured, &rr)

	res, updates := editWith(t, set, rr.Header, "replace 4:\n+\treturn a + b\n", false)
	if res.IsError {
		t.Fatalf("edit with current tag should succeed, got: %s", res.Content)
	}
	got, _ := os.ReadFile(filepath.Join(root, "foo.go"))
	if !strings.Contains(string(got), "return a + b") {
		t.Errorf("file not edited: %q", got)
	}
	// Audit event emitted via the sink.
	if len(updates) == 0 {
		t.Error("expected an audit update from edit_hashline")
	} else {
		u := updates[0]
		if u.Kind != "coding.edit_hashline" || u.Data["diffHash"] == "" {
			t.Errorf("audit update missing fields: %+v", u)
		}
	}

	var er EditHashlineResult
	if err := json.Unmarshal(res.Structured, &er); err != nil {
		t.Fatalf("unmarshal edit result: %v", err)
	}
	if len(er.Sections) != 1 || er.Sections[0].FirstChangedLine != 4 {
		t.Errorf("unexpected section result: %+v", er.Sections)
	}
}

func TestEditHashline_StaleTagFails(t *testing.T) {
	const content = "package a\n\nfunc Add(a, b int) int {\n\treturn a - b\n}\n"
	ws, root := newTestWorkspace(t, map[string]string{"foo.go": content})
	set := NewToolSet(ws)

	// Use a deliberately wrong tag (0000 cannot match the live file content).
	res, _ := editWith(t, set, "¶foo.go#0000", "replace 4:\n+\treturn a + b\n", false)
	if !res.IsError {
		t.Fatal("stale tag must be rejected")
	}
	if !strings.Contains(res.Content, "re-read") {
		t.Errorf("stale rejection should instruct a re-read:\n%s", res.Content)
	}
	// File must be unchanged.
	got, _ := os.ReadFile(filepath.Join(root, "foo.go"))
	if string(got) != content {
		t.Errorf("stale edit mutated the file: %q", got)
	}
}

func TestEditHashline_NewHeaderAfterEditSucceeds(t *testing.T) {
	const content = "1\n2\n3\n4\n5\n"
	ws, _ := newTestWorkspace(t, map[string]string{"f.txt": content})
	set := NewToolSet(ws)

	read, _ := callJSON(t, driverByName(t, set, ToolReadFile), readFileInput{Path: "f.txt"})
	var rr ReadFileToolResult
	_ = json.Unmarshal(read.Structured, &rr)

	first, _ := editWith(t, set, rr.Header, "replace 2:\n+two\n", false)
	if first.IsError {
		t.Fatalf("first edit failed: %s", first.Content)
	}
	var er EditHashlineResult
	_ = json.Unmarshal(first.Structured, &er)
	newHeader := er.Sections[0].Header

	// Old tag now dead; the new header must work for the next edit.
	second, _ := editWith(t, set, newHeader, "replace 4:\n+four\n", false)
	if second.IsError {
		t.Fatalf("edit with the fresh header should succeed, got: %s", second.Content)
	}
	// Re-using the original (now stale) header to edit a line the intervening
	// edits already changed (line 2 -> "two") conflicts with the live file, so
	// it must be rejected with the re-read message — no silent clobber.
	stale, _ := editWith(t, set, rr.Header, "replace 2:\n+TWO-CONFLICT\n", false)
	if !stale.IsError {
		t.Error("re-using the original header to re-edit a changed line must be rejected")
	}
	if !strings.Contains(stale.Content, "re-read") {
		t.Errorf("conflicting stale re-use should instruct a re-read:\n%s", stale.Content)
	}
}

func TestEditHashline_DryRunDoesNotWrite(t *testing.T) {
	const content = "a\nb\nc\n"
	ws, root := newTestWorkspace(t, map[string]string{"f.txt": content})
	set := NewToolSet(ws)

	read, _ := callJSON(t, driverByName(t, set, ToolReadFile), readFileInput{Path: "f.txt"})
	var rr ReadFileToolResult
	_ = json.Unmarshal(read.Structured, &rr)

	res, _ := editWith(t, set, rr.Header, "replace 2:\n+B\n", true)
	if res.IsError {
		t.Fatalf("dry run errored: %s", res.Content)
	}
	var er EditHashlineResult
	_ = json.Unmarshal(res.Structured, &er)
	if !er.DryRun {
		t.Error("result should be marked dry run")
	}
	got, _ := os.ReadFile(filepath.Join(root, "f.txt"))
	if string(got) != content {
		t.Errorf("dry run mutated the file: %q", got)
	}
}

func TestEditHashline_MultiFileAllOrNothing(t *testing.T) {
	ws, root := newTestWorkspace(t, map[string]string{
		"a.txt": "alpha\n",
		"b.txt": "beta\n",
	})
	set := NewToolSet(ws)

	readA, _ := callJSON(t, driverByName(t, set, ToolReadFile), readFileInput{Path: "a.txt"})
	readB, _ := callJSON(t, driverByName(t, set, ToolReadFile), readFileInput{Path: "b.txt"})
	var rrA, rrB ReadFileToolResult
	_ = json.Unmarshal(readA.Structured, &rrA)
	_ = json.Unmarshal(readB.Structured, &rrB)

	// One good section (a.txt with its real tag) and one bad section (b.txt
	// with a wrong tag). The whole patch must be rejected and NOTHING written.
	patch := rrA.Header + "\nreplace 1:\n+ALPHA\n\n" + "¶b.txt#0000\nreplace 1:\n+BETA\n"
	res, _ := callJSON(t, driverByName(t, set, ToolEditHashline), editHashlineInput{Input: patch})
	if !res.IsError {
		t.Fatal("multi-file patch with one stale section must be rejected")
	}
	a, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	b, _ := os.ReadFile(filepath.Join(root, "b.txt"))
	if string(a) != "alpha\n" || string(b) != "beta\n" {
		t.Errorf("all-or-nothing violated: a=%q b=%q", a, b)
	}

	// Now both sections valid: both files updated.
	good := rrA.Header + "\nreplace 1:\n+ALPHA\n\n" + rrB.Header + "\nreplace 1:\n+BETA\n"
	res2, _ := callJSON(t, driverByName(t, set, ToolEditHashline), editHashlineInput{Input: good})
	if res2.IsError {
		t.Fatalf("valid multi-file patch should succeed: %s", res2.Content)
	}
	a2, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	b2, _ := os.ReadFile(filepath.Join(root, "b.txt"))
	if string(a2) != "ALPHA\n" || string(b2) != "BETA\n" {
		t.Errorf("both files should be updated: a=%q b=%q", a2, b2)
	}
}

func TestEditHashline_ParseErrorAsksReRead(t *testing.T) {
	ws, _ := newTestWorkspace(t, map[string]string{"f.txt": "a\n"})
	set := NewToolSet(ws)
	// A body row that uses -old (forbidden) triggers a parse error.
	res, _ := callJSON(t, driverByName(t, set, ToolEditHashline), editHashlineInput{
		Input: "¶f.txt#0000\nreplace 1:\n-old\n",
	})
	if !res.IsError {
		t.Fatal("malformed patch must be rejected")
	}
	if !strings.Contains(res.Content, "did not parse") {
		t.Errorf("parse rejection message unexpected:\n%s", res.Content)
	}
}

func TestWriteFile_ExistingRedirectsToEdit(t *testing.T) {
	ws, _ := newTestWorkspace(t, map[string]string{"exists.go": "package a\n"})
	set := NewToolSet(ws)

	// New file: succeeds with a header.
	res, _ := callJSON(t, driverByName(t, set, ToolWriteFile), writeFileInput{Path: "fresh.go", Content: "package fresh\n"})
	if res.IsError {
		t.Fatalf("creating a new file should succeed: %s", res.Content)
	}
	if !strings.HasPrefix(res.Content, "¶fresh.go#") {
		t.Errorf("write_file content should start with a header:\n%s", res.Content)
	}

	// Existing file: redirected to edit_hashline.
	res2, _ := callJSON(t, driverByName(t, set, ToolWriteFile), writeFileInput{Path: "exists.go", Content: "package a\n//x\n"})
	if !res2.IsError {
		t.Fatal("writing over an existing file must be rejected")
	}
	if !strings.Contains(res2.Content, "edit_hashline") {
		t.Errorf("write_file over existing file should redirect to edit_hashline:\n%s", res2.Content)
	}
}

func TestGofmt_FormatsInProcess(t *testing.T) {
	// Deliberately unformatted Go source.
	const unformatted = "package a\nfunc  Add(a int,b int)int{return a+b}\n"
	ws, root := newTestWorkspace(t, map[string]string{"m.go": unformatted})
	set := NewToolSet(ws)

	res, _ := callJSON(t, driverByName(t, set, ToolGofmt), gofmtInput{Path: "m.go"})
	if res.IsError {
		t.Fatalf("gofmt errored: %s", res.Content)
	}
	var gr GofmtToolResult
	if err := json.Unmarshal(res.Structured, &gr); err != nil {
		t.Fatalf("unmarshal gofmt result: %v", err)
	}
	if !gr.Changed {
		t.Error("gofmt should report a change for unformatted source")
	}
	got, _ := os.ReadFile(filepath.Join(root, "m.go"))
	if !strings.Contains(string(got), "func Add(a int, b int) int {") {
		t.Errorf("file was not gofmt-formatted in place:\n%s", got)
	}

	// Idempotent: a second run reports no change.
	res2, _ := callJSON(t, driverByName(t, set, ToolGofmt), gofmtInput{Path: "m.go"})
	var gr2 GofmtToolResult
	_ = json.Unmarshal(res2.Structured, &gr2)
	if gr2.Changed {
		t.Error("gofmt on already-formatted source should report no change")
	}
}

func TestGofmt_RejectsNonGo(t *testing.T) {
	ws, _ := newTestWorkspace(t, map[string]string{"f.txt": "not go\n"})
	set := NewToolSet(ws)
	res, _ := callJSON(t, driverByName(t, set, ToolGofmt), gofmtInput{Path: "f.txt"})
	if !res.IsError {
		t.Fatal("gofmt must reject non-.go files")
	}
}

// fakeSearchWorkspace lets a test drive searchDriver with a Search result whose
// full text is known, while a follow-up ReadFile (which the driver must NOT make)
// would return different content. The embedded nil Workspace provides the methods
// searchDriver never calls; they panic if exercised, which is the intent.
type fakeSearchWorkspace struct {
	Workspace
	searchText     string
	searchTag      string
	readFileCalled bool
}

func (w *fakeSearchWorkspace) Search(_ context.Context, _ SearchRequest) (SearchResult, error) {
	return SearchResult{Files: []SearchFileResult{{
		Path:    "f.go",
		Tag:     w.searchTag,
		Text:    w.searchText,
		Matches: []SearchMatch{{LineNumber: 1, Line: "match"}},
	}}}, nil
}

func (w *fakeSearchWorkspace) ReadFile(_ context.Context, req ReadFileRequest) (ReadFileResult, error) {
	w.readFileCalled = true
	return ReadFileResult{Path: req.Path, Text: "DIFFERENT-OUT-OF-BAND\n", Tag: "FFFF"}, nil
}

// TestSearch_RecordsExactSearchedSnapshot proves coding.search records the exact
// content the search result exposed (the text from the same read that minted the
// header tag) rather than re-reading the file, so a concurrent out-of-band change
// cannot make the recorded snapshot diverge from the header the model received.
func TestSearch_RecordsExactSearchedSnapshot(t *testing.T) {
	const searched = "package a\n\nfunc match() {}\n"
	tag := hashline.ComputeFileHash(hashline.Normalize(searched).Text)
	ws := &fakeSearchWorkspace{searchText: searched, searchTag: tag}
	store := hashline.NewMemorySnapshotStore()
	d := searchDriver{ws: ws, store: store}

	res, _ := callJSON(t, d, searchInput{Query: "match"})
	if res.IsError {
		t.Fatalf("search errored: %s", res.Content)
	}
	if ws.readFileCalled {
		t.Error("search must record the searched text, not re-read the file")
	}
	snap, ok := store.UniqueByHash("f.go", tag)
	if !ok {
		t.Fatalf("search did not record a unique snapshot for the searched tag %s", tag)
	}
	if want := hashline.Normalize(searched).Text; snap.Text != want {
		t.Errorf("recorded snapshot text = %q, want the searched content %q", snap.Text, want)
	}
}

// TestWriteFile_RecordsSnapshot proves coding.write_file records the new file's
// content into the shared snapshot store, so the ¶PATH#TAG it mints is backed by
// recorded history (UniqueByHash) and collision-guarded rather than fast-pathed
// on the 16-bit hash alone.
func TestWriteFile_RecordsSnapshot(t *testing.T) {
	ws, _ := newTestWorkspace(t, nil)
	store := hashline.NewMemorySnapshotStore()
	set := NewToolSet(ws, WithSnapshotStore(store))

	const content = "package fresh\n\nfunc F() {}\n"
	res, _ := callJSON(t, driverByName(t, set, ToolWriteFile), writeFileInput{Path: "fresh.go", Content: content})
	if res.IsError {
		t.Fatalf("write_file errored: %s", res.Content)
	}
	var wr WriteFileToolResult
	if err := json.Unmarshal(res.Structured, &wr); err != nil {
		t.Fatalf("unmarshal write result: %v", err)
	}

	snap, ok := store.UniqueByHash(wr.Path, wr.Tag)
	if !ok {
		t.Fatalf("write_file did not record a unique snapshot for tag %s under %q", wr.Tag, wr.Path)
	}
	if want := hashline.Normalize(content).Text; snap.Text != want {
		t.Errorf("recorded snapshot text = %q, want %q", snap.Text, want)
	}
}

// TestGofmt_RecordsSnapshot proves coding.gofmt records the formatted content
// into the shared snapshot store in both the changed and already-formatted
// branches, so the tag it returns is backed by recorded history.
func TestGofmt_RecordsSnapshot(t *testing.T) {
	t.Run("changed", func(t *testing.T) {
		const unformatted = "package a\nfunc  Add(a int,b int)int{return a+b}\n"
		ws, _ := newTestWorkspace(t, map[string]string{"m.go": unformatted})
		store := hashline.NewMemorySnapshotStore()
		set := NewToolSet(ws, WithSnapshotStore(store))

		res, _ := callJSON(t, driverByName(t, set, ToolGofmt), gofmtInput{Path: "m.go"})
		if res.IsError {
			t.Fatalf("gofmt errored: %s", res.Content)
		}
		var gr GofmtToolResult
		if err := json.Unmarshal(res.Structured, &gr); err != nil {
			t.Fatalf("unmarshal gofmt result: %v", err)
		}
		if !gr.Changed {
			t.Fatal("expected a change for unformatted source")
		}
		if _, ok := store.UniqueByHash(gr.Path, gr.Tag); !ok {
			t.Fatalf("gofmt (changed) did not record a unique snapshot for tag %s under %q", gr.Tag, gr.Path)
		}
	})

	t.Run("already-formatted", func(t *testing.T) {
		const formatted = "package a\n\nfunc Add(a, b int) int { return a + b }\n"
		ws, _ := newTestWorkspace(t, map[string]string{"m.go": formatted})
		store := hashline.NewMemorySnapshotStore()
		set := NewToolSet(ws, WithSnapshotStore(store))

		res, _ := callJSON(t, driverByName(t, set, ToolGofmt), gofmtInput{Path: "m.go"})
		if res.IsError {
			t.Fatalf("gofmt errored: %s", res.Content)
		}
		var gr GofmtToolResult
		if err := json.Unmarshal(res.Structured, &gr); err != nil {
			t.Fatalf("unmarshal gofmt result: %v", err)
		}
		if gr.Changed {
			t.Fatal("expected no change for already-formatted source")
		}
		if _, ok := store.UniqueByHash(gr.Path, gr.Tag); !ok {
			t.Fatalf("gofmt (already formatted) did not record a unique snapshot for tag %s under %q", gr.Tag, gr.Path)
		}
	})
}

// TestEditHashline_RecoversStaleTagAfterWriteFile proves the write_file→edit
// recording is wired through the default NewToolSet store end-to-end: write_file
// records the new file's base version, an out-of-band change to a DIFFERENT
// region makes the minted tag stale, and an edit carrying that tag recovers via
// a 3-way merge against the recorded base. Without the recording, recoverStale
// would find no unique base for the tag and reject the edit with a re-read.
func TestEditHashline_RecoversStaleTagAfterWriteFile(t *testing.T) {
	ws, root := newTestWorkspace(t, nil)
	set := NewToolSet(ws)

	const content = "line1\nline2\nline3\nline4\nline5\n"
	res, _ := callJSON(t, driverByName(t, set, ToolWriteFile), writeFileInput{Path: "f.txt", Content: content})
	if res.IsError {
		t.Fatalf("write_file errored: %s", res.Content)
	}
	var wr WriteFileToolResult
	if err := json.Unmarshal(res.Structured, &wr); err != nil {
		t.Fatalf("unmarshal write result: %v", err)
	}

	// Out-of-band change to a DIFFERENT region (line5) makes the minted tag stale.
	const live = "line1\nline2\nline3\nline4\nLINE5-EXTERNAL\n"
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte(live), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}

	// Edit line2 with the now-stale write_file header; disjoint from line5, so the
	// 3-way merge against the recorded base recovers.
	edit, _ := editWith(t, set, wr.Header, "replace 2:\n+LINE2-EDITED\n", false)
	if edit.IsError {
		t.Fatalf("stale edit after write_file should recover against the recorded base, got: %s", edit.Content)
	}
	got, _ := os.ReadFile(filepath.Join(root, "f.txt"))
	const want = "line1\nLINE2-EDITED\nline3\nline4\nLINE5-EXTERNAL\n"
	if string(got) != want {
		t.Errorf("merged file = %q, want %q", got, want)
	}
}
