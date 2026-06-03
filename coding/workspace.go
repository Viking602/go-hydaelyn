// Package coding is the Hydaelyn coding capability: a set of sandboxed
// tool.Drivers (read_file, search, edit_hashline, write_file, gofmt,
// go_test, git_diff) over a workspace-relative filesystem, plus the policy
// engine, agent class, and system instructions that govern them.
//
// The package is a sibling of provider/, tool/, policy/, and memory/. Tool
// implementations live here; the path/command/glob sandbox lives in
// coding/internal/workspace and the line-anchored edit protocol in
// coding/internal/hashline. Public results are typed structs — the package
// never returns []any or exposes loose any fields.
//
// Spec anchor: docs/coding-agent-hashline.md sections 2, 5, 6, 7, 8.
package coding

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Viking602/go-hydaelyn/coding/internal/hashline"
	"github.com/Viking602/go-hydaelyn/coding/internal/workspace"
)

// Workspace is the sandboxed filesystem and command surface the coding tools
// operate over. Every path is workspace-relative; escapes, absolute paths,
// symlinked ancestors, and the .git tree are rejected by the implementation.
//
// Spec anchor: docs/coding-agent-hashline.md section 5.1.
type Workspace interface {
	// Root returns the absolute workspace root directory.
	Root() string
	// ListFiles walks the workspace returning workspace-relative paths.
	ListFiles(ctx context.Context, req ListFilesRequest) (ListFilesResult, error)
	// ReadFile reads a workspace-relative file, optionally a line slice.
	ReadFile(ctx context.Context, req ReadFileRequest) (ReadFileResult, error)
	// Search scans workspace text files for a substring or regexp.
	Search(ctx context.Context, req SearchRequest) (SearchResult, error)
	// WriteFile writes a workspace-relative file's full content.
	WriteFile(ctx context.Context, req WriteFileRequest) (WriteFileResult, error)
	// RunCommand runs an allowlisted argv command (no shell) in the workspace.
	RunCommand(ctx context.Context, req RunCommandRequest) (RunCommandResult, error)
	// Diff returns a bounded git diff for the given workspace paths.
	Diff(ctx context.Context, req DiffRequest) (DiffResult, error)
}

// ListFilesRequest controls a workspace file walk. It mirrors the internal
// walker's knobs without leaking the internal package on the public surface.
type ListFilesRequest struct {
	Glob          string   `json:"glob,omitempty"`
	Ignore        []string `json:"ignore,omitempty"`
	Limit         int      `json:"limit,omitempty"`
	MaxFileBytes  int64    `json:"maxFileBytes,omitempty"`
	IncludeBinary bool     `json:"includeBinary,omitempty"`
}

// ListFilesResult is the typed outcome of a workspace file walk.
type ListFilesResult struct {
	Files     []string `json:"files"`
	Truncated bool     `json:"truncated"`
}

// ReadFileRequest selects a workspace-relative file and an optional 1-based
// inclusive line range. Zero StartLine/EndLine mean "whole file".
type ReadFileRequest struct {
	Path      string `json:"path"`
	StartLine int    `json:"startLine,omitempty"`
	EndLine   int    `json:"endLine,omitempty"`
	MaxBytes  int    `json:"maxBytes,omitempty"`
}

// ReadFileResult carries the live file content normalized to LF with its
// minted tag, plus the slice bounds actually returned.
type ReadFileResult struct {
	// Path is the canonical workspace-relative path.
	Path string `json:"path"`
	// Tag is the tag of the full normalized file (minted from the whole file
	// even when only a slice is returned).
	Tag string `json:"tag"`
	// Text is the full normalized (LF, BOM-free) file content.
	Text string `json:"text"`
	// StartLine/EndLine are the 1-based inclusive bounds of the returned slice.
	StartLine int `json:"startLine"`
	EndLine   int `json:"endLine"`
	// SliceText is the LF-internal text of the returned slice.
	SliceText string `json:"sliceText"`
	// LineCount is the number of lines in the full file.
	LineCount int `json:"lineCount"`
	// Truncated reports that MaxBytes capped the returned slice.
	Truncated bool `json:"truncated"`
}

