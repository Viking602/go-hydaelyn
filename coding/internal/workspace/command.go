package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
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
//	git [-c core.fsmonitor=false] diff [--no-ext-diff] [--no-textconv] -- <paths...>
//	git [-c core.fsmonitor=false] status --short
//
// Everything else is rejected, including shell wrappers, network tools, and
// destructive or history-mutating git verbs.
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
	// Optional global config override, accepted only as the exact leading pair
	// `-c core.fsmonitor=false` before the subcommand. A repo-configured
	// filesystem monitor (core.fsmonitor pointing at a program/hook) is executed
	// by git while it refreshes the index for a read-only `git diff`/`git status`,
	// so a hostile .git/config could turn that path into command execution.
	// Pinning it off neutralizes that vector. No other `-c key=value` is allowed —
	// only this specific, execution-disabling override.
	if len(rest) >= 2 && rest[0] == "-c" && rest[1] == gitFSMonitorOff {
		rest = rest[2:]
	}
	if len(rest) == 0 {
		return fmt.Errorf("%w: bare git", ErrCommandNotAllowed)
	}
	switch rest[0] {
	case "status":
		// git status --short
		if len(rest) == 2 && rest[1] == "--short" {
			return nil
		}
	case "diff":
		// git diff [--no-ext-diff] [--no-textconv] -- <paths...>
		// The hardening flags disable any repo-configured external diff/textconv
		// helper, so a hostile .git/config or .gitattributes cannot turn the
		// read-only git_diff tool into arbitrary command execution. They are
		// optional and may appear in any order before the "--" separator.
		i := 1
		for i < len(rest) && isAllowedDiffFlag(rest[i]) {
			i++
		}
		if i < len(rest) && rest[i] == "--" {
			return nil
		}
	}
	return fmt.Errorf("%w: %v", ErrCommandNotAllowed, args)
}

// isAllowedDiffFlag reports whether s is one of the hardening flags git_diff may
// pass before the "--" path separator. Only flags that disable external-helper
// execution are permitted — no value-taking or behavior-broadening flags.
func isAllowedDiffFlag(s string) bool {
	switch s {
	case "--no-ext-diff", "--no-textconv":
		return true
	default:
		return false
	}
}

// isPackagePattern reports whether s is an accepted Go package pattern: the
// recursive root "./..." or any single "./"-prefixed import path that stays
// inside the workspace. Patterns containing a ".." traversal segment are
// rejected so the agent cannot run (and thereby compile and execute) test code
// in sibling or ancestor directories outside the sandbox — "go test ./../foo"
// would otherwise reach packages above the working directory.
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

// scrubEnv builds a minimal environment for a sandboxed command. It does not
// pass through the parent process environment (no tokens or secrets); only a
// short whitelist needed by the Go toolchain plus the caller-supplied req.Env
// are included.
func scrubEnv(extra map[string]string) []string {
	base := map[string]string{}
	for _, key := range passthroughEnvKeys {
		if v, ok := os.LookupEnv(key); ok {
			base[key] = v
		}
	}
	for k, v := range extra {
		base[k] = v
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
