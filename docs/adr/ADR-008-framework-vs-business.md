# ADR-008 Responsibility Boundary Between Framework and Business (P-1)

## Status

Accepted — enforced starting from the v2.0 roadmap.

**Revised 2026-08-15:** directory names below that still say `orchestrator/**`,
`legacy/**`, or `flow/` as framework roots are historical. The current tree
is recorded in *Current package map*. Import seams are enforced by
`scripts/check-import-boundaries.sh` and documented in
`docs/architecture-boundaries.md` (ADR-024).

**Revised 2026-05-24:** v0.8.0 reconstruction (master spec `docs/superpowers/specs/2026-05-24-agent-layer-business-stance.md`, ADR-016) introduces a first-class `multiagent/` kernel package whose vocabulary includes terms like `Scheduler`, `Supervisor`, `Voting`, `Debate`. These are *framework primitives* for multi-agent coordination, not business vocabulary, and are explicitly exempted from the ban below. See *Revised — multi-agent primitive exception list* at the bottom of this ADR.

## Context

Hydaelyn's goal is to be a "Go-native multi-agent runtime" framework: to make the **capabilities** of "how to schedule tasks concurrently, coordinate multiple Agents, pass evidence around, and perform approval and disposition" thick and correct, so that developers can freely define their own business architecture on top of it (the incident-response reference architecture the user provided is only one possibility).

But the current `internal/core/types.go` directly defines:

- `TaskTypeSynthesis` / `TaskTypeReview` / `TaskTypeAction`
- `BlackboardItemSynthesis` / `BlackboardItemReviewResult` / `BlackboardItemActionResult`

And `internal/core/report.go` uses `TaskTypeAction` to branch its behavior decisions, and uses `BlackboardItemActionResult` to write the blackboard. This welds the **business semantics** of "attribution/review/disposition" into the framework. Consequences:

- Any domain that does not do incident response is forced to either ignore these constants or be semantically disturbed by them;
- The framework has to modify the core just to add or remove a business process;
- When users write new scenarios, they don't know which APIs are "framework" and which are "business examples."

## Decision

Draw two red lines that must not be crossed:

### 1. Framework Responsibilities (keep / complete)

| Capability | Form |
| ---- | ---- |
| Run / Task state machine | `Run`, `Task`, `RunStatus`, `TaskStatus`, `TaskType` serve only as string aliases (no semantics bound) |
| Blackboard | read/write + filter + Subscribe (M2.2); item kind named arbitrarily by the caller |
| Mailbox | routing + fan-out (M2.1) + Lease + Ack/DeadLetter |
| Handoff protocol | owner history + depth limit + cycle detection |
| Approval protocol | request / decision / ResumeToken |
| Trace | Span lifecycle |
| Tool invocation contract | `Tool.RequiresActionTask`, parameter/output schema, PolicyEngine hooks |
| Aggregation barrier | `AwaitMode{All,Any,Quorum}` + `OnDependencyFailed{Skip,Fail,Continue}` (M2.3) |

### 2. Business Responsibilities (developer side — the framework must not preset)

| Business concept | Implementation approach (framework provides the raw materials) |
| -------- | ------------------------ |
| Roles (Monitor / Reviewer / Hazard…) | `AgentProfile.Role` + `AgentProfile.Metadata` |
| Task kind semantics | `Task.Tags []string` + `Task.Type` (developer-defined string) |
| Blackboard item kinds | Caller passes in `BlackboardItemKind` (arbitrary string) |
| Attribution / review / disposition processes | Orchestrated by the developer using a combination of Tool + Handoff + Approval |
| Whether approval is triggered | Driven by the `Tool.RequiresActionTask` tool metadata; the framework does not recognize the literal "Action" |

### 3. Hard Constraints Effective Immediately

- Framework code (`internal/core/**`, `orchestrator/**`, `agent/**`, `blackboard/**`, `mailbox/**`, `tool/**`, `flow/**`, `hook/**`, `message/**`, `policy/**`, `provider/**`) **must not** add the following literals:
  `Synthesis` / `Review` / `ReviewResult` / `Action` (when used as a type word) / `ActionResult` / `Hazard` / `Incident`
- Existing occurrences (M3 cleanup target) are locked by `.sentrux/business-words.baseline` with baseline = 45. CI verifies "actual count ≤ baseline", only decrease is allowed.
- Framework code **must not** import `legacy/**`. `.sentrux/rules.toml` uses `[[boundaries]]` to lock down each clean module individually (`internal/core`, `orchestrator`, `agent`, `blackboard`, `mailbox`, `tool`, `flow`, `hook`, `message`, `policy`, `provider`); any PR that introduces a new dependency fails CI.
- Do not use sentrux's `[[layers]]` + `layer_direction`: in 0.5.7 this rule is too coarse and would block legitimate façade→runtime internal calls during the transition period; using explicit `[[boundaries]]` allows tightening incrementally.
- `no_god_files` is temporarily turned off: `archive/legacy-v1 host runtime` (fan-out=17) is legitimate residue; restart this rule after M6 removes `legacy/`.

### 4. Reasonable Exceptions Within Scope

