// coding_hashline demonstrates the sandboxed coding capability end to end:
// a scripted agent reads a buggy Go file, fixes it with a line-anchored
// hashline edit, formats it with the in-process gofmt tool, runs the package
// tests, and shows the resulting git diff — all through the Runner/worker path
// under coding.PolicyEngine(), and with NO shell access.
//
// The flow exercises spec section 12 item 9 (read -> edit_hashline -> gofmt ->
// go_test -> git_diff). Every tool call is dispatched by an agent.Engine whose
// tool bus is the coding toolset wrapped by the worker's GovernedToolBus, so
// each write is gated by the runtime's tool gate (Task.AllowsAction) and the
// coding policy engine before it ever touches the workspace.
//
// The model is a deterministic scripted provider: each turn emits exactly one
// tool call, and the edit turn carries the real ¶PATH#TAG header minted from
// the seeded file (read back through the workspace before the run), so the
// example reproduces the exact protocol the spec defines without a live model.
//
//	go run ./_examples/coding_hashline
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/Viking602/go-hydaelyn"
	"github.com/Viking602/go-hydaelyn/agent"
	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/coding"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/policy"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/stream"
	"github.com/Viking602/go-hydaelyn/tool"
	"github.com/Viking602/go-hydaelyn/worker"
)

const (
	agentID  = "code-editor"
	model    = "scripted"
	buggyRel = "add.go" // workspace-relative path the agent edits
)

func main() {
	ctx := context.Background()

	// 1. A temp workspace seeded with a tiny buggy Go package: Add subtracts
	//    instead of adding, and its test fails until the bug is fixed. The
	//    package is git-initialized and committed so git_diff has a baseline.
	root := mustSeedWorkspace()
	defer os.RemoveAll(root)
	pkgDir := filepath.Join(root, "mathutil")

	// The workspace is the single sandbox boundary: every coding tool resolves
	// paths and runs commands relative to this root, and the same value backs
	// the hashline patcher's disk writes.
	ws := coding.NewLocalWorkspace(pkgDir)

	// 2. Mint the real tag for the buggy file by reading it back through the
	//    workspace. The scripted "model" cannot observe tool results between
	//    turns, so it must carry the current ¶PATH#TAG in the edit it emits —
	//    exactly the header a real model would copy out of the read result.
	read, err := ws.ReadFile(ctx, coding.ReadFileRequest{Path: buggyRel})
	must(err)
	header := "¶" + read.Path + "#" + read.Tag
	fmt.Printf("seeded %s with tag %s\n\n", buggyRel, read.Tag)

	// 3. The Runner owns the policy gate. coding.PolicyEngine() is the central
	//    engine (read tools allowed, delete/run escalated, other writes denied
	//    by default); the host grants the workspace-write allowance for this
	//    action task by composing an allow rule for the two coding write tools
	//    the run uses. That composition is exactly the "explicit allowance"
	//    spec section 7.1 says must clear the default deny.
	runner := hydaelyn.New(api.Config{PolicyEngine: hostPolicy()})
	runner.RegisterAgent(api.AgentProfile{ID: agentID})

	run, _, err := runner.StartRun(ctx, api.StartRunCommand{Request: "fix the Add bug"})
	must(err)
	task, err := runner.CreateTask(ctx, api.CreateTaskCommand{
		RunID:        run.ID,
		TaskID:       "fix-add",
		Goal:         "Fix the Add function so its test passes, then format and verify.",
		OwnerAgentID: agentID,
		AllowsAction: true, // the tool gate requires an action task for writes
		WriteTargets: []string{buggyRel},
	})
	must(err)
	env, err := runner.DispatchTask(ctx, api.DispatchTaskCommand{
		RunID: run.ID, TaskID: task.ID, TargetAgentID: agentID,
	})
	must(err)
	lease, _, err := runner.AcquireTaskExecution(ctx, api.AcquireTaskExecutionCommand{
		RunID: run.ID, TaskID: task.ID, EnvelopeID: env.ID,
		HolderType: api.HolderAgent, HolderID: agentID, TTL: time.Minute,
	})
	must(err)

	// 4. Wire the engine the way the worker does. The engine's own tool bus is
	//    the raw coding toolset; the worker.GovernedToolBus binds it to this
	//    run/task/lease and routes every call through runner.InvokeTool, where
	//    the tool gate (Task.AllowsAction) and the policy engine run before the
	//    driver executes. ToolBus() returns that governed bus as a plain
	//    tool.Bus the engine can drive. (worker.AgentWorker.ExecuteEnvelope
	//    performs this exact substitution internally; doing it inline lets the
	//    example observe each tool result as the loop runs.)
	governed := worker.GovernedToolBus{
		Runner:      runner,
		Bus:         tool.NewBus(coding.NewToolSet(ws)...),
		RunID:       run.ID,
		TaskID:      task.ID,
		LeaseID:     lease.ID,
		HolderType:  api.HolderAgent,
		HolderID:    agentID,
		TaskVersion: task.Version,
	}.ToolBus()

	engine := agent.Engine{
		Provider: newScriptedAgent(header),
		Tools:    governed,
		Model:    model,
	}

	// The sink reports each tool result as the loop produces it, so the output
	// shows the read -> edit -> gofmt -> go_test -> git_diff sequence actually
	// executing through the gate. The returned Result is byte-for-byte the same
	// as a plain Run; the stream is a transient side-channel.
	fmt.Println("--- tool sequence (each call gated by the runtime) ---")
	sink := stream.SinkFunc(func(_ context.Context, frame stream.Frame) error {
		if frame.Kind == stream.FrameToolResult && frame.ToolResult != nil {
			reportToolResult(*frame.ToolResult)
		}
		return nil
	})

	result := engine.RunStream(ctx, reloadTask(ctx, runner, run.ID, task.ID), agent.OutputPolicy{}, sink)
	if result.Failure != nil {
		panic(result.Failure)
	}

	// 5. Report the outcome: the agent's final answer plus the applied diff,
	//    obtained through the sandboxed git_diff path (no shell access).
	fmt.Printf("\nfinal answer: %s\n", result.Text)

	diff, err := ws.Diff(ctx, coding.DiffRequest{})
	must(err)
	fmt.Println("\n--- final git diff ---")
	if diff.Diff == "" {
		fmt.Println("(no changes)")
	} else {
		fmt.Print(diff.Diff)
	}

	fixed, err := os.ReadFile(filepath.Join(pkgDir, "add.go"))
	must(err)
	fmt.Println("\n--- fixed add.go ---")
	fmt.Print(string(fixed))
}

