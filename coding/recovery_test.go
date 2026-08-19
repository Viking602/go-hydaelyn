package coding

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Viking602/venat/tool"
)

// readHeader reads a file through the toolset and returns its current ¶PATH#TAG
// header. Going through read_file also records the file into the shared
// snapshot store, which is what makes stale-tag recovery possible.
func readHeader(t *testing.T, set []tool.Driver, path string) string {
	t.Helper()
	read, _ := callJSON(t, driverByName(t, set, ToolReadFile), readFileInput{Path: path})
	if read.IsError {
		t.Fatalf("read_file %q errored: %s", path, read.Content)
	}
	var rr ReadFileToolResult
	if err := json.Unmarshal(read.Structured, &rr); err != nil {
		t.Fatalf("unmarshal read result: %v", err)
	}
	return rr.Header
}

// TestEditHashline_RecoversStaleTagOnDifferentRegion proves the history-backed
// wiring: a read records the file; an out-of-band change to a DIFFERENT region
// makes the tag stale; verified unchanged anchors let the edit replay against
// live content, preserving BOTH the out-of-band change and the model's edit.
func TestEditHashline_RecoversStaleTagOnDifferentRegion(t *testing.T) {
	const content = "line1\nline2\nline3\nline4\nline5\n"
	ws, root := newTestWorkspace(t, map[string]string{"f.txt": content})
	set := NewToolSet(ws)

	// 1. Read (records the base version under f.txt's canonical path).
	header := readHeader(t, set, "f.txt")

	// 2. Externally modify a DIFFERENT region (line5) behind the tool's back.
	const live = "line1\nline2\nline3\nline4\nLINE5-EXTERNAL\n"
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte(live), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}

	// 3. Edit line2 using the now-stale header. The target anchor is unchanged,
	//    so it remaps cleanly while the unrelated line5 change survives.
	res, updates := editWith(t, set, header, "replace 2:\n+LINE2-EDITED\n", false)
	if res.IsError {
		t.Fatalf("stale edit on a different region should recover, got: %s", res.Content)
	}

	// Both changes survive on disk.
	got, _ := os.ReadFile(filepath.Join(root, "f.txt"))
	const want = "line1\nLINE2-EDITED\nline3\nline4\nLINE5-EXTERNAL\n"
	if string(got) != want {
		t.Errorf("merged file = %q, want %q", got, want)
	}

	// The result and audit event flag the recovery.
	var er EditHashlineResult
	if err := json.Unmarshal(res.Structured, &er); err != nil {
		t.Fatalf("unmarshal edit result: %v", err)
	}
	if !er.Recovered {
		t.Errorf("result should be flagged Recovered: %+v", er)
	}
	if len(er.Sections) != 1 || !er.Sections[0].Recovered {
		t.Errorf("section should be flagged Recovered: %+v", er.Sections)
	}
	if !strings.Contains(res.Content, "recovered stale tag against current content") {
		t.Errorf("content should announce the recovery:\n%s", res.Content)
	}
	if len(updates) == 0 || updates[0].Data["recovered"] != "true" {
		t.Errorf("audit event should carry recovered=true: %+v", updates)
	}
}

// TestEditHashline_RecoversStaleTagInTypicalGoFile covers the common case
// that the old line-level merge rejects: real Go files repeat blank lines and
// closing braces. A disjoint insertion before an unchanged target must not
// force a re-read merely because unrelated line values are duplicated.
func TestEditHashline_RecoversStaleTagInTypicalGoFile(t *testing.T) {
	const base = "package sample\n\nfunc first() int {\n\treturn 1\n}\n\nfunc second() int {\n\treturn 2\n}\n"
	ws, root := newTestWorkspace(t, map[string]string{"sample.go": base})
	set := NewToolSet(ws)
	header := readHeader(t, set, "sample.go")

	const live = "// generated file\n" + base
	if err := os.WriteFile(filepath.Join(root, "sample.go"), []byte(live), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}

	res, _ := editWith(t, set, header, "replace 8:\n+\treturn 3\n", false)
	if res.IsError {
		t.Fatalf("stale edit with unchanged anchors should recover despite duplicate lines: %s", res.Content)
	}

	got, err := os.ReadFile(filepath.Join(root, "sample.go"))
	if err != nil {
		t.Fatalf("read recovered file: %v", err)
	}
	const want = "// generated file\npackage sample\n\nfunc first() int {\n\treturn 1\n}\n\nfunc second() int {\n\treturn 3\n}\n"
	if string(got) != want {
		t.Fatalf("recovered file = %q, want %q", got, want)
	}
}

