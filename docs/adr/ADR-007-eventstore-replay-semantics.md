# ADR-007 EventStore and Replay Semantics

## Status

Accepted

## Context

Before v0.6, the state of the team runtime relied mainly on the in-memory `RunState` and `TeamStore` snapshots. The problems with this were:

- After an interruption, there was no reconstructable event stream
- pause / approval / abort only had terminal states, with no evidence of the process
- admin inspect could not replay the task lifecycle

## Decision

- Introduce `storage.EventStore`
- The first batch of event types:
  - `TeamStarted`
  - `PlanCreated`
  - `TaskScheduled`
  - `TaskStarted`
  - `TaskCompleted`
  - `TaskFailed`
  - `ApprovalRequested`
  - `CheckpointSaved`
  - `TeamCompleted`
- The runtime writes events synchronously during the team/task lifecycle
- `ReplayTeamState` reconstructs `RunState` from the event stream

## Current Semantics

- Creating a team records `TeamStarted`
- When a planner exists, `PlanCreated` is recorded
- When tasks are initially generated, `TaskScheduled` is recorded
- When a task enters execution, `TaskStarted` is recorded
- When a task succeeds/fails, `TaskCompleted` / `TaskFailed` is recorded
- `ask-human` records `ApprovalRequested`
- `AbortTeam` writes `CheckpointSaved`
- When a team completes normally, `TeamCompleted` is written

## Impact

- pause / resume / replay / abort now have a persistable foundation
- admin can view team events and replay team state
- The subsequent v0.7 checkpoint / idempotency / lease mechanisms can continue to extend on this event model