// reportToolResult prints a one-line summary of a tool result. For go_test it
// decodes the typed result so the example can assert the tests actually passed
// in-loop rather than trusting the scripted final answer.
func reportToolResult(res message.ToolResult) {
	status := "ok"
	if res.IsError {
		status = "ERROR"
	}
	switch res.Name {
	case coding.ToolGoTest:
		var gt coding.GoTestToolResult
		if err := json.Unmarshal(res.Structured, &gt); err == nil {
			verdict := "FAIL"
			if gt.Passed {
				verdict = "PASS"
			}
			fmt.Printf("  %-22s %s (go test exit %d)\n", res.Name, verdict, gt.ExitCode)
			return
		}
	case coding.ToolReadFile:
		var rf coding.ReadFileToolResult
		if err := json.Unmarshal(res.Structured, &rf); err == nil {
			fmt.Printf("  %-22s %s (%d lines)\n", res.Name, rf.Header, rf.LineCount)
			return
		}
	case coding.ToolEditHashline:
		var er coding.EditHashlineResult
		if err := json.Unmarshal(res.Structured, &er); err == nil && len(er.Sections) > 0 {
			fmt.Printf("  %-22s %s firstChangedLine=%d\n", res.Name, er.Sections[0].Header, er.Sections[0].FirstChangedLine)
			return
		}
	}
	fmt.Printf("  %-22s %s\n", res.Name, status)
}

// reloadTask reloads the durable task so the engine runs against the same
// record the runtime gate validates against (Version, AllowsAction, Goal).
func reloadTask(ctx context.Context, runner *hydaelyn.Runner, runID, taskID string) api.Task {
	task, err := runner.Task(ctx, runID, taskID)
	must(err)
	return task
}

// hostPolicy composes the central coding policy engine with the host's
// workspace-write allowance. coding.PolicyEngine() denies edit/gofmt by default
// (they are side effects); the host clears that deny for the two write tools
// this action task is allowed to use, and defers every other decision —
// read-only tools, delete/run escalation, unknown tools — back to the coding
// engine unchanged.
func hostPolicy() policy.Engine {
	coded := coding.PolicyEngine()
	allowed := map[string]bool{
		coding.ToolEditHashline: true,
		coding.ToolGofmt:        true,
	}
	return policy.EngineFunc(func(ctx context.Context, request policy.Request) (policy.Decision, error) {
		if request.Operation == policy.OperationToolCall && request.Tool != nil && allowed[request.Tool.Name] {
			return policy.Decision{
				Effect: policy.EffectAllow,
				Reason: "host: workspace-write allowance for the coding edit task",
			}, nil
		}
		return coded.Authorize(ctx, request)
	})
}

