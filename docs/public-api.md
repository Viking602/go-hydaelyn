# Public API Freeze

## Stable Packages

The v1 public surface includes:

- `agent`
- `blackboard`
- `capability`
- `host`
- `mcp`
- `observe`
- `planner`
- `plugin`
- `security`
- `recipe`
- `scheduler`
- `team`
- `tool`
- `toolkit`
- `evaluation`

These packages follow the compatibility rules in [SemVer And Compatibility](semver.md).

## Additive Runtime Contracts

The following additive fields and behaviors are now part of the public contract:

### `planner.TaskSpec`

- `Reads []string`
- `Writes []string`
- `Publish []team.OutputVisibility`
- `VerifyClaims []string`
- `ExchangeSchema string`

### `blackboard.VerificationResult`

- `ClaimID string`
- `Status blackboard.VerificationStatus`
- `Confidence float64`
- `EvidenceIDs []string`

Supported claim semantics are now claim-level, not summary-level. A claim only counts as supported when:

- `status == supported`
- `confidence >= 0.7`
- `evidenceIds` is not empty

### Runtime / Replay Event Payloads

Task lifecycle events now carry additive execution metadata:

- `statusBefore`
- `statusAfter`
- `taskVersionBefore`
- `taskVersionAfter`
- `idempotencyKey`
- `workerId`
- `leaseId` when queue-backed execution is active

Consumers must tolerate these additive fields and may rely on them for replay validation.

## CLI Surface

`cli validate --recipe ... --strict-dataflow` is a supported additive validation mode. It reports:

- `unused_write`
- `missing_read`
- `ambiguous_producer`
- `synthesis_reads_unknown_key`
- `verify_task_has_no_claim_source`
- `blackboard_publish_has_no_schema`

## Internal Surface

These packages remain implementation detail:

- `providers/*`
- `transport/*`
- `tooltest`

`transport/http/control` remains internal and only exposes callable runtime
control capabilities. Hydaelyn does not ship endpoint catalogs, a
standard-library router, or a canonical `net/http` route tree for these
operations.
