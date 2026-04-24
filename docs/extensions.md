# Runtime Extension Points

Hydaelyn exposes four extension layers:

1. `Stage Middleware`
2. `Capability Policy`
3. `Output Guardrail`
4. `Engine Hook`

## Which One Should I Use?

| Need | Use |
| --- | --- |
| Observe team / task / planner / synthesize lifecycle | `Stage Middleware` |
| Enforce permission / approval / retry / rate limit / budget | `Capability Policy` |
| Validate or block the final assistant answer | `Output Guardrail` |
| Mutate provider request or tool call structs directly | `Engine Hook` |

## Recommended API

```go
runner.UseStageMiddleware(observe.RuntimeMiddleware(observer))

runner.UseCapabilityPolicy(capability.RequirePermissions())
runner.UseCapabilityPolicy(capability.RequireApproval())
runner.UseCapabilityPolicy(capability.BudgetEnforcer())

runner.RegisterOutputGuardrail("safe-json", safeJSON)
runner.UseOutputGuardrail(noSecrets)

runner.RegisterHook(customHook) // advanced / low-level
```

## Provider Body Extras

Business callers can pass provider-specific request body fields through
`AgentOptions.ExtraBody`. This is intended for OpenAI-compatible extensions
such as `chat_template_kwargs` or sampling fields not modeled by Hydaelyn yet:

```go
response, err := runner.Prompt(ctx, host.PromptRequest{
	SessionID: sessionID,
	Provider:  "openai",
	Model:     "qwen",
	Messages:  messages,
	Agent: host.AgentOptions{
		ExtraBody: map[string]any{
			"chat_template_kwargs": map[string]any{
				"thinking": true,
			},
		},
	},
})
```

The OpenAI provider appends extra fields to the JSON body after Hydaelyn builds
its managed request. Managed fields such as `model`, `messages`, `tools`,
`stream`, `stream_options`, `stop`, `reasoning`, and `response_format` are not
overridden by `ExtraBody`.

## Execution Order

For a prompt or task turn, Hydaelyn now runs:

1. Outer runtime stage middleware (`team` / `task` / `agent`)
2. `Stage Middleware` for `llm.transform_context`
3. `Engine Hook.TransformContext`
4. `Stage Middleware` for `llm.before`
5. `Engine Hook.BeforeModelCall`
6. `Capability Policy` for the LLM call
7. Provider event callback
8. `Stage Middleware` for `llm.event`
9. `Engine Hook.OnEvent`
10. Provider event normalization
11. Tool turns: `Stage Middleware` -> `Engine Hook` -> `Capability Policy` -> tool handler -> `Stage Middleware` -> `Engine Hook`
12. `Output Guardrail` on terminal assistant output
