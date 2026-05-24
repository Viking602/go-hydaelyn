# `contract/` — Ecosystem Adapter Contract Test Suites

This package is the **public** validation gate for any Hydaelyn ecosystem adapter
that implements a kernel interface. Spec anchor:
[`docs/product-spec/v0.8.0/05-storage.md`](../docs/product-spec/v0.8.0/05-storage.md)
§"Contract test suite" + [ADR-012](../docs/adr/ADR-012-storage-contract-position-c.md).

## Position D in one sentence

> The framework owns the contract and the contract test suite. The framework
> ships no `api.StoreProvider` implementation. Applications implement the
> contract against their own data stack.

If your provider passes `contract.RunStoreProviderContractTests`, it is correct
as far as Hydaelyn is concerned. The framework runs the same suite on every PR
via a non-exported in-memory adapter at `contract/internal/inmemfake/` — see
[ADR-012 (revised, Position D)](../docs/adr/ADR-012-storage-contract-position-c.md).

## Usage from an external adapter

```go
package mystorage_test

import (
    "context"
    "testing"

    "github.com/Viking602/go-hydaelyn/api"
    "github.com/Viking602/go-hydaelyn/contract"
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

## What this package contains (v0.8.0 scope)

| Suite | Status | Notes |
| ----- | ------ | ----- |
| `RunStoreProviderContractTests` | **Skeleton (Phase 2 of v0.8.0)** — ~35 named `t.Run` subtests, bodies `t.Skip("TODO: contract test, Phase 2")` | Test names ARE the contract surface. Per ADR-012 (revised, Position D) the framework ships no reference implementations; the framework's own CI exercises the suite via the non-exported `contract/internal/inmemfake` adapter. |
| `RunProviderContractTests` (model provider) | Stub for v0.8.x | Doc 01 / `provider/` adapter validation |
| `RunToolDriverContractTests` | Stub for v0.8.x | `tool/` driver validation |
| `RunPolicyEngineContractTests` | Stub for v0.8.x | `api.PolicyEngine` adapter validation |
| `RunOutputGatewayContractTests` | Stub for v0.8.x | `api.OutputGateway` adapter validation |

## Authoring rule (v0.8.0 Phase 2)

The order of operations for any new contract test is:

1. **Add the test name** to the appropriate suite as a `t.Run(name, ...)` with
   `t.Skip("TODO: contract test, Phase 2")` body.
2. **Update the table** in `05-storage.md` §"Contract test suite" if the count
   changes.
3. **Land at least one passing implementation** before flipping the body from
   skip to assertion. In the framework's own CI the implementation that closes
   this loop is `contract/internal/inmemfake` — the non-exported adapter the
   framework self-tests against. This guarantees the test specifies behavior
   that ≥1 working provider exhibits.
4. **Flip the body** to real assertions and verify the self-test still passes.

This sequence is what makes Position D credible: the contract is written
*independent of any specific implementation*, and the only implementation the
framework owns is the one it uses to self-test the contract.
