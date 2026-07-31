// Package research is the v0.8.0 worked-example pack: a small bundle of
// agent definitions, capabilities, and an eval suite that demonstrates
// the shape every pack should follow.
//
// The pack mounts cleanly via:
//
//	r := packs.NewRegistry()
//	_ = packs.Register(r, research.Pack)
//
// All identifiers here are intentionally generic — the framework owns
// no opinion about what "research" means in the host application. The
// pack only proves that AgentDefinition + Capability + Suite values can
// be co-located under one umbrella without touching the kernel.
package research

import (
	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/eval"
	"github.com/Viking602/venat/eval/assertions"
	"github.com/Viking602/venat/packs"
	"github.com/Viking602/venat/provider"
)

// PackName is the registry identifier for this pack.
const PackName = "research"

// Pack is the top-level exported value mounted into a packs.Registry.
var Pack = packs.Pack{
	Name:        PackName,
	Version:     "0.8.0",
	Description: "Reference pack: a planner / researcher / writer triad with a smoke eval suite.",
	Agents:      []api.AgentDefinition{planner, researcher, writer},
	Capabilities: []api.CapabilityManifest{
		{
			Name:        "research.core",
			Version:     "0.8.0",
			Description: "Core research capabilities: web search, document fetch, citation extraction.",
			Capabilities: []api.Capability{
				{
					Name:        "web_search",
					Version:     "1",
					Description: "Run a keyword query against a search backend and return ranked hits.",
					EffectType:  api.ToolEffectReadOnly,
					RiskLevel:   "low",
					Idempotent:  true,
					Tags:        []string{"research", "search"},
				},
				{
					Name:        "fetch_document",
					Version:     "1",
					Description: "Fetch a document by URL or document ID and return its plaintext body.",
					EffectType:  api.ToolEffectReadOnly,
					RiskLevel:   "low",
					Idempotent:  true,
					Tags:        []string{"research", "fetch"},
				},
				{
					Name:        "extract_citations",
					Version:     "1",
					Description: "Extract structured citations from a document body.",
					EffectType:  api.ToolEffectReadOnly,
					RiskLevel:   "low",
					Idempotent:  true,
					Tags:        []string{"research", "citations"},
				},
			},
		},
	},
	Recipes: []packs.Recipe{
		{
			Name:        "planner-researcher-writer",
			Description: "Planner decomposes the question; researchers fan out on subqueries; writer composes the final answer.",
			DocumentURL: "docs/recipes/research-triad.md",
		},
	},
	EvalCases: SmokeCases,
}

var planner = api.AgentDefinition{
	ID:           "research.planner",
	Name:         "Research Planner",
	Version:      "0.8.0",
	Description:  "Decomposes a high-level research question into ordered subqueries.",
	Instructions: "You are the planner. Read the user's question and emit 3-5 focused subqueries.",
	Model:        api.ModelPolicy{Model: "claude-sonnet-4-6", Temperature: 0.2},
	Capabilities: []string{"web_search"},
	Triggers: []api.Trigger{
		{ID: "manual", Type: api.TriggerManual, Enabled: true},
	},
	Governance: api.GovernancePolicy{
		Budget: api.Budget{MaxToolCalls: 10, MaxModelCalls: 4},
	},
}

var researcher = api.AgentDefinition{
	ID:           "research.researcher",
	Name:         "Researcher",
	Version:      "0.8.0",
	Description:  "Runs one subquery against the configured search/fetch tools and writes findings to the blackboard.",
	Instructions: "Take a single subquery, gather sources, and post evidence items.",
	Model:        api.ModelPolicy{Model: "claude-sonnet-4-6", Temperature: 0.3},
	Capabilities: []string{"web_search", "fetch_document", "extract_citations"},
	Triggers: []api.Trigger{
		{ID: "manual", Type: api.TriggerManual, Enabled: true},
	},
	Governance: api.GovernancePolicy{
		Budget: api.Budget{MaxToolCalls: 30, MaxModelCalls: 6},
	},
}

var writer = api.AgentDefinition{
	ID:           "research.writer",
	Name:         "Writer",
	Version:      "0.8.0",
	Description:  "Reads the blackboard, drafts an answer, and produces a final user-visible message.",
	Instructions: "Combine the evidence on the blackboard into a clearly cited answer.",
	Model:        api.ModelPolicy{Model: "claude-opus-4-7", Temperature: 0.4},
	Capabilities: []string{},
	Triggers: []api.Trigger{
		{ID: "manual", Type: api.TriggerManual, Enabled: true},
	},
	Governance: api.GovernancePolicy{
		Budget: api.Budget{MaxModelCalls: 2},
	},
}

// SmokeCases is a one-case eval suite that drives the pack against a
// deterministic scripted model and verifies the run completes with a
// non-empty answer. Hosts run it in CI via eval.RunSuite(t, SmokeCases).
// Swapping the harness's scripted provider for a live one turns this into
// a full quality gate without changing the case shape.
var SmokeCases = []eval.EvalCase{
	{
		Name:        "cited-answer",
		Description: "the final answer is non-empty and mentions a source",
		Setup: func() eval.Harness {
			return eval.NewHarness(eval.WithScript([]provider.Event{
				{Kind: provider.EventTextDelta, Text: "Answer with source [1] attached."},
				{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
			}))
		},
		Input: api.StartRunCommand{
			RunID:      "research-smoke",
			RootTaskID: "root",
			Request:    "summarize the question with at least one source",
		},
		Assertions: []eval.Assertion{
			assertions.RunTerminatedWithStatus{Status: api.RunStatusCompleted},
			assertions.OutputContains{Substring: "source"},
		},
	},
}
