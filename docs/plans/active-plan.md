# Hydaelyn Active Plan

> **Historical note (2026-06-10):** the old v0.2–v1.0 milestone list
> described the legacy v1 tree, archived on the `legacy-v1` branch. Several
> items formerly marked done (`BudgetEnforcer`, `RateLimitPerWindow`,
> scheduler backpressure, the OTel observer adapter, and CLI
> `run --provider`) were removed during the v2.0 framework cleanup and are
> not present in the current tree.
>
> Current planning authority lives under `docs/product-spec/`. The active
> product line is v0.8.x, with v0.9.0 tracked by
> `docs/product-spec/v0.9.0/README.md`.

## Status vocabulary

- `active`: currently being implemented or corrected
- `next`: next batch after the active work passes gates
- `queued`: agreed direction, blocked on prerequisite milestones
- `done`: completed and verified in the current tree
- `reserved`: declared vocabulary or contract, intentionally deferred

## Current conclusion

The current priority is runtime correctness, not new pattern expansion.
The audit found real release-blocking bugs in streaming transports,
provider stream parsing, scheduler determinism, webhook verification,
MCP JSON-RPC handling, stdio process lifetime, and JSON schema
serialization. Those fixes take precedence over feature work.

## Active remediation batch

### Correctness fixes `active`

- [x] Supervisor retry stale-decision bug: `reportForClass` now returns the
  latest finished report for a class.
- [x] Streaming thinking signature round trip: Anthropic thinking
  signatures/redacted thinking survive `provider.Event` →
  `stream.Frame` → normalized message conversion.
- [x] Cron handler lifecycle: handler panics are recovered and logged; driver
  shutdown cancels handler contexts without leaving orphaned cron work.
- [x] MCP StreamTransport: concurrent calls no longer serialize behind a
  full request/response mutex; notifications are ignored as responses; JSON-
  RPC error codes surface as typed errors.
- [x] MCP DialStdio: config env is additive to the parent environment, and
  the setup context no longer owns the child process lifetime after dial.
- [x] JSON-RPC protocol validation: malformed frames, invalid versions,
  empty methods, duplicate content length, and invalid responses fail early.
- [x] SSE writer lifecycle: `Close`, request-disconnect cancellation, and
  heartbeat comments are explicit and serialized.
- [x] Webhook verification: verify-after-body is supported through
  `VerifyRequest`, with a built-in HMAC-SHA256 helper.
- [x] Provider SSE truncation detection: mid-frame EOF now returns
  `io.ErrUnexpectedEOF` instead of a partial valid event.
- [x] Anthropic stream errors: upstream error type and message are preserved.
- [x] JSON schema strictness: `additionalProperties:false` serializes
  through `message.JSONSchema`.
- [x] Graph mapped input: missing mapped fields now surface a scheduler input
  resolution error instead of dispatching a child with empty input.

### v0.9.0 reserved contracts `active`

These surfaces are intentionally documented as v0.9.0 debt rather than
silently treated as complete in v0.8.x:

- [x] `agent.ToolSafety` / `agent.ToolPolicy`: v0.8.x declares the policy
  vocabulary, while concrete side-effect gating is through
  `tool.Definition.RequiresActionTask` and `worker.GovernedToolBus`.
  Engine-level retry/idempotency semantics consuming `ToolSafety` are
  reserved for v0.9.0.
- [x] `multiagent.Handoff`: the type and store contracts exist, but
  reference schedulers do not yet emit, validate against
  `AgentClass.InputSchema`, or persist Handoffs automatically. Full
  HandoffStore-backed routing is reserved for v0.9.0.
- [x] `FailureKindInsufficientEvidence`: the v0.8.0 scheduler-action table
  is advisory; no reference Scheduler automatically launches upstream
  evidence-gathering work for this failure kind.

## Current known gaps

- `Team.Start` / `Team.Resume` remain unresolved because the original spec
  signature imported the durable runner into `multiagent`, contradicting the
  current architecture boundary. v0.9.0 must resolve that layering before
  implementing the integration.
- `contract/integration` three-surface kill/resume tests depend on the Team
  integration above.
- Capability namespace constants and guardrails for `hydaelyn.self.*` remain
  planned v0.9.0 work.
- `packs/aiops/incident-triage/` is still a skeleton, not a worked demo.
- `ContextSource` remains a config struct rather than a `Fetch()` interface
  backed by `api.Artifact` storage.
- MCP server support and durable-runner streaming remain future work; the
  current tree only ships an MCP client and non-runner streaming surfaces.

## Verification gates

Before this remediation batch is considered complete:

- [ ] `gofmt` reports no touched files.
- [ ] Targeted regression tests for each fixed package pass.
- [ ] `make ci-local` (or the repository's equivalent local gate) passes, or
  any environment limitation is documented with the exact failing command.
- [ ] v0.9.0 reserved/debt docs match the implemented code surface.

## Operating notes

Current sentrux MCP surfaces may not be available in every harness run. When
they are unavailable, use repository-local structure checks, package tests,
and targeted code review to preserve the same architecture constraints:

- no new cycles
- no God files
- no new dependency from `multiagent` into the durable runner
- no speculative v0.9.0 interfaces without production feedback
