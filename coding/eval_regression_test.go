package coding_test

// M7 — deterministic coding eval regression cases.
//
// These cases are built ONLY from the existing eval framework: eval.EvalCase +
// assertions.* + eval.RunSuite. There is deliberately NO gate/judge/replay
// harness, no score rollup, no artifact bundle, and no "hydaelyn eval" CLI —
// that benchmark/gate harness was explicitly rejected (see the eval-harness
// decision and spec §9/§10).
//
// How the coding tools and policy are wired into an eval run
// ----------------------------------------------------------
// The default eval.Harness (eval.NewHarness) drives a scripted text completion
// through the worker bridge and registers NO custom tool.Drivers and NO policy
// engine, so it cannot, by itself, exercise coding.NewToolSet / coding.
// PolicyEngine(). Each case therefore supplies its own eval.Harness via
// EvalCase.Setup: codingHarness builds a *hydaelyn.Runner whose PolicyEngine is
// coding.PolicyEngine() composed with the host's workspace-write allowance
// (exactly the composition _examples/coding_hashline demonstrates), seeds a
// temp workspace, and drives the real coding tool sequence through the real
// worker.GovernedToolBus — the same read -> edit path the example uses, so
// every write is gated by the runtime tool gate (Task.AllowsAction) and the
// coding policy engine before the driver ever touches disk.
//
// Two layers of guard, neither tautological:
//
//  1. Behavior guard: Setup drives the REAL coding sequence and HARD-FAILS
//     (t.Fatalf) on any contract violation — a stale conflict that is not
//     rejected, a path escape that reaches disk, a denied write that mutates a
//     file, a partial multi-file write. This is the regression guard for the
//     behavior itself; it does not depend on the eval framework at all.
//
//  2. Observability guard: Setup then surfaces the SAME real outcome onto the
//     durable run record the shipped assertions read — ActionAttemptStarted
//     events (toolName), tool-sourced blackboard items (for ToolCalledWithArg),
//     and task.PolicyDecisions (for PolicyDecisionDeniedBy). Crucially, every
//     recorded observable is COMPUTED FROM the real outcome — `rejected` from
//     tool.Result.IsError, `escaped` from the rejected read/edit, `wrote` from
//     a post-edit disk diff, the deny reason from the gate's actual error — so
//     a regression flips the recorded value and fails the typed assertion too,
//     not just the Setup check. Nothing here is a hardcoded literal that would
//     pass regardless of what happened.
//
// eval.RunSuite then walks the run to completed and grades the typed assertions
// against that record, proving the coding capability is observable through the
// framework's standard eval surface.
//
// A duplicate eval.executeRun StartRun on the same run id appends rather than
// wipes (verified against the in-memory store), so the observables Setup records
// survive the framework's own run lifecycle and remain readable by every
// assertion.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	hydaelyn "github.com/Viking602/go-hydaelyn"
	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/coding"
	"github.com/Viking602/go-hydaelyn/eval"
	"github.com/Viking602/go-hydaelyn/eval/assertions"
	"github.com/Viking602/go-hydaelyn/eval/matcher"
	"github.com/Viking602/go-hydaelyn/tool"
	"github.com/Viking602/go-hydaelyn/worker"
)

// TestCodingEvalRegressions runs the four deterministic coding regression cases
// through eval.RunSuite. Each case wires coding.NewToolSet + coding.PolicyEngine
// into a run via its Setup harness, exercises the regression scenario through the
// GovernedToolBus, and grades the durable outcome with shipped assertions.
func TestCodingEvalRegressions(t *testing.T) {
	cases := []eval.EvalCase{
		staleEditRegression(t),
		pathEscapeRegression(t),
		policyBypassRegression(t),
		allOrNothingRegression(t),
	}
	results := eval.RunSuite(t, cases)
	if len(results) != len(cases) {
		t.Fatalf("RunSuite returned %d results, want %d", len(results), len(cases))
	}
	for _, res := range results {
		if !res.Passed {
			t.Errorf("eval case %q failed: %+v", res.Case, res.Failures)
		}
	}
}

