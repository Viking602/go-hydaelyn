// Package contract is the public contract test suite for Hydaelyn ecosystem
// adapters. External adapter authors (custom api.StoreProvider, api.PolicyEngine,
// provider.Driver, api.OutputGateway, tool.Driver) import this package from
// their own test files to verify their implementation satisfies the framework
// contract.
//
// Position C (see ADR-012): the framework owns the contract and the contract
// test suite. Reference implementations under storage/{memory,sqlite,mysql,
// postgres} are starting points for forking, not production endorsements.
// Production-grade providers are expected to be written by downstream teams
// against their own data stack (ent / gorm / DBA-controlled DDL) and validated
// by RunStoreProviderContractTests.
//
// This package is part of the Extension tier (see docs/architecture-boundaries.md
// and docs/product-spec/v0.8.0/10-package-structure.md). It MUST NOT depend on
// any pack or example; it MAY depend on api/ and standard library only.
package contract
