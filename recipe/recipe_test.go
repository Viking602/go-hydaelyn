package recipe

import (
	"testing"

	"github.com/Viking602/go-hydaelyn/team"
)

func TestDecodeYAMLAndCompileRecipePlan(t *testing.T) {
	spec, err := Decode([]byte(`
name: deepsearch-structured
pattern: deepsearch
supervisor_profile: supervisor
worker_profiles:
  - researcher
input:
  query: recipe compile
flow:
  - mode: sequential
    steps:
      - mode: parallel
        steps:
          - task:
              id: branch-1
              kind: research
              input: architecture
              required_role: researcher
              writes: [branch.arch]
              publish: [shared, blackboard]
          - task:
              id: branch-2
              kind: research
              input: tooling
              required_role: researcher
              writes: [branch.tools]
              publish: [shared, blackboard]
      - task:
          id: synth
          kind: synthesize
          assignee_agent_id: supervisor
          reads: [branch.arch, branch.tools]
          publish: [shared]
`))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	compiled, err := Compile(spec)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if compiled.Request.Pattern != "deepsearch" || compiled.Request.SupervisorProfile != "supervisor" {
		t.Fatalf("expected request metadata, got %#v", compiled.Request)
	}
	if len(compiled.Plan.Tasks) != 3 {
		t.Fatalf("expected compiled tasks, got %#v", compiled.Plan.Tasks)
	}
	if compiled.Plan.Tasks[2].ID != "synth" {
		t.Fatalf("expected synth task to remain last, got %#v", compiled.Plan.Tasks)
	}
	if len(compiled.Plan.Tasks[2].DependsOn) != 2 {
		t.Fatalf("expected sequential synth to depend on both parallel branches, got %#v", compiled.Plan.Tasks[2])
	}
}

func TestCompileLoopAndToolSugar(t *testing.T) {
	spec := Spec{
		Pattern:           "deepsearch",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher"},
		Flow: []Step{
			{
				Mode:    "loop",
				ForEach: []string{"one", "two"},
				Template: &Task{
					ID:           "branch-{{index}}",
					Kind:         "research",
					Input:        "{{item}}",
					RequiredRole: team.RoleResearcher,
					Writes:       []string{"branch.{{item}}"},
				},
			},
			{
				Mode:  "tool",
				ID:    "search-call",
				Tool:  "search",
				Input: "lookup evidence",
			},
		},
	}
	compiled, err := Compile(spec)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if len(compiled.Plan.Tasks) != 3 {
		t.Fatalf("expected 3 compiled tasks, got %#v", compiled.Plan.Tasks)
	}
	if compiled.Plan.Tasks[0].ID != "branch-1" || compiled.Plan.Tasks[1].ID != "branch-2" {
		t.Fatalf("expected loop expansion, got %#v", compiled.Plan.Tasks)
	}
	toolTask := compiled.Plan.Tasks[2]
	if toolTask.AssigneeAgentID != "supervisor" {
		t.Fatalf("expected tool sugar to target supervisor, got %#v", toolTask)
	}
	if len(toolTask.RequiredCapabilities) != 1 || toolTask.RequiredCapabilities[0] != "search" {
		t.Fatalf("expected tool capability sugar, got %#v", toolTask)
	}
	if len(toolTask.DependsOn) != 2 {
		t.Fatalf("expected tool step to depend on prior loop tasks, got %#v", toolTask)
	}
}
