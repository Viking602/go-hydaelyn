package workspace

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestFilteredPathsFromCheckAttr(t *testing.T) {
	// `git check-attr filter -z` emits NUL-separated (path, "filter", value)
	// triplets. Only a value naming a driver (not unspecified/unset) is a filter
	// git would execute.
	out := []byte("a.txt\x00filter\x00evil\x00" +
		"b.bin\x00filter\x00unspecified\x00" +
		"c.txt\x00filter\x00lfs\x00" +
		"d.txt\x00filter\x00unset\x00")
	got := filteredPathsFromCheckAttr(out)
	want := []string{"a.txt", "c.txt"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	if filteredPathsFromCheckAttr(nil) != nil {
		t.Errorf("empty input should yield no filtered paths")
	}
}

func TestSplitNUL(t *testing.T) {
	// Trailing and doubled NULs (as ls-files -z emits) must not produce empty
	// fields, since those would desync the check-attr triplet stride.
	got := splitNUL([]byte("a\x00b\x00\x00c\x00"))
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestValidateCommand_Allowlist(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr error // nil means accept
	}{
		// Accepted forms.
		{name: "go test all", args: []string{"go", "test", "./..."}},
		{name: "go test pkg recursive", args: []string{"go", "test", "./coding/..."}},
		{name: "go test single pkg", args: []string{"go", "test", "./coding"}},
		{name: "go test run", args: []string{"go", "test", "./coding", "-run", "TestThing"}},
		{name: "go vet all", args: []string{"go", "vet", "./..."}},
		// The fsmonitor-off global override is MANDATORY before the subcommand, and
		// git diff additionally REQUIRES both --no-ext-diff and --no-textconv: these
		// disable a repo-configured filesystem-monitor hook, diff.external driver,
		// and textconv filters git would otherwise run while refreshing the index
		// and rendering a read-only diff. Both forms must also be scoped to the
		// workspace subtree: diff needs >=1 workspace pathspec ("." is the root) and
		// status needs the trailing "-- ." so neither enumerates a parent repo. This
		// is the form the git_diff tool emits; the un-hardened, partially-hardened,
		// and unscoped forms are rejected below.
		{name: "git diff hardened dot", args: []string{"git", "-c", "core.fsmonitor=false", "diff", "--no-ext-diff", "--no-textconv", "--", "."}},
		{name: "git diff hardened with paths", args: []string{"git", "-c", "core.fsmonitor=false", "diff", "--no-ext-diff", "--no-textconv", "--", "a.go", "b.go"}},
		{name: "git diff hardened flags swapped", args: []string{"git", "-c", "core.fsmonitor=false", "diff", "--no-textconv", "--no-ext-diff", "--", "a.go"}},
		{name: "git diff hardened nested path", args: []string{"git", "-c", "core.fsmonitor=false", "diff", "--no-ext-diff", "--no-textconv", "--", "sub/a.go"}},
		{name: "git status fsmonitor off scoped", args: []string{"git", "-c", "core.fsmonitor=false", "status", "--short", "--", "."}},

		// Rejected forms.
		{name: "empty", args: nil, wantErr: ErrEmptyCommand},
		{name: "bare go", args: []string{"go"}, wantErr: ErrCommandNotAllowed},
		{name: "go build", args: []string{"go", "build", "./..."}, wantErr: ErrCommandNotAllowed},
		{name: "go run", args: []string{"go", "run", "."}, wantErr: ErrCommandNotAllowed},
		{name: "go test absolute pkg", args: []string{"go", "test", "/etc"}, wantErr: ErrCommandNotAllowed},
		{name: "go test extra flag", args: []string{"go", "test", "./...", "-count=1"}, wantErr: ErrCommandNotAllowed},
		{name: "go test bad run shape", args: []string{"go", "test", "./pkg", "-bench", "."}, wantErr: ErrCommandNotAllowed},
		{name: "go vet pkg", args: []string{"go", "vet", "./pkg"}, wantErr: ErrCommandNotAllowed},
		{name: "git commit", args: []string{"git", "-c", "core.fsmonitor=false", "commit", "-m", "x"}, wantErr: ErrCommandNotAllowed},
		{name: "git push", args: []string{"git", "-c", "core.fsmonitor=false", "push"}, wantErr: ErrCommandNotAllowed},
		{name: "git status no flag", args: []string{"git", "-c", "core.fsmonitor=false", "status"}, wantErr: ErrCommandNotAllowed},
		{name: "git diff unknown flag", args: []string{"git", "-c", "core.fsmonitor=false", "diff", "--stat", "--", "a.go"}, wantErr: ErrCommandNotAllowed},
		{name: "bare git", args: []string{"git"}, wantErr: ErrCommandNotAllowed},
		// The fsmonitor-off override is mandatory: the un-hardened diff/status
		// forms are rejected so a repo-configured fsmonitor hook can never run
		// during the index refresh of a read-only git invocation.
		{name: "git diff without override", args: []string{"git", "diff", "--", "a.go"}, wantErr: ErrCommandNotAllowed},
		{name: "git diff hardened without override", args: []string{"git", "diff", "--no-ext-diff", "--no-textconv", "--"}, wantErr: ErrCommandNotAllowed},
		{name: "git status without override", args: []string{"git", "status", "--short"}, wantErr: ErrCommandNotAllowed},
		{name: "git diff missing sep with override", args: []string{"git", "-c", "core.fsmonitor=false", "diff"}, wantErr: ErrCommandNotAllowed},
		{name: "git diff flag without sep with override", args: []string{"git", "-c", "core.fsmonitor=false", "diff", "--no-ext-diff"}, wantErr: ErrCommandNotAllowed},
		// git diff REQUIRES both hardening flags: omitting either leaves a
		// diff.external or textconv helper reachable from a hostile repo config.
		{name: "git diff no hardening flags", args: []string{"git", "-c", "core.fsmonitor=false", "diff", "--", "a.go"}, wantErr: ErrCommandNotAllowed},
		{name: "git diff only no-ext-diff", args: []string{"git", "-c", "core.fsmonitor=false", "diff", "--no-ext-diff", "--", "a.go"}, wantErr: ErrCommandNotAllowed},
		{name: "git diff only no-textconv", args: []string{"git", "-c", "core.fsmonitor=false", "diff", "--no-textconv", "--", "a.go"}, wantErr: ErrCommandNotAllowed},
		{name: "git diff duplicate hardening flag", args: []string{"git", "-c", "core.fsmonitor=false", "diff", "--no-ext-diff", "--no-ext-diff", "--no-textconv", "--"}, wantErr: ErrCommandNotAllowed},
		// Both git read forms must be scoped to the workspace subtree: an unscoped
		// status/diff reports the whole containing repo, and a pathspec may not
		// escape via "..", an absolute path, or git pathspec magic. (Symlinked
		// pathspecs are caught by localWorkspace.guardCommandGitPaths.)
		{name: "git status no pathspec", args: []string{"git", "-c", "core.fsmonitor=false", "status", "--short"}, wantErr: ErrCommandNotAllowed},
		{name: "git status wrong pathspec", args: []string{"git", "-c", "core.fsmonitor=false", "status", "--short", "--", "sub"}, wantErr: ErrCommandNotAllowed},
		{name: "git diff no pathspec", args: []string{"git", "-c", "core.fsmonitor=false", "diff", "--no-ext-diff", "--no-textconv", "--"}, wantErr: ErrCommandNotAllowed},
		{name: "git diff parent escape pathspec", args: []string{"git", "-c", "core.fsmonitor=false", "diff", "--no-ext-diff", "--no-textconv", "--", "../secret"}, wantErr: ErrCommandNotAllowed},
		{name: "git diff deep escape pathspec", args: []string{"git", "-c", "core.fsmonitor=false", "diff", "--no-ext-diff", "--no-textconv", "--", "a/../../secret"}, wantErr: ErrCommandNotAllowed},
		{name: "git diff absolute pathspec", args: []string{"git", "-c", "core.fsmonitor=false", "diff", "--no-ext-diff", "--no-textconv", "--", "/etc/passwd"}, wantErr: ErrCommandNotAllowed},
		{name: "git diff pathspec magic top", args: []string{"git", "-c", "core.fsmonitor=false", "diff", "--no-ext-diff", "--no-textconv", "--", ":/secret"}, wantErr: ErrCommandNotAllowed},
		{name: "git diff pathspec magic paren", args: []string{"git", "-c", "core.fsmonitor=false", "diff", "--no-ext-diff", "--no-textconv", "--", ":(top)secret"}, wantErr: ErrCommandNotAllowed},
		{name: "git diff empty pathspec", args: []string{"git", "-c", "core.fsmonitor=false", "diff", "--no-ext-diff", "--no-textconv", "--", ""}, wantErr: ErrCommandNotAllowed},
		// Only the exact `-c core.fsmonitor=false` override is allowed: any other
		// -c key=value (or a different fsmonitor value) is rejected so the global
		// flag cannot become a generic config-injection vector.
		{name: "git -c arbitrary config", args: []string{"git", "-c", "core.pager=evil", "diff", "--"}, wantErr: ErrCommandNotAllowed},
		{name: "git -c fsmonitor true", args: []string{"git", "-c", "core.fsmonitor=true", "diff", "--"}, wantErr: ErrCommandNotAllowed},
		{name: "git -c fsmonitor path", args: []string{"git", "-c", "core.fsmonitor=/evil.sh", "diff", "--"}, wantErr: ErrCommandNotAllowed},
		{name: "git -c without value", args: []string{"git", "-c", "diff", "--"}, wantErr: ErrCommandNotAllowed},
		{name: "git -c fsmonitor off no subcommand", args: []string{"git", "-c", "core.fsmonitor=false"}, wantErr: ErrCommandNotAllowed},
		{name: "git double -c", args: []string{"git", "-c", "core.fsmonitor=false", "-c", "core.pager=evil", "diff", "--"}, wantErr: ErrCommandNotAllowed},

		// Package-pattern escapes: a "./..": prefix must not let ".." traverse
		// out of the workspace (go test compiles and runs the package code).
		{name: "go test parent escape", args: []string{"go", "test", "./../foo"}, wantErr: ErrCommandNotAllowed},
		{name: "go test parent escape recursive", args: []string{"go", "test", "./../..."}, wantErr: ErrCommandNotAllowed},
		{name: "go test deep parent escape", args: []string{"go", "test", "./a/../../b"}, wantErr: ErrCommandNotAllowed},
		{name: "go test run parent escape", args: []string{"go", "test", "./../pkg", "-run", "T"}, wantErr: ErrCommandNotAllowed},
		{name: "go test bare dotdot", args: []string{"go", "test", ".."}, wantErr: ErrCommandNotAllowed},
		{name: "go test no slash", args: []string{"go", "test", "."}, wantErr: ErrCommandNotAllowed},
		{name: "go test run empty name", args: []string{"go", "test", "./pkg", "-run", ""}, wantErr: ErrCommandNotAllowed},

		// Accepted: an interior "..." marker is a recursive wildcard, not a
		// traversal, and a nested in-bounds package is fine.
		{name: "go test nested recursive ok", args: []string{"go", "test", "./a/b/..."}},
		{name: "go test nested pkg ok", args: []string{"go", "test", "./a/b/c"}},
		{name: "sh -c", args: []string{"sh", "-c", "echo hi"}, wantErr: ErrCommandNotAllowed},
		{name: "bash -c", args: []string{"bash", "-c", "echo hi"}, wantErr: ErrCommandNotAllowed},
		{name: "curl", args: []string{"curl", "http://x"}, wantErr: ErrCommandNotAllowed},
		{name: "wget", args: []string{"wget", "http://x"}, wantErr: ErrCommandNotAllowed},
		{name: "rm", args: []string{"rm", "-rf", "/"}, wantErr: ErrCommandNotAllowed},
		{name: "python", args: []string{"python", "-c", "x"}, wantErr: ErrCommandNotAllowed},
		{name: "node", args: []string{"node", "x.js"}, wantErr: ErrCommandNotAllowed},
		{name: "npm", args: []string{"npm", "install"}, wantErr: ErrCommandNotAllowed},
		{name: "bun", args: []string{"bun", "run"}, wantErr: ErrCommandNotAllowed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateCommand(tc.args)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("ValidateCommand(%v) = %v, want nil", tc.args, err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("ValidateCommand(%v) = %v, want %v", tc.args, err, tc.wantErr)
			}
		})
	}
}

