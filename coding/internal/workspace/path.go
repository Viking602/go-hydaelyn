// Package workspace provides low-level sandbox helpers for the coding
// capability: workspace-relative path resolution, an argv command allowlist
// and runner, and a bounded file-listing walker. These helpers operate on the
// real filesystem and on argv slices only; they never import the hashline
// protocol package and never invoke a shell.
package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Path resolution errors. Callers should match these with errors.Is.
var (
	// ErrEmptyPath is returned when the requested relative path is empty.
	ErrEmptyPath = errors.New("workspace: empty path")
	// ErrAbsolutePath is returned when the requested path is absolute.
	ErrAbsolutePath = errors.New("workspace: absolute path not allowed")
	// ErrPathEscape is returned when the requested path resolves outside the
	// workspace root (via "..", an absolute join, or a symlinked ancestor).
	ErrPathEscape = errors.New("workspace: path escapes workspace root")
	// ErrNULByte is returned when the requested path contains a NUL byte.
	ErrNULByte = errors.New("workspace: path contains NUL byte")
	// ErrDeniedPath is returned when the requested path targets a denied
	// location (the .git directory by default).
	ErrDeniedPath = errors.New("workspace: path is denied")
)

// ResolveWorkspacePath validates rel against root and returns the absolute path
// and the cleaned workspace-relative path.
//
// It rejects empty, absolute, NUL-bearing, and ".."-escaping inputs, denies the
// .git tree by default, and guards against symlink escape by resolving the
// deepest existing ancestor of the target (the parent directory for a
// not-yet-created file) and confirming it stays within the resolved root.
func ResolveWorkspacePath(root, rel string) (abs string, canonicalRel string, err error) {
	if rel == "" {
		return "", "", ErrEmptyPath
	}
	if strings.IndexByte(rel, 0) >= 0 {
		return "", "", ErrNULByte
	}

	clean := filepath.Clean(rel)
	if filepath.IsAbs(clean) {
		return "", "", ErrAbsolutePath
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", "", ErrPathEscape
	}
	if clean == "." {
		return "", "", ErrEmptyPath
	}

	if isDeniedRel(clean) {
		return "", "", ErrDeniedPath
	}

	// Build abs from the absolute root so the lexical containment check below
	// compares like with like even when the caller passes a relative root.
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", "", fmt.Errorf("workspace: resolve root: %w", err)
	}
	abs = filepath.Join(rootAbs, clean)

	// Lexical containment check: defends against join surprises before we ever
	// touch the filesystem.
	if !withinRoot(rootAbs, abs) {
		return "", "", ErrPathEscape
	}

	// Symlink containment check: resolve the deepest existing ancestor of abs.
	// For a not-yet-created file the parent directory is the relevant ancestor,
	// so a symlinked parent that points outside the workspace is rejected.
	resolvedRoot, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", "", fmt.Errorf("workspace: resolve root symlinks: %w", err)
	}
	resolvedAncestor, err := evalDeepestExistingAncestor(abs)
	if err != nil {
		return "", "", err
	}
	if !withinRoot(resolvedRoot, resolvedAncestor) {
		return "", "", ErrPathEscape
	}

	// Denied-tree symlink check. The lexical isDeniedRel guard above only catches
	// a path whose cleaned form is ".git" or starts with ".git/". A symlink alias
	// such as "g -> .git" has a benign cleaned path ("g/config") yet resolves
	// *inside* the .git tree, which is in-bounds and so survives the containment
	// check above — letting read/write tools reach the denied tree through the
	// link. Reject when the canonical target lands inside the canonical .git
	// directory so the deny boundary cannot be bypassed by aliasing.
	denied, err := resolvesIntoDeniedTree(resolvedRoot, resolvedAncestor)
	if err != nil {
		return "", "", err
	}
	if denied {
		return "", "", ErrDeniedPath
	}

	return abs, clean, nil
}

// resolvesIntoDeniedTree reports whether the canonical target lands inside the
// workspace's denied .git tree. isDeniedRel only inspects the lexical path, so a
// symlink alias resolves into .git undetected; this checks the canonical
// location. The .git entry is itself canonicalized (it may be a symlink) before
// the containment test, and a workspace with no .git entry (a dangling or
// missing link) has nothing in-bounds to alias into.
func resolvesIntoDeniedTree(resolvedRoot, resolvedTarget string) (bool, error) {
	resolvedGit, err := filepath.EvalSymlinks(filepath.Join(resolvedRoot, ".git"))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("workspace: resolve .git: %w", err)
	}
	return withinRoot(resolvedGit, resolvedTarget), nil
}

