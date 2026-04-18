# Task 3 - persisted team state CAS

- Added `team.RunState.Version` with `omitempty` JSON support and default normalization to version `1` for loaded/runtime snapshots.
- Extended `storage.TeamStore` with `SaveCAS(ctx, state, expectedVersion) (newVersion, error)` plus explicit `storage.ErrStaleState` for stale writes.
- Memory team storage now performs optimistic locking under its existing mutex: `Save` preserves backward-compatible last-writer behavior while incrementing persisted versions, and `SaveCAS` rejects mismatched versions.
- Runtime persistence now threads team-state versions through `saveTeam` and queued persistence so resumed/driven teams and queued workers fail fast on stale snapshots instead of overwriting newer state.
- Verification passed: `go test ./storage ./host -run '^(TestTeamStoreCompareAndSwap|TestQueuedStatePersistRejectsStaleVersion)$'`.
