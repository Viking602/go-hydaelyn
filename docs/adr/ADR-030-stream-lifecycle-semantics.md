# ADR-030 Stream Lifecycle Semantics

## Status

Accepted — 2026-08-29. This ADR defines the live lifecycle contract shared by provider streams and tool updates without merging their APIs.

## Context

Provider output and tool output cross different boundaries. A provider naturally exposes a pull stream because the Agent controls response consumption. A tool naturally exposes push updates because `Driver.Execute` owns execution and already receives an `UpdateSink` callback.

Treating both as one generic stream would either force tools to manage an unnecessary pull lifecycle or hide provider terminal behavior behind callbacks. Adding a second tool `StreamDriver` would create competing execution contracts and make interceptor, settlement, and replay order ambiguous. The two APIs instead need aligned lifecycle invariants and explicit durable semantics.

## Decision

### Keep domain-specific protocols

Provider output remains a pull protocol:

```go
type Stream interface {
    Recv() (Event, error)
    Close() error
}
```

Tool live output remains the one push seam already carried by the driver:

```go
type Driver interface {
    Definition() Definition
    Execute(context.Context, Call, UpdateSink) (Result, error)
}
```

No generic `stream` package, tool pull stream, or second driver interface is introduced.

### Common lifecycle contract

Both protocols represent one logical effect and obey these invariants:

1. Events are ordered per source. Concurrent sources have independent sequence spaces.
2. Delivery applies synchronous backpressure. The producer does not advance past a callback or receive boundary until the consumer returns.
3. Event count and decoded payload bytes are bounded. One provider turn permits at most 65,536 events and 64 MiB; one tool call permits at most 65,536 updates and 64 MiB under its domain accounting rules.
4. There is exactly one terminal outcome.
5. No event is delivered after the terminal outcome.
6. Partial delivery is never an automatic-retry boundary.
7. Live delivery is a transient side channel, not an exactly-once event log.

The common contract does not imply a common Go type. Provider event kinds and tool update kinds remain separate because their terminal and replay semantics differ.

### Provider pull lifecycle

`provider.EventDone` and `provider.EventError` are terminal. A conforming stream emits exactly one of them and then EOF. The Agent serially pulls events, enforces its event and byte limits, and synchronously emits corresponding `agent.Frame` values.

`provider.OpenRetryingStream` may reopen only an open failure or a receive failure before the first valid event, and only when `StreamRetryOptions.ShouldRetry` approves it. Cancellation and deadlines are hard stops. After any valid output, a receive or terminal failure becomes `provider.PartialStreamError`; `Retryable` is always false. The caller must reconcile possible partial delivery instead of silently duplicating it.

### Tool push lifecycle

`tool.UpdateKind` has two values:

- `UpdateProgress` carries `Message` and `Data`; it carries no output parts.
- `UpdateOutput` carries one or more `message.ContentPart` values.

Structured output exists only on the terminal `tool.Result`. `tool.Bus` deep-clones each callback value, overwrites caller-supplied identity, and assigns a strictly increasing per-call `Sequence` starting at one. Parallel calls may each emit sequence one; `OperationID` distinguishes their logical slots. A shared batch sink is serialized, and its cross-call order records actual arrival only.

The Bus permits at most 65,536 updates and 64 MiB of decoded update data per call. Invalid kinds or payloads return `tool.ErrToolUpdateProtocol`; limit violations return `tool.ErrToolUpdateLimit`. A sink error is returned synchronously and cancels the driver's child context. If a driver ignores the callback error, the Bus still returns the recorded error after `Execute` returns. None of these post-start failures carries `tool.ErrNotExecuted` proof.

Output parts accumulate in emission order inside a terminal driver wrapper. That wrapper is the `next` driver supplied to interceptors, so an outer durable interceptor observes the complete final result before settlement. If the terminal result omits `Parts`, the Bus fills them from accumulated output. If it supplies `Parts`, those parts must equal the accumulated output after `SyncLegacyContent`; disagreement returns `ErrToolUpdateProtocol`.

