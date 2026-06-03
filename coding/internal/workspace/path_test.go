package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveWorkspacePath_Accepts(t *testing.T) {
	root := t.TempDir()

	cases := []struct {
		name         string
		rel          string
		wantCanonRel string
	}{
		{name: "simple file", rel: "foo.go", wantCanonRel: "foo.go"},
		{name: "nested file", rel: "pkg/sub/bar.go", wantCanonRel: filepath.Join("pkg", "sub", "bar.go")},
		{name: "dot-prefixed redundancy", rel: "./baz.go", wantCanonRel: "baz.go"},
		{name: "interior dot-dot that stays inside", rel: "pkg/../qux.go", wantCanonRel: "qux.go"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			abs, canon, err := ResolveWorkspacePath(root, tc.rel)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if canon != tc.wantCanonRel {
				t.Errorf("canonicalRel = %q, want %q", canon, tc.wantCanonRel)
			}
			wantAbs := filepath.Join(root, tc.wantCanonRel)
			if abs != wantAbs {
				t.Errorf("abs = %q, want %q", abs, wantAbs)
			}
		})
	}
}

func TestResolveWorkspacePath_Rejects(t *testing.T) {
	root := t.TempDir()

	cases := []struct {
		name    string
		rel     string
		wantErr error
	}{
		{name: "empty", rel: "", wantErr: ErrEmptyPath},
		{name: "dot only", rel: ".", wantErr: ErrEmptyPath},
		{name: "absolute path", rel: "/etc/passwd", wantErr: ErrAbsolutePath},
		{name: "dot-dot escape", rel: "../outside.go", wantErr: ErrPathEscape},
		{name: "dot-dot only", rel: "..", wantErr: ErrPathEscape},
		{name: "deep dot-dot escape", rel: "a/b/../../../outside.go", wantErr: ErrPathEscape},
		{name: "NUL byte", rel: "foo\x00bar.go", wantErr: ErrNULByte},
		{name: "git dir", rel: ".git", wantErr: ErrDeniedPath},
		{name: "git child", rel: ".git/config", wantErr: ErrDeniedPath},
		{name: "git nested", rel: ".git/hooks/pre-commit", wantErr: ErrDeniedPath},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := ResolveWorkspacePath(root, tc.rel)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// TestResolveWorkspacePath_SymlinkEscape_LeafSymlink ensures that an existing
// leaf which is a symlink pointing outside the workspace is rejected.
func TestResolveWorkspacePath_SymlinkEscape_LeafSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	outsideFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	link := filepath.Join(root, "leak.txt")
	if err := os.Symlink(outsideFile, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	_, _, err := ResolveWorkspacePath(root, "leak.txt")
	if !errors.Is(err, ErrPathEscape) {
		t.Fatalf("err = %v, want ErrPathEscape", err)
	}
}

// TestResolveWorkspacePath_SymlinkEscape_ParentDir is the spec's core case: a
// not-yet-created file under a symlinked parent directory that points outside
// the workspace must be rejected by resolving the deepest existing ancestor.
func TestResolveWorkspacePath_SymlinkEscape_ParentDir(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	// root/escape -> outside  (a directory symlink)
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	// "escape/newfile.go" does not exist yet; its parent "escape" resolves to
	// the outside directory, so the path must be rejected.
	_, _, err := ResolveWorkspacePath(root, "escape/newfile.go")
	if !errors.Is(err, ErrPathEscape) {
		t.Fatalf("err = %v, want ErrPathEscape", err)
	}
}

// TestResolveWorkspacePath_SymlinkWithinRoot confirms a symlink that stays
// inside the workspace is accepted.
func TestResolveWorkspacePath_SymlinkWithinRoot(t *testing.T) {
	root := t.TempDir()

	realDir := filepath.Join(root, "real")
	if err := os.Mkdir(realDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	link := filepath.Join(root, "alias")
	if err := os.Symlink(realDir, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	_, _, err := ResolveWorkspacePath(root, "alias/inner.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestResolveWorkspacePath_SymlinkAliasIntoGit covers the deny-tree bypass: a
// symlink alias such as "g -> .git" has a benign cleaned path, so the lexical
// .git deny check passes, and the alias resolves *inside* the in-bounds .git
// tree, so the containment check passes too. The resolver must still deny it so
// read/write tools cannot reach .git through the link.
func TestResolveWorkspacePath_SymlinkAliasIntoGit(t *testing.T) {
	root := t.TempDir()
	gitDir := filepath.Join(root, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte("[core]\n"), 0o644); err != nil {
		t.Fatalf("write .git/config: %v", err)
	}
	if err := os.Symlink(".git", filepath.Join(root, "g")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	// Also a nested alias pointing back up at the root .git.
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	if err := os.Symlink(filepath.Join("..", ".git"), filepath.Join(sub, "g")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	for _, rel := range []string{"g", "g/config", "g/hooks/pre-commit", "sub/g/config"} {
		if _, _, err := ResolveWorkspacePath(root, rel); !errors.Is(err, ErrDeniedPath) {
			t.Errorf("ResolveWorkspacePath(%q) err = %v, want ErrDeniedPath", rel, err)
		}
	}
}

// TestResolveWorkspacePath_SymlinkAliasOutsideGitOK confirms the deny-tree check
// does not over-reach: a symlink to an ordinary in-root directory whose name
// merely starts with ".git" (here ".gitkeep-data") is still allowed, since only
// the actual .git tree is denied.
func TestResolveWorkspacePath_SymlinkAliasOutsideGitOK(t *testing.T) {
	root := t.TempDir()
	// A real .git is present so the deny-tree check is actually consulted; the
	// alias targets a sibling whose name only shares the ".git" prefix.
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	dataDir := filepath.Join(root, ".gitkeep-data")
	if err := os.Mkdir(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink(".gitkeep-data", filepath.Join(root, "alias")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	if _, _, err := ResolveWorkspacePath(root, "alias/inner.go"); err != nil {
		t.Fatalf("alias to non-.git dir should be allowed, got %v", err)
	}
}

// TestResolveWorkspacePath_DanglingLeafSymlinkEscape covers a leaf symlink whose
// target does NOT yet exist but points outside the workspace. EvalSymlinks
// reports such a link as not-existing, so a naive ancestor walk would skip past
// it and judge the path in-bounds; a later WriteText would then follow the link
// and create the file outside the sandbox. The resolver must reject it.
func TestResolveWorkspacePath_DanglingLeafSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	// root/leak.txt -> outside/nonexistent.txt (target absent ⇒ dangling).
	target := filepath.Join(outside, "nonexistent.txt")
	link := filepath.Join(root, "leak.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	_, _, err := ResolveWorkspacePath(root, "leak.txt")
	if !errors.Is(err, ErrPathEscape) {
		t.Fatalf("err = %v, want ErrPathEscape", err)
	}
}

// TestResolveWorkspacePath_DanglingParentSymlinkEscape covers a directory
// symlink pointing outside whose own target does not exist, with a not-yet-
// created child file beneath it.
func TestResolveWorkspacePath_DanglingParentSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	// root/escape -> outside/missingdir (the dir does not exist).
	target := filepath.Join(outside, "missingdir")
	link := filepath.Join(root, "escape")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	_, _, err := ResolveWorkspacePath(root, "escape/newfile.go")
	if !errors.Is(err, ErrPathEscape) {
		t.Fatalf("err = %v, want ErrPathEscape", err)
	}
}

// TestResolveWorkspacePath_DanglingSymlinkWithinRoot confirms a dangling symlink
// whose (absent) target stays inside the workspace is accepted: the agent may
// legitimately create the target later.
func TestResolveWorkspacePath_DanglingSymlinkWithinRoot(t *testing.T) {
	root := t.TempDir()

	// root/pending -> root/real/target.go (target absent but in-bounds).
	target := filepath.Join(root, "real", "target.go")
	link := filepath.Join(root, "pending")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	if _, _, err := ResolveWorkspacePath(root, "pending"); err != nil {
		t.Fatalf("unexpected error for in-bounds dangling symlink: %v", err)
	}
}

// TestResolveWorkspacePath_RelativeRoot ensures a relative workspace root is
// resolved against the working directory rather than spuriously rejected: the
// lexical containment check must compare absolute paths on both sides.
func TestResolveWorkspacePath_RelativeRoot(t *testing.T) {
	parent := t.TempDir()
	// Create a real subdir under parent and chdir into parent so "ws" is a valid
	// relative root.
	if err := os.Mkdir(filepath.Join(parent, "ws"), 0o755); err != nil {
		t.Fatalf("mkdir ws: %v", err)
	}
	t.Chdir(parent)

	abs, canon, err := ResolveWorkspacePath("ws", "foo.go")
	if err != nil {
		t.Fatalf("relative root rejected: %v", err)
	}
	if canon != "foo.go" {
		t.Errorf("canonicalRel = %q, want %q", canon, "foo.go")
	}
	if !filepath.IsAbs(abs) {
		t.Errorf("abs = %q, want an absolute path", abs)
	}
	wantAbs := filepath.Join(parent, "ws", "foo.go")
	if abs != wantAbs {
		t.Errorf("abs = %q, want %q", abs, wantAbs)
	}
}

// TestResolveWorkspacePath_SiblingPrefix ensures a sibling directory sharing a
// name prefix with root is not treated as inside root.
func TestResolveWorkspacePath_SiblingPrefix(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "ws")
	sibling := filepath.Join(parent, "ws-evil")
	for _, d := range []string{root, sibling} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	link := filepath.Join(root, "out")
	if err := os.Symlink(sibling, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	_, _, err := ResolveWorkspacePath(root, "out/x.go")
	if !errors.Is(err, ErrPathEscape) {
		t.Fatalf("err = %v, want ErrPathEscape", err)
	}
}