// isDeniedRel reports whether a cleaned workspace-relative path targets a denied
// location. The .git tree is denied by default.
func isDeniedRel(clean string) bool {
	if clean == ".git" {
		return true
	}
	return strings.HasPrefix(clean, ".git"+string(filepath.Separator))
}

// withinRoot reports whether target is root or lies beneath it, comparing whole
// path segments so that a sibling like "/root-evil" is not treated as inside
// "/root".
func withinRoot(root, target string) bool {
	if root == target {
		return true
	}
	withSep := root
	if !strings.HasSuffix(withSep, string(filepath.Separator)) {
		withSep += string(filepath.Separator)
	}
	return strings.HasPrefix(target, withSep)
}

// evalDeepestExistingAncestor walks up from abs until it finds an existing path,
// then resolves symlinks on it. If abs itself exists its own resolution is
// returned; otherwise the resolved deepest existing ancestor is returned with
// the remaining (non-existent) suffix re-appended so containment can be checked
// against the real on-disk location.
//
// Existence is probed with os.Lstat rather than filepath.EvalSymlinks so that a
// *dangling* symlink (one whose target does not exist) counts as an existing
// component: a leaf or ancestor symlink that points outside the workspace would
// otherwise be skipped over and the path mistakenly judged in-bounds, allowing a
// later write to follow the link out of the sandbox.
func evalDeepestExistingAncestor(abs string) (string, error) {
	cur := abs
	var trailing []string
	for {
		fi, err := os.Lstat(cur)
		if err == nil {
			resolved, rErr := resolveExistingComponent(cur, fi)
			if rErr != nil {
				return "", rErr
			}
			// Re-attach any non-existent suffix to the resolved ancestor.
			parts := append([]string{resolved}, reverse(trailing)...)
			return filepath.Join(parts...), nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("workspace: resolve symlinks: %w", err)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			// Reached the filesystem root without finding an existing path.
			return abs, nil
		}
		trailing = append(trailing, filepath.Base(cur))
		cur = parent
	}
}

// resolveExistingComponent resolves cur, which is known to exist (per fi), to a
// canonical location for the containment check. A non-symlink resolves via
// filepath.EvalSymlinks (which also canonicalizes any symlinked ancestors). A
// *dangling* symlink — one whose target does not exist, so EvalSymlinks would
// report NotExist — is resolved one hop with os.Readlink, joined against the
// canonical parent directory, so its outside-pointing target is still subject to
// the workspace-containment check.
func resolveExistingComponent(cur string, fi os.FileInfo) (string, error) {
	resolved, err := filepath.EvalSymlinks(cur)
	if err == nil {
		return resolved, nil
	}
	if !os.IsNotExist(err) {
		return "", fmt.Errorf("workspace: resolve symlinks: %w", err)
	}
	// EvalSymlinks failed with NotExist even though cur exists: cur must be a
	// dangling symlink. Resolve its parent canonically, then one link hop.
	if fi.Mode()&os.ModeSymlink == 0 {
		// Not a symlink but EvalSymlinks reported NotExist: surface the error
		// rather than silently treating the path as in-bounds.
		return "", fmt.Errorf("workspace: resolve symlinks: %w", err)
	}
	target, readErr := os.Readlink(cur)
	if readErr != nil {
		return "", fmt.Errorf("workspace: read symlink: %w", readErr)
	}
	if !filepath.IsAbs(target) {
		parentResolved, parentErr := filepath.EvalSymlinks(filepath.Dir(cur))
		if parentErr != nil {
			return "", fmt.Errorf("workspace: resolve symlink parent: %w", parentErr)
		}
		target = filepath.Join(parentResolved, target)
	}
	// The target may itself be missing or have symlinked ancestors. Resolve it
	// the same way as the original path so the containment check compares a fully
	// canonicalized location (e.g. /var vs /private/var on macOS) rather than a
	// raw target string.
	return evalDeepestExistingAncestor(filepath.Clean(target))
}

// reverse returns a reversed copy of in.
func reverse(in []string) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[len(in)-1-i] = v
	}
	return out
}