// TestEditHashline_RejectsStaleTagOnSameLines proves the conflicting-recovery
// path: a read records the base; an out-of-band change to the SAME target line
// removes its unchanged anchor, so recovery is rejected with a re-read message
// and the file is NOT mutated.
func TestEditHashline_RejectsStaleTagOnSameLines(t *testing.T) {
	const content = "alpha\nbeta\ngamma\n"
	ws, root := newTestWorkspace(t, map[string]string{"f.txt": content})
	set := NewToolSet(ws)

	header := readHeader(t, set, "f.txt")

	// Externally change the SAME line the model is about to edit, differently.
	const live = "alpha\nBETA-FROM-DISK\ngamma\n"
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte(live), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}

	res, _ := editWith(t, set, header, "replace 2:\n+BETA-FROM-MODEL\n", false)
	if !res.IsError {
		t.Fatal("a stale edit on the same lines must be rejected (target anchor changed)")
	}
	if !strings.Contains(res.Content, "re-read") {
		t.Errorf("conflicting stale edit should instruct a re-read:\n%s", res.Content)
	}

	// The file must be untouched (no partial/clobbering write).
	got, _ := os.ReadFile(filepath.Join(root, "f.txt"))
	if string(got) != live {
		t.Errorf("rejected edit mutated the file: got %q, want %q", got, live)
	}
}

// TestEditHashline_RecoversStaleBlockEditAgainstBase proves the deferred
// block-resolution fix: a "replace block N" edit whose tag is stale recovers
// against the recorded base even when the block no longer resolves against the
// LIVE file. Here an out-of-band insertion shifts the function down, so the
// stale "block 4" lands on a blank line in the live file (no Go block starts
// there). Block resolution is therefore deferred out of the unconditional
// preflight path and run against the base the tag referred to inside
// recoverStale, where line 4 still starts the function. The resolved range then
// remaps to the unchanged live function and replays beside the insertion.
//
// Before the fix, Preflight resolved block ops against the live file first and
// aborted with ErrBlockResolve, never reaching recovery; this test asserts the
// edit instead recovers.
func TestEditHashline_RecoversStaleBlockEditAgainstBase(t *testing.T) {
	// "func Add" starts on line 4. Its doc comment starts on line 3, so
	// "replace block 4" resolves to lines 3..6 against the recorded base.
	const base = "package calc\nconst anchor = 0\n// Add sums two integers.\nfunc Add(a, b int) int {\n\treturn a - b\n}\n"
	ws, root := newTestWorkspace(t, map[string]string{"calc.go": base})
	set := NewToolSet(ws)

	// 1. Read records the base under calc.go's canonical path and mints the tag.
	header := readHeader(t, set, "calc.go")

	// 2. Out-of-band insertion of two const decls plus a blank right after the
	//    package clause. The function shifts down, so the original line 4 now
	//    holds a blank line: "replace block 4" no longer resolves against live.
	//    The insertion is disjoint from the edited function, so the merge is clean.
	const live = "package calc\nconst c1 = 1\nconst c2 = 2\n\nconst anchor = 0\n// Add sums two integers.\nfunc Add(a, b int) int {\n\treturn a - b\n}\n"
	if err := os.WriteFile(filepath.Join(root, "calc.go"), []byte(live), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}

	// 3. Edit "block 4" with the now-stale header. Against live, line 4 starts no
	//    Go block; recovery resolves it against the base (where line 4 is the func
	//    keyword) and merges with the disjoint insertion.
	body := "replace block 4:\n" +
		"+// Add returns the sum of two integers.\n" +
		"+func Add(a, b int) int {\n" +
		"+\treturn a + b\n" +
		"+}\n"
	res, _ := editWith(t, set, header, body, false)
	if res.IsError {
		t.Fatalf("stale block edit should recover against the recorded base, got: %s", res.Content)
	}

	got, _ := os.ReadFile(filepath.Join(root, "calc.go"))
	gotStr := string(got)
	// The out-of-band insertion survives.
	for _, want := range []string{"const c1 = 1", "const c2 = 2", "const anchor = 0"} {
		if !strings.Contains(gotStr, want) {
			t.Errorf("out-of-band line %q lost in merge:\n%s", want, gotStr)
		}
	}
	// The model's edit applies.
	if !strings.Contains(gotStr, "return a + b") || !strings.Contains(gotStr, "Add returns the sum of two integers.") {
		t.Errorf("block edit was not applied:\n%s", gotStr)
	}
	if strings.Contains(gotStr, "return a - b") {
		t.Errorf("stale Add body still present:\n%s", gotStr)
	}

	// The result is flagged as a recovery.
	var er EditHashlineResult
	if err := json.Unmarshal(res.Structured, &er); err != nil {
		t.Fatalf("unmarshal edit result: %v", err)
	}
	if !er.Recovered || len(er.Sections) != 1 || !er.Sections[0].Recovered {
		t.Errorf("edit should be flagged Recovered: %+v", er)
	}
	if !strings.Contains(res.Content, "recovered stale tag against current content") {
		t.Errorf("content should announce the recovery:\n%s", res.Content)
	}
}

