package coding

import (
	"context"
	"errors"

	"github.com/Viking602/venat/coding/internal/hashline"
	"github.com/Viking602/venat/multiagent"
	"github.com/Viking602/venat/policy"
	"github.com/Viking602/venat/tool"
)

// AgentClassName is the AgentClass name for the coding agent.
const AgentClassName = "code-editor"

// ToolSetOption configures a coding tool set built by NewToolSet.
type ToolSetOption func(*toolSetConfig)

// toolSetConfig accumulates NewToolSet options before the drivers are built.
type toolSetConfig struct {
	store hashline.SnapshotStore
}

// WithSnapshotStore overrides the per-tool-set snapshot store. By default
// NewToolSet creates one hashline.MemorySnapshotStore and shares it across the
// read_file, search, edit_hashline, write_file, and gofmt drivers so an edit
// can recover a stale tag against the history those tools recorded, and so the
// tags write_file/gofmt mint are collision-guarded. A host that wants to share
// one store across several tool sets (e.g. one per run) can supply it here.
func WithSnapshotStore(store hashline.SnapshotStore) ToolSetOption {
	return func(c *toolSetConfig) {
		if store != nil {
			c.store = store
		}
	}
}

// NewToolSet builds the coding tool drivers over ws. The same workspace value
// also satisfies hashline.Filesystem, so the edit/gofmt tools share the disk
// boundary the read/search tools validate paths through. The returned slice is
// registered with a tool.Bus by the host (then wrapped by the worker's
// GovernedToolBus, which is where policy is enforced).
//
// The tool set shares ONE hashline.MemorySnapshotStore (override via
// WithSnapshotStore) across read_file, search, edit_hashline, write_file, and
// gofmt: read/search/write/gofmt record the file's full normalized content into
// the store, so a later edit whose ¶PATH#TAG is stale (the file changed
// out-of-band, or after an earlier edit) can recover via a three-way merge
// against that recorded history when the changes do not conflict, and so the
// tags write_file/gofmt mint are collision-guarded against out-of-band change
// instead of fast-pathing on the 16-bit hash alone. Conflicting or unrecorded
// edits still get the stale-reject re-read message. The store is per-tool-set
// so history does not leak across runs.
func NewToolSet(ws Workspace, opts ...ToolSetOption) []tool.Driver {
	cfg := toolSetConfig{store: hashline.NewMemorySnapshotStore()}
	for _, opt := range opts {
		opt(&cfg)
	}

	fs, ok := ws.(hashline.Filesystem)
	if !ok {
		// A Workspace that is not also a hashline.Filesystem cannot back the
		// patcher. NewLocalWorkspace always satisfies both; a custom Workspace
		// must too. Install an error-returning FS (NOT a nil FS, which would
		// panic in the patcher's preflight) so read-only tools still work and
		// edit_hashline surfaces a clear write error instead of crashing.
		fs = errFilesystem{}
	}
	patcher := &hashline.Patcher{FS: fs, Snapshots: cfg.store}
	return []tool.Driver{
		listFilesDriver{ws: ws},
		readFileDriver{ws: ws, store: cfg.store},
		searchDriver{ws: ws, store: cfg.store},
		editHashlineDriver{ws: ws, patcher: patcher},
		writeFileDriver{ws: ws, store: cfg.store},
		gofmtDriver{ws: ws, store: cfg.store},
		goTestDriver{ws: ws},
		gitDiffDriver{ws: ws},
	}
}

// ErrWorkspaceNotWritable is the error edit_hashline surfaces when the host's
// Workspace does not also implement hashline.Filesystem, so no in-place write
// boundary is available. NewLocalWorkspace always satisfies both interfaces; a
// custom Workspace that omits the write methods hits this instead of a panic.
var ErrWorkspaceNotWritable = errors.New("coding: workspace does not implement hashline.Filesystem; edit_hashline is unavailable")

