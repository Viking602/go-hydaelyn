# Ecosystem Split Boundary

## Core Repo

`go-hydaelyn` now keeps the following in-tree:

- runtime core
- unified team/planner/blackboard/capability/scheduler abstractions
- CLI and official examples
- `recipe` compiler
- `evaluation` harness

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

`recipe` and `evaluation` are implemented in-tree now, but they still follow the same constraint:

- compile into `planner -> team -> host`
- do not create a second runtime
- keep external-service assumptions out of the minimal core

If they are extracted later, the compatibility goal is to preserve the public contracts rather than the directory layout.
