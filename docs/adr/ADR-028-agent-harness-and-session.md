# ADR-028 Agent Harness and the Session Store

## Status

Experimental — 2026-08-25; hardened for the v0.16.0 candidate on
2026-08-28. Introduces `agent.Harness` and the `session/` package. Not
covered by the compatibility promise: the exported surface, register
namespaces, and persisted JSON may change in any minor release until the
harness reaches capability parity with `agent.Engine` (see Convergence).
No other layer directly consumes the Harness API; packages importing `agent`
compile `session` transitively.

## Context

`agent.Engine` (ADR-015) is a strong bounded loop: tools, hooks, budgets,
output policy, step traces. It is also entirely in-memory within one call.
`Engine.RunMessages` takes messages and returns messages; if the process
dies mid-turn there is nothing to resume from, and the caller owns the
problem of deciding whether the provider call it lost had already reached
the model.

The durable Runner (ADR-017) solves that at a different altitude: it
persists tasks and runs, not model turns. Between the two sits a gap that
hosts keep re-implementing — a single agent, one conversation, resumable
after a crash, with the "did the provider call happen?" question answered
by the store rather than by guesswork.

`agent.Harness` fills that gap and `session/` is the storage contract it
drives. A session is an entry tree (messages, parent-linked), a usage
table, and a small set of registers under fixed namespaces
(`lane.leaf`, `lane.config`, `lane.state`, `lane.lastResult`, `op.meta`,
`op.state`). A run advances by committing register transitions; every
state a run can be interrupted in is a state it can be restored from.

`session.Memory` is a process-local implementation for development and
tests. Consistent with Position D (ADR-012), it is not a reference
backend: applications implement `session.Storage` against their own
database.

## Decision

1. **The harness is a durable single-agent driver, not a second Engine.**
   It drives exactly one lane (`main`) through one phase machine:
   checkpoint → assistant generation → checkpoint. It supports provider
   streaming, retries, and interruption. It does **not** support tools,
   hooks, budgets, output policy, sub-agents, or compaction. Any stream that
   carries a tool-call event terminates the run as `tools_unsupported`
   before provider retry classification, so tool calls are never dropped.

2. **Engine and Harness coexist; neither wraps the other.** Engine stays
   the capability-complete loop for callers that own their own
   persistence. Harness stays the durable driver for callers that want
   the store to own the loop state. They share the package and the
   provider-turn ceilings (`maxProviderTurnEvents` /
   `maxProviderTurnBytes`) but no control flow. Do not grow the harness a
   tool bus; that is the convergence work, and it needs its own ADR.

3. **Finalization writes are detached from the caller's context.**
   Once a provider stream has run, the store still says a generation is in
   flight. Recording what happened is no longer optional, and the caller's
   cancellation is no longer relevant to it: settlement uses
   `context.WithoutCancel` with an independent `LeaseTTL` deadline.
   Cancellation is still reported to the caller, while an unresponsive store
   cannot block `Close` forever.

   The dividing line is the irreversible external effect. Writes that
   *precede* the provider call (claiming the operation, reserving the
   response id) use the live context, because abandoning them costs
   nothing. Writes that *follow* it are detached.

4. **Faults are for broken state, not for unreachable state.** A fault
   retires the harness: every later call fails with `ErrHarnessFault` and
   the instance cannot recover. That is only correct when the session
   state — or the harness's reading of it — is incoherent:

   | Class | Examples | Handling |
   | ----- | -------- | -------- |
   | Corruption | `session.ErrCorrupt`, `ErrDuplicateID`, `ErrInvalidWrite`, `ErrNotFound` for a referenced branch, a register that will not decode, a required missing register (`ErrRegisterMissing`), a dangling entry/usage pointer, an invalid ancestry chain, or an unreachable phase | Fault; instance is done |
   | Transient / contention | cancelled or expired context, `session.ErrClosed`, transport failures, or a register CAS conflict | Returned unchanged or as `ErrLaneBusy`; the next call may succeed |

   Unclassified errors default to transient. Retiring a live harness over
   a timeout is worse than letting a caller retry into a real problem.

