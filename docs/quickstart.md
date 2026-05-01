# Quickstart

## 1. Minimal Runner

```go
runner := hydaelyn.New()

run, err := runner.QueueRun(context.Background(), hydaelyn.StartRunCommand{
	Request: "compare options for a Go research assistant",
})
if err != nil {
	panic(err)
}

timeline, err := runner.RunTimeline(context.Background(), run.ID)
```

`hydaelyn.New()` starts the default in-memory runner. Pass `hydaelyn.Config`
only when overriding defaults, for example a custom policy engine or output
gateway.

`QueueRun` uses the primary runner path:

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
accepted under an active lease. If you want the library to bridge a dispatched
envelope to `agent.Engine`, use the optional `worker.AgentWorker` package.

## 3. Optional Config

```go
runner := hydaelyn.New(hydaelyn.Config{
	PolicyEngine: customPolicy,
})
```

The zero config is equivalent to the default config, so prefer `hydaelyn.New()`
for ordinary startup.

## 4. Flow Presets

`Flow` is the preset contract. A flow can select planner, router, policy, and
projector presets, but it cannot bypass runner primitives.

```go
err := runner.RegisterFlow(hydaelyn.Flow{Name: "deepsearch"})
```

## 5. CLI

The v2 CLI is deliberately small and library-first:

```bash
hydaelyn version
hydaelyn inspect-events --events events.json
hydaelyn help
```

## 6. Next Docs

- [Runner Runtime](orchestrator-runtime.md)
- [Migration Notes](migration.md)
- [Public API](public-api.md)
- [Durable Execution](durable-execution.md)
