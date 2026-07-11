# Runtime Extension Points

Hydaelyn currently exposes extension points at two levels:

1. `hydaelyn.Runner` runtime contracts configured through `api.Config`
2. `agent.Engine` model/tool turn hooks configured through `agent.Input` and
   `hook.Chain`

## Which One Should I Use?

| Need | Use |
| ---- | --- |
| Override durable storage | `api.Config.StoreProvider` |
| Enforce dispatch, blackboard, handoff, tool, action, or response policy | `api.Config.PolicyEngine` |
| Deliver queued user messages | `api.Config.OutputGateway` |
| Replace intent / planner / validator / router / dispatcher / monitor stages | `api.Config.Pipeline` |
| Run a single agent model/tool loop | `agent.Engine` |
| Mutate provider requests or tool calls directly | `hook.Chain` on `agent.Engine` |
| Validate or block final assistant output | `agent.Input.OutputGuardrails` |

## Runtime Configuration

```go
runner := hydaelyn.NewDevelopment(api.Config{
	StoreProvider: durableStore,
	PolicyEngine:  policyEngine,
	OutputGateway: outputGateway,
	Pipeline: api.PipelineComponents{
		IntentAnalyzer: analyzer,
		Planner:        planner,
		Validator:      validator,
		Router:         router,
		Dispatcher:     dispatcher,
		TaskMonitor:    monitor,
	},
})
```

Runtime state changes should go through `Runner.ExecuteCommand(ctx, api.Command)`
or the typed `Runner` methods. The store interfaces remain available for
durable driver integration and inspection, but they are lower-level than the
Run/Task command contract.

## Agent Engine Hooks

```go
engine := agent.Engine{
	Provider: providerDriver,
	Tools:    tools,
	Hooks:    hook.NewChain(customHook),
}

result, err := engine.Run(ctx, agent.Input{
	Model:            "model-name",
	Messages:         messages,
	ExtraBody:        extraBody,
	OutputGuardrails: []agent.OutputGuardrail{guardrail},
})
```

## Provider Body Extras

Callers can pass provider-specific request body fields through
`agent.Input.ExtraBody`. This is intended for OpenAI-compatible extensions such
as `chat_template_kwargs` or sampling fields not modeled by Hydaelyn yet:

```go
result, err := engine.Run(ctx, agent.Input{
	Model:    "qwen",
	Messages: messages,
	ExtraBody: map[string]any{
		"chat_template_kwargs": map[string]any{
			"thinking": true,
		},
	},
})
```

The OpenAI provider appends extra fields to the JSON body after Hydaelyn builds
its managed request. Managed fields such as `model`, `messages`, `tools`,
`stream`, `stream_options`, `stop`, `reasoning`, and `response_format` are not
overridden by `ExtraBody`.

## Agent Turn Order

For an `agent.Engine` turn, Hydaelyn runs:

1. `hook.Chain.TransformContext`
2. `hook.Chain.BeforeModelCall`
3. provider stream
4. provider event callback from `agent.Input.OnEvent`
5. `hook.Chain.OnEvent`
6. tool calls through `hook.Chain.BeforeToolCall`
7. tool execution
8. `hook.Chain.AfterToolCall`
9. output guardrails on terminal assistant output
