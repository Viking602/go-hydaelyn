package coding

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Viking602/go-hydaelyn/coding/internal/hashline"
	"github.com/Viking602/go-hydaelyn/coding/internal/workspace"
)

// gitRepoCmd runs a git command in dir for test setup, failing on error.
func gitRepoCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// newTestWorkspace creates a temp directory, seeds it with the given files
// (relative path -> content), and returns a local workspace over it plus the
// absolute root.
func newTestWorkspace(t *testing.T, files map[string]string) (Workspace, string) {
	t.Helper()
	root := t.TempDir()
	// Resolve symlinks so the macOS /var -> /private/var indirection does not
	// confuse the containment checks during assertions.
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	for rel, content := range files {
		abs := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", abs, err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", abs, err)
		}
	}
	return NewLocalWorkspace(root), root
}

func TestLocalWorkspace_RootAndCanonicalPath(t *testing.T) {
	ws, root := newTestWorkspace(t, map[string]string{"a/b.go": "package a\n"})
	if ws.Root() != root {
		t.Errorf("Root() = %q, want %q", ws.Root(), root)
	}
	fs, ok := ws.(hashline.Filesystem)
	if !ok {
		t.Fatal("local workspace must satisfy hashline.Filesystem")
	}
	canon, err := fs.CanonicalPath("a/b.go")
	if err != nil {
		t.Fatalf("CanonicalPath: %v", err)
	}
	if canon != "a/b.go" {
		t.Errorf("CanonicalPath = %q, want a/b.go", canon)
	}
}

func TestLocalWorkspace_ReadFile_MintsTag(t *testing.T) {
	const content = "package a\n\nfunc Add(a, b int) int {\n\treturn a + b\n}\n"
	ws, _ := newTestWorkspace(t, map[string]string{"foo.go": content})
	res, err := ws.ReadFile(context.Background(), ReadFileRequest{Path: "foo.go"})
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	want := hashline.ComputeFileHash(hashline.Normalize(content).Text)
	if res.Tag != want {
		t.Errorf("tag = %q, want %q", res.Tag, want)
	}
	if res.LineCount != 6 {
		t.Errorf("lineCount = %d, want 6", res.LineCount)
	}
}

func TestLocalWorkspace_ReadFile_Slice(t *testing.T) {
	const content = "l1\nl2\nl3\nl4\nl5\n"
	ws, _ := newTestWorkspace(t, map[string]string{"f.txt": content})
	res, err := ws.ReadFile(context.Background(), ReadFileRequest{Path: "f.txt", StartLine: 2, EndLine: 4})
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if res.SliceText != "l2\nl3\nl4" {
		t.Errorf("sliceText = %q, want l2\\nl3\\nl4", res.SliceText)
	}
	// Tag is over the FULL file, not the slice.
	full := hashline.ComputeFileHash(hashline.Normalize(content).Text)
	if res.Tag != full {
		t.Errorf("tag = %q, want full-file tag %q", res.Tag, full)
	}
}

// TestLocalWorkspace_ReadFile_TooLarge confirms the read-side ceiling: a file
// larger than maxFileBytes is rejected with ErrFileTooLarge on BOTH read paths
// (ReadFile and the patcher's ReadText), and a file within the ceiling still
// reads cleanly.
func TestLocalWorkspace_ReadFile_TooLarge(t *testing.T) {
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	if err := os.WriteFile(filepath.Join(root, "big.txt"), []byte("0123456789abcdef0123456789"), 0o644); err != nil {
		t.Fatalf("seed big.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "small.txt"), []byte("ok\n"), 0o644); err != nil {
		t.Fatalf("seed small.txt: %v", err)
	}
	ws := NewLocalWorkspace(root, WithMaxFileBytes(16))

	if _, err := ws.ReadFile(context.Background(), ReadFileRequest{Path: "big.txt"}); !errors.Is(err, ErrFileTooLarge) {
		t.Errorf("ReadFile(big) error = %v, want ErrFileTooLarge", err)
	}
	fs, ok := ws.(hashline.Filesystem)
	if !ok {
		t.Fatal("local workspace must satisfy hashline.Filesystem")
	}
	if _, err := fs.ReadText(context.Background(), "big.txt"); !errors.Is(err, ErrFileTooLarge) {
		t.Errorf("ReadText(big) error = %v, want ErrFileTooLarge", err)
	}
	if _, err := ws.ReadFile(context.Background(), ReadFileRequest{Path: "small.txt"}); err != nil {
		t.Errorf("ReadFile(small) within the ceiling must succeed, got %v", err)
	}
}

// TestLocalWorkspace_ReadFile_NotRegular confirms a non-regular target (here a
// directory, which the resolver still admits as an in-bounds path) is rejected
// with ErrNotRegularFile rather than read — reading a FIFO/device could block
// forever and reading a directory is meaningless.
func TestLocalWorkspace_ReadFile_NotRegular(t *testing.T) {
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	ws := NewLocalWorkspace(root)
	if _, err := ws.ReadFile(context.Background(), ReadFileRequest{Path: "sub"}); !errors.Is(err, ErrNotRegularFile) {
		t.Errorf("ReadFile(dir) error = %v, want ErrNotRegularFile", err)
	}
}

func TestLocalWorkspace_PathEscapeRejected(t *testing.T) {
	ws, _ := newTestWorkspace(t, map[string]string{"a.go": "package a\n"})
	ctx := context.Background()
	cases := []struct {
		name string
		path string
	}{
		{"parent traversal", "../escape.go"},
		{"deep traversal", "a/../../escape.go"},
		{"git tree", ".git/config"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ws.ReadFile(ctx, ReadFileRequest{Path: tc.path}); err == nil {
				t.Errorf("expected rejection for %q", tc.path)
			}
		})
	}
}

