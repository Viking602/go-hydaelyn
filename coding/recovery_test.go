package coding

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Viking602/go-hydaelyn/tool"
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

// TestEditHashline_RecoversStaleTagOnDifferentRegion proves the M6 wiring: a
// read records the file into the shared store; an out-of-band change to a
// DIFFERENT region makes the read's tag stale; an edit carrying that stale tag
// is salvaged by a 3-way merge, and BOTH the out-of-band change and the model's
// edit survive.
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

	// 3. Edit line2 using the now-stale header. The change does not touch
	//    line5, so the 3-way merge is non-conflicting and recovery succeeds.
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
	if !strings.Contains(res.Content, "recovered stale tag via 3-way merge") {
		t.Errorf("content should announce the recovery:\n%s", res.Content)
	}
	if len(updates) == 0 || updates[0].Data["recovered"] != "true" {
		t.Errorf("audit event should carry recovered=true: %+v", updates)
	}
}

// TestEditHashline_RejectsStaleTagOnSameLines proves the conflicting-recovery
// path: a read records the base; an out-of-band change to the SAME lines the
// model is about to edit makes the tag stale; the 3-way merge conflicts, so the
// edit is rejected with the re-read message and the file is NOT mutated.
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
		t.Fatal("a stale edit on the same lines must be rejected (3-way merge conflict)")
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
