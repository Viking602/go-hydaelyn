# Quickstart

## 1. Minimal Deepsearch Run

```go
runner := host.New(host.Config{})
runner.RegisterProvider("echo", myProvider)
runner.RegisterPattern(deepsearch.New())

runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "echo", Model: "test"})
runner.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "echo", Model: "test"})

state, err := runner.StartTeam(context.Background(), host.StartTeamRequest{
	Pattern:           "deepsearch",
	SupervisorProfile: "supervisor",
	WorkerProfiles:    []string{"researcher", "researcher"},
	Input: map[string]any{
		"query":      "compare options for a Go research assistant",
		"subqueries": []string{"runtime design", "tool integration"},
	},
})
```

`deepsearch` now runs as:

1. parallel research tasks
2. optional verification tasks
3. an explicit synthesize task

Research tasks publish named outputs to the blackboard and the final synthesize task reads them explicitly.

## 2. Panel Task Board

Use `pattern/panel` when the user question should become a visible todo board
that experts claim, execute in parallel, cross-review, and synthesize from
verified findings.

```go
runner.RegisterPattern(panel.New())

state, err := runner.StartTeam(context.Background(), host.StartTeamRequest{
	Pattern:           "panel",
	SupervisorProfile: "supervisor",
	WorkerProfiles:    []string{"security", "frontend"},
	Input: map[string]any{
		"query":               "launch auth feature",
		"requireVerification": true,
		"todos": []any{
			map[string]any{"id": "security-review", "title": "review auth threat model", "domain": "security"},
			map[string]any{"id": "ui-review", "title": "review login UI", "domain": "frontend"},
		},
	},
})

timeline, err := runner.TeamTimeline(context.Background(), state.ID)
```

Panel uses worker profile names as default domains when `experts` is omitted,
so the `security` profile claims `domain: "security"` todos. Panel tasks use
typed reports for research, verification, and synthesis; research reports must
include at least one claim. The final result also carries
`Result.Structured["panel"]` with adopted findings, excluded claims, evidence,
todos, and participants.

## 3. Planner-Driven Teams

Planner tasks can now declare:

- `reads`
- `writes`
- `publish`

```go
plan := planner.Plan{
	Tasks: []planner.TaskSpec{
		{
			ID:      "research-1",
			Kind:    string(team.TaskKindResearch),
			Input:   "branch one",
			Writes:  []string{"research.branch-1"},
			Publish: []team.OutputVisibility{team.OutputVisibilityShared, team.OutputVisibilityBlackboard},
		},
		{
			ID:              "synth-1",
			Kind:            string(team.TaskKindSynthesize),
			AssigneeAgentID: "supervisor",
			Reads:           []string{"research.branch-1"},
			Publish:         []team.OutputVisibility{team.OutputVisibilityShared},
			DependsOn:       []string{"research-1"},
		},
	},
}
```

## 4. CLI

```bash
hydaelyn init .
hydaelyn new team.json
hydaelyn validate --recipe recipe.yaml
hydaelyn compile --recipe recipe.yaml
hydaelyn validate --request team.json
hydaelyn run --request team.json --events events.json
hydaelyn inspect team --events events.json
hydaelyn inspect events --events events.json --task task-1
hydaelyn evaluate --events events.json
hydaelyn replay --events events.json
```

## 5. Replay And Durable Dataflow

- `runner.TeamEvents(ctx, teamID)` returns the full event stream.
- `runner.TeamTimeline(ctx, teamID)` returns user-facing collaboration steps.
- `runner.ReplayTeamState(ctx, teamID)` rebuilds tasks, outputs, artifact refs, and blackboard exchanges.
- `TaskInputsMaterialized` and `TaskOutputsPublished` events make dataflow visible in replay and inspection.

## 6. Next Docs

- [Panel Task Board](panel.md)
- [Task Dataflow](task-dataflow.md)
- [Recipe Compiler](recipe.md)
- [Evaluation](evaluation.md)
- [Durable Execution](durable-execution.md)
- [Plugin Development](plugin-development.md)
