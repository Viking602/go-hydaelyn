# Task 2 - blackboard exchange rules

- Added `blackboard.Exchange.Namespace`, `Version`, and `ETag` with `omitempty` JSON tags, plus `ErrExchangeConflict` and `UpsertExchangeCAS` for deterministic conflict checks.
- `UpsertExchangeCAS` preserves legacy unversioned behavior, auto-hashes payloads into ETags when CAS metadata is active, and rejects stale or same-version/different-payload writes for the same authoritative slot.
- Runtime blackboard publication now stamps exchanges with `task.Namespace` and `task.Version`, and verifier-published `supported_findings` exchanges also flow through CAS writes.
- Guarded synthesis reads now filter blackboard exchanges to `verify.*` namespaces only; `supported_findings` fallback remains available for non-guarded compatibility paths.
- Verification passed: `go test ./blackboard ./host -run '^(TestCollaborationBlackboard_RejectsStaleExchangeWrite|TestCollaborationBlackboard_RequiresVerifierNamespaces)$'`.
