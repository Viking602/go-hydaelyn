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

## Context-Aware Compaction

`api.TaskBudget.MaxTokens` limits cumulative token spend across an agent run; it
is not a model context-window setting. Derive a usable per-request history target
from the selected model after reserving room for output, reasoning, tool schemas,
and provider framing, then set `LoopPolicy.ContextTokenTarget`:

```go
engine.LoopPolicy.ContextTokenTarget = modelContextWindow * 3 / 4
```

Implement `agent.TargetContextManager` to receive that target in `CompactTo`.
When the target is positive, the engine invokes `CompactTo` before every model
request, including the first request and requests following tool results. The
manager owns model-appropriate token estimation, returns unchanged history when
it already fits, and must preserve complete tool turns and framework-owned skill
context. Existing `ContextManager` implementations remain compatible; their
`Compact` method is used as a best-effort fallback but cannot guarantee a token
target because it does not receive one.

## OpenAI Wire APIs

The OpenAI provider uses Chat Completions by default, including when
`Config.WireAPI` is empty. Select the Responses API explicitly for Codex models:

```go
providerDriver := openai.New(openai.Config{
	APIKey:  os.Getenv("OPENAI_API_KEY"),
	WireAPI: openai.WireResponses,
})
```

`openai.WireChatCompletions` selects `/chat/completions`;
`openai.WireResponses` selects `/responses`. Selection is driver configuration,
not model-name inference, so a request's model can change without silently
changing its wire protocol.

Responses requests do not support `agent.Input.StopSequences`. A non-empty stop
sequence list returns an error before the HTTP request is sent.

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
its managed request. `ExtraBody` cannot override protocol-managed fields:

- Chat Completions: `model`, `messages`, `tools`, `stream`, `stream_options`,
  `stop`, `reasoning`, and `response_format`
- Responses: `model`, `input`, `tools`, `stream`, `include`, `reasoning`, and
  `text`

Responses requests always include `reasoning.encrypted_content` so opaque
reasoning can be replayed in stateless and zero-data-retention tool loops. An
`include` array supplied through `ExtraBody` is merged with that required value
and deduplicated.

## Opaque Provider State

Responses turns store the terminal API `output` array in
`message.Message.ProviderState`. Hydaelyn keeps normalized text, reasoning, and
tool calls for provider-neutral consumers, while replaying the opaque output
items exactly before the following `function_call_output`. This preserves
reasoning items, function-call identity, encrypted fields, and phased Codex
messages across tool turns. Applications that persist or resume message history
should preserve `ProviderState` without interpreting or rewriting it.

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