- `_examples/`, `docs/`, `legacy/`, `pattern/` (before being moved out in M4.5) **are allowed** to freely use business words and domain types.
- `internal/cli/`, `internal/slow/` currently still import legacy, to be excised by M4.2 / M4.3; the rule scope only expands to all of `internal/**` after M4 ends.

## Impact

- Any PR can cite this ADR during review as grounds for rejecting business words from entering the framework.
- M3 (business-word stripping) becomes a hard breaking change for v2.0: strip `TaskTypeSynthesis/Review/Action` and `BlackboardItem*Result`, leaving them to be defined by developers.
- Score regression: compare `sentrux session_end` against the M0 baseline (quality_signal=6166, modularity=3233); modularity should recover after the M5 split.

## Revised 2026-05-24 — multi-agent primitive exception list

The v0.8.0 reconstruction introduces a `multiagent/` top-level kernel package (see ADR-016 and `docs/product-spec/v0.8.0/05-multi-agent-layer.md`). Its public surface uses the following nouns and verbs. These are framework primitives for multi-agent coordination — they are **not** business vocabulary and are explicitly exempted from the §3 hard constraint:

```
Scheduler        (multiagent.Scheduler interface, multiagent.SequentialScheduler, ...)
Supervisor       (multiagent.SupervisorScheduler, multiagent/supervisor.go)
Voting           (multiagent/voting.go)
Debate           (reserved for v0.9.0 schedulers)
Handoff          (multiagent.Handoff, api.HandoffStore — already in use)
Dispatch         (multiagent.Dispatch)
Team             (multiagent.Team)
AgentClass       (multiagent.AgentClass)
AgentInstance    (multiagent.AgentInstance — ADR-014 revised)
TypedReport      (the Blackboard write of agent.Result.Structured)
TeamState        (multiagent.TeamState)
```

Permitted locations for these primitives: `api/`, `multiagent/**`, `agent/**` (when referencing the Scheduler boundary), `worker/**` (when adapting to Scheduler dispatches), `internal/**` mirrors of the above, `_examples/`, `examples/`, `packs/`, `docs/`.

What is NOT exempted (the §3 ban still applies in full to):

```
incident, change, ticket, customer, sales, deploy, repository, document,
synthesis, hazard, lead, agent_review,
review (as a TaskType), action (as a TaskType)
```

The exception list is *closed* — adding a new framework primitive requires an ADR amendment. Adding a business word still requires removal during code review.

### Baseline update

`.sentrux/business-words.baseline` and `scripts/check-business-words.sh` are updated alongside the v0.8.0 reconstruction to:

1. Remove `Scheduler`, `Supervisor`, `Voting`, `Debate`, `Handoff`, `Dispatch`, `Team`, `AgentClass`, `AgentInstance`, `TypedReport`, `TeamState` from the banned-word list if they were ever included (most never were; this is defensive).
2. Keep the existing business-word baseline = 45 ceiling. CI continues to enforce "≤ baseline; only allows decrease."
3. Add a comment header to `business-words.baseline` referencing this ADR revision so future contributors can trace why the exception list exists.

### Compatibility with the original decision

The original §1/§2/§3 split holds:

- §1 (framework responsibilities) — extended to include multi-agent coordination primitives. The ADR-016 surface lands here.
- §2 (business responsibilities) — unchanged. Domain concepts (incident response, code review workflows, customer support routing) still live in `packs/` and recipes.
- §3 (immediately-effective hard constraints) — narrowed only by the closed exception list above. All other bans hold.

The intent of ADR-008 — "framework primitives are mechanism, business concepts are policy" — is unchanged. The revision recognizes that multi-agent coordination *is* a framework mechanism, and gives it vocabulary accordingly.

## Current package map (2026-08-28)

The live tree (ADR-024 five layers):

```
api/                 public contracts; no Venat imports
agent/               agent loop; must not import multiagent/
multiagent/          scheduler layer; must not import root, worker, internal/
worker/              integration layer; may import root; must not import packs/ or coding/
coding/              domain runtime; must not import worker/, packs/, or root
packs/               domain manifests; must not import coding/, worker/, or root
session/             experimental durable conversation store; imports only message/
internal/core        durable runner composition root
internal/memory      process-local development/test StoreProvider; not Position D
internal/*           domain services behind the runner
contract/            public StoreProvider conformance suite
```

`legacy/` and `orchestrator/` are gone. `internal/memory` is the default
development store and is not a shipped production backend.

## References

- Plan file: `/Users/viking/.claude/plans/sunny-hugging-goose.md`
- Enforcement config: `.sentrux/rules.toml`, `.sentrux/business-words.baseline`, `.github/workflows/ci.yml` (`architecture-gate` job)
- Master spec: `docs/superpowers/specs/2026-05-24-agent-layer-business-stance.md`
- Multi-agent layer design: `docs/product-spec/v0.8.0/05-multi-agent-layer.md`
- Related ADRs: ADR-015 (Strong Bounded Agent Loop), ADR-016 (Explicit Multi-Agent Scheduler), ADR-017 (Durable Runner Boundary), ADR-014 (Agent Ontology Stance — revised)
