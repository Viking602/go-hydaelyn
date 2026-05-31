# ADR-005 `CapabilityInvoker` Unified Governance Layer

## Status

Accepted

## Context

Before v0.5, although both LLM and Tool could be executed through the runtime, they followed two different invocation paths:

- LLM was invoked directly through `provider.Driver.Stream`
- Tool was invoked directly through `tool.Driver.Execute`

This prevented governance capabilities such as timeout, retry, permission, approval, and rate limit from being consolidated in a unified layer.

## Decision

- Introduce the `capability` package
- Use `CapabilityInvoker` as the unified entry point for capability invocation
- capability currently covers:
  - `llm`
  - `tool`
- The runtime connects provider/tool calls into the invoker through adapters:
  - `capabilityProviderDriver`
  - `capabilityToolDriver`
- capability results are uniformly consolidated into:
  - `Result`
  - `Usage`
  - `Error`

## Current Capabilities

- timeout
- retry
- permission
- approval
- rate limit

These capabilities are all injected through capability policy, rather than being scattered across the individual implementations of provider/tool.

## Impact

- The runtime can now observe and govern llm/tool calls in the same layer
- When MCP, search, and remote agent are connected later, there is no need to build a third governance model
- usage / timeout / error type already have a unified structure; going forward we only need to continue filling in cost and the coverage of external capabilities
