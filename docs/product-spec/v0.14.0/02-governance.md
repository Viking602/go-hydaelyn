# Governance and Coordination

## Run admission

Aggregate governance is enforced through durable admission reservations. The
application supplies limits for concurrent runs, runs per window, credits per
window, and a trailing-failure breaker. The framework owns these invariants:

- preview is read-only and never grants capacity;
- reserve evaluates capacity and inserts the reservation atomically;
- each transition uses an expected version;
- reactivation re-evaluates concurrent capacity atomically;
- caller timestamps only define requested lifetimes; the runtime replaces them
  with its own clock;
- settlement derives priced credits and the durable agent-task or terminal-run
  outcome inside the same transaction.

A missing transactional admission backend fails closed whenever a definition
requires aggregate admission. Per-run `Budget.MaxCredits` and
`Budget.MaxActionCalls` remain unsupported by `DefinitionDeployment` and
`SingleRunner`; positive values are rejected instead of being silently ignored.

## Resource claims

Tasks may request opaque shared or exclusive resource keys. Claim acquisition
is tied to the task lease and is all-or-nothing. Renewals, releases, and expiry
use expected versions. A conflicting exclusive claim prevents dispatch rather
than allowing two workers to perform the same protected work.

## Usage accounting

Usage records distinguish model calls, tool calls, action calls, and legacy
execution totals. Granular records carry pricing state and idempotency metadata.
The runtime persists unresolved pricing explicitly and can reconcile it later;
it does not silently treat unknown cost as zero.

## Policy obligations

Policy decisions compose deterministically and may attach typed obligations.
Built-in enforcement covers Blackboard reads and writes, tool results,
handoffs, responses, and trace export. Unknown or malformed obligations fail
closed at their data boundary.
