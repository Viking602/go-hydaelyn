package coding

import (
	"context"

	"github.com/Viking602/go-hydaelyn/tool"
)

// gitDiffInput is the decoded argument shape for coding.git_diff.
type gitDiffInput struct {
	// Paths optionally restricts the diff to these workspace-relative paths.
	// Empty diffs the whole worktree.
	Paths []string `json:"paths,omitempty"`
}

// GitDiffToolResult is the typed structured result of coding.git_diff.
type GitDiffToolResult struct {
	Diff      string `json:"diff"`
	Truncated bool   `json:"truncated"`
}

// gitDiffDriver returns a bounded `git diff` over the sandbox command
// allowlist.
type gitDiffDriver struct {
	ws Workspace
}

func (d gitDiffDriver) Definition() tool.Definition {
	return tool.Definition{
		Name:        ToolGitDiff,
		Description: "Show the git diff of the workspace (or specific paths). Read-only and bounded.",
		InputSchema: objectSchema(
			nil,
			property{"paths", stringArraySchema("Optional workspace-relative paths to diff.")},
		),
		EffectType:         tool.EffectReadOnly,
		RequiresActionTask: false,
		RiskLevel:          riskLow,
		PolicyTags:         []string{tagCoding, tagGit, tagDiff},
	}
}

func (d gitDiffDriver) Execute(ctx context.Context, call tool.Call, _ tool.UpdateSink) (tool.Result, error) {
	var in gitDiffInput
	if err := decodeArgs(call.Arguments, &in); err != nil {
		return tool.Result{}, err
	}
	res, err := d.ws.Diff(ctx, DiffRequest{Paths: in.Paths})
	if err != nil {
		return errorResult(call, "coding.git_diff failed: "+err.Error()), nil
	}
	structured := GitDiffToolResult(res)
	content := res.Diff
	if content == "" {
		content = "(no changes)"
	}
	return successResult(call, content, structured)
}