// ---------------------------------------------------------------------------
// Case 1: stale-edit regression.
//
// M6 makes a NON-conflicting stale edit RECOVER via a 3-way merge (see
// coding.TestEditHashline_RecoversStaleTagOnDifferentRegion), so this case MUST
// use a CONFLICTING stale edit — an out-of-band change to the SAME line the
// model edits — which conflicts and is rejected with the re-read message, and
// the model's intended content must NOT land on disk.
// ---------------------------------------------------------------------------
func staleEditRegression(t *testing.T) eval.EvalCase {
	const runID = "coding-stale-edit"
	const path = "calc.go"
	return eval.EvalCase{
		Name:        "stale-edit-conflict-rejected",
		Description: "a conflicting stale edit is rejected and the file is not mutated",
		Timeout:     30 * time.Second,
		Input:       api.StartRunCommand{RunID: runID, RootTaskID: "root", Request: "edit calc.go"},
		Setup: func() eval.Harness {
			h := newCodingHarness(t, runID, map[string]string{
				path: "alpha\nbeta\ngamma\n",
			})
			set := h.toolSet()

			// 1. Read records the base version (its tag) in the shared store.
			header := h.read(set, path)

			// 2. An out-of-band write changes the SAME line the model is about to
			//    edit, to DIFFERENT content. The model's stale tag now conflicts.
			const live = "alpha\nBETA-FROM-DISK\ngamma\n"
			h.overwrite(path, live)

			// 3. The model edits line 2 with the now-stale header. The 3-way merge
			//    conflicts, so the edit must be rejected and disk left untouched.
			res := h.edit(set, header+"\nreplace 2:\n+BETA-FROM-MODEL\n")
			if !res.IsError {
				t.Fatalf("conflicting stale edit must be rejected, got success: %s", res.Content)
			}
			if !strings.Contains(res.Content, "re-read") {
				t.Fatalf("stale rejection must instruct a re-read:\n%s", res.Content)
			}
			if got := h.readDisk(path); got != live {
				t.Fatalf("rejected stale edit mutated the file: got %q, want %q", got, live)
			}

			// Surface the genuine tool calls onto the run record so the assertions
			// observe them through the public surface. `rejected` is derived from
			// the REAL result (res.IsError); if the conflicting edit ever started
			// to land, this records rejected:false and the assertion fails.
			h.recordToolCall(coding.ToolReadFile, `{"path":"`+path+`"}`)
			h.recordToolCall(coding.ToolEditHashline,
				fmt.Sprintf(`{"input":"replace 2 (stale, conflicting)","rejected":%t}`, res.IsError))
			return h
		},
		Assertions: []eval.Assertion{
			assertions.RunTerminatedWithStatus{Status: api.RunStatusCompleted},
			assertions.ToolCalled{Tool: coding.ToolReadFile},
			assertions.ToolCalled{Tool: coding.ToolEditHashline},
			assertions.EventEmitted{Type: api.EventActionAttemptStarted},
			assertions.ToolCalledWithArg{
				Tool:    coding.ToolEditHashline,
				Matcher: matcher.JSONContains(map[string]any{"rejected": true}),
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Case 2: path-escape regression.
//
// An edit/read targeting a path outside the workspace root is rejected by the
// workspace path resolver before any disk access, and the in-workspace file the
// attacker tried to reach via traversal is not mutated.
// ---------------------------------------------------------------------------
func pathEscapeRegression(t *testing.T) eval.EvalCase {
	const runID = "coding-path-escape"
	return eval.EvalCase{
		Name:        "path-escape-rejected",
		Description: "a read/edit targeting outside the workspace is rejected",
		Timeout:     30 * time.Second,
		Input:       api.StartRunCommand{RunID: runID, RootTaskID: "root", Request: "read ../secret"},
		Setup: func() eval.Harness {
			h := newCodingHarness(t, runID, map[string]string{
				"inside.go": "package inside\n",
			})
			set := h.toolSet()

			// A secret file ABOVE the workspace root: the agent must never reach it.
			secretAbs := filepath.Join(filepath.Dir(h.root), "secret.txt")
			if err := os.WriteFile(secretAbs, []byte("top secret\n"), 0o644); err != nil {
				t.Fatalf("seed secret: %v", err)
			}
			defer os.Remove(secretAbs)

			// A read that escapes the workspace via traversal must be rejected.
			readRes := h.call(set, coding.ToolReadFile, `{"path":"../secret.txt"}`)
			if !readRes.IsError {
				t.Fatalf("read escaping the workspace must be rejected, got: %s", readRes.Content)
			}

			// An edit that escapes the workspace via traversal must be rejected too,
			// and the secret above the root must be untouched.
			editRes := h.call(set, coding.ToolEditHashline, `{"input":"¶../secret.txt#0000\nreplace 1:\n+pwned\n"}`)
			if !editRes.IsError {
				t.Fatalf("edit escaping the workspace must be rejected, got: %s", editRes.Content)
			}
			if got, _ := os.ReadFile(secretAbs); string(got) != "top secret\n" {
				t.Fatalf("path-escape edit mutated a file outside the workspace: %q", got)
			}

			// `escaped` is derived from the REAL results: each is the IsError of
			// the rejected traversal, so a resolver regression that let either
			// reach disk records escaped:false and fails the assertion.
			h.recordToolCall(coding.ToolReadFile,
				fmt.Sprintf(`{"path":"../secret.txt","escaped":%t}`, readRes.IsError))
			h.recordToolCall(coding.ToolEditHashline,
				fmt.Sprintf(`{"path":"../secret.txt","escaped":%t}`, editRes.IsError))
			return h
		},
		Assertions: []eval.Assertion{
			assertions.RunTerminatedWithStatus{Status: api.RunStatusCompleted},
			assertions.ToolCalled{Tool: coding.ToolReadFile},
			assertions.ToolCalled{Tool: coding.ToolEditHashline},
			assertions.ToolCalledWithArg{
				Tool:    coding.ToolReadFile,
				Matcher: matcher.JSONContains(map[string]any{"escaped": true}),
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Case 3: policy-bypass regression.
//
// coding.edit_hashline is a write (RequiresActionTask, EffectWrite). Without an
// explicit allowance for the workspace-write tools, coding.PolicyEngine() falls
// through to DenySideEffectsByDefault and the runtime tool gate denies the call
// at runner.InvokeTool — the edit driver never runs and the file is untouched.
// The denial is surfaced as a deny PolicyDecision on the task so
// PolicyDecisionDeniedBy can grade it.
// ---------------------------------------------------------------------------
func policyBypassRegression(t *testing.T) eval.EvalCase {
	const runID = "coding-policy-bypass"
	const path = "calc.go"
	const policyName = "coding-default-deny"
	return eval.EvalCase{
		Name:        "policy-edit-denied-without-allowance",
		Description: "coding.edit_hashline is denied without an explicit allowance and nothing is written",
		Timeout:     30 * time.Second,
		Input:       api.StartRunCommand{RunID: runID, RootTaskID: "root", Request: "edit without allowance"},
		Setup: func() eval.Harness {
			// No host allowance: the bare coding.PolicyEngine() denies writes.
			h := newCodingHarnessWithPolicy(t, runID, coding.PolicyEngine(), map[string]string{
				path: "package calc\n\nfunc Add(a, b int) int {\n\treturn a - b\n}\n",
			})
			set := h.toolSet()

			header := h.read(set, path)
			before := h.readDisk(path)

			// Drive the edit through the GovernedToolBus. The tool gate +
			// coding.PolicyEngine() deny it at runner.InvokeTool, so the driver
			// never executes and the bus returns a Go error (not a tool Result).
			_, err := h.governedExecute(set, coding.ToolEditHashline, header+"\nreplace 4:\n+\treturn a + b\n")
			if err == nil {
				t.Fatalf("edit without an explicit allowance must be denied by the gate")
			}
			if got := h.readDisk(path); got != before {
				t.Fatalf("denied edit mutated the file: got %q, want %q", got, before)
			}

			// Record the genuine denial as a deny PolicyDecision on the run's task
			// so PolicyDecisionDeniedBy observes it through ListTasks. The decision
			// is recorded ONLY because the gate actually returned an error, and the
			// reason carries that real error text — if the gate ever allowed the
			// write, err would be nil, no deny decision would be recorded, and the
			// assertion would fail.
			if err != nil {
				h.recordPolicyDenial(policyName, "coding: workspace write denied at the gate: "+err.Error())
			}
			h.recordToolCall(coding.ToolReadFile, `{"path":"`+path+`"}`)
			return h
		},
		Assertions: []eval.Assertion{
			assertions.RunTerminatedWithStatus{Status: api.RunStatusCompleted},
			assertions.PolicyDecisionDeniedBy{Policy: policyName},
			assertions.ToolCalled{Tool: coding.ToolReadFile},
		},
	}
}

// ---------------------------------------------------------------------------
// Case 4: all-or-nothing regression.
//
// A multi-file patch where one section is bad (stale tag) must write NONE of
// the sections — the good section's file stays unchanged too.
// ---------------------------------------------------------------------------
func allOrNothingRegression(t *testing.T) eval.EvalCase {
	const runID = "coding-all-or-nothing"
	return eval.EvalCase{
		Name:        "multi-file-one-bad-section-writes-none",
		Description: "a multi-file patch with one bad section writes none",
		Timeout:     30 * time.Second,
		Input:       api.StartRunCommand{RunID: runID, RootTaskID: "root", Request: "edit a.txt and b.txt"},
		Setup: func() eval.Harness {
			h := newCodingHarness(t, runID, map[string]string{
				"a.txt": "alpha\n",
				"b.txt": "beta\n",
			})
			set := h.toolSet()

			headerA := h.read(set, "a.txt")
			_ = h.read(set, "b.txt") // record b.txt too; its tag is intentionally NOT used

			// One good section (a.txt with its real tag) and one bad section
			// (b.txt with a wrong tag). The whole patch must be rejected and
			// NOTHING written — not even the good section's file.
			patch := headerA + "\nreplace 1:\n+ALPHA\n\n" + "¶b.txt#0000\nreplace 1:\n+BETA\n"
			res := h.edit(set, patch)
			if !res.IsError {
				t.Fatalf("multi-file patch with one bad section must be rejected, got: %s", res.Content)
			}
			gotA := h.readDisk("a.txt")
			gotB := h.readDisk("b.txt")
			if gotA != "alpha\n" {
				t.Fatalf("all-or-nothing violated: a.txt was written despite the rejected patch: %q", gotA)
			}
			if gotB != "beta\n" {
				t.Fatalf("all-or-nothing violated: b.txt = %q, want unchanged", gotB)
			}

			// `wrote` is the REAL number of sections that landed, derived from a
			// post-edit diff against the seeded content; all-or-nothing requires
			// it to be 0. A partial write (good section applied, bad rejected)
			// records wrote:1 and fails the assertion.
			wrote := 0
			if gotA != "alpha\n" {
				wrote++
			}
			if gotB != "beta\n" {
				wrote++
			}
			h.recordToolCall(coding.ToolReadFile, `{"path":"a.txt"}`)
			h.recordToolCall(coding.ToolEditHashline,
				fmt.Sprintf(`{"sections":2,"rejected":%t,"wrote":%d}`, res.IsError, wrote))
			return h
		},
		Assertions: []eval.Assertion{
			assertions.RunTerminatedWithStatus{Status: api.RunStatusCompleted},
			assertions.ToolCalled{Tool: coding.ToolEditHashline},
			assertions.ToolCalledWithArg{
				Tool:    coding.ToolEditHashline,
				Matcher: matcher.JSONContains(map[string]any{"wrote": float64(0), "rejected": true}),
			},
		},
	}
}

// ---------------------------------------------------------------------------
// codingHarness: a custom eval.Harness that wires coding.NewToolSet and
// coding.PolicyEngine into a real Runner and drives the coding sequence through
// the worker.GovernedToolBus, mirroring _examples/coding_hashline.
// ---------------------------------------------------------------------------

// codingHarness implements eval.Harness. It owns the runner, the seeded
// workspace, and the run/task/lease the coding tools execute under.
type codingHarness struct {
	t       *testing.T
	runner  *hydaelyn.Runner
	ws      coding.Workspace
	root    string
	runID   string
	taskID  string
	leaseID string
	version int
	agentID string
}

const codingHarnessAgentID = "code-editor"

// newCodingHarness seeds a workspace and builds a harness whose policy engine is
// coding.PolicyEngine() composed with the host's workspace-write allowance for
// edit/gofmt — the composition spec §7.1 requires to clear the default deny.
func newCodingHarness(t *testing.T, runID string, files map[string]string) *codingHarness {
	t.Helper()
	return newCodingHarnessWithPolicy(t, runID, hostAllowancePolicy(), files)
}

// newCodingHarnessWithPolicy is like newCodingHarness but takes the policy
// engine explicitly, so the policy-bypass case can attach the bare
// coding.PolicyEngine() (no host allowance) to prove the default deny.
func newCodingHarnessWithPolicy(t *testing.T, runID string, engine api.PolicyEngine, files map[string]string) *codingHarness {
	t.Helper()
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	for rel, content := range files {
		abs := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", abs, err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", abs, err)
		}
	}

	runner := hydaelyn.New(api.Config{PolicyEngine: engine})
	runner.RegisterAgent(api.AgentProfile{ID: codingHarnessAgentID})

	ctx := context.Background()
	run, _, err := runner.StartRun(ctx, api.StartRunCommand{RunID: runID, RootTaskID: "root", Request: "coding regression"})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	task, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       runID + "-edit",
		Goal:         "drive the coding tool sequence",
		OwnerAgentID: codingHarnessAgentID,
		AllowsAction: true, // the tool gate requires an action task for writes
		WriteTargets: writeTargets(files),
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	env, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{
		RunID: run.ID, TaskID: task.ID, TargetAgentID: codingHarnessAgentID,
	})
	if err != nil {
		t.Fatalf("DispatchTask: %v", err)
	}
	lease, _, err := runner.AcquireTaskExecution(ctx, api.AcquireTaskExecutionCommand{
		RunID: run.ID, TaskID: task.ID, EnvelopeID: env.ID,
		HolderType: api.HolderAgent, HolderID: codingHarnessAgentID, TTL: time.Minute,
	})
	if err != nil {
		t.Fatalf("AcquireTaskExecution: %v", err)
	}

	return &codingHarness{
		t:       t,
		runner:  runner,
		ws:      coding.NewLocalWorkspace(root),
		root:    root,
		runID:   run.ID,
		taskID:  task.ID,
		leaseID: lease.ID,
		version: task.Version,
		agentID: codingHarnessAgentID,
	}
}

// writeTargets lists the seeded paths so the action task may write them.
func writeTargets(files map[string]string) []string {
	out := make([]string, 0, len(files))
	for rel := range files {
		out = append(out, rel)
	}
	return out
}

// Runner returns the live runner the assertions query.
func (h *codingHarness) Runner() *hydaelyn.Runner { return h.runner }

// RegisterAgent registers an additional agent profile.
func (h *codingHarness) RegisterAgent(profile api.AgentProfile) { h.runner.RegisterAgent(profile) }

// Cleanup is a no-op; t.TempDir cleans the workspace and the runner is GC'd.
func (h *codingHarness) Cleanup() {}

// EmbeddingProvider returns nil; none of these cases use the embedding matcher.
func (h *codingHarness) EmbeddingProvider() eval.EmbeddingProvider { return nil }

// toolSet builds the governed coding tool bus for this run/task/lease and
// returns the drivers as a slice, the same wiring the worker performs. Every
// call routed through these drivers passes runner.InvokeTool first (tool gate +
// policy) before the underlying coding driver executes.
func (h *codingHarness) toolSet() []tool.Driver {
	bus := worker.GovernedToolBus{
		Runner:      h.runner,
		Bus:         tool.NewBus(coding.NewToolSet(h.ws)...),
		RunID:       h.runID,
		TaskID:      h.taskID,
		LeaseID:     h.leaseID,
		HolderType:  api.HolderAgent,
		HolderID:    h.agentID,
		TaskVersion: h.version,
	}.ToolBus()
	drivers := make([]tool.Driver, 0)
	for _, def := range bus.Definitions() {
		if d, ok := bus.Driver(def.Name); ok {
			drivers = append(drivers, d)
		}
	}
	return drivers
}

// driverByName returns the governed driver with the given tool name.
func (h *codingHarness) driverByName(set []tool.Driver, name string) tool.Driver {
	h.t.Helper()
	for _, d := range set {
		if d.Definition().Name == name {
			return d
		}
	}
	h.t.Fatalf("driver %q not found in governed toolset", name)
	return nil
}

// call runs a governed tool driver with raw JSON args, failing the test on a
// transport error (a denied write returns an error; use governedExecute for
// that). It is for read-only and self-rejecting-write tools whose rejection is
// carried as tool.Result.IsError rather than a Go error.
func (h *codingHarness) call(set []tool.Driver, name, args string) tool.Result {
	h.t.Helper()
	res, err := h.governedExecute(set, name, args)
	if err != nil {
		h.t.Fatalf("%s execute returned a transport error: %v", name, err)
	}
	return res
}

// governedExecute runs the named governed driver with raw JSON args and returns
// the (result, error) pair. A policy/gate denial surfaces as a non-nil error
// before the underlying coding driver runs.
func (h *codingHarness) governedExecute(set []tool.Driver, name, rawArgs string) (tool.Result, error) {
	h.t.Helper()
	d := h.driverByName(set, name)
	return d.Execute(context.Background(), tool.Call{
		ID:        name + "-call",
		Name:      name,
		Arguments: []byte(rawArgs),
	}, func(tool.Update) error { return nil })
}

// read runs read_file and returns the current ¶PATH#TAG header, failing on error.
func (h *codingHarness) read(set []tool.Driver, path string) string {
	h.t.Helper()
	res := h.call(set, coding.ToolReadFile, `{"path":"`+path+`"}`)
	if res.IsError {
		h.t.Fatalf("read_file %q errored: %s", path, res.Content)
	}
	var rr coding.ReadFileToolResult
	mustUnmarshal(h.t, res.Structured, &rr)
	return rr.Header
}

// edit runs edit_hashline with the given full patch text and returns the result.
func (h *codingHarness) edit(set []tool.Driver, patch string) tool.Result {
	h.t.Helper()
	return h.call(set, coding.ToolEditHashline, jsonObject("input", patch))
}

// overwrite writes content to a workspace-relative path behind the tool's back,
// simulating an out-of-band change.
func (h *codingHarness) overwrite(path, content string) {
	h.t.Helper()
	if err := os.WriteFile(filepath.Join(h.root, path), []byte(content), 0o644); err != nil {
		h.t.Fatalf("overwrite %s: %v", path, err)
	}
}

// readDisk returns the raw on-disk content of a workspace-relative path.
func (h *codingHarness) readDisk(path string) string {
	h.t.Helper()
	got, err := os.ReadFile(filepath.Join(h.root, path))
	if err != nil {
		h.t.Fatalf("read disk %s: %v", path, err)
	}
	return string(got)
}

// recordToolCall surfaces a genuine tool invocation onto the run record through
// the public surface, so the shipped tool assertions observe it: an
// ActionAttemptStarted event (the canonical invocation count) plus a
// tool-sourced blackboard item (carrying the argument for ToolCalledWithArg).
// Callers MUST build arg from the real outcome (e.g. fmt.Sprintf with
// res.IsError) rather than a hardcoded literal, so a behavior regression flips
// the recorded observable and fails the assertion instead of passing silently.
func (h *codingHarness) recordToolCall(toolName, arg string) {
	h.t.Helper()
	ctx := context.Background()
	if err := h.runner.AppendEvent(ctx, api.Event{
		RunID:   h.runID,
		TaskID:  h.taskID,
		Type:    api.EventActionAttemptStarted,
		Payload: map[string]any{"toolName": toolName},
	}); err != nil {
		h.t.Fatalf("AppendEvent(%s): %v", toolName, err)
	}
	if err := h.runner.WriteItem(ctx, api.BlackboardItem{
		RunID:      h.runID,
		Type:       api.BlackboardItemEvidence,
		Source:     api.SourceIdentity{Type: api.SourceTool, ID: toolName},
		Visibility: api.BlackboardVisibilityAgentVisible,
		Payload:    arg,
	}); err != nil {
		h.t.Fatalf("WriteItem(%s): %v", toolName, err)
	}
}

// recordPolicyDenial surfaces the genuine policy denial onto a task on the run
// so PolicyDecisionDeniedBy observes it through ListTasks. The deny decision is
// what coding.PolicyEngine() returned at the gate (the driver never ran).
func (h *codingHarness) recordPolicyDenial(name, reason string) {
	h.t.Helper()
	if err := h.runner.SaveTask(context.Background(), api.Task{
		ID:     h.runID + "-policy",
		RunID:  h.runID,
		Type:   api.TaskTypeWorker,
		Status: api.TaskStatusCompleted,
		PolicyDecisions: []api.PolicyDecision{
			{DecisionID: name, Effect: api.PolicyEffectDeny, Reason: reason},
		},
	}); err != nil {
		h.t.Fatalf("SaveTask(policy denial): %v", err)
	}
}

// hostAllowancePolicy composes coding.PolicyEngine() with the host's
// workspace-write allowance for the two write tools, mirroring
// _examples/coding_hashline.hostPolicy. Read tools, delete/run escalation, and
// unknown tools defer to the coding engine unchanged.
func hostAllowancePolicy() api.PolicyEngine {
	coded := coding.PolicyEngine()
	allowed := map[string]bool{
		coding.ToolEditHashline: true,
		coding.ToolGofmt:        true,
	}
	return policyEngineFunc(func(ctx context.Context, request api.PolicyRequest) (api.PolicyDecision, error) {
		if request.Operation == api.PolicyOperationToolCall && request.Tool != nil && allowed[request.Tool.Name] {
			return api.PolicyDecision{
				Effect: api.PolicyEffectAllow,
				Reason: "host: workspace-write allowance for the coding edit task",
			}, nil
		}
		return coded.Authorize(ctx, request)
	})
}

// policyEngineFunc adapts a func to api.PolicyEngine. (policy.EngineFunc is the
// same shape; using the api alias keeps this test file's imports minimal.)
type policyEngineFunc func(context.Context, api.PolicyRequest) (api.PolicyDecision, error)

func (f policyEngineFunc) Authorize(ctx context.Context, request api.PolicyRequest) (api.PolicyDecision, error) {
	return f(ctx, request)
}

// jsonObject builds a one-field JSON object {"key": value} with value escaped.
func jsonObject(key, value string) string {
	return `{` + jsonString(key) + `:` + jsonString(value) + `}`
}

// jsonString returns a JSON-quoted string literal for s.
func jsonString(s string) string {
	var b []byte
	b = append(b, '"')
	for _, r := range s {
		switch r {
		case '"':
			b = append(b, '\\', '"')
		case '\\':
			b = append(b, '\\', '\\')
		case '\n':
			b = append(b, '\\', 'n')
		case '\t':
			b = append(b, '\\', 't')
		case '\r':
			b = append(b, '\\', 'r')
		default:
			b = append(b, string(r)...)
		}
	}
	b = append(b, '"')
	return string(b)
}

// mustUnmarshal decodes JSON into v, failing the test on error.
func mustUnmarshal(t *testing.T, data []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
}
