# Architecture Safety Hardening Implementation Plan

> **For agentic workers:** execute the checked tasks in order and verify the final gates before changing this document to complete.

**Goal:** Apply the valid safety fixes from the proposed A-K improvement plan without replacing accepted storage and scheduler architecture.

**Architecture:** Keep ADR-016's pure scheduler and ADR-017's durable Runner boundary. Reuse existing policy, lease, store, and agent contracts; do not add duplicate SchedulerDecision or Dispatch stores.

**Tech Stack:** Go 1.25, standard library HTTP, existing contract tests, sentrux, Make gates.

---

## Scope And Rules

Finished means the production/development constructor boundary, bounded scheduler concurrency, streaming HTTP lifetime, cancellable façade replacements, and repository hygiene all pass the full local gates. The following proposal items are rejected because they conflict with accepted ADRs or have no current consumer: SchedulerDecisionStore/DispatchStore, feature-gated multi-agent stores, expression DSL, eight new Engine component interfaces, a production FailureInjector, generated maturity pages, and the `flow` package rename.

## Quality Gates

- `make verify`
- `make ci-local`
- `make architecture-check`
- `git diff --check`
- sentrux session comparison with no architecture regression

## Baseline

- Source: `main` at `b708152`.
- `make verify`: pass.
- sentrux quality signal: 6204; dependency direction clean, zero upward edges.

## Execution Status

- Harness fallback: implemented and reviewed through the available Codex parent/evaluator flow.
- The source proposal was assessed against ADR-012 and ADR-015 through ADR-018 before editing.
- Final sentrux signal is 6203 (-1) with slightly improved coupling, zero cycles,
  no rule violations, and a passing session comparison.

### Task 1: Separate development and production construction

**Files:** `venat.go`, `runner.go`, `api/config.go`, `public_api_test.go`

- [x] Add explicit development and validated production constructors.
- [x] Preserve the pre-v1 `New` entry point as Deprecated because Go cannot overload it with a new return signature.
- [x] Report the selected runtime mode and test missing production dependencies.

### Task 2: Bound zero-value scheduler concurrency

**Files:** `multiagent/drive.go`, `multiagent/drive_test.go`

- [x] Default a zero-value `DriveOptions` to four concurrent dispatches.
- [x] Require an explicit option for unbounded execution.
- [x] Verify bounded and explicit-unbounded behavior without sleeps.

### Task 3: Remove provider stream lifetime deadlines

**Files:** `provider/openai/driver.go`, `provider/anthropic/driver.go`, provider tests

- [x] Remove `http.Client.Timeout` from default streaming clients.
- [x] Keep a response-header timeout on cloned standard transports.
- [x] Preserve caller-supplied clients and verify both providers.

### Task 4: Add cancellable façade replacements

**Files:** `run.go`, `task.go`, `governance.go`, `response.go`

- [x] Add Context variants for replay, ready-task, active-lease, and response-outbox reads.
- [x] Deprecate background-context convenience methods in favor of cancellable/error-aware alternatives.
- [x] Make undeclared Store capabilities fail closed and deprecate the untyped command façade.

### Task 5: Align docs and remove local workflow state

**Files:** `.gitignore`, `docs/`, tracked `.omo/` and `.DS_Store` files

- [x] Document development versus production construction.
- [x] Route this assessed implementation plan from the docs index and active plan.
- [x] Remove local AI session state and macOS metadata from version control.

## Final Acceptance

- [x] All task checkboxes are complete.
- [x] Full quality gates pass.
- [x] Independent evaluator review passes.
- [x] Draft PR is open against `main`.
