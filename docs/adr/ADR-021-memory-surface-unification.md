# ADR-021 Memory Surface Unification

## Status

Accepted — 2026-08-15. Effective from v0.15.0. Amends ADR-013. The
`memory/` package is removed in a later minor after the deprecation
window (not in v0.15.0).

## Context

ADR-013 made `api.Memory[T Identified]` the optional plugin contract:
`Write` / `Read` / `Forget`, isolation by `(Scope, SubjectID)`. A second
public surface later appeared in `memory/`: `Put` / `Get` / `Query` /
`Delete` with a weaker `Identified` (ID only) and a `Query` /
`EmbeddingMatch` vocabulary. Package docs on `memory/` incorrectly called
that second surface "v0.8.0+ canonical."

The two method sets are not interchangeable. Applications and recipes
cannot share one backend type. The duplicate also re-imports retrieval
shape (`EmbeddingMatch`) into a kernel package, which ADR-013 rejected.

## Decision

1. **`api.Memory[T api.Identified]` is the only Memory contract.**
   Isolation, verbs, and optionality stay as ADR-013 specified.
2. **`memory/` is a deprecated compatibility package.** It keeps the
   historical `Put`/`Get`/`Query`/`Delete` types so existing importers
   compile, and its godoc points at `api.Memory`. It is not an alias of
   `api.Memory` because the method sets differ; a forced alias would be a
   silent behavioral break.
3. **Do not add `memory/inmem` or any other framework Memory backend.**
   Position D still applies.
4. **Delete `memory/` in a later minor** once callers have moved. That
   deletion is a separate PR and requires an ADR-021 amendment or a
   short follow-up ADR.

## Anti-patterns rejected by this ADR

| Anti-pattern | Why it is wrong |
| ------------ | --------------- |
| Calling `memory.Memory` canonical in docs or recipes | Conflicts with ADR-013 |
| Aliasing `memory.Memory` to `api.Memory` in v0.15 | Changes method names under the same type |
| Shipping `memory/inmem` as a migration aid | Reintroduces schema bias |

## Impact

New code uses `api.Memory`. Existing `memory` importers get a compiler
deprecation signal and a documented migration: implement `api.Identified`
(`Scope`, `SubjectID`) and map `Put`→`Write`, `Get`/`Query`→`Read`,
`Delete`→`Forget`. Retrieval (`EmbeddingMatch`) stays application or
recipe code.

## References

- ADR-013 (`api.Memory` optional plugin)
- ADR-012 Position D
- `api/memory.go`, `memory/memory.go`
- Spec: `docs/product-spec/v0.8.0/13-memory-optional-plugin.md`
