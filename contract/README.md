# `contract/`: Ecosystem Adapter Contract Test Suite

This package is the **public** validation gate for any Venat ecosystem adapter
that implements a kernel interface. Spec anchor:
[`docs/product-spec/v0.8.0/07-storage.md`](../docs/product-spec/v0.8.0/07-storage.md)
§"Contract test suite" + [ADR-012](../docs/adr/ADR-012-storage-contract-position-c.md).

## Position D in one sentence

> The framework owns the contract and the contract test suite. The framework
> ships no `api.StoreProvider` implementation. Applications implement the
> contract against their own data stack.

If your provider passes `contract.RunStoreProviderContractTests`, it is correct
as far as Venat is concerned. The framework runs the same suite on every PR
via a non-exported in-memory adapter at `contract/internal/inmemfake/`; see
[ADR-012 (revised, Position D)](../docs/adr/ADR-012-storage-contract-position-c.md).

## Usage from an external adapter

```go
package mystorage_test

import (
    "context"
    "testing"

    "github.com/Viking602/venat/api"
    "github.com/Viking602/venat/contract"
    "example.com/myorg/mystorage"
)

func TestMyProvider_ContractCompliance(t *testing.T) {
    contract.RunStoreProviderContractTests(t, func(t *testing.T) (api.StoreProvider, func()) {
        db := newTestDB(t) // your test setup
        provider := mystorage.New(db)
        cleanup := func() { _ = provider.Close(context.Background()) }
        return provider, cleanup
    })
}
```

## What this package contains

`RunStoreProviderContractTests` is the package's only exported contract suite.
It runs eight top-level groups with executable assertions:

| Group | Coverage |
| ----- | -------- |
| `CRUD` | Required store reads, writes, and selectors |
| `Transactions` | Commit, rollback, read-own-writes, and isolation |
| `LeaseCAS` | Version checks, ownership, transfer, and concurrent acquisition |
| `EventOrdering` | Append order, run filtering, and monotonic sequence |
| `ResumeAndOutbox` | Pending resume tokens, pagination, queue state, and FIFO order |
| `ReplayDeterminism` | Stable full and partial replay |
| `CapabilitySelfConsistency` | Declared optional capabilities match runtime behavior |
| `MultiAgentStores` | Handoff, team-state, and agent-instance persistence |

These are real tests, not placeholder stubs. `t.Skip` is used only for the
optional list-pending scenarios when a provider reports that capability as
unsupported. Required store behavior cannot be skipped. The framework verifies
the suite against `contract/internal/inmemfake` on every PR.

## Extending the suite

1. Add the smallest executable assertion to the relevant group. Create a new
   top-level group only for a distinct contract area.
2. Obtain a fresh provider through `ProviderFactory` and register cleanup with
   the test.
3. Gate a test with `t.Skip` only when the public capability contract declares
   the behavior optional.
4. Update [`07-storage.md`](../docs/product-spec/v0.8.0/07-storage.md) when the
   storage contract changes.
5. Run `go test ./contract` against the internal self-check adapter.
