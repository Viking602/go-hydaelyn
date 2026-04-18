# Durable Execution

## Current Capabilities

Hydaelyn now persists enough runtime detail to replay task dataflow, not only final summaries.

Current durable surfaces:

- `EventStore`
- `ReplayTeamState`
- `recipe.Compile(...)`
- `evaluation.Evaluate(...)`
- `pause / resume / abort`
- queue-backed `QueueTeam / RunQueueWorker / RecoverQueueLeases`
- task input/output dataflow events

## Event Types

Important team events now include:

- `TaskScheduled`
- `TaskStarted`
- `TaskInputsMaterialized`
- `TaskCompleted`
- `TaskOutputsPublished`
- `ApprovalRequested`
- `TeamCompleted`

`TaskOutputsPublished` carries named exchanges, artifact refs, and verification deltas needed for replay.

## Replay Scope

`ReplayTeamState` restores:

- task status
- task results
- structured outputs
- artifact ids
- blackboard exchanges
- verification state
- final team result

## Admin Surface

Current admin endpoints:

- `/teams`
- `/teams/{id}`
- `/teams/{id}/events`
- `/teams/{id}/resume`
- `/teams/{id}/replay`
- `/teams/{id}/abort`
- `/scheduler/drain`
- `/scheduler/recover`

## Current Limits

- The default queue is still in-memory.
- There is still no external production durable backend in-tree.
- Replay is deterministic over recorded events, but richer evaluation tooling remains an ecosystem-layer follow-up.
