package agent

import (
	"testing"

	"github.com/Viking602/venat/api"
)

func TestAgentProfileIdentityProjection(t *testing.T) {
	profile := AgentProfile{
		ID:           "agent-1",
		Role:         "reviewer",
		Model:        "test-model",
		Instructions: "be brief",
		Metadata:     map[string]string{"k": "v"},
	}
	identity := profile.Identity()
	if identity.ID != "agent-1" || identity.Role != "reviewer" || identity.Metadata["k"] != "v" {
		t.Fatalf("Identity() = %#v", identity)
	}
	round := ProfileFromIdentity(api.AgentProfile{ID: "agent-2", Role: "writer"})
	if round.ID != "agent-2" || round.Role != "writer" || round.Model != "" {
		t.Fatalf("ProfileFromIdentity() = %#v", round)
	}
}
