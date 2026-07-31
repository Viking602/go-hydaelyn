package eval

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Viking602/venat/agent"
	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/provider/scripted"
	"github.com/Viking602/venat/worker"
)

// scriptedHarness is the optional capability a Harness advertises so runCase
// can drive the agent loop with a deterministic scripted provider through the
// worker bridge (the M0 wiring). DefaultHarness satisfies it.
type scriptedHarness interface {
	AgentID() string
	Model() string
	Script() []provider.Event
}

// runFailure is the synthetic assertion name used for framework-level failures
// (setup, execution, timeout) that are not an Assertion verdict.
const runFailure = "eval:run"

// isTerminalRunStatus reports whether s is a terminal run status. It mirrors the
// internal predicate (internal/core/state.IsTerminalRun), which is not exported
// on the public surface, so the harness re-hosts it here (see M0 spike note).
func isTerminalRunStatus(s api.RunStatus) bool {
	switch s {
	case api.RunStatusCompleted, api.RunStatusFailed, api.RunStatusCancelled:
		return true
	default:
		return false
	}
}

// Run executes a single EvalCase under go test and returns its typed result.
// It marks the test failed (via t.Errorf) when the case does not pass, so a
// case can be used directly as a subtest assertion.
func Run(t *testing.T, c EvalCase) EvalResult {
	t.Helper()
	var h Harness
	if c.Setup != nil {
		h = c.Setup()
	} else {
		h = NewHarness()
	}
	if h != nil {
		defer h.Cleanup()
	}
	res := runCase(context.Background(), c, h)
	if !res.Passed {
		for _, f := range res.Failures {
			t.Errorf("eval case %q: assertion %q failed: %s", c.Name, f.Assertion, f.Detail)
		}
	}
	return res
}

// RunSuite executes every case in cases under go test and returns their typed
// results in declaration order. Each result is written to its own slot, so the
// collection stays correct even if a future caller marks the subtests parallel.
func RunSuite(t *testing.T, cases []EvalCase) []EvalResult {
	t.Helper()
	results := make([]EvalResult, len(cases))
	for i, c := range cases {
		i, c := i, c
		t.Run(c.Name, func(t *testing.T) {
			results[i] = Run(t, c)
		})
	}
	return results
}

// MatrixParam is one named parameter set RunMatrix sweeps every case across —
// typically a model or provider variant for a regression sweep. Apply derives a
// case variant for this param from the base case; when nil the base case runs
// unchanged under the param's name. Name is appended to every case and run id so
// each combination occupies its own slot in the runner's stores.
type MatrixParam struct {
	// Name identifies the param set in reports and disambiguates run ids.
	Name string
	// Apply returns the case variant to run under this param. It receives a copy
	// of the base case and may rewrite its Setup, Input, or Assertions (e.g. to
	// swap the scripted model). When nil the base case runs unchanged.
	Apply func(base EvalCase) EvalCase
}

// MatrixParams configures a RunMatrix sweep. Params lists the parameter sets
// every case is run across; an empty Params runs each case once under an
// implicit "default" param so RunMatrix degenerates to RunSuite.
type MatrixParams struct {
	// Params are the parameter sets swept across every case.
	Params []MatrixParam
}

// RunMatrix executes every case across every parameter set in params and returns
// one EvalResult per (param, case) combination, ordered param-major then case.
// Combinations run as nested subtests named "<param>/<case>"; each combination
// gets a distinct run id (base run id suffixed with the param name) so a Setup
// that shares a store across params never sees a run-id collision — the default
// per-case Harness is already isolated, but a custom shared-store Harness relies
// on this. With no params configured, every case runs once under a "default"
// param, making RunMatrix a superset of RunSuite for single-axis suites.
func RunMatrix(t *testing.T, cases []EvalCase, params MatrixParams) []EvalResult {
	t.Helper()
	sweep := params.Params
	if len(sweep) == 0 {
		sweep = []MatrixParam{{Name: "default"}}
	}
	results := make([]EvalResult, len(sweep)*len(cases))
	for pi, param := range sweep {
		pi, param := pi, param
		t.Run(param.Name, func(t *testing.T) {
			for ci, base := range cases {
				ci, base := ci, base
				variant := applyMatrixParam(base, param)
				idx := pi*len(cases) + ci
				// Nest under the param subtest, named by the base case so the
				// subtest tree reads "<param>/<case>" without doubling the
				// param. The full "<param>/<case>" identifier lives on the
				// EvalResult.Case for reporting. Each combination writes its own
				// slot so the collection is parallel-safe.
				t.Run(base.Name, func(t *testing.T) {
					results[idx] = Run(t, variant)
				})
			}
		})
	}
	return results
}

// applyMatrixParam derives the case variant to run under param: it runs the
// param's Apply hook (if any), then namespaces the variant's Name and Input.RunID
// with the param name so every combination is independently identifiable and
// store-isolated.
func applyMatrixParam(base EvalCase, param MatrixParam) EvalCase {
	variant := base
	if param.Apply != nil {
		variant = param.Apply(base)
	}
	variant.Name = param.Name + "/" + variant.Name
	if variant.Input.RunID != "" {
		variant.Input.RunID = variant.Input.RunID + "-" + param.Name
	}
	return variant
}

