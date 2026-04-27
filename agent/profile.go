package agent

// AgentProfile is the stable public description of an actor the
// Orchestrator can route work to.
type AgentProfile struct {
	ID           string            `json:"id"`
	Role         string            `json:"role,omitempty"`
	Model        string            `json:"model,omitempty"`
	Instructions string            `json:"instructions,omitempty"`
	ToolNames    []string          `json:"toolNames,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}
