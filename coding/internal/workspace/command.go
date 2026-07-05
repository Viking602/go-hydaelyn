package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Command execution defaults.
const (
	// DefaultCommandTimeout caps how long a single command may run.
	DefaultCommandTimeout = 30 * time.Second
	// DefaultMaxOutputBytes caps captured stdout/stderr per stream.
	DefaultMaxOutputBytes = 64 << 10 // 64 KiB

	// truncationMarker is appended to a captured stream that hit the cap.
	truncationMarker = "\n[truncated]"

	// gitFSMonitorOff is the sole `-c key=value` override git_diff may pass. It
	// disables any repo-configured filesystem-monitor hook, which git would
	// otherwise execute while refreshing the index for a read-only diff/status.
	gitFSMonitorOff = "core.fsmonitor=false"
)

// Command validation errors.
var (
	// ErrEmptyCommand is returned when no argv is supplied.
	ErrEmptyCommand = errors.New("workspace: empty command")
	// ErrCommandNotAllowed is returned when argv does not match the allowlist.
	ErrCommandNotAllowed = errors.New("workspace: command not allowed")
)

// RunCommandRequest describes a single sandboxed command invocation. Args is the
// explicit argv (no shell); the first element is the program name.
type RunCommandRequest struct {
	Args           []string
	WorkingDir     string
	Timeout        time.Duration     // default DefaultCommandTimeout
	MaxOutputBytes int               // default DefaultMaxOutputBytes
	Env            map[string]string // extra environment, scrubbed onto a clean base
}

// RunCommandResult is the typed outcome of a sandboxed command invocation.
type RunCommandResult struct {
	Args      []string `json:"args"`
	ExitCode  int      `json:"exitCode"`
	Stdout    string   `json:"stdout"`
	Stderr    string   `json:"stderr"`
	Truncated bool     `json:"truncated"`
	TimedOut  bool     `json:"timedOut"`
	Duration  string   `json:"duration"`
}

// ValidateCommand reports whether args matches the coding-capability allowlist.
//
// Allowed forms (matched on argv, never via a shell):
//
//	go test ./...
//	go test ./<pkg>...            (any single ./-prefixed package pattern)
//	go test ./<pkg> -run <Name>
//	go vet ./...
//	git -c core.fsmonitor=false diff --no-ext-diff --no-textconv -- <paths...>
//	git -c core.fsmonitor=false status --short -- .
//
// The `-c core.fsmonitor=false` override is mandatory before any git subcommand,
// and `git diff` additionally requires both `--no-ext-diff` and `--no-textconv`.
// Both read-only forms must be scoped to the workspace subtree: `status` requires
// the trailing `-- .` pathspec and `diff` requires at least one workspace-relative
// pathspec, so neither can enumerate a parent repository when the workspace root
// is a repo subdirectory (see validateGit). Everything else is rejected, including
// shell wrappers, network tools, and destructive or history-mutating git verbs.
func ValidateCommand(args []string) error {
	if len(args) == 0 {
		return ErrEmptyCommand
	}
	switch args[0] {
	case "go":
		return validateGo(args)
	case "git":
		return validateGit(args)
	default:
		return fmt.Errorf("%w: %q", ErrCommandNotAllowed, args[0])
	}
}

func validateGo(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("%w: bare go", ErrCommandNotAllowed)
	}
	switch args[1] {
	case "vet":
		// go vet ./...
		if len(args) == 3 && args[2] == "./..." {
			return nil
		}
		return fmt.Errorf("%w: %v", ErrCommandNotAllowed, args)
	case "test":
		return validateGoTest(args)
	default:
		return fmt.Errorf("%w: %v", ErrCommandNotAllowed, args)
	}
}

func validateGoTest(args []string) error {
	rest := args[2:]
	switch len(rest) {
	case 1:
		// go test ./...  or  go test ./<pkg>...
		if isPackagePattern(rest[0]) {
			return nil
		}
	case 3:
		// go test ./<pkg> -run <Name>
		if isPackagePattern(rest[0]) && rest[1] == "-run" && rest[2] != "" {
			return nil
		}
	}
	return fmt.Errorf("%w: %v", ErrCommandNotAllowed, args)
}

