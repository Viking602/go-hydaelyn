# Plugin Development

## Plugin Model

Venat has no plugin registry. Extension is done by implementing public `api`
interfaces (and the `provider`/`tool` driver interfaces) and passing them in
through `api.Config` or the constructor that accepts them. There is no
runtime `type/name` lookup and no dynamic plugin loading.

Extension points:

- `api.StoreProvider` — durable storage backend (ADR-012, Position D; the
  framework ships no reference implementation).
- `api.PolicyEngine` — authorization for dispatch, blackboard, handoff, tool
  calls, actions, and response publish.
- `api.Memory[T Identified]` — optional keyed memory plugin (ADR-013); no
  reference implementation ships.
- `provider.Driver` — model provider drivers (Anthropic, OpenAI, scripted).
- `tool.Driver` — tool execution drivers.
- `api.Planner` — task-planning integrations.
- `api.OutputGateway` — response/output delivery.

## Recommended Public Integration

Most application code should integrate through the public runner surface:

```go
runner := venat.NewDevelopment(api.Config{
	PolicyEngine:  customPolicy,
	StoreProvider: customStore,
})
```

Use public interfaces for extension work:

- `provider.Driver`
- `tool.Driver`
- `policy.Engine`
- `api.Planner`
- `api.StoreProvider`
- `api.OutputGateway`

## Planner Plugins And Dataflow

Planner integrations can emit task-level dataflow contracts through
`api.Task` / `api.TodoPlan`:

- `ReadSelectors`
- `WriteTargets`
- dependencies and await mode
- task owner / route metadata

This lets a planner describe what a task consumes, what it produces, and how
other tasks should wait for it while still using the standard runner lifecycle.

## Recommended Integration Order

Prefer integrating cross-cutting behavior through:

- policy engine
- output guardrails
- hooks
- provider/tool drivers
- storage contracts

instead of re-implementing timeout, retry, approval, and permission handling
inside each plugin.
