package toolkit

import (
	"testing"

	"hydaelyn/team"
)

func TestProfileAndTeamBuilders(t *testing.T) {
	profile := Profile(
		"researcher",
		WithRole(team.RoleResearcher),
		WithModel("openai", "gpt-test"),
		WithToolNames("search", "fetch"),
		WithPrompt("search broadly"),
		WithMaxTurns(3),
		WithMaxConcurrency(2),
	)
	if profile.Name != "researcher" || profile.Provider != "openai" || profile.Model != "gpt-test" {
		t.Fatalf("unexpected profile: %#v", profile)
	}
	request := Team("deepsearch", "supervisor", "research-a", "research-b").
		Input(map[string]any{"query": "hydaelyn"}).
		Metadata(map[string]string{"mode": "deep"}).
		Build()
	if request.Pattern != "deepsearch" || request.SupervisorProfile != "supervisor" {
		t.Fatalf("unexpected team request: %#v", request)
	}
	if len(request.WorkerProfiles) != 2 {
		t.Fatalf("expected worker profiles, got %#v", request.WorkerProfiles)
	}
}