func validateGit(args []string) error {
	rest := args[1:]
	// Mandatory global config override: the exact leading pair `-c
	// core.fsmonitor=false` is REQUIRED before the subcommand. A repo-configured
	// filesystem monitor (core.fsmonitor pointing at a program/hook) is executed
	// by git while it refreshes the index for BOTH `git diff` and `git status`,
	// so a hostile .git/config could turn either read-only path into command
	// execution. Requiring the override (rather than merely accepting it) means no
	// accepted git invocation can run the monitor hook, closing the vector for
	// every caller of the allowlist — not just the git_diff driver, which already
	// supplies it. No other `-c key=value` is allowed.
	if len(rest) < 2 || rest[0] != "-c" || rest[1] != gitFSMonitorOff {
		return fmt.Errorf("%w: git requires the leading %q override", ErrCommandNotAllowed, "-c "+gitFSMonitorOff)
	}
	rest = rest[2:]
	if len(rest) == 0 {
		return fmt.Errorf("%w: bare git", ErrCommandNotAllowed)
	}
	switch rest[0] {
	case "status":
		return validateGitStatus(rest, args)
	case "diff":
		return validateGitDiff(rest, args)
	}
	return fmt.Errorf("%w: %v", ErrCommandNotAllowed, args)
}

// validateGitStatus accepts only `status --short -- .`. The "-- ." pathspec
// scopes the report to the workspace subtree: without a pathspec git status
// reports the ENTIRE containing repository, so when the workspace root is a
// subdirectory of a larger repo a bare `status` would enumerate paths outside the
// sandbox. The pathspec resolves relative to the run's working directory, which is
// always the workspace root. args is the full argv, for the error message.
func validateGitStatus(rest, args []string) error {
	if len(rest) == 4 && rest[1] == "--short" && rest[2] == "--" && rest[3] == "." {
		return nil
	}
	return fmt.Errorf("%w: %v", ErrCommandNotAllowed, args)
}

// validateGitDiff accepts `diff --no-ext-diff --no-textconv -- <paths...>`. Both
// hardening flags are MANDATORY (not optional): --no-ext-diff disables a
// repo-configured diff.external driver and --no-textconv disables textconv filters
// (which git diff enables by default), either of which a hostile .git/config or
// .gitattributes could otherwise use to execute a helper while the read-only diff
// refreshes the index and renders output. Requiring them — like the fsmonitor
// override — means no accepted diff form can run an external helper, for every
// caller of the allowlist, not just the git_diff driver (which already passes
// both). The flags may appear in either order before the "--" separator; each is
// accepted at most once. rest is the argv after the fsmonitor override; args is
// the full argv, for the error message.
func validateGitDiff(rest, args []string) error {
	var sawExtDiff, sawTextconv bool
	i := 1
	for i < len(rest) && isAllowedDiffFlag(rest[i]) {
		switch rest[i] {
		case "--no-ext-diff":
			if sawExtDiff {
				return fmt.Errorf("%w: duplicate %q", ErrCommandNotAllowed, rest[i])
			}
			sawExtDiff = true
		case "--no-textconv":
			if sawTextconv {
				return fmt.Errorf("%w: duplicate %q", ErrCommandNotAllowed, rest[i])
			}
			sawTextconv = true
		}
		i++
	}
	if !sawExtDiff || !sawTextconv || i >= len(rest) || rest[i] != "--" {
		return fmt.Errorf("%w: %v", ErrCommandNotAllowed, args)
	}
	pathspecs := rest[i+1:]
	// A diff with NO pathspec covers the whole repository — when the workspace root
	// is a subdirectory of a larger repo that leaks changes outside the sandbox —
	// so require at least one workspace-scoped pathspec (the git_diff driver always
	// supplies "." or resolved paths). Each is screened lexically here; the
	// symlink-aware check is applied by localWorkspace.guardCommandGitPaths.
	if len(pathspecs) == 0 {
		return fmt.Errorf("%w: git diff requires a workspace pathspec after --", ErrCommandNotAllowed)
	}
	for _, p := range pathspecs {
		if err := validateGitPathspec(p); err != nil {
			return err
		}
	}
	return nil
}