func TestLocalWorkspace_AbsolutePathRejected(t *testing.T) {
	ws, _ := newTestWorkspace(t, nil)
	_, err := ws.ReadFile(context.Background(), ReadFileRequest{Path: "/etc/passwd"})
	if !errors.Is(err, workspace.ErrAbsolutePath) {
		t.Errorf("want ErrAbsolutePath, got %v", err)
	}
}

func TestLocalWorkspace_SymlinkEscapeRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	ws, root := newTestWorkspace(t, map[string]string{"keep.go": "package a\n"})
	outside := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(outside); err == nil {
		outside = resolved
	}
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("top secret\n"), 0o644); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	// A symlink inside the workspace that points at an outside directory.
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	_, err := ws.ReadFile(context.Background(), ReadFileRequest{Path: "link/secret.txt"})
	if !errors.Is(err, workspace.ErrPathEscape) {
		t.Errorf("want ErrPathEscape via symlink, got %v", err)
	}
}

// TestLocalWorkspace_GoTestPackageSymlinkRejected proves the command package-path
// boundary: a directory symlink inside the workspace that points outward passes
// the lexical argv allowlist (it has no ".." segment), but RunCommand resolves
// the `go test` package directory through the path-safety boundary and rejects
// the escape before the go toolchain can follow the link out of the sandbox. A
// real in-workspace package and the recursive root "./..." are not rejected.
func TestLocalWorkspace_GoTestPackageSymlinkRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	ws, root := newTestWorkspace(t, map[string]string{
		"go.mod":          "module sample\n\ngo 1.25\n",
		"pkg/pkg_test.go": "package pkg\n",
		"keep.go":         "package sample\n",
	})
	outside := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(outside); err == nil {
		outside = resolved
	}
	// A directory symlink inside the workspace pointing at an outside directory.
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// An explicit ./link package — and its recursive form — escape the sandbox.
	for _, pattern := range []string{"./link", "./link/..."} {
		_, err := ws.RunCommand(context.Background(), RunCommandRequest{Args: []string{"go", "test", pattern}})
		if !errors.Is(err, workspace.ErrPathEscape) {
			t.Errorf("go test %q: want ErrPathEscape, got %v", pattern, err)
		}
	}

	// The guard must not false-reject in-workspace patterns: a real package
	// directory and the recursive root resolve cleanly. (Checked at the guard so
	// the assertion does not depend on invoking the go toolchain.)
	lw, ok := ws.(*localWorkspace)
	if !ok {
		t.Fatal("NewLocalWorkspace must return *localWorkspace")
	}
	for _, pattern := range []string{"./pkg", "./pkg/...", "./..."} {
		if err := lw.guardCommandPackagePath([]string{"go", "test", pattern}); err != nil {
			t.Errorf("go test %q should be allowed by the package-path guard, got %v", pattern, err)
		}
	}
}

