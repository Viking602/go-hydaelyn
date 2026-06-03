package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListFiles_BasicAndSorted(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"a.go":         "package a\n",
		"b.go":         "package b\n",
		"pkg/c.go":     "package pkg\n",
		"pkg/sub/d.go": "package sub\n",
		"README.md":    "# readme\n",
	})

	res, err := ListFiles(context.Background(), root, ListFilesRequest{})
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	want := []string{"README.md", "a.go", "b.go", "pkg/c.go", "pkg/sub/d.go"}
	if !equalStrings(res.Files, want) {
		t.Fatalf("Files = %v, want %v", res.Files, want)
	}
	if res.Truncated {
		t.Errorf("did not expect truncation")
	}
}

func TestListFiles_DeniesGit(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"keep.go":         "package keep\n",
		".git/config":     "[core]\n",
		".git/HEAD":       "ref: refs/heads/main\n",
		".git/hooks/x.sh": "#!/bin/sh\n",
	})

	res, err := ListFiles(context.Background(), root, ListFilesRequest{})
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	for _, f := range res.Files {
		if strings.HasPrefix(f, ".git/") || f == ".git" {
			t.Errorf("listing leaked .git entry: %q", f)
		}
	}
	if !equalStrings(res.Files, []string{"keep.go"}) {
		t.Fatalf("Files = %v, want [keep.go]", res.Files)
	}
}

// TestListFiles_GitDenialIsExact ensures the .git denylist is segment-precise:
// sibling names that merely share the ".git" prefix (.github, .gitignore) are
// listed, while the .git tree is excluded.
func TestListFiles_GitDenialIsExact(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		".gitignore":           "*.tmp\n",
		".github/workflow.yml": "on: push\n",
		".git/config":          "[core]\n",
		"keep.go":              "package keep\n",
	})

	res, err := ListFiles(context.Background(), root, ListFilesRequest{})
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	want := []string{".github/workflow.yml", ".gitignore", "keep.go"}
	if !equalStrings(res.Files, want) {
		t.Fatalf("Files = %v, want %v", res.Files, want)
	}
}

// TestListFiles_DoesNotFollowSymlinkedDir ensures a directory symlink pointing
// outside the workspace is neither followed nor reported as a file: WalkDir does
// not descend symlinks and the symlink entry is not a regular file.
func TestListFiles_DoesNotFollowSymlinkedDir(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeFiles(t, root, map[string]string{"keep.go": "package keep\n"})
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("s"), 0o600); err != nil {
		t.Fatalf("write outside: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "leak")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	res, err := ListFiles(context.Background(), root, ListFilesRequest{})
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	for _, f := range res.Files {
		if strings.HasPrefix(f, "leak") {
			t.Errorf("symlinked dir leaked entry: %q", f)
		}
		if strings.Contains(f, "secret") {
			t.Errorf("walk followed symlink out of workspace: %q", f)
		}
	}
	if !equalStrings(res.Files, []string{"keep.go"}) {
		t.Fatalf("Files = %v, want [keep.go]", res.Files)
	}
}

func TestListFiles_GlobAndIgnore(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"a.go":  "package a\n",
		"b.go":  "package b\n",
		"c.txt": "text\n",
		"d.tmp": "tmp\n",
	})

	// Glob restricts to top-level .go files.
	res, err := ListFiles(context.Background(), root, ListFilesRequest{Glob: "*.go"})
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if !equalStrings(res.Files, []string{"a.go", "b.go"}) {
		t.Fatalf("glob Files = %v, want [a.go b.go]", res.Files)
	}

	// Ignore drops *.tmp.
	res, err = ListFiles(context.Background(), root, ListFilesRequest{Ignore: []string{"*.tmp"}})
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	for _, f := range res.Files {
		if strings.HasSuffix(f, ".tmp") {
			t.Errorf("ignore failed, found %q", f)
		}
	}
}

func TestListFiles_Limit(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{}
	for i := 0; i < 10; i++ {
		files[string(rune('a'+i))+".go"] = "package x\n"
	}
	writeFiles(t, root, files)

	res, err := ListFiles(context.Background(), root, ListFilesRequest{Limit: 3})
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(res.Files) != 3 {
		t.Fatalf("len(Files) = %d, want 3", len(res.Files))
	}
	if !res.Truncated {
		t.Errorf("expected Truncated=true")
	}
}

func TestListFiles_SkipsBinaryAndLarge(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"text.go": "package x\n",
	})
	// Binary file: contains a NUL byte.
	if err := os.WriteFile(filepath.Join(root, "blob.bin"), []byte("abc\x00def"), 0o600); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	// Large file beyond a small cap.
	big := strings.Repeat("x", 2048)
	if err := os.WriteFile(filepath.Join(root, "big.txt"), []byte(big), 0o600); err != nil {
		t.Fatalf("write big: %v", err)
	}

	res, err := ListFiles(context.Background(), root, ListFilesRequest{MaxFileBytes: 1024})
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	for _, f := range res.Files {
		if f == "blob.bin" {
			t.Errorf("binary file should be skipped")
		}
		if f == "big.txt" {
			t.Errorf("large file should be skipped")
		}
	}
	if !equalStrings(res.Files, []string{"text.go"}) {
		t.Fatalf("Files = %v, want [text.go]", res.Files)
	}
}

func TestListFiles_IncludeBinary(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "blob.bin"), []byte("abc\x00def"), 0o600); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	res, err := ListFiles(context.Background(), root, ListFilesRequest{IncludeBinary: true})
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if !equalStrings(res.Files, []string{"blob.bin"}) {
		t.Fatalf("Files = %v, want [blob.bin]", res.Files)
	}
}

func TestListFiles_ContextCancelled(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{"a.go": "package a\n"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ListFiles(ctx, root, ListFilesRequest{})
	if err == nil {
		t.Fatalf("expected context error")
	}
}

func TestLooksBinary(t *testing.T) {
	root := t.TempDir()
	textPath := filepath.Join(root, "t.txt")
	binPath := filepath.Join(root, "b.bin")
	emptyPath := filepath.Join(root, "e.txt")
	if err := os.WriteFile(textPath, []byte("hello\nworld\t!\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binPath, []byte{0x00, 0x01, 0x02, 0x03}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(emptyPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if looksBinary(textPath) {
		t.Errorf("text file flagged as binary")
	}
	if !looksBinary(binPath) {
		t.Errorf("binary file not flagged")
	}
	if looksBinary(emptyPath) {
		t.Errorf("empty file should not be binary")
	}
}

// --- test helpers ---

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// repoRoot walks up from the working directory to find the module root (the
// directory containing go.mod). Used by command tests that need a real git repo.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}
