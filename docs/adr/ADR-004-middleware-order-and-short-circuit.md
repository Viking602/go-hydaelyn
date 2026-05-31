# ADR-004 Middleware Execution Order and Short-Circuit Mechanism

## Status

Accepted

## Context

The original `hook.Chain` only covered before and after model calls and tool calls, and could not express higher-level governance pipelines such as team, task, agent, and memory.

## Decision

- Introduce a unified `middleware.Chain`
- Adopt onion ordering:
  - Middleware registered earlier enters `before` first
  - Middleware registered later enters `after` first
- Not calling `next` is treated as a short-circuit
- The runtime currently wires in the following stages:
  - `team`
  - `task`
  - `agent`
  - `llm`
  - `tool`
  - `memory`
  - `planner` / `verify` / `synthesize` derived from the phase mapping
- `llm` / `tool` reuse the existing agent engine interfaces via the hook adapter

## Impact

- Governance logic such as timeout, retry, logging, tracing, and permission no longer needs to be embedded separately into each submodule
- Middleware now has independent start/stop capability
- The capability layer and durable runtime can continue to reuse the same pipeline going forward