// SearchRequest configures a workspace text search.
type SearchRequest struct {
	Query        string   `json:"query"`
	Regexp       bool     `json:"regexp,omitempty"`
	Glob         string   `json:"glob,omitempty"`
	Ignore       []string `json:"ignore,omitempty"`
	MaxResults   int      `json:"maxResults,omitempty"`
	ContextLines int      `json:"contextLines,omitempty"`
}

// SearchMatch is one matched line in one file, with its surrounding context.
type SearchMatch struct {
	LineNumber int    `json:"lineNumber"`
	Line       string `json:"line"`
}

// SearchFileResult groups matches for a single file under its minted tag.
type SearchFileResult struct {
	Path    string        `json:"path"`
	Tag     string        `json:"tag"`
	Matches []SearchMatch `json:"matches"`
}

// SearchResult is the typed outcome of a workspace search.
type SearchResult struct {
	Files     []SearchFileResult `json:"files"`
	Truncated bool               `json:"truncated"`
}

// WriteFileRequest creates a new workspace-relative file. Writing over an
// existing file is rejected (use edit_hashline instead).
type WriteFileRequest struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	MaxBytes int    `json:"maxBytes,omitempty"`
}

// WriteFileResult reports the new file's path and minted tag.
type WriteFileResult struct {
	Path string `json:"path"`
	Tag  string `json:"tag"`
}

