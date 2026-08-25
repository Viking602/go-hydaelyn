# Venat Future Backlog

This backlog is intentionally unversioned. Items listed here have no release
date or version commitment.

## Scheduler and durable multi-agent integration

- Resolve the `Team.Start` and `Team.Resume` integration boundary without
  importing the durable Runner into `multiagent`.
- Evaluate debate, MapReduce, and swarm schedulers after production feedback
  justifies their maintenance cost.
- Complete automatic handoff validation and persistence through the existing
  store contracts.

## Memory, context, and artifacts

- Define optional memory extraction and retrieval procedures over an
  application-provided `Memory[T]` implementation.
- Evaluate symbolic task context and knowledge-graph resolvers.
- Add an artifact contract only after concrete downstream storage requirements
  are available.

Venat does not plan to ship a general storage backend under
[ADR-012](../adr/ADR-012-storage-contract-position-d.md), or a framework memory
backend under [ADR-013](../adr/ADR-013-memory-kernel-vs-pipeline.md).

## Observability

- Add an OpenTelemetry exporter that maps the existing trace and event data to
  OTLP without changing Runner ownership.
- Validate scheduler, dispatch, agent-loop, step, and tool parent links against
  real collectors before supporting backend-specific integrations.

## Packs and external surfaces

- Replace skeleton packs with production-grade content only when each pack has
  a maintained owner and evaluation suite.
- Evaluate an MCP server surface and durable Runner streaming independently of
  the MCP client.
- Expand capability exports only when the high-level contracts remain neutral
  across provider, CLI, and transport consumers.

The active execution scope is documented in
[the current plan](./active-plan.md). The latest published release is
described in [the v0.15.4 notes](../release-notes/v0.15.4.md).