// TestLocalWorkspace_GitDiffPathspecSymlinkRejected proves the git-diff pathspec
// boundary: a file symlink inside the workspace that points outward passes the
// lexical argv allowlist (no "..", not absolute, no pathspec magic), but
// RunCommand resolves each diff pathspec through the path-safety boundary and
// rejects the escape before git can read the outside file. In-workspace paths and
// the "." root pathspec are not rejected.
func TestLocalWorkspace_GitDiffPathspecSymlinkRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	ws, root := newTestWorkspace(t, map[string]string{"keep.go": "package a\n"})
	outside := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(outside); err == nil {
		outside = resolved
	}
	secret := filepath.Join(outside, "secret")
	if err := os.WriteFile(secret, []byte("top secret\n"), 0o600); err != nil {
		t.Fatalf("write outside secret: %v", err)
	}
	// An in-workspace file symlink pointing at the outside secret.
	if err := os.Symlink(secret, filepath.Join(root, "leak")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	diffArgs := func(pathspec string) []string {
		return []string{"git", "-c", "core.fsmonitor=false", "diff", "--no-ext-diff", "--no-textconv", "--", pathspec}
	}
	_, err := ws.RunCommand(context.Background(), RunCommandRequest{Args: diffArgs("leak")})
	if !errors.Is(err, workspace.ErrPathEscape) {
		t.Errorf("git diff -- leak: want ErrPathEscape, got %v", err)
	}

	lw, ok := ws.(*localWorkspace)
	if !ok {
		t.Fatal("NewLocalWorkspace must return *localWorkspace")
	}
	// The guard must not false-reject in-workspace pathspecs or the "." root.
	for _, pathspec := range []string{".", "keep.go"} {
		if err := lw.guardCommandGitPaths(diffArgs(pathspec)); err != nil {
			t.Errorf("git diff pathspec %q should be allowed by the git-paths guard, got %v", pathspec, err)
		}
	}
}

// TestLocalWorkspace_GitFilterAttributeRefused proves a hostile gitattributes
// clean filter cannot turn the read-only git diff/status path into command
// execution. git normalizes worktree files through filter.<driver>.clean while
// computing a diff or status, and --no-ext-diff/--no-textconv do NOT disable
// that, so the workspace must refuse any diff/status whose paths carry a filter
// attribute — from either the worktree .gitattributes or .git/info/attributes —
// before git runs the filter.
func TestLocalWorkspace_GitFilterAttributeRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hostile clean filter uses sh -c; not portable to Windows")
	}
	marker := filepath.Join(t.TempDir(), "PWNED")

	setup := func(t *testing.T, attrsInGitDir bool) Workspace {
		ws, root := newTestWorkspace(t, map[string]string{"f.txt": "hello\n"})
		gitRepoCmd(t, root, "init", "-q")
		gitRepoCmd(t, root, "config", "user.email", "t@t.t")
		gitRepoCmd(t, root, "config", "user.name", "t")
		// Hostile clean filter: a marker write stands in for arbitrary command
		// execution; `cat` passes the content through so git is otherwise happy.
		gitRepoCmd(t, root, "config", "filter.evil.clean", "sh -c 'echo PWNED > "+marker+"; cat'")
		attrLine := []byte("* filter=evil\n")
		dest := filepath.Join(root, ".gitattributes")
		if attrsInGitDir {
			dest = filepath.Join(root, ".git", "info", "attributes")
		}
		if err := os.WriteFile(dest, attrLine, 0o644); err != nil {
			t.Fatalf("write %s: %v", dest, err)
		}
		gitRepoCmd(t, root, "add", "-A")
		gitRepoCmd(t, root, "commit", "-qm", "init")
		// Modify the worktree so diff/status have content to normalize.
		if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("hello world\n"), 0o644); err != nil {
			t.Fatalf("modify f.txt: %v", err)
		}
		return ws
	}

	gitDiff := []string{"git", "-c", "core.fsmonitor=false", "diff", "--no-ext-diff", "--no-textconv", "--", "."}
	gitStatus := []string{"git", "-c", "core.fsmonitor=false", "status", "--short", "--", "."}

	for _, tc := range []struct {
		name          string
		attrsInGitDir bool
	}{
		{"worktree .gitattributes", false},
		{".git/info/attributes", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws := setup(t, tc.attrsInGitDir)
			ctx := context.Background()
			assertRefused := func(label string, run func() error) {
				_ = os.Remove(marker)
				err := run()
				if !errors.Is(err, workspace.ErrCommandNotAllowed) {
					t.Fatalf("%s: want ErrCommandNotAllowed, got %v", label, err)
				}
				if _, statErr := os.Stat(marker); statErr == nil {
					t.Fatalf("%s: clean filter executed (marker created)", label)
				}
			}
			assertRefused("RunCommand git diff", func() error {
				_, err := ws.RunCommand(ctx, RunCommandRequest{Args: gitDiff})
				return err
			})
			assertRefused("RunCommand git status", func() error {
				_, err := ws.RunCommand(ctx, RunCommandRequest{Args: gitStatus})
				return err
			})
			assertRefused("Diff driver", func() error {
				_, err := ws.Diff(ctx, DiffRequest{})
				return err
			})
		})
	}
}

