# Storage Extensions and Conformance

## Position D remains unchanged

Venat owns storage verbs and invariants; applications own schemas and storage
implementations. The framework does not ship a public database adapter.
Application adapters continue to validate themselves with
`contract.RunStoreProviderContractTests`.

## v0.14.0 extensions

The store contract adds optional capabilities for:

- immutable `AgentDefinitionSnapshot` revisions;
- aggregate admission reservations;
- shared and exclusive resource claims;
- granular usage and pricing reconciliation needed by admission settlement.

Capability flags must describe the same behavior exposed by the store and its
unit of work. Admission reservations require atomic evaluation and persistence.
Resource claims require transactions because a batch must acquire or transition
every claim or none.

## Conformance gate

The public contract suite now covers the base CRUD and lifecycle contracts plus
lease compare-and-swap, event ordering, resume and outbox behavior, replay
determinism, capability self-consistency, multi-agent records, definition
snapshot immutability, admission reservations, and resource claims.

Optional suites skip only when the provider truthfully declares the capability
unsupported. A provider that declares support must pass the corresponding
behavioral contract.