// TestEditHashline_RollbackRestoresOverCapOriginal proves Commit's
// all-or-nothing rollback holds even when a section's original content exceeds
// the forward write cap. Section A shrinks an over-cap (but readable) file so
// its own write succeeds; section B's result exceeds the cap so its write fails
// and triggers rollback. Restoring A's original (still over-cap) goes through
// the uncapped restorer path, so A is put back rather than left shrunk.
func TestEditHashline_RollbackRestoresOverCapOriginal(t *testing.T) {
	// A is ~98 bytes — above the 64-byte write cap below, but well within the
	// (default) read cap, so it reads cleanly and mints a tag.
	aOriginal := "keepme\n" + strings.Repeat("b", 90) + "\n"
	_, root := newTestWorkspace(t, map[string]string{
		"a.txt": aOriginal,
		"b.txt": "hello\n",
	})
	// Re-open the same root with a tiny write cap (newTestWorkspace uses defaults).
	capped := NewLocalWorkspace(root, WithMaxWriteBytes(64))
	set := NewToolSet(capped)

	headerA := readHeader(t, set, "a.txt")
	headerB := readHeader(t, set, "b.txt")

	// Section A shrinks the big file (write succeeds); section B's replacement is
	// 81 bytes > 64, so committing B fails and rolls A back.
	long := strings.Repeat("Z", 80)
	patch := headerA + "\nreplace 2:\n+x\n" +
		headerB + "\nreplace 1:\n+" + long + "\n"
	res, _ := callJSON(t, driverByName(t, set, ToolEditHashline), editHashlineInput{Input: patch})
	if !res.IsError {
		t.Fatalf("over-cap section B should fail the commit, got success: %s", res.Content)
	}

	// A must be rolled back to its original over-cap content, not left shrunk.
	gotA, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(gotA) != aOriginal {
		t.Errorf("rollback did not restore the over-cap original:\n got %q\nwant %q", gotA, aOriginal)
	}
	// B was never written (cap rejected before the write), so it is untouched.
	gotB, _ := os.ReadFile(filepath.Join(root, "b.txt"))
	if string(gotB) != "hello\n" {
		t.Errorf("b.txt should be untouched, got %q", gotB)
	}
}

// TestEditHashline_BlockReplaceFunc proves block edit flows end-to-end through
// coding.edit_hashline: "replace block N" pointed at a func's line replaces the
// whole function on a real Go file in a temp workspace.
func TestEditHashline_BlockReplaceFunc(t *testing.T) {
	const content = `package calc

// Add sums two integers.
func Add(a, b int) int {
	return a - b
}

// Keep is left untouched.
func Keep() string {
	return "keep"
}
`
	ws, root := newTestWorkspace(t, map[string]string{"calc.go": content})
	set := NewToolSet(ws)

	header := readHeader(t, set, "calc.go")

	// "replace block N" where N is the func's doc-comment/keyword line. The Add
	// doc comment is line 3 and the func keyword is line 4; pointing at the
	// keyword line selects the whole function (doc comment included).
	body := "replace block 4:\n" +
		"+// Add sums two integers correctly.\n" +
		"+func Add(a, b int) int {\n" +
		"+\treturn a + b\n" +
		"+}\n"
	res, _ := editWith(t, set, header, body, false)
	if res.IsError {
		t.Fatalf("block replace through edit_hashline should succeed, got: %s", res.Content)
	}

	got, _ := os.ReadFile(filepath.Join(root, "calc.go"))
	gotStr := string(got)
	if !strings.Contains(gotStr, "return a + b") {
		t.Errorf("Add body was not replaced:\n%s", gotStr)
	}
	if strings.Contains(gotStr, "return a - b") {
		t.Errorf("stale Add body still present:\n%s", gotStr)
	}
	if !strings.Contains(gotStr, "Add sums two integers correctly.") {
		t.Errorf("Add doc comment was not replaced:\n%s", gotStr)
	}
	// The untouched second function must survive verbatim.
	if !strings.Contains(gotStr, `func Keep() string {`) || !strings.Contains(gotStr, `return "keep"`) {
		t.Errorf("Keep() should be untouched:\n%s", gotStr)
	}

	var er EditHashlineResult
	if err := json.Unmarshal(res.Structured, &er); err != nil {
		t.Fatalf("unmarshal edit result: %v", err)
	}
	if len(er.Sections) != 1 {
		t.Fatalf("expected one section, got %+v", er.Sections)
	}
	if !strings.Contains(er.Sections[0].Op, "replace block 4") {
		t.Errorf("section op should record the block edit: %q", er.Sections[0].Op)
	}
}