func TestRunCommand_RejectsNonAllowlisted(t *testing.T) {
	_, err := RunCommand(context.Background(), RunCommandRequest{Args: []string{"rm", "-rf", "."}})
	if !errors.Is(err, ErrCommandNotAllowed) {
		t.Fatalf("err = %v, want ErrCommandNotAllowed", err)
	}
}

// TestRunCommand_RejectsPackageEscape confirms the validator blocks (before any
// process starts) a "go test" pattern that would traverse out of the working
// directory and compile/run code outside the sandbox.
func TestRunCommand_RejectsPackageEscape(t *testing.T) {
	_, err := RunCommand(context.Background(), RunCommandRequest{
		Args:       []string{"go", "test", "./../..."},
		WorkingDir: t.TempDir(),
	})
	if !errors.Is(err, ErrCommandNotAllowed) {
		t.Fatalf("err = %v, want ErrCommandNotAllowed", err)
	}
}

// TestRunCommand_Timeout uses an allowlisted command that blocks until killed.
// "go test" of a package whose test sleeps would require a fixture; instead we
// exercise the timeout path through the validator allowance plus a long-running
// argv by validating against a custom allowlist entry is not possible, so we use
// "go test" with a tiny timeout against the whole module which reliably exceeds
// the budget.
func TestRunCommand_Timeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timeout test in short mode")
	}
	// go test ./... over the whole module cannot finish within 1ms; this drives
	// the DeadlineExceeded branch deterministically.
	res, err := RunCommand(context.Background(), RunCommandRequest{
		Args:       []string{"go", "test", "./..."},
		WorkingDir: ".",
		Timeout:    1 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.TimedOut {
		t.Fatalf("expected TimedOut=true, got %+v", res)
	}
	if res.ExitCode != -1 {
		t.Errorf("ExitCode = %d, want -1 on timeout", res.ExitCode)
	}
}

func TestRunCommand_OutputCap(t *testing.T) {
	// A regular "go test" with no tests in an empty-ish dir would not emit much.
	// We assert the cap logic directly via the capWriter, which RunCommand uses.
	var w capWriter
	w.limit = 10
	big := strings.Repeat("a", 100)
	n, err := w.Write([]byte(big))
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if n != len(big) {
		t.Errorf("Write returned %d, want %d (process must not see backpressure)", n, len(big))
	}
	got := w.String()
	if !w.truncated {
		t.Fatalf("expected truncated=true")
	}
	wantPrefix := strings.Repeat("a", 10) + truncationMarker
	if got != wantPrefix {
		t.Errorf("String() = %q, want %q", got, wantPrefix)
	}
}

func TestRunCommand_OutputCapNoTruncation(t *testing.T) {
	var w capWriter
	w.limit = 100
	_, _ = w.Write([]byte("hello"))
	if w.truncated {
		t.Fatalf("did not expect truncation")
	}
	if got := w.String(); got != "hello" {
		t.Errorf("String() = %q, want %q", got, "hello")
	}
}

func TestRunCommand_OutputCapAcrossWrites(t *testing.T) {
	var w capWriter
	w.limit = 6
	_, _ = w.Write([]byte("abcd"))
	_, _ = w.Write([]byte("efgh"))
	_, _ = w.Write([]byte("ijkl"))
	if !w.truncated {
		t.Fatalf("expected truncation across writes")
	}
	want := "abcdef" + truncationMarker
	if got := w.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

// TestRunCommand_SuccessAndExitCode runs an allowlisted command that succeeds
// to confirm the happy path returns exit code 0 and captured output.
func TestRunCommand_SuccessAndExitCode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess test in short mode")
	}
	root, err := repoRoot()
	if err != nil {
		t.Skipf("cannot locate repo root: %v", err)
	}
	res, err := RunCommand(context.Background(), RunCommandRequest{
		Args:       []string{"git", "-c", "core.fsmonitor=false", "status", "--short", "--", "."},
		WorkingDir: root,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.TimedOut {
		t.Fatalf("did not expect timeout")
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d (stderr=%q), want 0", res.ExitCode, res.Stderr)
	}
}

func TestScrubEnv_NoSecretPassthrough(t *testing.T) {
	t.Setenv("AWS_SECRET_ACCESS_KEY", "super-secret")
	t.Setenv("GITHUB_TOKEN", "ghp_secret")
	t.Setenv("HOME", "/home/agent")

	env := scrubEnv(map[string]string{"FOO": "bar"})
	for _, kv := range env {
		if strings.HasPrefix(kv, "AWS_SECRET_ACCESS_KEY=") {
			t.Errorf("scrubbed env leaked AWS secret: %q", kv)
		}
		if strings.HasPrefix(kv, "GITHUB_TOKEN=") {
			t.Errorf("scrubbed env leaked github token: %q", kv)
		}
	}
	if !containsKV(env, "HOME=/home/agent") {
		t.Errorf("HOME should pass through; env=%v", env)
	}
	if !containsKV(env, "FOO=bar") {
		t.Errorf("explicit Env entry should be present; env=%v", env)
	}
}

func containsKV(env []string, kv string) bool {
	for _, e := range env {
		if e == kv {
			return true
		}
	}
	return false
}

// TestRunCommand_RequiresWorkingDir confirms an allowlisted command is still
// refused when no working directory is set: an empty Dir would run in the
// process CWD, outside the sandbox boundary. The check fires before any process
// starts.
func TestRunCommand_RequiresWorkingDir(t *testing.T) {
	_, err := RunCommand(context.Background(), RunCommandRequest{
		Args: []string{"git", "-c", "core.fsmonitor=false", "status", "--short", "--", "."},
	})
	if err == nil {
		t.Fatalf("RunCommand with empty WorkingDir must error")
	}
	if !strings.Contains(err.Error(), "working directory") {
		t.Errorf("error = %v, want it to mention the working directory", err)
	}
}

// TestRunCommand_RejectsNonAllowlistedEnvKey confirms a caller cannot inject an
// execution-shaping variable (here GOFLAGS) through req.Env even with a valid
// argv and working directory. The rejection happens before exec.
func TestRunCommand_RejectsNonAllowlistedEnvKey(t *testing.T) {
	_, err := RunCommand(context.Background(), RunCommandRequest{
		Args:       []string{"git", "-c", "core.fsmonitor=false", "status", "--short", "--", "."},
		WorkingDir: t.TempDir(),
		Env:        map[string]string{"GOFLAGS": "-toolexec=/bin/false"},
	})
	if err == nil {
		t.Fatalf("RunCommand with a non-allowlisted Env key must error")
	}
	if !strings.Contains(err.Error(), "GOFLAGS") || !strings.Contains(err.Error(), "not permitted") {
		t.Errorf("error = %v, want it to name GOFLAGS as not permitted", err)
	}
}

// TestScrubEnv_ExcludesGOFLAGS pins the GOFLAGS exclusion: a poisoned host
// GOFLAGS (which the toolchain honors and which can carry -exec/-toolexec/
// -gcflags that spawn an arbitrary helper) must never leak into a sandboxed
// command's environment.
func TestScrubEnv_ExcludesGOFLAGS(t *testing.T) {
	t.Setenv("GOFLAGS", "-toolexec=/usr/bin/evil")
	env := scrubEnv(nil)
	for _, kv := range env {
		if strings.HasPrefix(kv, "GOFLAGS=") {
			t.Fatalf("scrubbed env leaked GOFLAGS: %q", kv)
		}
	}
}

// TestScrubEnv_PinsGOENVOff confirms the scrubbed env pins GOENV=off so the Go
// toolchain ignores the per-user `go env` config file (under the passed-through
// HOME). Without it, a GOFLAGS entry written into that file (via a prior `go env
// -w`, or a poisoned HOME) would still be honored by the sandboxed go test/go
// vet, defeating the GOFLAGS exclusion. The pin is authoritative: even an
// attempt to set GOENV through extra is overridden.
func TestScrubEnv_PinsGOENVOff(t *testing.T) {
	if !containsKV(scrubEnv(nil), "GOENV=off") {
		t.Errorf("scrubbed env must pin GOENV=off; env=%v", scrubEnv(nil))
	}
	env := scrubEnv(map[string]string{"GOENV": "/tmp/evil/go/env"})
	if !containsKV(env, "GOENV=off") {
		t.Errorf("GOENV pin must win over extra; env=%v", env)
	}
	for _, kv := range env {
		if kv == "GOENV=/tmp/evil/go/env" {
			t.Errorf("extra GOENV must not survive the pin: %q", kv)
		}
	}
}

// TestIsAllowedEnvKey checks the passthrough allowlist directly: locator
// variables the toolchain needs are allowed, execution-shaping ones are not.
func TestIsAllowedEnvKey(t *testing.T) {
	for _, k := range []string{"HOME", "PATH", "GOPROXY", "GOCACHE"} {
		if !isAllowedEnvKey(k) {
			t.Errorf("isAllowedEnvKey(%q) = false, want true", k)
		}
	}
	for _, k := range []string{"GOFLAGS", "GODEBUG", "AWS_SECRET_ACCESS_KEY", "LD_PRELOAD", ""} {
		if isAllowedEnvKey(k) {
			t.Errorf("isAllowedEnvKey(%q) = true, want false", k)
		}
	}
}