// runCase is the testing-agnostic core: it executes the case in h, grades the
// assertions, and returns the typed verdict. It does not import testing, so
// future pack self-checks can reuse it (plan §6.2).
func runCase(ctx context.Context, c EvalCase, h Harness) EvalResult {
	start := time.Now()
	res := EvalResult{Case: c.Name, Passed: true}

	if h == nil {
		res.Passed = false
		res.Failures = append(res.Failures, AssertionFailure{Assertion: runFailure, Detail: "setup returned a nil harness"})
		res.Duration = time.Since(start)
		return res
	}

	runCtx := ctx
	var cancel context.CancelFunc
	if c.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}

	run, execErr := executeRun(runCtx, c, h)
	res.Run = run
	if run.ID != "" {
		res.Usage = summarizeRunUsage(ctx, h, run.ID)
	}

	if execErr != nil {
		res.Passed = false
		detail := execErr.Error()
		if runCtx.Err() == context.DeadlineExceeded {
			detail = fmt.Sprintf("run exceeded timeout %s: %v", c.Timeout, execErr)
		}
		res.Failures = append(res.Failures, AssertionFailure{Assertion: runFailure, Detail: detail})
		res.Duration = time.Since(start)
		return res
	}

	for _, a := range c.Assertions {
		if err := a.Check(ctx, run, h); err != nil {
			res.Passed = false
			res.Failures = append(res.Failures, AssertionFailure{Assertion: a.Name(), Detail: err.Error()})
		}
	}

	res.Duration = time.Since(start)
	return res
}

// summarizeRunUsage folds the run's metering ledger into a typed UsageSummary
// through a read-only unit of work on the public Runner façade (the same access
// path WithinBudget uses). A nil runner, a failed begin, or a query error yields
// the zero UsageSummary rather than failing the case — usage is reporting
// metadata, not a verdict. The unit of work is rolled back so no state leaks.
func summarizeRunUsage(ctx context.Context, h Harness, runID string) UsageSummary {
	runner := h.Runner()
	if runner == nil {
		return UsageSummary{}
	}
	uow, err := runner.Begin(ctx)
	if err != nil {
		return UsageSummary{}
	}
	records, qErr := uow.UsageRecords().QueryUsage(ctx, api.UsageSelector{RunID: runID})
	_ = uow.Rollback(ctx)
	if qErr != nil {
		return UsageSummary{}
	}
	return SummarizeUsage(records)
}

// executeRun drives the full agent run to a terminal api.RunStatus using the
// exact public-surface wiring proven by the M0 spike: StartRun -> CreateTask ->
// DispatchTask -> worker.AgentWorker.ExecuteEnvelope (scripted provider) ->
// AdvanceRun (created->running) -> TransitionRun (running->composing->completed).
func executeRun(ctx context.Context, c EvalCase, h Harness) (api.Run, error) {
	runner := h.Runner()
	if runner == nil {
		return api.Run{}, fmt.Errorf("harness returned a nil runner")
	}

	agentID, model, script := scriptedWiring(h)

	run, _, err := runner.StartRun(ctx, c.Input)
	if err != nil {
		return api.Run{}, fmt.Errorf("StartRun: %w", err)
	}

	taskID := run.ID + "-task"
	task, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       taskID,
		Goal:         c.Description,
		OwnerAgentID: agentID,
		WriteTargets: []string{"output"},
	})
	if err != nil {
		return run, fmt.Errorf("CreateTask: %w", err)
	}

	env, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{
		RunID:         run.ID,
		TaskID:        task.ID,
		TargetAgentID: agentID,
	})
	if err != nil {
		return run, fmt.Errorf("DispatchTask: %w", err)
	}

	engine := agent.Engine{Provider: scripted.New(script)}
	executor := worker.AgentWorker{Runner: runner, Engine: engine, AgentID: agentID, Model: model}
	if err := executor.ExecuteEnvelope(ctx, worker.ExecuteEnvelopeRequest{Envelope: env}); err != nil {
		return run, fmt.Errorf("ExecuteEnvelope: %w", err)
	}

	if _, err := runner.AdvanceRun(ctx, api.AdvanceRunCommand{RunID: run.ID}); err != nil {
		return run, fmt.Errorf("AdvanceRun: %w", err)
	}
	for _, to := range []api.RunStatus{api.RunStatusComposingResponse, api.RunStatusCompleted} {
		if err := runner.TransitionRun(ctx, api.TransitionRunCommand{RunID: run.ID, To: to}); err != nil {
			return run, fmt.Errorf("TransitionRun(%q): %w", to, err)
		}
	}

	final, err := runner.Run(ctx, run.ID)
	if err != nil {
		return run, fmt.Errorf("Run: %w", err)
	}
	if !isTerminalRunStatus(final.Status) {
		return final, fmt.Errorf("run did not reach a terminal status: %q", final.Status)
	}
	return final, nil
}

// scriptedWiring resolves the agent id, model, and deterministic script for the
// worker bridge. A DefaultHarness supplies all three; any other Harness falls
// back to framework defaults so executeRun can still drive the loop.
func scriptedWiring(h Harness) (agentID, model string, script []provider.Event) {
	if sh, ok := h.(scriptedHarness); ok {
		return sh.AgentID(), sh.Model(), sh.Script()
	}
	return "agent", "scripted", []provider.Event{
		{Kind: provider.EventTextDelta, Text: "ok"},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}
}
