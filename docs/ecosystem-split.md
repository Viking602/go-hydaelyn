# Ecosystem Split Boundary

## Core Repo

`go-hydaelyn` now keeps the following in-tree:

- runtime core
- public `hydaelyn.Runner` and `api` Run/Task contracts
- blackboard, policy, provider, tool, worker, and MCP integration packages
- CLI and official examples

These are kept in-tree because they define the authoring and verification surface for the core runtime.

## Still Good Candidates For Extraction

The following remain good ecosystem-layer candidates when they outgrow the core repo:

- provider-specific packages
- storage backends
- OTEL / hosted observation integrations
- MCP tool bridges
- pattern packs
- richer evaluation suites and datasets

## Current In-Tree Incubation Rule

Incubating integrations should follow the same constraint:

- compile into `api.PipelineComponents` or `worker.AgentWorker`
- do not create a second runtime
- keep external-service assumptions out of the minimal core

If they are extracted later, the compatibility goal is to preserve the public contracts rather than the directory layout.
