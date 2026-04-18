# Quickstart

## 1. Minimal Deepsearch Run

```go
runtime := host.New(host.Config{})
runtime.RegisterProvider("fake", myProvider)
runtime.RegisterPattern(deepsearch.New())

runtime.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "fake", Model: "test"})
runtime.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "fake", Model: "test"})

state, err := runtime.StartTeam(context.Background(), host.StartTeamRequest{
	Pattern:           "deepsearch",
	SupervisorProfile: "supervisor",
	WorkerProfiles:    []string{"researcher", "researcher"},
	Input: map[string]any{
		"query":      "parallel research",
		"subqueries": []string{"architecture", "tooling"},
	},
})
```

`deepsearch` now runs as:

1. parallel research tasks
2. optional verification tasks
3. an explicit synthesize task

Research tasks publish named outputs to the blackboard and the final synthesize task reads them explicitly.

## 2. Planner-Driven Teams

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

## 3. CLI

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

## 4. Replay And Durable Dataflow

- `runtime.TeamEvents(ctx, teamID)` returns the full event stream.
- `runtime.ReplayTeamState(ctx, teamID)` rebuilds tasks, outputs, artifact refs, and blackboard exchanges.
- `TaskInputsMaterialized` and `TaskOutputsPublished` events make dataflow visible in replay and inspection.

## 5. Next Docs

- [Task Dataflow](task-dataflow.md)
- [Recipe Compiler](recipe.md)
- [Evaluation](evaluation.md)
- [Durable Execution](durable-execution.md)
- [Plugin Development](plugin-development.md)