// validateGitPathspec lexically screens a single `git diff` pathspec so it cannot
// reach outside the workspace. "." (the workspace root) is always allowed. The
// filesystem-aware symlink check is applied separately by
// localWorkspace.guardCommandGitPaths, since resolving a symlinked pathspec needs
// the real filesystem and the workspace root.
func validateGitPathspec(p string) error {
	if p == "." {
		return nil
	}
	if p == "" {
		return fmt.Errorf("%w: empty git pathspec", ErrCommandNotAllowed)
	}
	// Git pathspec "magic" (":/", ":(top)", ":!", ...) can re-anchor a path at the
	// repository root, above the workspace, so reject any leading colon.
	if strings.HasPrefix(p, ":") {
		return fmt.Errorf("%w: git pathspec magic %q not allowed", ErrCommandNotAllowed, p)
	}
	// Reject absolute pathspecs (OS-absolute or a leading slash on any platform,
	// since git accepts forward slashes everywhere).
	if filepath.IsAbs(p) || strings.HasPrefix(p, "/") {
		return fmt.Errorf("%w: absolute git pathspec %q not allowed", ErrCommandNotAllowed, p)
	}
	if containsParentTraversal(p) {
		return fmt.Errorf("%w: git pathspec %q escapes the workspace", ErrCommandNotAllowed, p)
	}
	return nil
}

// isAllowedDiffFlag reports whether s is one of the hardening flags git_diff must
// pass before the "--" path separator. Only flags that disable external-helper
// execution are permitted — no value-taking or behavior-broadening flags. Both
// flags are required (see validateGit); this only screens which flag tokens may
// appear in the pre-separator run.
func isAllowedDiffFlag(s string) bool {
	switch s {
	case "--no-ext-diff", "--no-textconv":
		return true
	default:
		return false
	}
}

// isPackagePattern reports whether s is an accepted Go package pattern: the
// recursive root "./..." or any single "./"-prefixed import path. Patterns
// containing a ".." traversal segment are rejected so the agent cannot run (and
// thereby compile and execute) test code in sibling or ancestor directories
// outside the sandbox — "go test ./../foo" would otherwise reach packages above
// the working directory.
//
// This screen is purely lexical: it cannot catch a directory symlink inside the
// workspace that points outward (e.g. "./link"), because resolving that needs
// the filesystem and the root. The symlink-containment boundary is enforced one
// layer up by localWorkspace.guardCommandPackagePath, which resolves the package
// directory through ResolveWorkspacePath before the command runs.
func isPackagePattern(s string) bool {
	if s == "./..." {
		return true
	}
	if len(s) < 2 || s[0] != '.' || s[1] != '/' {
		return false
	}
	if containsParentTraversal(s) {
		return false
	}
	return true
}

// containsParentTraversal reports whether the slash-separated pattern contains a
// ".." path segment (e.g. "./..", "./a/../../b", "./.../.."). Trailing "..."
// recursive markers are not traversals and are left intact.
func containsParentTraversal(s string) bool {
	for _, seg := range strings.Split(s, "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

// RunCommand validates req.Args against the allowlist and runs the command with
// exec.CommandContext using the explicit argv (no shell). Output is captured per
// stream up to the byte cap with a truncation marker, the process is bounded by
// the timeout, and the environment is scrubbed to a minimal base before req.Env
// is applied.
func RunCommand(ctx context.Context, req RunCommandRequest) (RunCommandResult, error) {
	if err := ValidateCommand(req.Args); err != nil {
		return RunCommandResult{}, err
	}
	// The execution directory is the security boundary alongside the argv
	// allowlist: an empty Dir would run the command in the process CWD, which
	// may be outside the sandbox. Require the caller (always localWorkspace,
	// which passes the workspace root) to set it explicitly.
	if req.WorkingDir == "" {
		return RunCommandResult{}, errors.New("workspace: run command: working directory must be set")
	}
	// Only allowlisted locator variables may be supplied via req.Env; anything
	// else (e.g. GOFLAGS/GODEBUG) could shape toolchain execution, so reject it
	// loudly rather than letting a caller reintroduce an execution-shaping var.
	for k := range req.Env {
		if !isAllowedEnvKey(k) {
			return RunCommandResult{}, fmt.Errorf(
				"workspace: run command: environment key %q is not permitted", k)
		}
	}

	timeout := req.Timeout
	if timeout <= 0 {
		timeout = DefaultCommandTimeout
	}
	maxOut := req.MaxOutputBytes
	if maxOut <= 0 {
		maxOut = DefaultMaxOutputBytes
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, req.Args[0], req.Args[1:]...)
	cmd.Dir = req.WorkingDir
	cmd.Env = scrubEnv(req.Env)

	var stdout, stderr capWriter
	stdout.limit = maxOut
	stderr.limit = maxOut
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	runErr := cmd.Run()
	elapsed := time.Since(start)

	result := RunCommandResult{
		Args:      append([]string(nil), req.Args...),
		Stdout:    stdout.String(),
		Stderr:    stderr.String(),
		Truncated: stdout.truncated || stderr.truncated,
		Duration:  elapsed.String(),
	}

	if runCtx.Err() == context.DeadlineExceeded {
		result.TimedOut = true
		result.ExitCode = -1
		return result, nil
	}

	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
			return result, nil
		}
		// Failure to start (binary missing, etc.) is surfaced as an error.
		return result, fmt.Errorf("workspace: run command: %w", runErr)
	}

	result.ExitCode = 0
	return result, nil
}