5. **`Close` waits and is repeat-safe.** Storage is caller-owned, so a closed
   harness may be replaced by `OpenHarness` over the same store. Lifecycle
   state has its own mutex and is never held across storage I/O. `Close`
   cancels the drive and waits for every tracked Harness operation; repeated
   calls wait on the same idle transition. Its context bounds each wait. On
   expiry it reports `ctx.Err()` and the store must not be reused yet.

6. **Retries back off exponentially with jitter.** The configured base
   delay doubles per attempt up to a 30s cap, and half of the resulting
   delay is randomized. A base of zero means the caller opted out of
   waiting. This mirrors `provider/shared.RetryPolicy` in shape; it is
   reimplemented locally rather than imported because that package is a
   provider-transport helper and the harness retries at the turn level.

7. **No speculative structure.** The harness ships the lane it has. There
   is no `Lane` handle type, no lane registry, no operation status field
   nobody reads, and no pending-work queue that is always empty. `main` is
   an internal storage key. When a second lane is actually needed, the
   abstraction gets designed then, against a real second case.

8. **The lane has durable CAS ownership.** `session.Storage.Commit` is
   all-or-nothing and register writes may compare the prior sequence. Prompt
   claims an idle lane in the same commit that writes its operation. Resume
   claims an expired or relinquished operation. A renewable `OwnerID` /
   `LeaseExpiresAt` record prevents two Harness instances from driving one
   provider turn concurrently; provider calls and retry waits renew at
   `LeaseTTL/3`. Every state transition checks ownership, and terminalization
   clears it atomically.

9. **Restore validates semantics, not only existence.** Operation IDs, phase
   combinations, prompt ancestry, lane leaf, reserved response IDs, and usage
   rows are checked before a resumed provider call. A contradictory snapshot
   faults instead of being interpreted leniently.

## Convergence

The end state is one durable loop with Engine's capabilities, not two
loops forever. The ordered gaps are: tool dispatch (the hard one — tool
results are entries, and a partially-dispatched parallel batch is a
resumable state), then hooks and budgets, then output policy. Until the
harness can drive a tool-using agent, it makes no compatibility promise
and no other layer should build on its exported types.

## Anti-patterns rejected by this ADR

| Anti-pattern | Why it is wrong |
| ------------ | --------------- |
| Persisting a settlement with the caller's cancelled context | Real backends reject the call; the run is left claiming a generation is in flight, and the next resume replays a provider call that already happened |
| Faulting the harness on a read timeout | Converts a retryable blip into a dead instance, and callers cannot tell the two apart from `ErrHarnessFault` |
| `fmt.Errorf("%w: %v", ErrX, err)` where `err` may be nil | Produces `%!w(<nil>)` and hides the real cause — split the "read failed" and "not found" branches instead |
| `Close` returning before the drive stops | The caller reuses or closes storage that a live drive is still writing to |
| Letting two Harness instances infer single-writer ownership | Both can replay the same external provider effect; register CAS plus a renewable lane lease makes one owner explicit |
| Treating a provider error as proof that no external effect happened | Network errors can arrive after delivery; leave the effect pending for explicit recovery |
| Shipping `Lane`/`Control.Status`/`PendingNextRun` for a future that has one lane, one status, and no queue | Write-only structure reads as a contract, and the next change has to preserve it |
| Adding a tool bus to the harness without an ADR | Tool dispatch is the convergence design, not an increment |

## Impact

Hosts get a resumable single-agent conversation over their own storage.
An interrupted run is either retried or materialized as an explicit
`interrupted` assistant entry, so the branch never silently loses a turn.
Transient storage trouble no longer costs the caller their harness.

The cost is a second loop in `agent/` with a narrower feature set, and the
convergence work above is now owed.

## References

- ADR-012 Position D — `session.Storage` is a contract; `session.Memory`
  is a development store, not a reference backend
- ADR-015 strong bounded agent loop — `agent.Engine`, the capability bar
- ADR-017 durable runner boundary — the altitude above this one
- `agent/harness.go`, `agent/harness_drive.go`, `agent/harness_restore.go`
- `session/storage.go`, `session/types.go`, `session/memory.go`