// scriptedAgent is a deterministic provider: turn N emits the Nth event slice.
// Each tool turn carries exactly one tool call so the engine round-trips the
// result before the next turn, mirroring a real read-then-edit-then-verify loop
// without a live model. The fixed script encodes the spec's tool sequence.
type scriptedAgent struct {
	turns     [][]provider.Event
	callIndex int
}

func newScriptedAgent(header string) *scriptedAgent {
	// The hashline patch replaces only the buggy line. In the seeded file line
	// 5 is the function body "\treturn a - b"; the body row is final content
	// only (+TEXT). The header carries the current tag minted from the seeded
	// file. Keeping the range tight (one line) follows the protocol rule to
	// replace only the lines whose content changes.
	patch := header + "\nreplace 5:\n+\treturn a + b\n"
	return &scriptedAgent{turns: [][]provider.Event{
		toolTurn("call-read", coding.ToolReadFile, `{"path":"add.go"}`),
		toolTurn("call-edit", coding.ToolEditHashline, jsonObject("input", patch)),
		toolTurn("call-fmt", coding.ToolGofmt, `{"path":"add.go"}`),
		toolTurn("call-test", coding.ToolGoTest, `{"package":"./..."}`),
		toolTurn("call-diff", coding.ToolGitDiff, `{}`),
		{
			{Kind: provider.EventTextDelta, Text: "Fixed Add (subtraction -> addition), formatted, and verified: tests pass."},
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
		},
	}}
}

func (*scriptedAgent) Metadata() provider.Metadata {
	return provider.Metadata{Name: "scripted", Models: []string{model}}
}

func (s *scriptedAgent) Stream(_ context.Context, _ provider.Request) (provider.Stream, error) {
	if s.callIndex >= len(s.turns) {
		// Defensive: the script is exhausted; end the loop cleanly.
		return provider.NewSliceStream([]provider.Event{
			{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
		}), nil
	}
	events := s.turns[s.callIndex]
	s.callIndex++
	return provider.NewSliceStream(events), nil
}

// toolTurn builds a single-tool-call assistant turn: the tool call event
// followed by a done event with the tool-use stop reason.
func toolTurn(id, name, args string) []provider.Event {
	return []provider.Event{
		{
			Kind: provider.EventToolCall,
			ToolCall: &message.ToolCall{
				ID:        id,
				Name:      name,
				Arguments: []byte(args),
			},
		},
		{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
	}
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

// mustSeedWorkspace creates a temp directory holding a buggy mathutil package
// in its own module, initializes git, and commits the baseline so git_diff has
// something to diff against. It returns the temp root.
func mustSeedWorkspace() string {
	root, err := os.MkdirTemp("", "coding-hashline-*")
	must(err)
	pkgDir := filepath.Join(root, "mathutil")
	must(os.MkdirAll(pkgDir, 0o755))

	// A self-contained module so go test runs without touching the host repo.
	// The Go version matches the toolchain so go test does not try to switch.
	write(filepath.Join(pkgDir, "go.mod"), "module example.com/mathutil\n\ngo 1.25\n")
	// The bug: Add subtracts. Deliberately unformatted so gofmt has work to do.
	write(filepath.Join(pkgDir, "add.go"), "package mathutil\n\n// Add returns the sum of a and b.\nfunc Add(a, b int) int {\n\treturn a - b\n}\n")
	write(filepath.Join(pkgDir, "add_test.go"), "package mathutil\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif got := Add(2, 3); got != 5 {\n\t\tt.Fatalf(\"Add(2, 3) = %d, want 5\", got)\n\t}\n}\n")

	gitInit(pkgDir)
	return root
}

// gitInit initializes a git repo in dir, sets a throwaway identity, and commits
// the seeded files so a later git diff shows the agent's edit.
func gitInit(dir string) {
	runGit(dir, "init", "-q")
	runGit(dir, "config", "user.email", "coding@example.com")
	runGit(dir, "config", "user.name", "coding-example")
	runGit(dir, "add", "-A")
	runGit(dir, "commit", "-q", "-m", "seed buggy mathutil")
}

// runGit runs a git subcommand in dir and panics on failure. This is example
// setup only; the agent itself never invokes git directly — git_diff goes
// through the sandboxed, allowlisted command path.
func runGit(dir string, args ...string) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		panic(fmt.Errorf("git %v: %w\n%s", args, err, out))
	}
}

func write(path, content string) {
	must(os.WriteFile(path, []byte(content), 0o644))
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
