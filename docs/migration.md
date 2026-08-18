# Migration Notes

## Pack eval cases moved to tests

`packs.Pack.EvalCases` remains for existing pack literals but is
deprecated. Shipped packs leave it empty. Hosts should keep smoke
suites in `_test.go` and call `eval.RunSuite` there so production pack
code does not import `eval`.

`transport/mcp.ToolDescriptor` now uses MCP wire names (`name`,
`description`, `inputSchema`) when serialized as JSON.

## Coding sandbox rejects FIFOs and replacement links

`coding.Workspace` reads Lstat the leaf before opening it. FIFOs, devices,
sockets, and directories return `ErrNotRegularFile` instead of blocking on
`os.Open`. In-workspace symlinks are resolved and the target must be a
regular file inside the workspace.

`WriteFile` creates with `O_EXCL` after re-resolving the parent. In-place
writes (`WriteText` / `RestoreText`) re-evaluate the path, re-check
containment, and require a regular file. Unix opens use `O_NOFOLLOW`;
Windows opens the leaf with `FILE_FLAG_OPEN_REPARSE_POINT` and rejects a
reparse point so a swapped symlink cannot leave the workspace.
Hosts do not need to change call sites.

## ProcessTool owns stdout/stderr pipes

`tool/kit.ProcessTool` now creates parent-owned `os.Pipe` pairs, assigns the
write ends to the child, and closes those write ends after `Start`. Copies
run concurrently with `Wait`. After the launched process exits, remaining
output is drained for up to 100ms; if copies are still blocked, the parent
closes the read ends. A descendant that still holds stdout or stderr cannot
block the tool, including on Windows where pipe deadlines do not interrupt a
blocked read. Cancellation closes the read ends immediately.
The previous `StdoutPipe`/`StderrPipe` path could close the pipes under the
readers during `Wait`. Hosts do not need to change call sites.

## Read APIs fail closed

`ReadyTasks`, `ReadyTasksContext`, `ActiveLeaseCount`,
`ActiveLeaseCountContext`, `ResponseOutbox`, `ResponseOutboxContext`,
`ResumeTokens`, and `ResumeTokensContext` now return `(T, error)`. Store and
unit-of-work failures are returned instead of collapsing to an empty slice,
`0`, or an empty map.

Hosts that treated a missing error as "no ready tasks / no lease / no outbox /
no resume tokens" must check the error. `0` from `ActiveLeaseCount` now means
the store confirmed there is no active lease.

`ResumeTokens` and `PendingResumeTokens` load pending tokens from the
configured store. Providers that do not advertise `SupportsListPending`
return `ErrInvalidConfiguration` instead of an empty map. Prefer
`PendingResumeTokens` for new crash-recovery code.

## Lease heartbeat starts at acquire

`worker.AgentWorker` and `worker.TeamRunner` now heartbeat immediately after
a successful lease acquire, then every `ttl/3`. Previously the first
heartbeat waited for the first ticker, so a slow ack/load or short TTL
could expire the lease and let recovery redispatch while the original
worker was still running. Hosts do not need to change call sites.

## Empty policy Effect is denied

A `PolicyEngine` that returns `(PolicyDecision{}, nil)` or any empty or
unknown `Effect` is now denied with reason `policy returned an unknown
effect`. The runner previously rewrote an empty Effect to `allow`. Hosts
that omitted `Effect` on an allow decision must set
`Effect: api.PolicyEffectAllow` (or `policy.EffectAllow`) explicitly.

`policy.Chain` applies the same rule: an engine that returns an empty
Effect fails closed instead of being treated as allow.

## OpenAI provider default: Responses API

An empty `openai.Config.WireAPI` now selects `/responses` instead of
`/chat/completions`. The default model catalog is now `gpt-5.6-sol`,
`gpt-5.6-terra`, `gpt-5.6-luna`, and `gpt-5.3-codex`.

Applications that depend on an OpenAI-compatible Chat Completions endpoint must
opt in explicitly:

```go
driver := openai.New(openai.Config{
	APIKey:  os.Getenv("OPENAI_API_KEY"),
	BaseURL: compatibleEndpoint,
	WireAPI: openai.WireChatCompletions,
})
```

For Responses requests:

- remove `StopSequences`, which the Responses API does not support;
- use `openai.ResponsesOptions.ExtraBody()` for output limits, storage,
  prompt-cache, reasoning, and text-verbosity controls;
- Responses requests send `store: false` by default; set
  `ResponsesOptions.Store` to `true` only to opt into provider-side retention;
- preserve `message.Message.ProviderState` across persistence and resume so
  encrypted reasoning and function-call items can be replayed;
- use `message.Message.CacheBoundary` only when an explicit cache breakpoint is
  required; automatic OpenAI prefix caching needs no marker;
