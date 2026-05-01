# Recipe Compiler

The v2 main branch does not ship the old declarative recipe compiler as a
primary runtime surface. New code should compose `Run`, `Task`, dependencies,
blackboard selectors, and flows directly through `hydaelyn.Runner`.

For direct orchestration, prefer:

```go
runner := hydaelyn.New()
run, _, err := runner.StartRun(ctx, hydaelyn.StartRunCommand{Request: "..."})
```

Recipe-style authoring can be implemented by applications as a planner layer
that emits `hydaelyn.CreateTaskCommand` values or a `hydaelyn.TodoPlan` for a
custom `Planner`.

## Suggested Authoring Primitives

- `task`
- `sequential`
- `parallel`
- `loop`
- `tool`

These should compile to first-class runner primitives; they should not bypass
`TaskStore`, `PolicyEngine`, `TaskExecutionLease`, response outbox, or replay.
