package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// File-listing defaults.
const (
	// DefaultListLimit caps how many entries ListFiles returns when the request
	// does not specify a limit.
	DefaultListLimit = 1000
	// DefaultMaxFileBytes is the size above which a file is treated as "large"
	// and skipped by the text-oriented walker.
	DefaultMaxFileBytes = 1 << 20 // 1 MiB
	// binarySniffBytes is how many leading bytes are inspected to decide whether
	// a file looks binary.
	binarySniffBytes = 8000
)

// ListFilesRequest controls the workspace-relative file walk.
type ListFilesRequest struct {
	// Glob is an optional filepath.Match pattern applied to each candidate's
	// workspace-relative path (with "/" separators). Empty matches everything.
	Glob string
	// Ignore is an optional list of filepath.Match patterns; a path matching any
	// of them is skipped.
	Ignore []string
	// Limit caps the number of returned entries (default DefaultListLimit).
	Limit int
	// MaxFileBytes is the size above which files are skipped (default
	// DefaultMaxFileBytes). Use a negative value to disable the size filter.
	MaxFileBytes int64
	// IncludeBinary, when true, keeps files that look binary; by default they
	// are skipped.
	IncludeBinary bool
}

// ListFilesResult is the typed outcome of a workspace file walk. Files holds
// cleaned workspace-relative paths using "/" separators, sorted lexically.
type ListFilesResult struct {
	Files     []string `json:"files"`
	Truncated bool     `json:"truncated"`
}

// ListFiles walks root and returns workspace-relative file paths matching the
// request. The .git tree is always skipped, binary and oversized files are
// skipped by default, and the result is capped at the requested limit (with
// Truncated set when more entries existed).
func ListFiles(ctx context.Context, root string, req ListFilesRequest) (ListFilesResult, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = DefaultListLimit
	}
	maxBytes := req.MaxFileBytes
	if maxBytes == 0 {
		maxBytes = DefaultMaxFileBytes
	}

	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return ListFilesResult{}, fmt.Errorf("workspace: resolve root: %w", err)
	}

	var (
		files     []string
		truncated bool
	)

	walkErr := filepath.WalkDir(rootAbs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}

		rel, relErr := filepath.Rel(rootAbs, path)
		if relErr != nil {
			return relErr
		}
		if rel == "." {
			return nil
		}
		slashRel := filepath.ToSlash(rel)

		if d.IsDir() {
			// Always skip the .git tree, and any directory matching an ignore.
			if isDeniedRel(filepath.FromSlash(slashRel)) || matchesAny(req.Ignore, slashRel) {
				return fs.SkipDir
			}
			return nil
		}

		keep, keepErr := keepFile(path, slashRel, d, maxBytes, req)
		if keepErr != nil {
			return keepErr
		}
		if !keep {
			return nil
		}

		if len(files) >= limit {
			truncated = true
			return fs.SkipAll
		}
		files = append(files, slashRel)
		return nil
	})
	if walkErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(walkErr, ctxErr) {
			return ListFilesResult{}, ctxErr
		}
		return ListFilesResult{}, fmt.Errorf("workspace: walk: %w", walkErr)
	}

	sort.Strings(files)
	return ListFilesResult{Files: files, Truncated: truncated}, nil
}

// keepFile reports whether a regular file at path (workspace-relative
// slashRel) survives the denylist, ignore, glob, regularity, size, and binary
// filters. It returns an error only for unrecoverable conditions (invalid glob
// pattern, unreadable file info).
func keepFile(path, slashRel string, d fs.DirEntry, maxBytes int64, req ListFilesRequest) (bool, error) {
	if isDeniedRel(filepath.FromSlash(slashRel)) {
		return false, nil
	}
	if matchesAny(req.Ignore, slashRel) {
		return false, nil
	}
	if req.Glob != "" {
		ok, matchErr := filepath.Match(req.Glob, slashRel)
		if matchErr != nil {
			return false, fmt.Errorf("workspace: invalid glob %q: %w", req.Glob, matchErr)
		}
		if !ok {
			return false, nil
		}
	}

	info, infoErr := d.Info()
	if infoErr != nil {
		return false, infoErr
	}
	if !info.Mode().IsRegular() {
		return false, nil
	}
	if maxBytes > 0 && info.Size() > maxBytes {
		return false, nil
	}
	if !req.IncludeBinary && looksBinary(path) {
		return false, nil
	}
	return true, nil
}

// matchesAny reports whether slashRel matches any of the given filepath.Match
// patterns. Invalid patterns are ignored (treated as non-matching).
func matchesAny(patterns []string, slashRel string) bool {
	for _, p := range patterns {
		if p == "" {
			continue
		}
		if ok, err := filepath.Match(p, slashRel); err == nil && ok {
			return true
		}
		// Also match against the base name so an ignore like "*.tmp" works on
		// nested paths.
		if ok, err := filepath.Match(p, filepath.Base(slashRel)); err == nil && ok {
			return true
		}
	}
	return false
}

// looksBinary reports whether the file at path appears to be binary by sniffing
// its leading bytes for a NUL or a high proportion of non-text bytes.
func looksBinary(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		// Unreadable: treat as binary so the text walker skips it.
		return true
	}
	defer f.Close()

	buf := make([]byte, binarySniffBytes)
	n, _ := f.Read(buf)
	if n == 0 {
		return false
	}
	chunk := buf[:n]
	if bytes.IndexByte(chunk, 0) >= 0 {
		return true
	}

	nonText := 0
	for _, b := range chunk {
		if b == '\t' || b == '\n' || b == '\r' || b == '\f' {
			continue
		}
		if b < 0x20 || b == 0x7f {
			nonText++
		}
	}
	return nonText*100 > n*30
}

// ToWorkspaceGlob normalizes a user-supplied glob to use "/" separators so it
// matches the paths produced by ListFiles regardless of host OS.
func ToWorkspaceGlob(glob string) string {
	return strings.ReplaceAll(glob, string(filepath.Separator), "/")
}