// gitProbeMaxOutput caps the NUL-separated output of the ls-files/check-attr
// probes below. A diff/status whose tracked file set is larger than this is
// refused rather than vetted partially (fail closed).
const gitProbeMaxOutput = 16 << 20 // 16 MiB

// CheckGitDiffFilters returns the tracked workspace paths under pathspecs that
// carry a `filter` gitattribute, without ever executing that filter.
//
// git diff and git status both normalize worktree files — converting each
// through any configured clean/process filter (filter.<driver>.clean or
// .process) — to compute their output, and --no-ext-diff/--no-textconv do NOT
// disable those filters. So a repo whose .gitattributes or $GIT_DIR/info/
// attributes assigns `filter=<driver>` to a path, with a .git/config defining
// that driver's command, would otherwise turn a read-only, approval-free diff or
// status into execution of the driver. `git check-attr` reports the attribute
// from every source (worktree, info/attributes, config) but never runs the
// filter, and `git ls-files` enumerates the tracked set without running it
// either — together they let the caller refuse the command before any filter
// executes. root must be the workspace root; pathspecs are the already-validated
// diff/status pathspecs (".", or workspace-relative files). A non-nil error
// means the probe itself failed (or overflowed its output cap) and the caller
// must refuse the command.
func CheckGitDiffFilters(ctx context.Context, root string, pathspecs []string) ([]string, error) {
	if root == "" {
		return nil, errors.New("workspace: check git filters: working directory must be set")
	}
	lsArgs := append([]string{"-c", gitFSMonitorOff, "ls-files", "-z", "--"}, pathspecs...)
	lsOut, err := runTrustedGit(ctx, root, nil, lsArgs)
	if err != nil {
		return nil, fmt.Errorf("workspace: list tracked files: %w", err)
	}
	if len(lsOut) == 0 {
		// Nothing tracked under the pathspecs, so git converts (and filters)
		// nothing — the command is safe.
		return nil, nil
	}
	caOut, err := runTrustedGit(ctx, root, lsOut,
		[]string{"-c", gitFSMonitorOff, "check-attr", "filter", "-z", "--stdin"})
	if err != nil {
		return nil, fmt.Errorf("workspace: check filter attributes: %w", err)
	}
	return filteredPathsFromCheckAttr(caOut), nil
}

// runTrustedGit runs a framework-controlled git invocation that is NOT subject
// to the argv allowlist — args is built by the framework (CheckGitDiffFilters),
// not the model — but carries the same hardening RunCommand applies: the working
// directory is pinned to root, the environment is scrubbed to a minimal base, a
// timeout bounds the process, and stdout is captured into a bounded buffer.
// stdin, when non-nil, is streamed to the process. It returns the raw stdout
// bytes, or an error if the process fails to start, exits non-zero, times out,
// or overflows the output cap.
func runTrustedGit(ctx context.Context, root string, stdin []byte, args []string) ([]byte, error) {
	runCtx, cancel := context.WithTimeout(ctx, DefaultCommandTimeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "git", args...)
	cmd.Dir = root
	cmd.Env = scrubEnv(nil)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr capWriter
	stdout.limit = gitProbeMaxOutput
	stderr.limit = DefaultMaxOutputBytes
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if runCtx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("git probe timed out after %s", DefaultCommandTimeout)
		}
		return nil, fmt.Errorf("git probe %v: %w: %s", args, err, strings.TrimSpace(stderr.buf.String()))
	}
	if stdout.truncated {
		return nil, fmt.Errorf("git probe output exceeded %d bytes", gitProbeMaxOutput)
	}
	return stdout.buf.Bytes(), nil
}

