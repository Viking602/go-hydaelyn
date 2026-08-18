//go:build unix

package coding

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestLocalWorkspace_ReadFile_FIFODoesNotBlock(t *testing.T) {
	root := resolvedTempDir(t)
	if err := syscall.Mkfifo(filepath.Join(root, "pipe"), 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}
	ws := NewLocalWorkspace(root)
	errc := make(chan error, 1)
	go func() {
		_, err := ws.ReadFile(context.Background(), ReadFileRequest{Path: "pipe"})
		errc <- err
	}()
	select {
	case err := <-errc:
		if !errors.Is(err, ErrNotRegularFile) {
			t.Fatalf("ReadFile(fifo) error = %v, want ErrNotRegularFile", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ReadFile blocked on FIFO")
	}
}

func TestLocalWorkspace_ReadFile_SymlinkToFIFODoesNotBlock(t *testing.T) {
	root := resolvedTempDir(t)
	if err := syscall.Mkfifo(filepath.Join(root, "pipe"), 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}
	if err := os.Symlink("pipe", filepath.Join(root, "alias")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	ws := NewLocalWorkspace(root)
	errc := make(chan error, 1)
	go func() {
		_, err := ws.ReadFile(context.Background(), ReadFileRequest{Path: "alias"})
		errc <- err
	}()
	select {
	case err := <-errc:
		if !errors.Is(err, ErrNotRegularFile) {
			t.Fatalf("ReadFile(symlink-to-fifo) error = %v, want ErrNotRegularFile", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ReadFile blocked on symlink to FIFO")
	}
}