// TestLocalWorkspace_GitDiffNoFilterAllowed is the negative control: the filter
// guard must not break a normal diff in a repo with no filter attributes.
func TestLocalWorkspace_GitDiffNoFilterAllowed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("git diff worktree semantics vary on Windows CI")
	}
	ws, root := newTestWorkspace(t, map[string]string{"f.txt": "hello\n"})
	gitRepoCmd(t, root, "init", "-q")
	gitRepoCmd(t, root, "config", "user.email", "t@t.t")
	gitRepoCmd(t, root, "config", "user.name", "t")
	gitRepoCmd(t, root, "add", "-A")
	gitRepoCmd(t, root, "commit", "-qm", "init")
	if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("hello world\n"), 0o644); err != nil {
		t.Fatalf("modify f.txt: %v", err)
	}

	res, err := ws.Diff(context.Background(), DiffRequest{})
	if err != nil {
		t.Fatalf("Diff on filter-free repo: %v", err)
	}
	if !strings.Contains(res.Diff, "hello world") {
		t.Errorf("diff missing the worktree change: %q", res.Diff)
	}
}

func TestLocalWorkspace_ResolveIdentity_AliasesCollapse(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	ws, root := newTestWorkspace(t, map[string]string{"a.txt": "hi\n"})
	// link.txt is an in-root symlink pointing at a.txt; both names resolve to the
	// same underlying file, so they must share a ResolveIdentity result.
	if err := os.Symlink("a.txt", filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	resolver, ok := ws.(interface {
		ResolveIdentity(string) (string, error)
	})
	if !ok {
		t.Fatal("local workspace must expose ResolveIdentity")
	}
	idA, err := resolver.ResolveIdentity("a.txt")
	if err != nil {
		t.Fatalf("ResolveIdentity(a.txt): %v", err)
	}
	idLink, err := resolver.ResolveIdentity("link.txt")
	if err != nil {
		t.Fatalf("ResolveIdentity(link.txt): %v", err)
	}
	if idA != idLink {
		t.Errorf("aliases resolved to distinct identities: %q vs %q", idA, idLink)
	}
}

func TestLocalWorkspace_WriteFile_NewFileAndExistingRejected(t *testing.T) {
	ws, root := newTestWorkspace(t, map[string]string{"existing.go": "package a\n"})
	ctx := context.Background()

	res, err := ws.WriteFile(ctx, WriteFileRequest{Path: "new/file.go", Content: "package newpkg\n"})
	if err != nil {
		t.Fatalf("WriteFile new: %v", err)
	}
	if res.Tag == "" {
		t.Error("expected a minted tag for the new file")
	}
	if _, statErr := os.Stat(filepath.Join(root, "new/file.go")); statErr != nil {
		t.Errorf("new file not written: %v", statErr)
	}

	_, err = ws.WriteFile(ctx, WriteFileRequest{Path: "existing.go", Content: "package a\n// extra\n"})
	if !errors.Is(err, ErrFileExists) {
		t.Errorf("want ErrFileExists writing over existing file, got %v", err)
	}
	// The existing file must be untouched.
	got, _ := os.ReadFile(filepath.Join(root, "existing.go"))
	if string(got) != "package a\n" {
		t.Errorf("existing file mutated: %q", got)
	}
}

func TestLocalWorkspace_HashlineFilesystemRoundTrip(t *testing.T) {
	ws, root := newTestWorkspace(t, map[string]string{"x.go": "package x\n"})
	fs := ws.(hashline.Filesystem)
	ctx := context.Background()

	text, err := fs.ReadText(ctx, "x.go")
	if err != nil {
		t.Fatalf("ReadText: %v", err)
	}
	if text != "package x\n" {
		t.Errorf("ReadText = %q", text)
	}
	if err := fs.PreflightWrite(ctx, "x.go"); err != nil {
		t.Fatalf("PreflightWrite: %v", err)
	}
	if err := fs.WriteText(ctx, "x.go", "package x\n// edited\n"); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "x.go"))
	if string(got) != "package x\n// edited\n" {
		t.Errorf("WriteText result = %q", got)
	}
}

