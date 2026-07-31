// Package devops is a v0.8.0 skeleton pack for build/release/operate
// style agents: a deploy assistant, a release-notes drafter, and a
// log-triage helper. The skeleton ships capability shapes and one
// AgentDefinition; concrete tool bindings live in the host application.
package devops

import (
	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/packs"
)

// PackName is the registry identifier for this pack.
const PackName = "devops"

// Pack is the top-level exported value mounted into a packs.Registry.
var Pack = packs.Pack{
	Name:        PackName,
	Version:     "0.8.0",
	Description: "Skeleton: a release-notes drafter that reads PRs and produces a draft for human review.",
	Agents:      []api.AgentDefinition{releaseNotes},
	Capabilities: []api.CapabilityManifest{
		{
			Name:        "devops.core",
			Version:     "0.8.0",
			Description: "Source-control reads, CI inspection, deploy gating.",
			Capabilities: []api.Capability{
				{
					Name:        "list_merged_prs",
					Description: "List PRs merged into a target branch within a date range.",
					EffectType:  api.ToolEffectReadOnly,
					Idempotent:  true,
					Tags:        []string{"devops", "scm"},
				},
				{
					Name:        "read_ci_status",
					Description: "Read CI run status for a commit or PR.",
					EffectType:  api.ToolEffectReadOnly,
					Idempotent:  true,
					Tags:        []string{"devops", "ci"},
				},
				{
					Name:             "trigger_deploy",
					Description:      "Trigger a deploy of a build to an environment.",
					EffectType:       api.ToolEffectExternalSideEffect,
					RequiresApproval: true,
					Idempotent:       false,
					Tags:             []string{"devops", "deploy"},
				},
			},
		},
	},
	Recipes: []packs.Recipe{
		{
			Name:        "release-notes",
			Description: "Read merged PRs since the last tag and draft release notes for human edit.",
			DocumentURL: "docs/recipes/devops-release-notes.md",
		},
	},
}

var releaseNotes = api.AgentDefinition{
	ID:           "devops.release-notes",
	Name:         "Release Notes Drafter",
	Version:      "0.8.0",
	Description:  "Reads merged PRs between two tags and drafts release notes.",
	Instructions: "Collect merged PRs in range, group by area, and draft release notes.",
	Model:        api.ModelPolicy{Model: "claude-sonnet-4-6", Temperature: 0.3},
	Capabilities: []string{"list_merged_prs", "read_ci_status"},
	Triggers: []api.Trigger{
		{ID: "manual", Type: api.TriggerManual, Enabled: true},
		{ID: "weekly", Type: api.TriggerSchedule, Config: map[string]string{"cron": "0 0 9 * * MON"}, Enabled: false},
	},
	Governance: api.GovernancePolicy{
		Budget:              api.Budget{MaxToolCalls: 20, MaxModelCalls: 4},
		ApprovalRequiredFor: []api.ToolEffectType{api.ToolEffectExternalSideEffect},
	},
}
