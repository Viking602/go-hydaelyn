package coding

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Viking602/venat/tool"
)

// initGitRepo initializes a git repo at root with one committed file so a
// later change produces a diff. It skips the test if git is unavailable.
func initGitRepo(t *testing.T, root string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("add", ".")
	run("commit", "-m", "initial")
}

func TestGitDiff_ShowsWorkspaceChange(t *testing.T) {
	ws, root := newTestWorkspace(t, map[string]string{"f.txt": "one\n"})
	initGitRepo(t, root)

	// Mutate the file through the hashline.Filesystem so a diff exists.
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	set := NewToolSet(ws)
	res, _ := callJSON(t, driverByName(t, set, ToolGitDiff), gitDiffInput{})
	if res.IsError {
		t.Fatalf("git_diff errored: %s", res.Content)
	}
	if !strings.Contains(res.Content, "+two") {
		t.Errorf("git_diff should show the added line:\n%s", res.Content)
	}
}

func TestGoTest_RejectsNonAllowlistedAndRuns(t *testing.T) {
	// A tiny passing test package inside the workspace.
	const goMod = "module sample\n\ngo 1.25\n"
	const testFile = "package sample\n\nimport \"testing\"\n\nfunc TestOK(t *testing.T) {}\n"
	ws, _ := newTestWorkspace(t, map[string]string{
		"go.mod":         goMod,
		"sample_test.go": testFile,
	})
	set := NewToolSet(ws)

	res, _ := callJSON(t, driverByName(t, set, ToolGoTest), goTestInput{Package: "./..."})
	if res.IsError {
		t.Fatalf("go_test errored: %s", res.Content)
	}
	var gr GoTestToolResult
	if err := json.Unmarshal(res.Structured, &gr); err != nil {
		t.Fatalf("unmarshal go_test result: %v", err)
	}
	if !gr.Passed {
		t.Errorf("expected the sample test to pass, got exit %d:\n%s\n%s", gr.ExitCode, gr.Stdout, gr.Stderr)
	}
}

func TestRunCommand_NonAllowlistedRejected(t *testing.T) {
	ws, _ := newTestWorkspace(t, nil)
	_, err := ws.RunCommand(context.Background(), RunCommandRequest{Args: []string{"rm", "-rf", "/"}})
	if err == nil {
		t.Fatal("non-allowlisted command must be rejected")
	}
	// And via the go_test driver: an out-of-sandbox package pattern is rejected.
	set := NewToolSet(ws)
	res, _ := callJSON(t, driverByName(t, set, ToolGoTest), goTestInput{Package: "./../escape"})
	if !res.IsError {
		t.Error("go_test must reject a traversal package pattern")
	}
}

var _ tool.Driver = readFileDriver{}