// filteredPathsFromCheckAttr parses `git check-attr filter -z` output — a stream
// of NUL-separated (path, "filter", value) triplets — and returns the paths
// whose filter value names an actual driver, i.e. anything other than
// "unspecified" or "unset" (a clean/process filter git would run).
func filteredPathsFromCheckAttr(out []byte) []string {
	fields := splitNUL(out)
	var filtered []string
	for i := 0; i+2 < len(fields); i += 3 {
		if value := fields[i+2]; value != "unspecified" && value != "unset" {
			filtered = append(filtered, fields[i])
		}
	}
	return filtered
}

// splitNUL splits a NUL-separated byte stream into its non-empty string fields.
func splitNUL(b []byte) []string {
	parts := bytes.Split(b, []byte{0})
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if len(p) > 0 {
			out = append(out, string(p))
		}
	}
	return out
}

// scrubEnv builds a minimal environment for a sandboxed command. It does not
// pass through the parent process environment (no tokens or secrets); only a
// short whitelist needed by the Go toolchain plus the caller-supplied req.Env
// are included.
func scrubEnv(extra map[string]string) []string {
	base := map[string]string{}
	for _, key := range passthroughEnvKeys {
		if v, ok := os.LookupEnv(key); ok && v != "" {
			base[key] = v
		}
	}
	for k, v := range extra {
		base[k] = v
	}
	if base["GOCACHE"] == "" {
		base["GOCACHE"] = filepath.Join(os.TempDir(), "hydaelyn-go-build-cache")
	}
	// Pin GOENV=off so the Go toolchain ignores the per-user `go env` config file
	// ($HOME/.config/go/env, or the OS equivalent under the passed-through HOME).
	// That file can carry a GOFLAGS entry (e.g. a stale `go env -w
	// GOFLAGS=-toolexec=...`, or a poisoned HOME) which the sandboxed go test/go
	// vet would otherwise honor, defeating the GOFLAGS exclusion above. Set last
	// so it is authoritative over any base/extra value.
	base["GOENV"] = "off"
	out := make([]string, 0, len(base))
	for k, v := range base {
		out = append(out, k+"="+v)
	}
	return out
}

// passthroughEnvKeys is the small set of locator variables the Go toolchain and
// VCS need to function. Anything resembling a credential is deliberately
// excluded, and so is GOFLAGS: it is honored by the toolchain and can carry
// execution-shaping flags (-exec=, -toolexec=, -gcflags, -ldflags=-extldflags)
// that make a build/test spawn an arbitrary helper binary. Keeping it out of
// the allowlist means a poisoned host GOFLAGS cannot extend the read-only
// go_test/go vet tools into running attacker-chosen code.
var passthroughEnvKeys = []string{
	"HOME",
	"PATH",
	"GOPATH",
	"GOROOT",
	"GOCACHE",
	"GOMODCACHE",
	"GOPROXY",
	"GONOSUMCHECK",
	"GONOSUMDB",
	"GONOSUMVERIFY",
	"TMPDIR",
	"TMP",
	"TEMP",
}

// isAllowedEnvKey reports whether a caller-supplied environment key may be
// layered onto the scrubbed base. It is the passthrough allowlist: callers may
// override a locator the toolchain needs, but cannot inject arbitrary
// (execution-shaping) variables.
func isAllowedEnvKey(key string) bool {
	for _, allowed := range passthroughEnvKeys {
		if key == allowed {
			return true
		}
	}
	return false
}

// capWriter accumulates bytes up to a limit, then drops the rest and records
// that truncation occurred. A truncation marker is appended once on read.
type capWriter struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (w *capWriter) Write(p []byte) (int, error) {
	if w.truncated {
		// Pretend the write succeeded so the process keeps running; we just
		// discard the overflow.
		return len(p), nil
	}
	remaining := w.limit - w.buf.Len()
	if remaining <= 0 {
		w.truncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		w.buf.Write(p[:remaining])
		w.truncated = true
		return len(p), nil
	}
	w.buf.Write(p)
	return len(p), nil
}

func (w *capWriter) String() string {
	if w.truncated {
		return w.buf.String() + truncationMarker
	}
	return w.buf.String()
}
