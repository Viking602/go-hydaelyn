# Public API Freeze

## Root Package Default

`hydaelyn.New(hydaelyn.Config{})` now returns the primary orchestrator runtime.
Legacy Team + Pattern execution is available through
`hydaelyn.NewTeamRuntime(hydaelyn.TeamConfig{})` and direct `legacy/host`
imports.

## Stable Packages

The major-version public surface includes:

- `agent`
- `blackboard`
- `flow`
- `orchestrator`
- `policy`
- `provider`
- `tool`
- `transport/mcp`

Deprecated Team + Pattern compatibility packages live under `legacy/` and are
not the primary runtime surface.

These packages follow the compatibility rules in [SemVer And Compatibility](semver.md).

## Runtime Contracts

The primary contract is Run/Task orchestration. Legacy planner/team/panel
contracts remain under `legacy/` and are not the vNext default path.

### Orchestrator Runtime

The run-level orchestration contract is the preferred surface for new durable
adapters:

- `orchestrator.StartRun`, `QueueRun`, `ExecuteCommand`, `RunEvents`, `RunTimeline`, `ReplayRunState`
- `orchestrator.Run`, `Task`, `TaskEnvelope`, `TaskExecutionLease`, `TypedReport`
- `orchestrator.PolicyEngine.Authorize(ctx, PolicyRequest)`
- `orchestrator.ApprovalRequest`, `ResumeToken`, `ActionAttempt`
- `orchestrator.Flow` as preset metadata, not a state-transition bypass

Legacy `legacy/host.StartTeam`, `QueueTeam`, `TeamEvents`, `TeamTimeline`, and
`ReplayTeamState` stay callable through `NewTeamRuntime` and `legacy/host`
imports for the current migration window.

### Legacy Collaboration

Panel, deepsearch, recipe, evaluation, queue, storage, mailbox, scheduler, and
observe packages moved under `legacy/`. They remain available for migration,
but new orchestration work should not depend on them as primary architecture.

## CLI Surface

`cli validate --recipe ... --strict-dataflow` is a supported additive validation mode. It reports:

- `unused_write`
- `missing_read`
- `ambiguous_producer`
- `synthesis_reads_unknown_key`
- `verify_task_has_no_claim_source`
- `blackboard_publish_has_no_schema`

## Internal Surface

These packages remain implementation detail:

- `internal/runtime/*`
- runtime storage and UnitOfWork implementations
- mailbox outbox dispatchers
- scheduler/observe internals
- command handlers and transition tables
- replay/recovery internals

Legacy HTTP control helpers moved under `legacy/transport/http/control`.
Hydaelyn does not ship endpoint catalogs, a standard-library router, or a
canonical `net/http` route tree for these operations as part of the primary
runtime API.
