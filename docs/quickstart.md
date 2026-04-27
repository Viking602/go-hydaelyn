# Quickstart

## 1. Minimal Orchestrator Run

```go
runner := hydaelyn.New(hydaelyn.Config{})

run, err := runner.QueueRun(context.Background(), hydaelyn.StartRunCommand{
	Request: "compare options for a Go research assistant",
})
if err != nil {
	panic(err)
}

timeline, err := runner.RunTimeline(context.Background(), run.ID)
```

`QueueRun` uses the primary runtime path:

1. `StartRun` creates `Run + RootTask`.
2. `IntentAnalyzer -> Planner -> Validator -> Router -> Dispatcher` advances the run.
3. `DispatchTask` writes mailbox outbox envelopes only.
4. `AcquireTaskExecution` grants the execution lease.
5. `SubmitTypedReport` is the task completion protocol.
6. Response tasks queue sanitized `UserMessage` records.
7. `OutputGateway` publishes queued messages.
8. `ReplayRunState` rebuilds state from events without redelivery or re-execution.

## 2. Executing A Task

```go
task, err := runner.CreateTask(ctx, hydaelyn.CreateTaskCommand{
	RunID:        run.ID,
	TaskID:       "research-1",
	OwnerAgentID: "agent-a",
})

env, err := runner.DispatchTask(ctx, hydaelyn.DispatchTaskCommand{
	RunID:         run.ID,
	TaskID:        task.ID,
	TargetAgentID: "agent-a",
})

lease, acquired, err := runner.AcquireTaskExecution(ctx, hydaelyn.AcquireTaskExecutionCommand{
	RunID:      run.ID,
	TaskID:     task.ID,
	EnvelopeID: env.ID,
	HolderType: hydaelyn.HolderAgent,
	HolderID:   "agent-a",
	TTL:        time.Minute,
})

err = runner.SubmitTypedReport(ctx, hydaelyn.SubmitTypedReportCommand{
	RunID:       run.ID,
	TaskID:      task.ID,
	LeaseID:     lease.ID,
	HolderType:  hydaelyn.HolderAgent,
	HolderID:    "agent-a",
	TaskVersion: task.Version,
	Report:      hydaelyn.TypedReport{Status: hydaelyn.ReportStatusSuccess, Summary: "done"},
})
```

Mailbox ack is not completion. A task can only complete through a typed report
accepted under an active lease.

## 3. Flow Presets

`Flow` replaces `Pattern` as the new preset contract. A flow can select planner,
router, policy, and projector presets, but it cannot bypass runtime primitives.

```go
err := runner.RegisterFlow(hydaelyn.Flow{Name: "deepsearch"})
```

## 4. Legacy Team + Pattern

Existing `host.StartTeam` and pattern packages remain callable for compatibility:

```go
teamRunner := hydaelyn.NewTeamRuntime(hydaelyn.TeamConfig{})
```

Use this path only for migration. New features should target `Run`, `Task`,
`TaskExecutionLease`, `TypedReport`, `Handoff`, `ResponseOutbox`, and replay.

## 5. CLI And Legacy Docs

The CLI still accepts Team request files while migration continues:

```bash
hydaelyn validate --recipe recipe.yaml
hydaelyn compile --recipe recipe.yaml
hydaelyn validate --request team.json
hydaelyn run --request team.json --events events.json
hydaelyn replay --events events.json
```

## 6. Next Docs

- [Orchestrator Runtime](orchestrator-runtime.md)
- [Migration Notes](migration.md)
- [Public API Freeze](public-api.md)
- [Task Dataflow](task-dataflow.md)
- [Recipe Compiler](recipe.md)
- [Evaluation](evaluation.md)
