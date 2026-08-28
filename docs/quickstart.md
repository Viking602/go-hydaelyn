# Quickstart

## 1. Minimal Runner

```go
runner := venat.NewDevelopment()

run, err := runner.QueueRun(context.Background(), api.StartRunCommand{
	Request: "compare options for a Go research assistant",
})
if err != nil {
	panic(err)
}

timeline, err := runner.RunTimeline(context.Background(), run.ID)
```

`venat.NewDevelopment()` starts an in-memory runner with development policy
defaults. Production hosts use `venat.NewProduction` and must provide both
a `StoreProvider` and `PolicyEngine`.

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
task, err := runner.CreateTask(ctx, api.CreateTaskCommand{
	RunID:        run.ID,
	TaskID:       "research-1",
	OwnerAgentID: "agent-a",
})

env, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{
	RunID:         run.ID,
	TaskID:        task.ID,
	TargetAgentID: "agent-a",
})

lease, acquired, err := runner.AcquireTaskExecution(ctx, api.AcquireTaskExecutionCommand{
	RunID:      run.ID,
	TaskID:     task.ID,
	EnvelopeID: env.ID,
	HolderType: api.HolderAgent,
	HolderID:   "agent-a",
	TTL:        time.Minute,
})

err = runner.SubmitTypedReport(ctx, api.SubmitTypedReportCommand{
	RunID:       run.ID,
	TaskID:      task.ID,
	LeaseID:     lease.ID,
	HolderType:  api.HolderAgent,
	HolderID:    "agent-a",
	TaskVersion: task.Version,
	Report:      api.TypedReport{Status: api.ReportStatusSuccess, Summary: "done"},
})
```

Mailbox ack is not completion. A task can only complete through a typed report
accepted under an active lease. If you want the library to bridge a dispatched
envelope to `agent.Engine`, use the optional `worker.AgentWorker` package.

## 3. Optional Config

```go
runner := venat.NewDevelopment(api.Config{
	PolicyEngine: customPolicy,
})
```

For production startup:

```go
runner, err := venat.NewProduction(api.Config{
	StoreProvider: durableStore,
	PolicyEngine:  policy.DenySideEffectsByDefault(),
})
```


## 4. CLI

The v2 CLI is deliberately small and library-first:

```bash
venat version
venat inspect-events --events events.json
venat help
```

## 5. Next Docs

- [Runner Runtime](orchestrator-runtime.md)
- [Migration Notes](migration.md)
- [Public API](public-api.md)
- [Durable Execution](durable-execution.md)
