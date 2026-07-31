package coding

import (
	"context"
	"strconv"
	"strings"

	"github.com/Viking602/venat/tool"
)

// goTestInput is the decoded argument shape for coding.go_test.
type goTestInput struct {
	// Package is the package pattern (default "./..."). Must be a ./-prefixed
	// pattern; the command sandbox rejects parent-directory traversal.
	Package string `json:"package,omitempty"`
	// Run optionally restricts the run to tests matching this name.
	Run string `json:"run,omitempty"`
}

// GoTestToolResult is the typed structured result of coding.go_test.
type GoTestToolResult struct {
	Args      []string `json:"args"`
	ExitCode  int      `json:"exitCode"`
	Passed    bool     `json:"passed"`
	Stdout    string   `json:"stdout"`
	Stderr    string   `json:"stderr"`
	Truncated bool     `json:"truncated"`
	TimedOut  bool     `json:"timedOut"`
	Duration  string   `json:"duration"`
}

// goTestDriver runs `go test` over the sandbox command allowlist. It touches
// only the toolchain's own cache/temp directories, but `go test` compiles and
// executes the workspace's own test code — it is not a pure read. It therefore
// carries the run PolicyTag so coding.PolicyEngine escalates it to require
// approval, putting execution-capable tools behind the same explicit-allowance
// gate as the workspace writes rather than letting them run unattended.
type goTestDriver struct {
	ws Workspace
}

func (d goTestDriver) Definition() tool.Definition {
	return tool.Definition{
		Name:        ToolGoTest,
		Description: "Run go test over a workspace package pattern (allowlisted; no shell).",
		InputSchema: objectSchema(
			nil,
			property{"package", stringSchema("Package pattern such as ./... or ./coding/... (default ./...).")},
			property{"run", stringSchema("Optional -run test name filter.")},
		),
		// EffectReadOnly: go_test never mutates workspace files. The run tag is
		// what gates it — coding.PolicyEngine escalates run-tagged tools to
		// require approval because `go test` executes workspace code.
		EffectType:         tool.EffectReadOnly,
		RequiresActionTask: false,
		RiskLevel:          riskMedium,
		PolicyTags:         []string{tagCoding, tagTest, tagRun},
	}
}

func (d goTestDriver) Execute(ctx context.Context, call tool.Call, _ tool.UpdateSink) (tool.Result, error) {
	var in goTestInput
	if err := decodeArgs(call.Arguments, &in); err != nil {
		return tool.Result{}, err
	}
	pkg := strings.TrimSpace(in.Package)
	if pkg == "" {
		pkg = "./..."
	}
	args := []string{"go", "test", pkg}
	if run := strings.TrimSpace(in.Run); run != "" {
		args = append(args, "-run", run)
	}

	res, err := d.ws.RunCommand(ctx, RunCommandRequest{Args: args})
	if err != nil {
		return errorResult(call, "coding.go_test rejected: "+err.Error()), nil
	}

	structured := GoTestToolResult{
		Args:      res.Args,
		ExitCode:  res.ExitCode,
		Passed:    res.ExitCode == 0 && !res.TimedOut,
		Stdout:    res.Stdout,
		Stderr:    res.Stderr,
		Truncated: res.Truncated,
		TimedOut:  res.TimedOut,
		Duration:  res.Duration,
	}
	content := renderCommand("go test", res.Stdout, res.Stderr, res.ExitCode, res.TimedOut)
	return successResult(call, content, structured)
}

// renderCommand formats command output for the human-facing content string.
func renderCommand(label, stdout, stderr string, exitCode int, timedOut bool) string {
	var b strings.Builder
	b.WriteString(label)
	if timedOut {
		b.WriteString(" timed out")
	} else if exitCode == 0 {
		b.WriteString(" passed")
	} else {
		b.WriteString(" failed (exit ")
		b.WriteString(strconv.Itoa(exitCode))
		b.WriteString(")")
	}
	if s := strings.TrimRight(stdout, "\n"); s != "" {
		b.WriteString("\n")
		b.WriteString(s)
	}
	if s := strings.TrimRight(stderr, "\n"); s != "" {
		b.WriteString("\n")
		b.WriteString(s)
	}
	return b.String()
}
