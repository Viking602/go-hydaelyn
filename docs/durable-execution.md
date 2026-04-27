# Durable Execution

## Current Capabilities

Hydaelyn persists enough runtime detail to replay task execution, blackboard exchange flow, and verifier evidence, not only final summaries.

Legacy durable surfaces:

- `EventStore`
- `ReplayTeamState`
- `legacy/recipe.Compile(...)`
- `legacy/recipe.ValidateStrictDataflow(...)`
- `legacy/eval.Evaluate(...)`
- `pause / resume / abort`
- queue-backed `QueueTeam / RunQueueWorker / RecoverQueueLeases`
- task input/output dataflow events

The primary Orchestrator path exposes durability through Run/Task commands,
internal UnitOfWork storage, append-only events, response outbox, and replay
projection. New code should use `hydaelyn.New` / `orchestrator.Runtime`.

## Queue Contract

Queue leases now model a concrete execution attempt:

- `leaseId`
- `teamId`
- `taskId`
- `taskVersion`
- `attempt`
- `idempotencyKey`
- `workerId`
- `state`

The in-tree memory queue is still a local implementation, but the contract is version-aware: the same `taskId` on different task versions no longer aliases the same lease.

## Event Contract

Important team events include:

- `TaskScheduled`
- `TaskStarted`
- `TaskInputsMaterialized`
- `TaskCompleted`
- `TaskOutputsPublished`
- `ApprovalRequested`
- `TeamCompleted`

Task lifecycle payloads now record:

- `statusBefore`
- `statusAfter`
- `taskVersionBefore`
- `taskVersionAfter`
- `idempotencyKey`
- `workerId`
- `leaseId` when available

`TaskOutputsPublished` carries exchanges, artifact refs, and claim-level verification deltas needed for replay.

## Replay Invariants

Replay validation now checks more than shape equivalence:

- required event subset exists for completed tasks
- ordering constraints remain valid
- task version is monotonic
- `completed -> running` is illegal
- each task version has at most one authoritative completion event
- blackboard exchanges trace back to completed tasks
- replayed final state still matches the stored authoritative state after normalization

## Dataflow And Verifier Contract

Strict recipe validation is available through:

- `hydaelyn validate --recipe recipe.yaml --strict-dataflow`

Claim-level verifier evidence drives synthesis input eligibility. A finding is only reusable when its backing claims are:

- `supported`
- confidence-qualified
- linked to evidence IDs

## Current Limits

- The default queue is still in-memory.
- There is still no external production durable backend in-tree.
- Trace enrichment exists in event payloads, but full OpenTelemetry export remains a follow-up layer.
