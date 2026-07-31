# Recipe Compiler

The v2 main branch does not ship the old declarative recipe compiler as a
primary runtime surface. New code should compose `api.Run`, `api.Task`,
dependencies, blackboard selectors, and flows directly through
`venat.Runner`.

For direct orchestration, prefer:

```go
runner := venat.NewDevelopment()
run, _, err := runner.StartRun(ctx, api.StartRunCommand{Request: "..."})
```

Recipe-style authoring can be implemented by applications as a planner layer
that emits `api.CreateTaskCommand` values or a `api.TodoPlan` for a custom
`api.Planner`.

## Suggested Authoring Primitives

- `task`
- `sequential`
- `parallel`
- `loop`
- `tool`

These should compile to first-class runner primitives; they should not bypass
`TaskStore`, `PolicyEngine`, `TaskExecutionLease`, response outbox, or replay.
