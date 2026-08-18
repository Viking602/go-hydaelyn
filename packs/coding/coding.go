// Package coding is the declarative manifest for the sandboxed coding
// capability specified in docs/coding-agent-hashline.md. It bundles the
// code-editor AgentDefinition and one CapabilityManifest describing the
// coding.* tools — nothing else.
//
// Like every pack (see packs/devops, packs/research) this file imports
// only api and MUST NOT import the coding/ runtime package: a pack is
// configuration the host mounts, and the host is responsible for binding
// each Capability to a concrete tool.Driver from coding.NewToolSet(ws)
// and attaching coding.PolicyEngine() on the worker path. The manifest
// mirrors the tool metadata table in spec §6 and the agent instructions
// in spec §8 so the catalog stays in sync with the implementation
// without coupling to it.
//
// Mount it via:
//
//	r := packs.NewRegistry()
//	_ = packs.Register(r, coding.Pack)
package coding

import (
	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/packs"
)

// PackName is the registry identifier for this pack.
const PackName = "coding"

// Tool name constants mirror coding/driver.go. They are duplicated here (not
// imported) to keep the pack boundary clean: the manifest names the tools the
// host binds to coding.NewToolSet drivers, but it never depends on the runtime
// package. If the runtime renames a tool, the packs test and the host wiring
// will surface the drift.
const (
	toolListFiles    = "coding.list_files"
	toolReadFile     = "coding.read_file"
	toolSearch       = "coding.search"
	toolGitDiff      = "coding.git_diff"
	toolEditHashline = "coding.edit_hashline"
	toolWriteFile    = "coding.write_file"
	toolGofmt        = "coding.gofmt"
	toolGoTest       = "coding.go_test"
)

// Pack is the top-level exported value mounted into a packs.Registry.
var Pack = packs.Pack{
	Name:        PackName,
	Version:     "0.1.0",
	Description: "Sandboxed coding agent: reads, searches, and edits a workspace via the hashline line-anchored protocol, with gofmt/go_test/git_diff. Pure Go, no shell.",
	Agents:      []api.AgentDefinition{codeEditor},
	Capabilities: []api.CapabilityManifest{
		{
			Name:        "coding.core",
			Version:     "0.1.0",
			Description: "The coding.* tools: workspace reads/search, hashline edits, file creation, in-process gofmt, allowlisted go_test, and git_diff.",
			Capabilities: []api.Capability{
				{
					Name:        toolListFiles,
					Version:     "1",
					Description: "List workspace-relative files, honoring ignores and the .git denylist.",
					EffectType:  api.ToolEffectReadOnly,
					RiskLevel:   "low",
					Idempotent:  true,
					Tags:        []string{"coding", "read"},
				},
				{
					Name:        toolReadFile,
					Version:     "1",
					Description: "Read a file and return hashline-grounded numbered lines (¶PATH#TAG header + N:TEXT rows).",
					EffectType:  api.ToolEffectReadOnly,
					RiskLevel:   "low",
					Idempotent:  true,
					Tags:        []string{"coding", "read"},
				},
				{
					Name:        toolSearch,
					Version:     "1",
					Description: "Search workspace text files (substring or regexp) and return grouped ¶PATH#TAG sections with context.",
					EffectType:  api.ToolEffectReadOnly,
					RiskLevel:   "low",
					Idempotent:  true,
					Tags:        []string{"coding", "search"},
				},
				{
					Name:        toolGitDiff,
					Version:     "1",
					Description: "Return the bounded git diff for the workspace (allowlisted subprocess, no shell).",
					EffectType:  api.ToolEffectReadOnly,
					RiskLevel:   "low",
					Idempotent:  true,
					Tags:        []string{"coding", "git", "diff"},
				},
				{
					Name:             toolEditHashline,
					Version:          "1",
					Description:      "Apply an all-or-nothing hashline patch to existing files; stale tags are rejected so the agent must re-read.",
					EffectType:       api.ToolEffectWrite,
					RiskLevel:        "medium",
					Idempotent:       false,
					RequiresApproval: true,
					Tags:             []string{"coding", "edit", "hashline", "workspace-write"},
				},
				{
					Name:             toolWriteFile,
					Version:          "1",
					Description:      "Create a new file (rejects existing paths; edits go through coding.edit_hashline).",
					EffectType:       api.ToolEffectWrite,
					RiskLevel:        "medium",
					Idempotent:       false,
					RequiresApproval: true,
					Tags:             []string{"coding", "create-file"},
				},
				{
					Name:             toolGofmt,
					Version:          "1",
					Description:      "Format a Go file in-process via go/format and write it back if it changed (no subprocess, no goimports).",
					EffectType:       api.ToolEffectWrite,
					RiskLevel:        "low",
					Idempotent:       true,
					RequiresApproval: true,
					Tags:             []string{"coding", "format"},
				},
				{
					Name:        toolGoTest,
					Version:     "1",
					Description: "Run an allowlisted `go test` invocation in the workspace (no shell; bounded output and timeout).",
					// go_test never mutates workspace files (ReadOnly), but `go test`
					// compiles and executes the workspace's own code, so it carries the
					// run tag at medium risk to mirror the runtime driver — hosts that
					// build governance from this manifest escalate it to require
					// approval just like coding.PolicyEngine does.
					EffectType: api.ToolEffectReadOnly,
					RiskLevel:  "medium",
					Idempotent: false,
					Tags:       []string{"coding", "test", "run"},
				},
			},
		},
	},
	Recipes: []packs.Recipe{
		{
			Name:        "hashline-edit-loop",
			Description: "read_file/search to mint a ¶PATH#TAG handle, edit_hashline to apply an all-or-nothing patch, gofmt, then go_test and git_diff to verify.",
			DocumentURL: "docs/coding-agent-hashline.md",
		},
	},
}

// codeEditor is the single coding agent (spec §8). Tools list the executable
// read/edit tools; Capabilities remains descriptive metadata. Model is a
// default the host may override.
var codeEditor = api.AgentDefinition{
	ID:           "coding.code-editor",
	Name:         "Code Editor",
	Version:      "0.1.0",
	Description:  "A careful coding agent that edits a sandboxed workspace via the hashline line-anchored protocol.",
	Instructions: instructions,
	Model:        api.ModelPolicy{Model: "claude-sonnet-4-6", Temperature: 0.2},
	Tools: []string{
		toolListFiles,
		toolReadFile,
		toolSearch,
		toolEditHashline,
		toolGofmt,
		toolGoTest,
		toolGitDiff,
	},
	Capabilities: []string{
		toolListFiles,
		toolReadFile,
		toolSearch,
		toolEditHashline,
		toolGofmt,
		toolGoTest,
		toolGitDiff,
	},
	Triggers: []api.Trigger{
		{ID: "manual", Type: api.TriggerManual, Enabled: true},
	},
	Governance: api.GovernancePolicy{
		Budget: api.Budget{MaxToolCalls: 60, MaxModelCalls: 12},
		// Edits/creates are classified write; the host pairs this with the
		// toolgate (Task.AllowsAction) and coding.PolicyEngine on the worker
		// path. Approval is required for any tool surfacing a delete/run tag.
		ApprovalRequiredFor: []api.ToolEffectType{api.ToolEffectExternalSideEffect},
	},
}

// instructions is the coding agent's system prompt: the hashline editing
// protocol from spec §8, kept in sync with coding.Instructions in the runtime
// package (which the pack does not import).
const instructions = `You are a careful coding agent in a sandboxed workspace.
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