`Driver.Execute` returning a result or error is the tool terminal boundary. `Result.IsError` is a completed business result and is replayable. A Go error is an uncertain effect unless its chain contains `tool.ErrNotExecuted`. An update followed by `ErrNotExecuted` is a protocol contradiction and does not retain not-executed proof.

### Agent frame and history semantics

`agent.FrameToolUpdate` carries a deep-copied `*tool.Update`. For one ordinary tool turn, observable frame order is:

```text
provider ToolCall / Done
zero or more ToolUpdate frames
final ToolResult frame
next provider turn
```

`FrameToolUpdate` has no provider-event equivalent. `agent.Accumulator`, provider normalization, messages, step traces, continuations, and checkpoints ignore it. Only the terminal tool result enters Agent history.

### Durable settlement and replay

Provider attempts persist the complete terminal event sequence. Replaying a provider attempt for an unfinished execution may therefore emit those provider-derived frames again.

Tool attempts persist only the normalized final result or failure. A tool update is never written to an attempt payload, continuation, or checkpoint. Durable settlement completes before the Agent can emit the final `FrameToolResult`. If the process reopens before the following checkpoint and the settled tool attempt is replayed, the runtime emits the final tool-result frame without reconstructing historical update frames. Replaying an already terminal execution emits no frames.

An ordinary tool error after an update becomes an unknown attempt. Applications investigate and explicitly reconcile it; the runtime does not infer success from partial output or retry it automatically.

## Invariants

- Provider and tool APIs remain domain-specific and depend only inward according to ADR-029.
- Operation IDs are stable across retry, checkpoint, reopen, and replay.
- Per-call tool sequences are strictly increasing; no global cross-call sequence is promised.
- Consumer mutation cannot alter retained update data or a final tool result.
- Sink delivery failure is visible to the caller and cannot be mislabeled not-executed.
- Tool settlement receives normalized accumulated output before a final result frame is delivered.
- Durable recovery never fabricates historical tool updates.

## Alternatives rejected

### Generic stream package

Rejected because one abstraction would erase meaningful pull-versus-push and terminal differences while adding another top-level concept.

### Tool `StreamDriver`

Rejected because it would compete with `Driver.Execute` and split interceptor, validation, cancellation, and durable settlement behavior across two execution paths.

### Persist every live frame

Rejected because transient progress is not an exactly-once audit log. Persisting it would increase write amplification and still require application-specific delivery acknowledgements and retention policy.

### Retry after partial delivery

Rejected because provider output or a tool effect may already be externally visible. Automatic replay can duplicate text, side effects, or both.

## Consequences

Tool authors can produce progress or content in real time through the existing callback. Older one-shot drivers remain valid. `tool/kit` function callbacks can emit updates, and process tools publish output as it is read.

Sink implementations must be fast or apply deliberate backpressure. Applications that require durable progress must build an independently persisted projection with their own idempotency and acknowledgement rules.

Durable backends need no chunk table or update API. Their existing attempt payload stores complete provider terminal sequences and final tool outcomes only.

## Migration impact

Callers using string update kinds should use `tool.UpdateProgress` or `tool.UpdateOutput`. Output producers must send non-empty `Parts`; progress producers must not send parts. Consumers should handle `agent.FrameToolUpdate` and continue treating final `FrameToolResult` as the only history-bearing tool output.

A driver that returns `ErrNotExecuted` after invoking its update callback is invalid. Drivers must stop on callback error or accept that the Bus will return the recorded error and cancel their child context.

## Verification

The focused contract tests cover:

- progress/output order, identity enrichment, deep copying, and per-call sequence
- parallel shared-sink serialization and synchronous backpressure
- sink errors, child cancellation, protocol violations, and count/byte limits
- terminal result consistency and `Result.IsError`
- Agent frame order and accumulator exclusion
- unknown durable outcomes after an update
- settlement before the final result frame
- process-reopen replay without historical updates
- function-callback and process-tool producers

Release validation additionally runs executable examples, `make verify`, and `make ci-local`.
