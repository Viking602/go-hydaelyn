# Public API Freeze

## Stable Packages

The v1 public surface still includes:

- `agent`
- `blackboard`
- `capability`
- `host`
- `mcp`
- `observe`
- `planner`
- `plugin`
- `recipe`
- `scheduler`
- `team`
- `tool`
- `toolkit`
- `evaluation`

These packages follow the compatibility rules in [SemVer And Compatibility](semver.md).

## Additive v1.1 Dataflow Surface

The following additive fields are now part of the public model:

### `planner.TaskSpec`

- `Reads []string`
- `Writes []string`
- `Publish []team.OutputVisibility`

### `team.Task`

- `Reads []string`
- `Writes []string`
- `Publish []team.OutputVisibility`

### `team.Result`

- `Structured map[string]any`
- `ArtifactIDs []string`

### `blackboard.State`

- `Exchanges []blackboard.Exchange`

## Compatibility Rules

- Adding optional fields to frozen structs is allowed.
- Removing fields or changing field meaning requires a major version.
- Adding commands to the CLI is allowed.
- Removing commands from the CLI requires a major version.

## Internal Surface

These packages remain implementation detail:

- `providers/*`
- `transport/*`
- `tooltest`
