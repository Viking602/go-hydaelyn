package recipe

import (
	"strings"
	"testing"

	"github.com/Viking602/go-hydaelyn/team"
)

func TestRecipeValidationCompilesValidRecipe(t *testing.T) {
	spec := Spec{
		Pattern:           "deepsearch",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher"},
		Input:             map[string]any{"query": "validate recipes"},
		Flow: []Step{
			{
				Mode: "parallel",
				Steps: []Step{
					{Task: &Task{ID: "branch-a", Kind: "research", Input: "A", RequiredRole: team.RoleResearcher, Writes: []string{"research.a"}}},
					{Task: &Task{ID: "branch-b", Kind: "research", Input: "B", RequiredRole: team.RoleResearcher, Writes: []string{"research.b"}}},
				},
			},
			{
				Task: &Task{ID: "synth", Kind: "synthesize", AssigneeAgentID: "supervisor", Reads: []string{"research.a", "research.b"}, Publish: []team.OutputVisibility{team.OutputVisibilityShared}},
			},
		},
	}

	compiled, err := Compile(spec)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if compiled.Request.Pattern != "deepsearch" || compiled.Request.SupervisorProfile != "supervisor" {
		t.Fatalf("expected start request metadata to survive compile, got %#v", compiled.Request)
	}
	if len(compiled.Plan.Tasks) != 3 {
		t.Fatalf("expected 3 compiled tasks, got %#v", compiled.Plan.Tasks)
	}
	if got := compiled.Plan.Tasks[2].DependsOn; len(got) != 2 || got[0] != "branch-a" || got[1] != "branch-b" {
		t.Fatalf("expected synth to depend on parallel branches, got %#v", got)
	}
}

func TestRecipeValidationRejectsInvalidPatterns(t *testing.T) {
	tests := []struct {
		name string
		spec Spec
		want string
	}{
		{
			name: "unsupported step mode",
			spec: Spec{
				Pattern:           "deepsearch",
				SupervisorProfile: "supervisor",
				WorkerProfiles:    []string{"researcher"},
				Flow:              []Step{{Mode: "fanout"}},
			},
			want: "unsupported recipe step mode",
		},
		{
			name: "missing loop template",
			spec: Spec{
				Pattern:           "deepsearch",
				SupervisorProfile: "supervisor",
				WorkerProfiles:    []string{"researcher"},
				Flow:              []Step{{Mode: "loop", ForEach: []string{"a", "b"}}},
			},
			want: "loop step missing template",
		},
		{
			name: "task mode missing body",
			spec: Spec{
				Pattern:           "deepsearch",
				SupervisorProfile: "supervisor",
				WorkerProfiles:    []string{"researcher"},
				Flow:              []Step{{Mode: "task"}},
			},
			want: "task step missing task body",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Compile(tt.spec)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Compile() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestRecipeValidationRejectsMissingFields(t *testing.T) {
	tests := []struct {
		name string
		spec Spec
		want string
	}{
		{
			name: "pattern",
			spec: Spec{SupervisorProfile: "supervisor", WorkerProfiles: []string{"researcher"}},
			want: "recipe pattern is required",
		},
		{
			name: "supervisor profile",
			spec: Spec{Pattern: "deepsearch", WorkerProfiles: []string{"researcher"}},
			want: "recipe supervisor_profile is required",
		},
		{
			name: "worker profiles",
			spec: Spec{Pattern: "deepsearch", SupervisorProfile: "supervisor"},
			want: "recipe worker_profiles must not be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := CompileStartTeamRequest(tt.spec)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("CompileStartTeamRequest() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}
