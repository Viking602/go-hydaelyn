# Agent Skills Full Lifecycle Implementation Plan

> **For agentic workers:** implement each checkbox in order, verify each task, and keep working until Final Acceptance is complete.

**Goal:** Deliver the complete local Agent Skills lifecycle for v0.10.0: trusted discovery, standards-compatible parsing, catalog disclosure, explicit and model-driven activation, bounded resource access, and compaction-safe context.

**Architecture:** `skill/` owns portable skill files, trusted-root discovery, resource manifests, and deterministic rendering. `agent/` owns runtime disclosure and two internal read-only tools (`activate_skill` and `read_skill_resource`); host tools and policy remain the only authority for side effects. `multiagent/` and `workflow/` only carry and snapshot configuration.

**Tech Stack:** Go 1.25, `gopkg.in/yaml.v3`, existing `tool.Bus`, standard `testing`, Sentrux, `make ci-local`.

---

## Scope And Rules

- Implement the five official lifecycle stages: discovery, parse, catalog, activation, and context retention.
- Keep existing `Spec.Skills` behavior as explicit eager activation.
- Add an explicit available-skill list for model activation; never scan implicit home or project paths.
- Treat caller-supplied discovery roots as the trust grant. Do not add remote downloads, marketplaces, signatures, package installation, or script auto-execution.
- Skill resources are read-only and must be pre-enumerated, path-contained, regular files, and size-bounded.
- `allowed-tools` remains advisory; it must never expand `Spec.Tools`, bypass `tool.Bus`, or grant policy permissions.
- Completion requires tests, docs, isolated consumer proof, `make ci-local`, and stable Sentrux quality/cycles.

## Quality Gates

- Official frontmatter fixtures parse exactly as specified.
- Invalid/ambiguous YAML, oversized files, symlinked resources, traversal, and unknown resources fail closed.
- Catalog activation exposes only configured skills and de-duplicates repeated activation per run.
- Custom context compaction cannot remove explicit or dynamically activated skill instructions.
- Workflow definitions defensively copy all skill slices.
- No new circular dependency, public `any`, business-word, lint, race, or architecture violations.

## Baseline

- Existing explicit chain works: `LoadDir -> Register -> Spec.Skills -> Build -> Engine.Run`.
- Missing: standard space-delimited `allowed-tools`, discovery, catalog, runtime activation/resource tools, compaction protection, workflow `Skills` clone, complete usage docs.
- Sentrux baseline: quality `6165`, cycles `0`.

## Execution Status

- [x] Plan approved by the user's instruction to plan and implement without deferral.
- [x] Implementation complete.
- [x] Independent evaluator approved.

### Task 1: Standards-Compatible And Bounded Skill Loading

**Files:**
- Modify: `skill/skill.go`
- Test: `skill/skill_test.go`

- [x] Parse scalar `allowed-tools` as whitespace-delimited entries while preserving whitespace inside parenthesized tool patterns.
- [x] Remove non-standard XML-description and reserved-name rejection rules.
- [x] Reject duplicate YAML mapping keys and empty provided `compatibility`.
- [x] Bound total `SKILL.md` bytes before allocation-heavy parsing.
- [x] Add official valid/invalid compatibility fixtures and regression tests.

Verification: `go test ./skill -run 'TestParse|TestLoadDir' -count=1` must pass.

### Task 2: Trusted Discovery And Resource Manifest

**Files:**
- Create: `skill/discovery.go`
- Create: `skill/resource.go`
- Test: `skill/discovery_test.go`
- Test: `skill/resource_test.go`

- [x] Discover direct skill children beneath caller-supplied roots in deterministic order.
- [x] Apply later-root precedence and report collisions without hidden implicit scanning.
- [x] Canonicalize loaded locations and enumerate every bundled regular file except `SKILL.md`.
- [x] Reject symlinks, traversal, changed directory identities, excess file counts, and oversized resources.
- [x] Expose exact-name resource reads only through the stored manifest.

Verification: `go test ./skill -run 'TestDiscover|TestResource' -count=1` and `go test -race ./skill -count=1` must pass.

### Task 3: Catalog And Runtime Activation

**Files:**
- Create: `agent/skill_tools.go`
- Modify: `agent/agent.go`
- Modify: `agent/spec.go`
- Modify: `agent/engine.go`
- Modify: `skill/skill.go`
- Test: `agent/skill_tools_test.go`
- Test: `agent/spec_test.go`

- [x] Add an explicit `AvailableSkills` configuration separate from eager `Skills`.
- [x] Render a metadata-only catalog with activation instructions.
- [x] Register internal read-only activation/resource tools only when available skills exist.
- [x] Restrict tool schemas and runtime lookup to the configured catalog.
- [x] De-duplicate activation per run and include compatibility plus resource names without eagerly loading resources.
- [x] Preserve existing explicit activation and tool-permission behavior.

Verification: `go test ./agent -run 'TestBuild.*Skill|TestSkill' -count=1` and `go test -race ./agent -count=1` must pass.

### Task 4: Compaction And Configuration Propagation

**Files:**
- Modify: `agent/engine.go`
- Modify: `multiagent/class.go`
- Modify: `api/agent_definition.go`
- Modify: `workflow/definition.go`
- Test: `agent/compaction_test.go`
- Test: `multiagent/class_test.go`
- Test: `workflow/definition_test.go`

- [x] Mark catalog and active-skill system messages as durable skill context.
- [x] Wrap custom compaction so explicit and runtime-activated skills are reconstituted exactly once.
- [x] Carry `AvailableSkills` through declarative class/config shapes.
- [x] Defensively copy both eager and available skill slices at workflow snapshot boundaries.

Verification: `go test ./agent ./multiagent ./workflow ./worker -count=1` must pass.

### Task 5: External Consumer And Documentation

**Files:**
- Create: `_examples/skills/main.go`
- Modify: `README.md`
- Modify: `docs/index.md`
- Modify: `docs/release-notes/v0.10.0.md`
- Modify: `.github/workflows/release.yml`

- [x] Add one runnable local-skill example covering discovery, registration, eager activation, model catalog activation, and resource reads.
- [x] Document trusted roots, advisory `allowed-tools`, resource behavior, and no script auto-execution.
- [x] Update release notes from explicit-only wording to the completed lifecycle.
- [x] Make the tagged consumer compile and exercise the public Skills wiring rather than only constructing a registry.

Verification: `go run ./_examples/skills` and Actionlint must pass.

### Task 6: Final Verification And Independent Review

**Files:**
- Modify: this plan's checkbox state only after evidence exists.

- [x] Run `gofmt`/`goimports` through project tooling.
- [x] Run targeted tests with repeated and race coverage.
- [x] Run `make ci-local`.
- [x] Run `git diff --check` and Actionlint.
- [x] Run Sentrux rescan, rules, cycles/quality comparison, and session end.
- [x] Obtain an independent evaluator review against every task and reopen failures.

Sentrux evidence: all configured rules pass, cycles remain `0`, coupling remains
`0.15345528455284552`, and quality improves from `6165` to `6166`. The session
comparison still reports one additional complex function (`24 -> 25`), without
a signal, coupling-grade, cycle, or rule regression.

## Final Acceptance

- [x] Official valid Skill fixtures load; invalid and ambiguous inputs fail predictably.
- [x] Trusted discovery and collision precedence are deterministic.
- [x] Catalog, explicit activation, model activation, resource reads, de-duplication, and compaction retention work end to end.
- [x] Skills never grant host tools or execute scripts implicitly.
- [x] Public docs and isolated tagged-consumer surface match the implementation.
- [x] Full CI and architecture gates pass with no quality-signal regression.
