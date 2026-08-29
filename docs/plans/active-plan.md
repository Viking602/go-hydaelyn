# Active plan

## Agent SDK, optional durability, and streaming cutover

Status: implementation complete; focused tests, executable smoke scenarios, architecture proof, `make verify`, and `make ci-local` passed on 2026-08-29.

Sources of truth: [ADR-029](../adr/ADR-029-agent-sdk-and-optional-durable-runtime.md) for package placement, [ADR-030](../adr/ADR-030-stream-lifecycle-semantics.md) for streaming lifecycle/replay semantics, and the approved refactor plan for the implementation sequence.

### Deliverables

- [x] Application-neutral `agent.Request`, hooks, output frames, strict v1 continuation codec, resume, and effect interception.
- [x] Policy-free `orchestration` scheduler, application catalog/event examples, and bounded deterministic drive loop.
- [x] Execution-semantic `durable.Backend`, diagnostic conformance suite, private test backend, `durable.Runtime`, targeted resume, and application-owned approval example.
- [x] Direct-import package graph, executable examples, current documentation, migration guidance, and fail-closed absence/boundary gates.
- [x] Bounded real-time tool updates across tool, Agent frames, and transient durable semantics.
- [x] ADR-030 stream lifecycle documentation and P2 focused verification.
- [x] Final example smoke, package-graph proof, `make verify`, and `make ci-local`.

### Acceptance

- The production package graph contains only the approved capability families.
- Agent and orchestration package dependencies do not include durability.
- An application can execute one Agent without adopting persistence or orchestration.
- An injected backend can recover checkpoints, replay settled effects, and block unknown effects until explicit reconciliation.
- All examples execute as normal packages included by `go test ./...`.
- Current documentation recommends only direct imports and application-owned composition.
