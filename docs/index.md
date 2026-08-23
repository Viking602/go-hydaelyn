# Documentation

## Start here

- [Quickstart](quickstart.md): install Venat and run the first task.
- [Public API](public-api.md): exported contracts and stability guarantees.
- [Examples](../_examples/): runnable programs for common integration paths.

## Runtime concepts

- [Runner Runtime](orchestrator-runtime.md): command execution and state ownership.
- [Durable Execution](durable-execution.md): storage, leases, events, and replay.
- [Task Dataflow](task-dataflow.md): task state and report propagation.
- [Workflow](workflow.md): declarative graph modeling.
- [Evaluation](evaluation.md): cases, assertions, matchers, and reporters.

## Extensions

- [Runtime Extension Points](extensions.md): storage, policy, output, pipeline,
  and agent hooks.
- [Plugin Development](plugin-development.md): plugin contracts and lifecycle.
- [Recipe Compiler](recipe.md): YAML configuration.

## Compatibility and releases

- [Migration Notes](migration.md): version-to-version API changes.
- [SemVer and Compatibility](semver.md): stability policy.
- [Product Release Status](product-spec/README.md): shipped and planned surfaces.
- [v0.15.0 Release Notes](release-notes/v0.15.0.md): latest published release.
- [Architecture Boundaries](architecture-boundaries.md): live import seams and ownership rules.

## Package map

| Path | Purpose |
|------|---------|
| `venat` (root) | `Runner` facade: construction, run/task commands, approvals, leases, action attempts, and event reads |
| `api/` | Public contracts: configuration, commands, models, stores, and policy interfaces |
| `agent/` | Bounded model/tool loop, output policy, tool safety, context management, and typed failures |
| `skill/` | Agent Skills discovery, parsing, registry, activation rendering, and bounded resource access |
| `multiagent/` | Teams, scheduling, dispatch, handoff, blackboard, voting, and supervision |
| `workflow/` | Declarative definitions compiled to `multiagent` graphs |
| `transport/` | MCP, cron, webhook, SSE, and event integrations |
| `provider/` | Anthropic, OpenAI, and scripted provider drivers |
| `tool/`, `hook/`, `policy/`, `message/` | Tool execution, hooks, policy, and messages |
| `memory/` | Deprecated compatibility Memory surface; use `api.Memory[T]` (ADR-021) |
| `worker/` | Integration layer: poll, lease, execute, team drive |
| `coding/` | Domain coding runtime; packs must not import it |
| `packs/` | Vertical pack skeletons |
| `eval/` | Evaluation cases, assertions, matchers, and reporters |
| `contract/` | Storage conformance tests |

## Architecture and maintenance

- [Architecture Boundaries](architecture-boundaries.md)
- [North Star Runtime](architecture/north-star-runtime.md)
- [Ecosystem Split Boundary](ecosystem-split.md)
- [ADR Index](adr/README.md)
- [GoLand Go Format Standard](architecture/goland-format-standard.md)
- [Active Plan](plans/active-plan.md)
- [Architecture Safety Hardening](plans/architecture-safety-hardening.md)
- [Future Backlog](plans/future-backlog.md)

Plans use a dual-track layout: `docs/plans/` contains the current execution
plan and backlog, while `docs/superpowers/` contains dated design and
implementation records from earlier workflows. The current execution source of
truth is `docs/plans/active-plan.md`.
