# Executable AgentDefinition

## Decision

`api.AgentDefinition` is the version-controlled deployment contract for one
agent. A definition declares instructions, model policy, executable tools,
skills, named hooks, input and output schemas, loop limits, context sources,
triggers, and governance. `Capabilities` remains discovery metadata; only
`Tools` selects executable tools.

`worker.DefinitionDeployment` materializes a definition into an `agent.Engine`
and a `worker.SingleRunner`. Deployment must reject every configured field it
cannot execute. It must never downgrade an executable field to display-only
metadata.

## Immutable identity

A deployed definition is stored as `api.AgentDefinitionSnapshot`:

- `(Definition.ID, Definition.Version)` identifies one immutable revision;
- `Digest` is the lowercase SHA-256 digest of canonical definition JSON;
- a duplicate revision is accepted only when its digest matches;
- runtime assembly deep-copies mutable maps, slices, and JSON payloads so later
  caller mutation cannot change the deployed revision.

Run and task records retain the definition identity needed to resume with the
same executable contract. Resume fails closed when the stored definition,
version, or digest cannot be recovered.

## Admission and validation

Before installation, deployment validates model selection, tool availability,
skill availability, hook names, trigger drivers, schemas, loop limits, TTL, and
governance. Missing dependencies and unsupported fields are deployment errors.
This keeps a persisted definition honest: if it was accepted, the worker can
execute the contract it records.

## Compatibility

`api.AgentProfile` remains the small runtime identity used for attribution and
routing. `AgentDefinition.AsProfile` derives that identity without turning the
profile into a second deployment model.
