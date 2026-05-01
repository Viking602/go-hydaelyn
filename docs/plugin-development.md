# Plugin Development

## Plugin Model

Hydaelyn uses an internal `plugin.Registry` with `type/name` keys for runtime
composition experiments and tests.

Supported plugin types:

- `provider`
- `tool`
- `planner`
- `verifier`
- `storage`
- `memory`
- `observer`
- `scheduler`
- `mcp_gateway`

## Recommended Public Integration

Most application code should integrate through the public runner surface:

```go
runner := hydaelyn.New(hydaelyn.Config{
	PolicyEngine:  customPolicy,
	StoreProvider: customStore,
})
```

Use public interfaces for extension work:

- `provider.Driver`
- `tool.Driver`
- `policy.Engine`
- `hydaelyn.Planner`
- `hydaelyn.StoreProvider`
- `hydaelyn.OutputGateway`

## Planner Plugins And Dataflow

Planner integrations can emit task-level dataflow contracts through
`hydaelyn.Task` / `hydaelyn.TodoPlan`:

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