// TestLocalWorkspace_WriteText_EnforcesWriteCap proves the in-place write path
// (the one edit_hashline and gofmt use) is bounded by maxWriteBytes just like
// WriteFile, so a patch that expands an existing file past the cap is rejected
// and the original content is left untouched.
func TestLocalWorkspace_WriteText_EnforcesWriteCap(t *testing.T) {
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	const original = "package x\n"
	if err := os.WriteFile(filepath.Join(root, "x.go"), []byte(original), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ws := NewLocalWorkspace(root, WithMaxWriteBytes(16))
	fs := ws.(hashline.Filesystem)
	ctx := context.Background()

	// A write within the cap succeeds.
	if err := fs.WriteText(ctx, "x.go", "package x\n//ok\n"); err != nil {
		t.Fatalf("within-cap WriteText: %v", err)
	}

	// A write past the cap is rejected and the file keeps its prior content.
	before, _ := os.ReadFile(filepath.Join(root, "x.go"))
	oversize := "package x\n// " + strings.Repeat("A", 64) + "\n"
	err := fs.WriteText(ctx, "x.go", oversize)
	if err == nil {
		t.Fatal("WriteText past maxWriteBytes must be rejected")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("error should report the byte cap: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "x.go"))
	if string(got) != string(before) {
		t.Errorf("rejected oversize write mutated the file: got %q, want %q", got, before)
	}
}

// nonWritableWorkspace implements coding.Workspace but deliberately omits the
// hashline.Filesystem write methods (CanonicalPath/ReadText/PreflightWrite/
// WriteText), modeling a custom host workspace that supports the read-only
// tools but not in-place edits.
type nonWritableWorkspace struct{ root string }

func (w nonWritableWorkspace) Root() string { return w.root }
func (nonWritableWorkspace) ListFiles(context.Context, ListFilesRequest) (ListFilesResult, error) {
	return ListFilesResult{}, nil
}
func (nonWritableWorkspace) ReadFile(context.Context, ReadFileRequest) (ReadFileResult, error) {
	return ReadFileResult{}, nil
}
func (nonWritableWorkspace) Search(context.Context, SearchRequest) (SearchResult, error) {
	return SearchResult{}, nil
}
func (nonWritableWorkspace) WriteFile(context.Context, WriteFileRequest) (WriteFileResult, error) {
	return WriteFileResult{}, nil
}
func (nonWritableWorkspace) RunCommand(context.Context, RunCommandRequest) (RunCommandResult, error) {
	return RunCommandResult{}, nil
}
func (nonWritableWorkspace) Diff(context.Context, DiffRequest) (DiffResult, error) {
	return DiffResult{}, nil
}

// TestNewToolSet_NonWritableWorkspace_EditErrorsNotPanic proves NewToolSet
// installs an error-returning filesystem (not a nil one) when the host's
// Workspace does not satisfy hashline.Filesystem: edit_hashline then fails fast
// in the patcher's preflight with a clear ErrWorkspaceNotWritable result rather
// than panicking on a nil patcher FS.
func TestNewToolSet_NonWritableWorkspace_EditErrorsNotPanic(t *testing.T) {
	var ws Workspace = nonWritableWorkspace{root: t.TempDir()}
	// Guard: the stub must genuinely NOT satisfy hashline.Filesystem, else the
	// test would exercise the real patcher path and prove nothing.
	if _, ok := ws.(hashline.Filesystem); ok {
		t.Fatal("test stub unexpectedly satisfies hashline.Filesystem")
	}

	set := NewToolSet(ws)
	edit := driverByName(t, set, ToolEditHashline)

	// A syntactically valid patch (tag is four hex digits) so parsing succeeds and
	// the call reaches the patcher's preflight, where the error FS surfaces.
	res, _ := callJSON(t, edit, editHashlineInput{Input: "¶f.go#0000\nreplace 1:\n+hello\n"})
	if !res.IsError {
		t.Fatalf("edit on a non-writable workspace must error, got: %s", res.Content)
	}
	if !strings.Contains(res.Content, "does not implement hashline.Filesystem") {
		t.Errorf("error result should explain the missing write boundary:\n%s", res.Content)
	}
}
