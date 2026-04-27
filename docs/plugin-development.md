# Plugin Development

## Plugin Model

Hydaelyn uses `plugin.Registry` with `type/name` keys.

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

## Registration

```go
err := runner.RegisterPlugin(plugin.Spec{
	Type:      plugin.TypeProvider,
	Name:      "openai",
	Component: myProvider,
})
```

## Planner Plugins And Dataflow

Planner plugins can now emit task-level dataflow contracts directly through `planner.TaskSpec`:

- `Reads`
- `Writes`
- `Publish`

This lets a planner describe:

- what a task consumes
- what it produces
- whether the output is published to private session, shared session, or blackboard

The planner still compiles into the existing `planner -> team -> host` runtime path. There is no separate orchestration engine in-tree.

For legacy declarative authoring, use the [`legacy/recipe`](recipe.md)
compiler. It produces `legacy/planner.Plan` plus the matching
`legacy/host.StartTeamRequest`, and can be wrapped in a static planner plugin
when needed.

## Recommended Integration Order

Prefer integrating cross-cutting behavior through:

- stage middleware
- capability policy
- output guardrails
- observer plugins

instead of re-implementing timeout, retry, and permission handling independently inside each plugin.
