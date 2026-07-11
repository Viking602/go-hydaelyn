// Package contract provides public conformance tests for application-owned
// api.StoreProvider implementations.
//
// Under Position D, Hydaelyn owns the storage contract verbs and their
// conformance tests. Applications own schema and implementation. The framework
// ships no reference StoreProvider implementation; contract/internal/inmemfake
// exists only to verify the suite itself. Application implementations should
// call RunStoreProviderContractTests from their own test packages.
//
// See [ADR-012], [ADR-013], and the [v0.8.0 storage contract].
//
// [ADR-012]: https://github.com/Viking602/go-hydaelyn/blob/main/docs/adr/ADR-012-storage-contract-position-c.md
// [ADR-013]: https://github.com/Viking602/go-hydaelyn/blob/main/docs/adr/ADR-013-memory-kernel-vs-pipeline.md
// [v0.8.0 storage contract]: https://github.com/Viking602/go-hydaelyn/blob/main/docs/product-spec/v0.8.0/07-storage.md
package contract
