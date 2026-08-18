package coding

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Viking602/venat/coding/internal/hashline"
	"github.com/Viking602/venat/coding/internal/workspace"
)

func TestLocalWorkspace_ReadFile_InWorkspaceSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	ws, root := newTestWorkspace(t, map[string]string{"real.txt": "hello\n"})
	if err := os.Symlink("real.txt", filepath.Join(root, "alias.txt")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	res, err := ws.ReadFile(context.Background(), ReadFileRequest{Path: "alias.txt"})
	if err != nil {
		t.Fatalf("ReadFile(in-workspace symlink) error = %v", err)
	}
	if res.Text != "hello\n" {
		t.Fatalf("ReadFile(in-workspace symlink) text = %q", res.Text)
	}
}

func TestLocalWorkspace_WriteFile_RejectsExistingSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	ws, root := newTestWorkspace(t, map[string]string{"target.txt": "safe\n"})
	if err := os.Symlink("target.txt", filepath.Join(root, "alias.txt")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	_, err := ws.WriteFile(context.Background(), WriteFileRequest{Path: "alias.txt", Content: "pwned\n"})
	if !errors.Is(err, ErrFileExists) {
		t.Fatalf("WriteFile(existing symlink) error = %v, want ErrFileExists", err)
	}
	got, readErr := os.ReadFile(filepath.Join(root, "target.txt"))
	if readErr != nil {
		t.Fatalf("read target: %v", readErr)
	}
	if string(got) != "safe\n" {
		t.Fatalf("WriteFile overwrote through an in-workspace symlink: %q", got)
	}
}

func TestLocalWorkspace_WriteFile_DoesNotFollowEscapingSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	root := resolvedTempDir(t)
	outside := filepath.Join(filepath.Dir(root), "secret.txt")
	if err := os.WriteFile(outside, []byte("classified\n"), 0o600); err != nil {
		t.Fatalf("seed outside: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(outside) })
	if err := os.Symlink(outside, filepath.Join(root, "planted.txt")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	ws := NewLocalWorkspace(root)
	_, err := ws.WriteFile(context.Background(), WriteFileRequest{Path: "planted.txt", Content: "pwned\n"})
	if err == nil {
		t.Fatal("WriteFile through an escaping symlink must fail")
	}
	if !errors.Is(err, workspace.ErrPathEscape) && !errors.Is(err, ErrFileExists) {
		t.Fatalf("WriteFile(escaping symlink) error = %v, want escape or exists", err)
	}
	got, readErr := os.ReadFile(outside)
	if readErr != nil {
		t.Fatalf("read outside: %v", readErr)
	}
	if string(got) != "classified\n" {
		t.Fatalf("WriteFile followed a symlink out of the workspace: %q", got)
	}
}

func TestLocalWorkspace_WriteText_DoesNotFollowReplacedSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows")
	}
	ws, root := newTestWorkspace(t, map[string]string{"inside.txt": "safe\n"})
	outside := filepath.Join(filepath.Dir(root), "secret.txt")
	if err := os.WriteFile(outside, []byte("classified\n"), 0o600); err != nil {
		t.Fatalf("seed outside: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(outside) })
	if err := os.Remove(filepath.Join(root, "inside.txt")); err != nil {
		t.Fatalf("remove inside: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "inside.txt")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	fs := ws.(hashline.Filesystem)
	err := fs.WriteText(context.Background(), "inside.txt", "pwned\n")
	if err == nil {
		t.Fatal("WriteText through an escaping symlink must fail")
	}
	if !errors.Is(err, workspace.ErrPathEscape) && !errors.Is(err, ErrNotRegularFile) {
		t.Fatalf("WriteText(escaping symlink) error = %v, want escape or not-regular", err)
	}
	got, readErr := os.ReadFile(outside)
	if readErr != nil {
		t.Fatalf("read outside: %v", readErr)
	}
	if string(got) != "classified\n" {
		t.Fatalf("WriteText wrote through a replaced symlink: %q", got)
	}
}

func resolvedTempDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		return resolved
	}
	return root
}
