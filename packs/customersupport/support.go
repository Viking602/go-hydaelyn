// Package customersupport is a v0.8.0 skeleton pack that bundles a
// triage agent plus the capability shape a typical support workflow
// needs (ticket lookup, knowledge-base search, reply drafting).
//
// The skeleton intentionally omits real backend wiring — hosts bind
// each capability to their own ticketing system, knowledge base, and
// messaging gateway when mounting this pack.
package customersupport

import (
	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/packs"
)

// PackName is the registry identifier for this pack.
const PackName = "customer-support"

// Pack is the top-level exported value mounted into a packs.Registry.
var Pack = packs.Pack{
	Name:        PackName,
	Version:     "0.8.0",
	Description: "Skeleton: a single triage agent that classifies a ticket and drafts a reply.",
	Agents:      []api.AgentDefinition{triage},
	Capabilities: []api.CapabilityManifest{
		{
			Name:        "support.core",
			Version:     "0.8.0",
			Description: "Ticketing, knowledge-base lookup, and reply drafting.",
			Capabilities: []api.Capability{
				{
					Name:        "lookup_ticket",
					Description: "Fetch a ticket by ID from the host ticketing system.",
					EffectType:  api.ToolEffectReadOnly,
					Idempotent:  true,
					Tags:        []string{"support", "ticket"},
				},
				{
					Name:        "search_kb",
					Description: "Search the knowledge base for relevant articles.",
					EffectType:  api.ToolEffectReadOnly,
					Idempotent:  true,
					Tags:        []string{"support", "kb"},
				},
				{
					Name:             "draft_reply",
					Description:      "Compose a candidate reply on the ticket. Reply is held for human review.",
					EffectType:       api.ToolEffectWrite,
					RequiresApproval: true,
					Tags:             []string{"support", "reply"},
				},
			},
		},
	},
	Recipes: []packs.Recipe{
		{
			Name:        "ticket-triage",
			Description: "Look up the ticket, search the KB, draft a reply, request human approval.",
			DocumentURL: "docs/recipes/customer-support-triage.md",
		},
	},
}

var triage = api.AgentDefinition{
	ID:           "support.triage",
	Name:         "Support Triage",
	Version:      "0.8.0",
	Description:  "Classifies an incoming ticket and drafts a reply for human approval.",
	Instructions: "Read the ticket, pick the most likely category, search the KB, and draft a reply.",
	Model:        api.ModelPolicy{Model: "claude-sonnet-4-6", Temperature: 0.2},
	Capabilities: []string{"lookup_ticket", "search_kb", "draft_reply"},
	Triggers: []api.Trigger{
		{ID: "manual", Type: api.TriggerManual, Enabled: true},
		{ID: "new-ticket", Type: api.TriggerEvent, Config: map[string]string{"topic": "ticket.created"}, Enabled: true},
	},
	Governance: api.GovernancePolicy{
		Budget:              api.Budget{MaxToolCalls: 8, MaxModelCalls: 3},
		ApprovalRequiredFor: []api.ToolEffectType{api.ToolEffectExternalSideEffect},
	},
}