- read `CachedInputTokens` for cache hits and `CacheWriteInputTokens` for cache
  creation usage.
- update column-mapped `api.UsageStore` implementations to persist the additive
  `cacheWriteInputTokens` field.

See [Runtime Extension Points](extensions.md#openai-wire-apis) for the complete
request and prompt-caching examples.

## v0.11 → v0.12 — project rename to Venat

v0.12.0 moves the project from `Hydaelyn` / `go-hydaelyn` to the canonical
`Venat` identity. This is a pre-v1 breaking module-path change; there is no
compatibility package or second CLI.

Update dependencies and imports:

```bash
go get github.com/Viking602/venat@v0.12.0
go mod tidy
```

| Old | New |
| --- | --- |
| `github.com/Viking602/go-hydaelyn` | `github.com/Viking602/venat` |
| `hydaelyn.NewDevelopment()` | `venat.NewDevelopment()` |
| `hydaelyn.NewProduction(...)` | `venat.NewProduction(...)` |
| `go install github.com/Viking602/go-hydaelyn/cmd/hydaelyn@...` | `go install github.com/Viking602/venat/cmd/venat@v0.12.0` |
| `hydaelyn version` | `venat version` |
| `.hydaelyn/skills` | `.venat/skills` |

After changing the import prefix, rename the root import qualifier from
`hydaelyn` to `venat`. Package-specific qualifiers such as `api`, `agent`, and
`tool` do not change.

Move product-owned skill files explicitly. Discovery scans `.venat/skills` and
reports a diagnostic when `.hydaelyn/skills` still exists; it does not merge
both directories. Explicit `AdditionalDirs` continue to work unchanged.

Three persisted skill wire identifiers deliberately keep the old prefix:
`hydaelyn_activate_skill`, `hydaelyn_read_skill_resource`, and
`hydaelyn.skill.context`. Do not rewrite stored transcripts or external
allowlists containing those values.

Published `github.com/Viking602/go-hydaelyn` versions remain immutable and
resolvable through the renamed GitHub repository's redirect. New releases use
only `github.com/Viking602/venat`. See
[ADR-019](adr/ADR-019-project-identity-venat.md) for the full decision.

## v0.7 → v0.8 — public framework release

v0.8.0 promotes Hydaelyn from "runnable runtime" to "publishable framework." Most v0.7 callers can upgrade with a single search-and-replace pass. The full plan, including the Path A (use a reference store impl) vs Path B (bring-your-own-provider) decision rule and an end-to-end ent-based provider template, lives in `docs/product-spec/v0.8.0/12-migration-guide.md`.

### Mechanical breaking changes

| Old | New |
| --- | --- |
| `api.Flow{BypassTaskStore, BypassPolicyEngine, BypassTaskExecutionLease, BypassHandoff, BypassResponseLayer, BypassOutputGateway}` | removed — recipes do not bypass runtime invariants |
| `api.ErrFlowBypass` | removed |
| `runner.ExecuteCommand(StartRunCommand)` returning `[]any{Run, RootTask}` | returns `api.StartRunResult{Run, RootTask, Created}` |
| `runner.ExecuteCommand(RequestApprovalCommand)` returning `[]any{Approval, Token}` | returns `api.RequestApprovalResult{Approval, Token}` |
| `runner.ExecuteCommand(AcquireTaskExecutionCommand)` returning `[]any{Lease, bool}` | returns `api.AcquireTaskExecutionResult{Lease, Acquired}` |

Use `Runner.StartRunWithResult` instead of the deprecated generic command path
when a coordinator must distinguish first creation from an idempotent retry.

`api.AgentProfile` itself is unchanged; new declarative fields land on the new `api.AgentDefinition` type instead.

### New surfaces to discover

| Need | Reach for |
| --- | --- |
| Declare an agent (instructions, model, tools, schemas, named hooks, triggers, governance) ahead of time | `api.AgentDefinition` (then `.AsProfile()` for runtime attribution) |
| Publish a system's callable surface to MCP / future renderers | `api.Capability` + `api.CapabilityManifest` |
| Cron / webhook / event / manual entrypoints | `transport/cron`, `transport/webhook`, `transport/event`, `api.Trigger` |
| Background worker that polls envelopes, leases, heartbeats, drains | `worker.Runtime` (plug your own `EnvelopePoller`) |
| Production-grade durable store | implement `api.StoreProvider` and run `contract.RunStoreProviderContractTests` against it |
| Local durable store for development | Implement `api.StoreProvider` against your own data stack — see `docs/product-spec/v0.8.0/12-migration-guide.md` for the ent-based template. The framework no longer ships reference storage backends; see ADR-012 (revised, Position D). |
| Bundle a vertical "research / support / devops / aiops" preset | `packs.Pack` + `packs.Registry` |
| Grade an agent run in CI | `eval.Eval` / `eval.Run` with assertions from `eval/assert` |

`AgentDefinition.Tools` now selects the exact executable tool subset.
`AgentDefinition.Capabilities` remains discovery/authorization metadata; it no
longer doubles as a tool list. Definition deployments persist resolved
`ToolMode`, `MaxIterations`, and `TTL` values so a resumed revision does not
inherit newer deployment defaults.

> Schedule-based triggers now live in `transport/cron`. The old
> `transport/scheduler` import path remains as a deprecated compatibility shim
> that re-exports `cron.Driver`, `cron.Options`, and `cron.New`; switch to
> `transport/cron` and update your imports.

Naming boundaries:
- `transport/cron`: time-based trigger transport; decides when a run starts.
- `workflow`: user-facing workflow definitions; compiles to `multiagent.Graph`.
- `multiagent.Scheduler`: dispatch decision primitive; decides which agent/task runs next.
- `flow` / `api.Flow`: preset adapter metadata; configures runtime adapters and never bypasses Runner invariants.

### What's not in v0.8.0 yet

- OpenAPI / CLI renderers for `CapabilityManifest` (MCP renderer ships).
- `api.ArtifactStore`, `api.BudgetPolicy`, `api.PolicyEnforcer` — types and interfaces deferred to v0.8.1+. The data shapes they will consume (UsageRecord, Budget, ContextSource) are stable in v0.8.0.
- `api.Memory[T Identified]` — ships in v0.8.0 as an optional plugin. The framework defines the verbs (Write/Read/Forget) and the identity contract (Identified). The application defines `T` and provides the storage backend; no reference implementation ships. See ADR-013 (revised) and `docs/product-spec/v0.8.0/13-memory-optional-plugin.md`.
- OpenTelemetry exporter (`observe/otel/`).

See `docs/release-notes/v0.8.0.md` for the full deferral list.

---

## 从旧 Team + Pattern 风格迁移到 Runner API

旧模型：

- pattern 或 host drive loop 直接推进 runnable task。
- mailbox、blackboard、queue lease 分散表达任务进度。
- 用户常需要记住多个入口名称。

当前模型：

- `venat.NewDevelopment()` 创建本地/测试 `Runner`；生产环境使用
  `venat.NewProduction(api.Config)`。
- `Runner.StartRun` 创建 `Run + RootTask`。
- `Runner.ExecuteCommand` 作为命令层入口，状态变更走 `api.StoreProvider + api.UnitOfWork`。
- planner/router adapter 创建一等 `api.Task`。
- `DispatchTask` 只写任务信封，不能授予执行权限。
- agent/component 必须 `AcquireTaskExecution` 后才能执行。
- `SubmitTypedReport` 是唯一正式任务提交协议。
- `ResponseTask` 和 `OutputGateway` 是唯一用户消息链路。
- `api.PolicyEngine.Authorize(ctx, api.PolicyRequest)` 统一覆盖 dispatch、blackboard、
  handoff、tool call、action、response publish。
- `needs_clarification` 进入 `waiting_user_input`，并通过 resume token 或用户输入恢复。

## 推荐入口

默认：

```go
runner := venat.NewDevelopment()
```

自定义配置：

```go
runner := venat.NewDevelopment(api.Config{
	PolicyEngine: customPolicy,
})
```

内部 `Runtime` 仍存在于实现层，但新代码应通过 `Runner` + `api` 契约扩展，
不要直接 import `internal/core`。

## API 映射

- old team start -> explicit constructor + `Runner.StartRun` / `Runner.QueueRun`
- team events -> `Runner.RunEvents`
- team timeline -> `Runner.RunTimeline`
- replay team state -> `Runner.ReplayRunState`
- pattern -> `api.Flow`
- message-only policy -> `api.PolicyEngine.Authorize(ctx, api.PolicyRequest)`
- direct user message write/publish -> `ResponseTask + ResponseOutbox + OutputGateway`

## Tool 与治理迁移

Side-effecting tool 必须补齐：

- `EffectType`
- `RequiresActionTask`
- `Idempotent`
- `PolicyTags` / risk metadata as needed

下游展示最终答案时优先消费 response outbox / `UserMessage`，不要让普通
agent 直接创建或发布用户可见消息。

## Durable runtime

当前版本支持：

- append-only events
- replay
- `api.StoreProvider / api.UnitOfWork` runtime contract
- durable mailbox and response outbox contracts
- approval / resume token / action attempt lifecycle

仍待后续补齐：

- 分布式 worker 调度
- 更完整的 governed tool bus
- 官方外部 durable storage driver
- 外部 observability backend
