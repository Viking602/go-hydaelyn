# Runtime Extension Points

Venat currently exposes extension points at two levels:

1. `venat.Runner` runtime contracts configured through `api.Config`
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
runner := venat.NewDevelopment(api.Config{
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
it already fits, and must preserve complete tool turns, framework-owned skill
context, and the exact prefix through the last `message.Message.CacheBoundary`.
Existing `ContextManager` implementations remain compatible; their `Compact`
method is used as a best-effort fallback but cannot guarantee a token target
because it does not receive one.

## OpenAI Wire APIs

The OpenAI provider uses the Responses API when `Config.WireAPI` is empty.
Its default catalog is `gpt-5.6-sol`, `gpt-5.6-terra`, `gpt-5.6-luna`, and
`gpt-5.3-codex`. Select Chat Completions explicitly only for a compatible
endpoint or model that still requires it:

```go
providerDriver := openai.New(openai.Config{
	APIKey:  os.Getenv("OPENAI_API_KEY"),
	WireAPI: openai.WireChatCompletions,
})
```

`openai.WireResponses` selects `/responses`;
`openai.WireChatCompletions` selects `/chat/completions`. Selection is driver
configuration, not model-name inference, so a request's model can change
without silently changing its wire protocol.

Responses requests do not support `agent.Input.StopSequences`. A non-empty stop
sequence list returns an error before the HTTP request is sent.

## Provider Body Extras

Callers can pass provider-specific request body fields through
`agent.Input.ExtraBody`. This is intended for OpenAI-compatible extensions such
as `chat_template_kwargs` or sampling fields not modeled by Venat yet:

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

The OpenAI provider appends extra fields to the JSON body after Venat builds
its managed request. Chat Completions extras cannot override `model`,
`messages`, `tools`, `stream`, `stream_options`, `stop`, `reasoning`, or
`response_format`.

Responses extras cannot override `model`, `input`, `tools`, `stream`,
`instructions`, `previous_response_id`, `conversation`, or `prompt`; Venat
owns the complete request context and replays it statelessly. Requests send
`store: false` by default. Set `ResponsesOptions.Store` explicitly to opt into
provider-side response retention.

`include` values are merged and deduplicated with the required
`reasoning.encrypted_content` value. The provider also protects
`reasoning.effort` and `text.format`, which are derived from
`ThinkingBudget` and `ResponseFormat`; conflicting values return an error
instead of silently winning. Other `reasoning` and `text` members are merged.

Prefer `openai.ResponsesOptions` for stable Responses controls:

```go
store := false
extraBody, err := (openai.ResponsesOptions{
	MaxOutputTokens: 4096,
	Store:           &store,
	PromptCacheKey:  "tenant:agent:prompt-v2",
	PromptCacheOptions: &openai.PromptCacheOptions{
		Mode: openai.PromptCacheModeExplicit,
		TTL:  openai.PromptCacheTTL30Minutes,
	},
	Reasoning: &openai.ResponsesReasoningOptions{
		Summary: openai.ReasoningSummaryDetailed,
		Mode:    openai.ReasoningModePro,
		Context: openai.ReasoningContextAllTurns,
	},
	Text: &openai.ResponsesTextOptions{Verbosity: openai.TextVerbosityLow},
}).ExtraBody()
if err != nil {
	return err
}

result, err := engine.Run(ctx, agent.Input{
	Model:     "gpt-5.3-codex",
	Messages:  messages,
	ExtraBody: extraBody,
})
```

The typed builder validates enum values and prevents invalid negative output
limits. `ExtraBody` remains available for endpoint-specific fields not modeled
by Venat.

## OpenAI Prompt Caching

OpenAI automatically caches eligible repeated prefixes. Use
`PromptCacheKey` to improve routing affinity. When an application needs a
specific stable prefix, mark the last message carrying text in that prefix:

```go
stable := message.NewText(message.RoleSystem, systemPrompt)
stable.CacheBoundary = true
messages := []message.Message{stable, message.NewText(message.RoleUser, task)}
```

Both OpenAI wire protocols serialize the marker on their supported text content
block. Configure request-wide cache keys, mode, and TTL with
`openai.ResponsesOptions` or `openai.ChatCompletionsOptions`; older models may
support only automatic caching and reject explicit controls. The engine rejects
custom compaction output that deletes or changes the protected prefix, and the
built-in compactors keep it intact.
OpenAI may create at most four new cache writes per request; historical
breakpoints can remain in replayed context, and cache matching considers the
latest 80 breakpoints in the conversation.

`provider.Usage.CachedInputTokens` and `api.UsageRecord.CachedInputTokens`
report cache reads. `CacheWriteInputTokens` on the same types reports cache
writes when the provider supplies that counter. Both values flow through
worker persistence and `eval.SummarizeUsage`.

## Opaque Provider State

Responses turns store the terminal API `output` array in
`message.Message.ProviderState`. Venat keeps normalized text, reasoning, and
tool calls for provider-neutral consumers, while replaying the opaque output
items exactly before the following `function_call_output`. This preserves
reasoning items, function-call identity, encrypted fields, and phased Codex
messages across tool turns. Applications that persist or resume message history
should preserve `ProviderState` without interpreting or rewriting it.

## Provider Failure And Retry Contract

Provider adapters should return `*provider.Error` (or implement
`provider.ClassifiedError`) and map wire-specific status/code values to
`provider.ErrorKind`. `provider.IsRetryableError` recognizes typed rate-limit,
server, stream, network, and short-I/O failures without parsing error strings.

The built-in OpenAI and Anthropic drivers retry idempotent stream initiation
with `provider/shared.RetryPolicy`, including exponential backoff, optional
jitter, and `Retry-After`. Custom streaming drivers can use
`provider.OpenRetryingStream`; it retries only before response content is
emitted. A failure after partial output is returned to the durable task layer so
the task can resume from its checkpoint instead of replaying a partial request.

## Agent Turn Order

For an `agent.Engine` turn, Venat runs:

1. `hook.Chain.TransformContext`
2. `hook.Chain.BeforeModelCall`
3. provider stream
4. provider event callback from `agent.Input.OnEvent`
5. `hook.Chain.OnEvent`
6. tool calls through `hook.Chain.BeforeToolCall`
7. tool execution
8. `hook.Chain.AfterToolCall`
9. output guardrails on terminal assistant output