// errFilesystem is the hashline.Filesystem installed by NewToolSet when the
// host's Workspace does not also satisfy hashline.Filesystem. Every method
// returns ErrWorkspaceNotWritable, so an edit_hashline call fails fast in the
// patcher's preflight with a clear, recoverable error rather than panicking on
// a nil patcher FS.
type errFilesystem struct{}

func (errFilesystem) CanonicalPath(string) (string, error) {
	return "", ErrWorkspaceNotWritable
}

func (errFilesystem) ReadText(context.Context, string) (string, error) {
	return "", ErrWorkspaceNotWritable
}

func (errFilesystem) PreflightWrite(context.Context, string) error {
	return ErrWorkspaceNotWritable
}

func (errFilesystem) WriteText(context.Context, string, string) error {
	return ErrWorkspaceNotWritable
}

// ToolNames lists the coding tool names in the order NewToolSet returns them.
func ToolNames() []string {
	return []string{
		ToolListFiles,
		ToolReadFile,
		ToolSearch,
		ToolEditHashline,
		ToolWriteFile,
		ToolGofmt,
		ToolGoTest,
		ToolGitDiff,
	}
}

// PolicyEngine returns the coding capability's policy engine. It composes over
// policy.DenySideEffectsByDefault: read-only coding tools are allowed, and any
// tool carrying a delete or run PolicyTag is escalated to require approval.
// Other writes (edit/write/gofmt) fall through to the default deny, which the
// toolgate (Task.AllowsAction) plus an explicit allowance must clear.
//
// The public policy/ package does not evaluate RiskLevel/PolicyTags itself, so
// this engine inspects request.Tool.PolicyTags directly.
func PolicyEngine() policy.Engine {
	base := policy.DenySideEffectsByDefault()
	return policy.EngineFunc(func(ctx context.Context, request policy.Request) (policy.Decision, error) {
		if request.Tool != nil && hasAnyTag(request.Tool.PolicyTags, tagDelete, tagRun) {
			return policy.Decision{
				Effect: policy.EffectRequireApproval,
				Reason: "coding: delete/run-tagged tool requires approval",
			}, nil
		}
		return base.Authorize(ctx, request)
	})
}

// hasAnyTag reports whether tags contains any of the wanted tags.
func hasAnyTag(tags []string, wanted ...string) bool {
	for _, t := range tags {
		for _, w := range wanted {
			if t == w {
				return true
			}
		}
	}
	return false
}

// AgentClass returns the single coding agent's declarative class. The model is
// left to the host to override; the tool list and instructions encode the
// hashline editing protocol.
func AgentClass() multiagent.AgentClass {
	return multiagent.AgentClass{
		Name:         AgentClassName,
		Description:  "A careful coding agent that edits a sandboxed workspace via the hashline line-anchored protocol.",
		Instructions: Instructions,
		Model:        "claude-sonnet-4-6",
		Tools: []string{
			ToolListFiles,
			ToolReadFile,
			ToolSearch,
			ToolEditHashline,
			ToolGofmt,
			ToolGoTest,
			ToolGitDiff,
		},
	}
}

// Instructions is the coding agent's system prompt: the hashline editing
// protocol from spec section 8.
const Instructions = `You are a careful coding agent in a sandboxed workspace.
1. Before editing, call coding.read_file or coding.search; use the returned ¶PATH#TAG
   header and N:TEXT line numbers.
2. Edit existing files only via coding.edit_hashline; every section starts with ¶PATH#TAG.
3. After a successful edit, the old tag and old line numbers are dead — use the new
   header from the edit result or re-read before the next edit.
4. If an edit is rejected (stale tag, mismatch, parse error), STOP and re-read. Never
   stack edits on stale context.
5. Keep ranges tight; replace only lines whose content changes.
6. Body rows are final content only (+TEXT); never -old or bare context lines.
7. Do not use hashline for formatting — use coding.gofmt.
8. Prefer small patches; review coding.git_diff; run focused go_test; report results.
9. Never access paths outside the workspace; never request arbitrary shell; never
   commit/push/delete unless explicitly asked.`
