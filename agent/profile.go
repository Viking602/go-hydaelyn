package agent

import "github.com/Viking602/venat/api"

// AgentProfile is the loop-layer description of an actor. Durable
// identity is api.AgentProfile / api.AgentDefinition (ADR-026).
type AgentProfile struct {
	ID           string            `json:"id"`
	Role         string            `json:"role,omitempty"`
	Model        string            `json:"model,omitempty"`
	Instructions string            `json:"instructions,omitempty"`
	ToolNames    []string          `json:"toolNames,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// Identity returns the durable api.AgentProfile subset (ID, Role, Metadata).
func (p AgentProfile) Identity() api.AgentProfile {
	return api.AgentProfile{
		ID:       p.ID,
		Role:     p.Role,
		Metadata: p.Metadata,
	}
}

// ProfileFromIdentity builds a loop-layer profile from a durable identity.
// Model, instructions, and tool names stay empty; fill them from Spec or
// AgentDefinition.
func ProfileFromIdentity(identity api.AgentProfile) AgentProfile {
	return AgentProfile{
		ID:       identity.ID,
		Role:     identity.Role,
		Metadata: identity.Metadata,
	}
}
