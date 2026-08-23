# ADR-022 UnitOfWork Capability Split

## Status

Accepted — 2026-08-15. Effective from v0.15.0. Additive for implementers
of the full `UnitOfWork`.

## Context

`api.UnitOfWork` is a single interface that returns every store. Callers
that only need runs and events still depend on handoffs, usage, and
dead letters. The contract suite is one entry point
(`RunStoreProviderContractTests`) even though providers often want to
prove one capability group at a time.

A hard split that removes methods from `UnitOfWork` would break every
host provider in the same release as the dual-model work (ADR-023).

## Decision

1. **Add narrow capability interfaces** on `api`:
   `RunStores`, `CollaborationStores`, `MessagingStores`,
   `GovernanceStores`, `IdentityStores`, `ObservabilityStores`.
2. **`UnitOfWork` embeds those interfaces** plus `Commit` / `Rollback`.
   Existing full implementations keep compiling. New helpers and
   internal callers may accept a narrow interface.
3. **`contract` grows grouped entry points** that run the existing named
   suites. `RunStoreProviderContractTests` remains the full gate and
   keeps its current subtest names (locked surface).
4. **Do not remove methods from `UnitOfWork` in v0.15.** A later ADR may
   make the composite optional once callers have moved.

## Anti-patterns rejected by this ADR

| Anti-pattern | Why it is wrong |
| ------------ | --------------- |
| Requiring every provider to implement only a subset in v0.15 | Breaks the current "one UoW, all required stores" contract |
| Renaming locked contract subtests to match the new groups | Breaks external adapter CI that asserts suite names |

## Impact

Hosts can type functions against `api.RunStores` without importing the
full composite. The conformance suite can be run per capability during
adapter development. Full providers still implement one `UnitOfWork`.

## References

- ADR-012 Position D
- `api/store.go`
- `contract/store_provider.go`
- Spec: `docs/product-spec/v0.14.0/03-storage.md`