// RunCommandRequest describes a single allowlisted argv invocation.
type RunCommandRequest struct {
	Args           []string          `json:"args"`
	Timeout        time.Duration     `json:"timeout,omitempty"`
	MaxOutputBytes int               `json:"maxOutputBytes,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
}

// RunCommandResult is the typed outcome of an allowlisted command.
type RunCommandResult struct {
	Args      []string `json:"args"`
	ExitCode  int      `json:"exitCode"`
	Stdout    string   `json:"stdout"`
	Stderr    string   `json:"stderr"`
	Truncated bool     `json:"truncated"`
	TimedOut  bool     `json:"timedOut"`
	Duration  string   `json:"duration"`
}

// DiffRequest selects the workspace paths to diff. Empty Paths diffs the
// whole worktree.
type DiffRequest struct {
	Paths          []string      `json:"paths,omitempty"`
	Timeout        time.Duration `json:"timeout,omitempty"`
	MaxOutputBytes int           `json:"maxOutputBytes,omitempty"`
}

// DiffResult carries the bounded git-diff output.
type DiffResult struct {
	Diff      string `json:"diff"`
	Truncated bool   `json:"truncated"`
}

// localWorkspace is the on-disk Workspace rooted at an absolute directory. It
// also satisfies hashline.Filesystem so the patcher writes through the same
// path-safety guards the tools use.
type localWorkspace struct {
	root string
	// maxReadBytes caps a single read/search file load.
	maxReadBytes int
	// maxWriteBytes caps a single write_file/edit write.
	maxWriteBytes int
	// maxFileBytes is a hard ceiling on the size of a single file the read and
	// edit paths will load into memory. Unlike maxReadBytes (which only caps
	// the returned slice), this bounds the underlying os read so a model
	// cannot point read_file/edit_hashline at an arbitrarily large in-workspace
	// blob and exhaust memory.
	maxFileBytes int64
}

// LocalWorkspaceOption configures a local workspace.
type LocalWorkspaceOption func(*localWorkspace)

// WithMaxReadBytes overrides the per-file read/search byte cap.
func WithMaxReadBytes(n int) LocalWorkspaceOption {
	return func(w *localWorkspace) {
		if n > 0 {
			w.maxReadBytes = n
		}
	}
}

// WithMaxWriteBytes overrides the per-file write byte cap.
func WithMaxWriteBytes(n int) LocalWorkspaceOption {
	return func(w *localWorkspace) {
		if n > 0 {
			w.maxWriteBytes = n
		}
	}
}

// WithMaxFileBytes overrides the hard ceiling on the size of a single file the
// read/edit paths will load into memory.
func WithMaxFileBytes(n int64) LocalWorkspaceOption {
	return func(w *localWorkspace) {
		if n > 0 {
			w.maxFileBytes = n
		}
	}
}

// Default byte caps for local workspace I/O.
const (
	defaultMaxReadBytes  = 1 << 20  // 1 MiB
	defaultMaxWriteBytes = 4 << 20  // 4 MiB
	defaultMaxFileBytes  = 16 << 20 // 16 MiB
)

// ErrFileTooLarge is returned by the read/edit paths when a target file
// exceeds the workspace's per-file read ceiling. The full file must be loaded
// to mint a tag and apply an edit, so an oversize file cannot be read; the
// model should leave it alone.
var ErrFileTooLarge = errors.New("coding: file exceeds the maximum readable size")

// ErrNotRegularFile is returned when a read/edit target is not a regular file
// (e.g. a FIFO, device, socket, or directory). Reading such a path could block
// indefinitely, so it is rejected up front.
var ErrNotRegularFile = errors.New("coding: not a regular file")

// NewLocalWorkspace returns a Workspace rooted at root. The returned value
// also satisfies hashline.Filesystem, so coding.NewToolSet can wire it as the
// patcher's disk boundary. The root is resolved to an absolute, symlink-free
// path so containment checks compare like with like.
func NewLocalWorkspace(root string, opts ...LocalWorkspaceOption) Workspace {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	if resolved, rerr := filepath.EvalSymlinks(abs); rerr == nil {
		abs = resolved
	}
	w := &localWorkspace{
		root:          abs,
		maxReadBytes:  defaultMaxReadBytes,
		maxWriteBytes: defaultMaxWriteBytes,
		maxFileBytes:  defaultMaxFileBytes,
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// Root returns the absolute workspace root.
func (w *localWorkspace) Root() string { return w.root }

// readBounded loads a resolved file into memory while rejecting non-regular
// files (FIFOs/devices/sockets/directories, which could block forever) and
// files larger than the per-file ceiling. The io.LimitReader guards against a
// file growing between the size check and the read (TOCTOU), so the in-memory
// copy is hard-bounded regardless. canon is used only for error messages.
func (w *localWorkspace) readBounded(abs, canon string) ([]byte, error) {
	f, err := os.Open(abs)
	if err != nil {
		return nil, fmt.Errorf("coding: read %q: %w", canon, err)
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("coding: read %q: %w", canon, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("coding: read %q: %w", canon, ErrNotRegularFile)
	}
	if info.Size() > w.maxFileBytes {
		return nil, fmt.Errorf("coding: read %q (%d bytes > %d): %w",
			canon, info.Size(), w.maxFileBytes, ErrFileTooLarge)
	}

	data, err := io.ReadAll(io.LimitReader(f, w.maxFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("coding: read %q: %w", canon, err)
	}
	if int64(len(data)) > w.maxFileBytes {
		return nil, fmt.Errorf("coding: read %q (> %d bytes): %w",
			canon, w.maxFileBytes, ErrFileTooLarge)
	}
	return data, nil
}

// resolve validates a workspace-relative path against the root, returning the
// absolute path and the canonical workspace-relative path (slash-separated).
func (w *localWorkspace) resolve(rel string) (abs, canon string, err error) {
	abs, canon, err = workspace.ResolveWorkspacePath(w.root, rel)
	if err != nil {
		return "", "", err
	}
	return abs, filepath.ToSlash(canon), nil
}

// --- Workspace implementation -------------------------------------------------

func (w *localWorkspace) ListFiles(ctx context.Context, req ListFilesRequest) (ListFilesResult, error) {
	res, err := workspace.ListFiles(ctx, w.root, workspace.ListFilesRequest{
		Glob:          req.Glob,
		Ignore:        req.Ignore,
		Limit:         req.Limit,
		MaxFileBytes:  req.MaxFileBytes,
		IncludeBinary: req.IncludeBinary,
	})
	if err != nil {
		return ListFilesResult{}, err
	}
	return ListFilesResult{Files: res.Files, Truncated: res.Truncated}, nil
}

func (w *localWorkspace) ReadFile(ctx context.Context, req ReadFileRequest) (ReadFileResult, error) {
	if err := ctx.Err(); err != nil {
		return ReadFileResult{}, err
	}
	abs, canon, err := w.resolve(req.Path)
	if err != nil {
		return ReadFileResult{}, err
	}
	raw, err := w.readBounded(abs, canon)
	if err != nil {
		return ReadFileResult{}, err
	}
	nf := hashline.Normalize(string(raw))
	tag := hashline.ComputeFileHash(nf.Text)

	lines := splitTextLines(nf.Text)
	lineCount := len(lines)

	start := req.StartLine
	end := req.EndLine
	if start <= 0 {
		start = 1
	}
	if end <= 0 || end > lineCount {
		end = lineCount
	}
	if lineCount == 0 {
		start, end = 0, 0
	}
	var slice string
	if lineCount > 0 && start <= end {
		slice = strings.Join(lines[start-1:end], "\n")
	}

	maxBytes := req.MaxBytes
	if maxBytes <= 0 || maxBytes > w.maxReadBytes {
		maxBytes = w.maxReadBytes
	}
	truncated := false
	if len(slice) > maxBytes {
		slice = slice[:maxBytes]
		truncated = true
	}

	return ReadFileResult{
		Path:      canon,
		Tag:       tag,
		Text:      nf.Text,
		StartLine: start,
		EndLine:   end,
		SliceText: slice,
		LineCount: lineCount,
		Truncated: truncated,
	}, nil
}

func (w *localWorkspace) Search(ctx context.Context, req SearchRequest) (SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return SearchResult{}, err
	}
	var re *regexp.Regexp
	if req.Regexp {
		compiled, err := regexp.Compile(req.Query)
		if err != nil {
			return SearchResult{}, fmt.Errorf("coding: search: invalid regexp: %w", err)
		}
		re = compiled
	}
	maxResults := req.MaxResults
	if maxResults <= 0 {
		maxResults = defaultSearchMaxResults
	}

	listed, err := w.ListFiles(ctx, ListFilesRequest{Glob: req.Glob, Ignore: req.Ignore})
	if err != nil {
		return SearchResult{}, err
	}

	result := SearchResult{}
	total := 0
walk:
	for _, path := range listed.Files {
		if cerr := ctx.Err(); cerr != nil {
			return SearchResult{}, cerr
		}
		read, rerr := w.ReadFile(ctx, ReadFileRequest{Path: path})
		if rerr != nil {
			continue
		}
		var matches []SearchMatch
		for i, line := range splitTextLines(read.Text) {
			matched := false
			if re != nil {
				matched = re.MatchString(line)
			} else {
				matched = strings.Contains(line, req.Query)
			}
			if !matched {
				continue
			}
			matches = append(matches, SearchMatch{LineNumber: i + 1, Line: line})
			total++
			if total >= maxResults {
				result.Files = append(result.Files, SearchFileResult{Path: read.Path, Tag: read.Tag, Matches: matches})
				result.Truncated = true
				break walk
			}
		}
		if len(matches) > 0 {
			result.Files = append(result.Files, SearchFileResult{Path: read.Path, Tag: read.Tag, Matches: matches})
		}
	}
	if listed.Truncated {
		result.Truncated = true
	}
	return result, nil
}

func (w *localWorkspace) WriteFile(ctx context.Context, req WriteFileRequest) (WriteFileResult, error) {
	if err := ctx.Err(); err != nil {
		return WriteFileResult{}, err
	}
	abs, canon, err := w.resolve(req.Path)
	if err != nil {
		return WriteFileResult{}, err
	}
	maxBytes := req.MaxBytes
	if maxBytes <= 0 || maxBytes > w.maxWriteBytes {
		maxBytes = w.maxWriteBytes
	}
	if len(req.Content) > maxBytes {
		return WriteFileResult{}, fmt.Errorf("coding: write %q: content exceeds %d bytes", canon, maxBytes)
	}
	if _, statErr := os.Lstat(abs); statErr == nil {
		return WriteFileResult{}, fmt.Errorf("%w: %q already exists; use edit_hashline", ErrFileExists, canon)
	} else if !os.IsNotExist(statErr) {
		return WriteFileResult{}, fmt.Errorf("coding: stat %q: %w", canon, statErr)
	}
	if mkErr := os.MkdirAll(filepath.Dir(abs), 0o755); mkErr != nil {
		return WriteFileResult{}, fmt.Errorf("coding: mkdir for %q: %w", canon, mkErr)
	}
	if wErr := os.WriteFile(abs, []byte(req.Content), 0o644); wErr != nil {
		return WriteFileResult{}, fmt.Errorf("coding: write %q: %w", canon, wErr)
	}
	tag := hashline.ComputeFileHash(hashline.Normalize(req.Content).Text)
	return WriteFileResult{Path: canon, Tag: tag}, nil
}

func (w *localWorkspace) RunCommand(ctx context.Context, req RunCommandRequest) (RunCommandResult, error) {
	res, err := workspace.RunCommand(ctx, workspace.RunCommandRequest{
		Args:           req.Args,
		WorkingDir:     w.root,
		Timeout:        req.Timeout,
		MaxOutputBytes: req.MaxOutputBytes,
		Env:            req.Env,
	})
	if err != nil {
		return RunCommandResult{}, err
	}
	return RunCommandResult{
		Args:      res.Args,
		ExitCode:  res.ExitCode,
		Stdout:    res.Stdout,
		Stderr:    res.Stderr,
		Truncated: res.Truncated,
		TimedOut:  res.TimedOut,
		Duration:  res.Duration,
	}, nil
}

func (w *localWorkspace) Diff(ctx context.Context, req DiffRequest) (DiffResult, error) {
	args := []string{"git", "diff", "--"}
	if len(req.Paths) == 0 {
		args = []string{"git", "diff", "--", "."}
	} else {
		// Validate each path against the sandbox before handing it to git.
		for _, p := range req.Paths {
			if _, _, err := w.resolve(p); err != nil {
				return DiffResult{}, err
			}
		}
		args = append(args, req.Paths...)
	}
	res, err := w.RunCommand(ctx, RunCommandRequest{
		Args:           args,
		Timeout:        req.Timeout,
		MaxOutputBytes: req.MaxOutputBytes,
	})
	if err != nil {
		return DiffResult{}, err
	}
	return DiffResult{Diff: res.Stdout, Truncated: res.Truncated}, nil
}

// --- hashline.Filesystem implementation ---------------------------------------

// CanonicalPath validates a workspace-relative path and returns the canonical
// (slash-separated) workspace-relative form used in result headers.
func (w *localWorkspace) CanonicalPath(path string) (string, error) {
	_, canon, err := w.resolve(path)
	return canon, err
}

// ReadText reads the full raw bytes of a workspace-relative file.
func (w *localWorkspace) ReadText(ctx context.Context, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	abs, canon, err := w.resolve(path)
	if err != nil {
		return "", err
	}
	raw, err := w.readBounded(abs, canon)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// PreflightWrite confirms a write to path can proceed: the path is in-bounds
// and its parent directory can be created.
func (w *localWorkspace) PreflightWrite(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	abs, canon, err := w.resolve(path)
	if err != nil {
		return err
	}
	if mkErr := os.MkdirAll(filepath.Dir(abs), 0o755); mkErr != nil {
		return fmt.Errorf("coding: preflight %q: %w", canon, mkErr)
	}
	return nil
}

// WriteText writes the full content of a workspace-relative file, preserving
// the existing file mode when the file already exists.
func (w *localWorkspace) WriteText(ctx context.Context, path, text string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	abs, canon, err := w.resolve(path)
	if err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if fi, statErr := os.Stat(abs); statErr == nil {
		mode = fi.Mode().Perm()
	}
	if wErr := os.WriteFile(abs, []byte(text), mode); wErr != nil {
		return fmt.Errorf("coding: write %q: %w", canon, wErr)
	}
	return nil
}

// ErrFileExists is returned by WriteFile when the target file already exists;
// callers must use edit_hashline to modify an existing file.
var ErrFileExists = errors.New("coding: file already exists")

// splitTextLines splits LF-internal text into lines. Empty text yields no
// lines (a zero-line file). A trailing newline yields a final empty element,
// matching the read_file numbered-line format.
func splitTextLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

// compile-time assertions that the local workspace satisfies both contracts.
var (
	_ Workspace           = (*localWorkspace)(nil)
	_ hashline.Filesystem = (*localWorkspace)(nil)
)
