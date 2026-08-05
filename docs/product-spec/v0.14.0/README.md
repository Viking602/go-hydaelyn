# Venat v0.14.0 Product Specification

Status: **Unreleased**

This directory records the v0.14.0 control-plane additions. Historical release
records, including `v0.8.0/`, remain unchanged; the corresponding Git tag is the
source of truth for each released version.

## Scope

v0.14.0 turns declarative agent configuration into an executable, durable
contract and adds the coordination primitives required to run that contract
safely across restarts and concurrent workers.

1. [Executable AgentDefinition](./01-agent-definition.md)
2. [Governance and coordination](./02-governance.md)
3. [Storage extensions and conformance](./03-storage.md)

The release notes are maintained in
[`docs/release-notes/v0.14.0.md`](../../release-notes/v0.14.0.md).
