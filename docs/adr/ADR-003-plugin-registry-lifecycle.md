# ADR-003 Plugin Registry and Lifecycle

## Status

Accepted

## Context

The repository's original extension points used scattered registration:

- `RegisterProvider`
- `RegisterTool`
- `RegisterHook`
- `RegisterWorkflow`

The problem with this approach is that the extension surface is not unified, and it cannot express plugin-level configuration and governance boundaries.

## Decision

- Introduce `plugin.Registry`
- The registration key is fixed as `type/name`
- The first batch of unified plugin types:
  - `provider`
  - `tool`
  - `planner`
  - `verifier`
  - `storage`
  - `memory`
  - `observer`
  - `scheduler`
  - `mcp_gateway`
- `Runtime.RegisterPlugin` becomes the unified entry point
- The existing `RegisterProvider` / `RegisterTool` are retained as compatibility APIs, but synchronously write into the plugin registry underneath

## Lifecycle

- On registration, an entry is first placed into the registry
- Wiring is performed for the types known to the current runtime:
  - `provider` is wired to the provider map
  - `tool` is wired to the tool bus
  - `storage` is wired to runtime storage
  - `observer` is wired to the hook chain
- Other types first enter the registry, waiting for subsequent milestones to connect the real execution surface

## Impact

- The extension surface shifts from loose registration to a unified control plane
- Subsequent plugin configuration, observability, governance, and ecosystem decomposition now have a stable attachment point
