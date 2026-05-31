// Package multiagent is the v0.8.0 multi-agent layer. It sits between
// the Agent Loop layer (agent/) and the Durable Runner (runner +
// internal/), defining the role/class/team primitives the Scheduler
// reasons over.
//
// Spec anchor: docs/product-spec/v0.8.0/05-multi-agent-layer.md.
package multiagent

import (
	"encoding/json"

	"github.com/Viking602/go-hydaelyn/agent"
	"github.com/Viking602/go-hydaelyn/api"
)

// AgentClass is the declarative description of a role an agent can play
// inside a Team. Distinct from api.AgentProfile (runtime identity) and
// from AgentInstance (per-run materialization). Multiple AgentInstances
// of the same AgentClass may execute concurrently inside a single Run.
//
// Spec anchor: docs/product-spec/v0.8.0/04-agent-class.md.
type AgentClass struct {
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	Instructions string `json:"instructions,omitempty"`

	Model string   `json:"model,omitempty"`
	Tools []string `json:"tools,omitempty"`

	InputSchema  json.RawMessage `json:"inputSchema,omitempty"`
	OutputSchema json.RawMessage `json:"outputSchema,omitempty"`

	LoopPolicy   agent.LoopPolicy `json:"loopPolicy,omitempty"`
	Capabilities []api.Capability `json:"capabilities,omitempty"`
}

// ToSpec projects this class onto the neutral agent.Spec the agent layer
// materializes through agent.Build. It carries only the executable fields a
// Spec defines — instructions, model, tools, loop policy, and the typed-handoff
// schemas — and deliberately drops the multiagent-only ontology (Name,
// Description, Capabilities), which describes the role's position in a Team, not
// how to run one loop.
//
// ToSpec is materialization, not positioning (ADR-018): it does not decide
// whether the resulting agent runs as a standalone agent, a subagent tool, or a
// team member. A Team member and a standalone agent built from the same class
// therefore share an identical Spec — and so an identical Engine once Build
// resolves the model and tools — which is what lets an application move a role
// between single-agent and multi-agent deployments without re-describing it.
func (c AgentClass) ToSpec() agent.Spec {
	return agent.Spec{
		Instructions: c.Instructions,
		Model:        c.Model,
		Tools:        c.Tools,
		LoopPolicy:   c.LoopPolicy,
		InputSchema:  c.InputSchema,
		OutputSchema: c.OutputSchema,
	}
}
