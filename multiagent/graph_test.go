package multiagent

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/Viking602/go-hydaelyn/api"
)

// graphExecutor returns a fixed Structured payload per node id, mirroring
// reportExecutor but keyed on the graph node (== ClassName slot).
func graphExecutor(runID string, byNode map[string]map[string]any) Executor {
	return ExecutorFunc(func(_ context.Context, dispatch Dispatch) (api.TypedReport, error) {
		node := classNameFromTaskID(runID, dispatch.Task.ID)
		return api.TypedReport{Status: api.ReportStatusSuccess, Structured: byNode[node]}, nil
	})
}

func mustCompile(t *testing.T, g *Graph) *CompiledGraph {
	t.Helper()
	compiled, err := g.Compile()
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	return compiled
}

func TestGraphLinearChainMatchesSequential(t *testing.T) {
	g := NewGraph().
		AddNode("research", AgentClass{Name: "research"}).
		AddNode("write", AgentClass{Name: "write"}).
		AddNode("review", AgentClass{Name: "review"}).
		AddEdge("research", "write").
		AddEdge("write", "review")

	result, err := Drive(context.Background(), "run-1", mustCompile(t, g), graphExecutor("run-1", nil), DriveOptions{})
	if err != nil {
		t.Fatalf("Drive error = %v", err)
	}
	got := classNames(result.State)
	want := []string{"research", "write", "review"}
	if len(got) != len(want) {
		t.Fatalf("executed nodes = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("node[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestGraphDiamondFanOutFanIn(t *testing.T) {
	g := NewGraph().
		AddNode("a", AgentClass{Name: "a"}).
		AddNode("b", AgentClass{Name: "b"}).
		AddNode("c", AgentClass{Name: "c"}).
		AddNode("d", AgentClass{Name: "d"}).
		AddEdge("a", "b").
		AddEdge("a", "c").
		AddEdge("b", "d").
		AddEdge("c", "d")

	result, err := Drive(context.Background(), "run-1", mustCompile(t, g), graphExecutor("run-1", nil), DriveOptions{})
	if err != nil {
		t.Fatalf("Drive error = %v", err)
	}
	finished := result.State.finishedClasses()
	for _, id := range []string{"a", "b", "c", "d"} {
		if !finished[id] {
			t.Fatalf("node %q did not finish; state = %#v", id, classNames(result.State))
		}
	}
	// a before b/c before d; ticks: a | b,c | d => 3 ticks.
	if result.Ticks != 3 {
		t.Fatalf("ticks = %d, want 3", result.Ticks)
	}
}

func TestGraphConditionalEdgePrunesBranch(t *testing.T) {
	high := func(r api.TypedReport) bool { return r.Structured["severity"] == "high" }
	low := func(r api.TypedReport) bool { return r.Structured["severity"] != "high" }

	g := NewGraph().
		AddNode("triage", AgentClass{Name: "triage"}).
		AddNode("page", AgentClass{Name: "page"}).
		AddNode("log", AgentClass{Name: "log"}).
		AddConditionalEdge("triage", "page", high).
		AddConditionalEdge("triage", "log", low)

	result, err := Drive(context.Background(), "run-1", mustCompile(t, g),
		graphExecutor("run-1", map[string]map[string]any{"triage": {"severity": "high"}}), DriveOptions{})
	if err != nil {
		t.Fatalf("Drive error = %v", err)
	}
	finished := result.State.finishedClasses()
	if !finished["triage"] || !finished["page"] {
		t.Fatalf("expected triage and page to run; got %#v", classNames(result.State))
	}
	if finished["log"] {
		t.Fatalf("log branch should have been pruned; got %#v", classNames(result.State))
	}
}

func TestGraphAllDeadJoinTerminates(t *testing.T) {
	never := func(api.TypedReport) bool { return false }
	g := NewGraph().
		AddNode("a", AgentClass{Name: "a"}).
		AddNode("b", AgentClass{Name: "b"}).
		AddNode("c", AgentClass{Name: "c"}).
		AddConditionalEdge("a", "b", never).
		AddEdge("b", "c")

	result, err := Drive(context.Background(), "run-1", mustCompile(t, g), graphExecutor("run-1", nil), DriveOptions{})
	if err != nil {
		t.Fatalf("Drive error = %v", err)
	}
	finished := result.State.finishedClasses()
	if !finished["a"] {
		t.Fatalf("entry node a must run")
	}
	if finished["b"] || finished["c"] {
		t.Fatalf("dead branch (b -> c) must not run; got %#v", classNames(result.State))
	}
}

func TestGraphFanInThreadsKeyedInput(t *testing.T) {
	var captured json.RawMessage
	exec := ExecutorFunc(func(_ context.Context, dispatch Dispatch) (api.TypedReport, error) {
		if classNameFromTaskID("run-1", dispatch.Task.ID) == "d" {
			captured = dispatch.Input
		}
		return api.TypedReport{Status: api.ReportStatusSuccess}, nil
	})
	g := NewGraph().
		AddNode("a", AgentClass{Name: "a"}).
		AddNode("b", AgentClass{Name: "b"}).
		AddNode("d", AgentClass{Name: "d"}).
		AddEdge("a", "d").
		AddEdge("b", "d")

	if _, err := Drive(context.Background(), "run-1", mustCompile(t, g), exec, DriveOptions{}); err != nil {
		t.Fatalf("Drive error = %v", err)
	}
	var merged map[string]api.TypedReport
	if err := json.Unmarshal(captured, &merged); err != nil {
		t.Fatalf("fan-in input not a keyed object: %v (raw=%s)", err, captured)
	}
	if _, ok := merged["a"]; !ok {
		t.Fatalf("fan-in input missing parent a: %s", captured)
	}
	if _, ok := merged["b"]; !ok {
		t.Fatalf("fan-in input missing parent b: %s", captured)
	}
}

func TestGraphConcurrentTickIsDeterministic(t *testing.T) {
	exec := graphExecutor("run-1", nil)
	build := func() *CompiledGraph {
		return mustCompile(t, NewGraph().
			AddNode("a", AgentClass{Name: "a"}).
			AddNode("b", AgentClass{Name: "b"}).
			AddNode("c", AgentClass{Name: "c"}).
			AddNode("d", AgentClass{Name: "d"}).
			AddEdge("a", "b").
			AddEdge("a", "c").
			AddEdge("b", "d").
			AddEdge("c", "d"))
	}
	order := func(opts DriveOptions) []string {
		result, err := Drive(context.Background(), "run-1", build(), exec, opts)
		if err != nil {
			t.Fatalf("Drive error = %v", err)
		}
		ids := make([]string, 0, len(result.State.Instances))
		for _, inst := range result.State.Instances {
			ids = append(ids, inst.ID)
		}
		return ids
	}

	// Sequential execution (MaxConcurrency=1) is the reference ordering:
	// nodes fold in node-id order — a | b,c | d.
	want := order(DriveOptions{MaxConcurrency: 1})

	// The concurrent path runs the parallel tick (b, c) in goroutines, but it
	// must fold into the exact same snapshot order every run — independent of
	// completion order and identical to the sequential path — so Next stays a
	// pure function of the snapshot.
	for run := 0; run < 8; run++ {
		got := order(DriveOptions{MaxConcurrency: 4})
		if len(got) != len(want) {
			t.Fatalf("instance count drift: got %v want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("concurrent order != sequential order on run %d: %v != %v", run, got, want)
			}
		}
	}
}

func TestGraphConcurrentFailureSurfacesRootCause(t *testing.T) {
	boom := errors.New("node b boom")
	exec := ExecutorFunc(func(_ context.Context, dispatch Dispatch) (api.TypedReport, error) {
		if classNameFromTaskID("run-1", dispatch.Task.ID) == "b" {
			return api.TypedReport{}, boom
		}
		return api.TypedReport{Status: api.ReportStatusSuccess}, nil
	})
	// a fans out to b and c; b fails. Under concurrency the fail-fast path
	// cancels c, but Drive must surface b's root cause every run — never a
	// sibling's context.Canceled — and must not run downstream d.
	g := NewGraph().
		AddNode("a", AgentClass{Name: "a"}).
		AddNode("b", AgentClass{Name: "b"}).
		AddNode("c", AgentClass{Name: "c"}).
		AddNode("d", AgentClass{Name: "d"}).
		AddEdge("a", "b").
		AddEdge("a", "c").
		AddEdge("b", "d").
		AddEdge("c", "d")

	for run := 0; run < 8; run++ {
		result, err := Drive(context.Background(), "run-1", mustCompile(t, g), exec, DriveOptions{MaxConcurrency: 4})
		if !errors.Is(err, boom) {
			t.Fatalf("run %d: Drive error = %v, want %v", run, err, boom)
		}
		if result.State.finishedClasses()["d"] {
			t.Fatalf("run %d: downstream d must not run after failure", run)
		}
	}
}

func TestGraphSubgraphInheritsParentOptions(t *testing.T) {
	// A 3-node linear subgraph needs 3 ticks. If the parent's DriveOptions
	// propagate into the nested Drive, MaxTicks=2 makes the subgraph exceed
	// its tick budget; without propagation it would silently run to the
	// default 64 and succeed.
	inner := mustCompile(t, NewGraph().
		AddNode("i1", AgentClass{Name: "i1"}).
		AddNode("i2", AgentClass{Name: "i2"}).
		AddNode("i3", AgentClass{Name: "i3"}).
		AddEdge("i1", "i2").
		AddEdge("i2", "i3"))
	parent := mustCompile(t, NewGraph().AddSubgraph("mid", inner))

	leaf := graphExecutor("", nil)
	opts := DriveOptions{MaxTicks: 2}
	if _, err := Drive(context.Background(), "run-1", parent, parent.Executor(leaf, opts), opts); !errors.Is(err, ErrMaxTicksExceeded) {
		t.Fatalf("subgraph did not inherit parent MaxTicks: err = %v, want %v", err, ErrMaxTicksExceeded)
	}
}

func TestGraphSubgraphNodeRunsNested(t *testing.T) {
	sub := mustCompile(t, NewGraph().
		AddNode("inner1", AgentClass{Name: "inner1"}).
		AddNode("inner2", AgentClass{Name: "inner2"}).
		AddEdge("inner1", "inner2"))

	g := NewGraph().
		AddNode("pre", AgentClass{Name: "pre"}).
		AddSubgraph("mid", sub).
		AddNode("post", AgentClass{Name: "post"}).
		AddEdge("pre", "mid").
		AddEdge("mid", "post")

	compiled := mustCompile(t, g)
	leaf := graphExecutor("", nil) // runID embedded per dispatch, not needed here
	result, err := Drive(context.Background(), "run-1", compiled, compiled.Executor(leaf, DriveOptions{}), DriveOptions{})
	if err != nil {
		t.Fatalf("Drive error = %v", err)
	}
	finished := result.State.finishedClasses()
	for _, id := range []string{"pre", "mid", "post"} {
		if !finished[id] {
			t.Fatalf("node %q did not finish; got %#v", id, classNames(result.State))
		}
	}
	// the mid node's report should fold the inner node reports.
	report := result.State.reportForClass("mid")
	if report == nil {
		t.Fatalf("subgraph node produced no report")
	}
	if _, ok := report.Structured["inner1"]; !ok {
		t.Fatalf("subgraph report missing inner1: %#v", report.Structured)
	}
}

type recordingObserver struct {
	mu     sync.Mutex
	starts []string
	ends   []string
}

func (r *recordingObserver) OnStart(d Dispatch) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.starts = append(r.starts, d.Task.ID)
}
func (r *recordingObserver) OnEnd(d Dispatch, _ api.TypedReport) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ends = append(r.ends, d.Task.ID)
}
func (r *recordingObserver) OnError(Dispatch, error) {}

func TestObservedExecutorFiresCallbacks(t *testing.T) {
	obs := &recordingObserver{}
	g := NewGraph().
		AddNode("a", AgentClass{Name: "a"}).
		AddNode("b", AgentClass{Name: "b"}).
		AddEdge("a", "b")
	exec := ObservedExecutor(graphExecutor("run-1", nil), obs)

	if _, err := Drive(context.Background(), "run-1", mustCompile(t, g), exec, DriveOptions{}); err != nil {
		t.Fatalf("Drive error = %v", err)
	}
	if len(obs.starts) != 2 || len(obs.ends) != 2 {
		t.Fatalf("expected 2 starts and 2 ends, got starts=%v ends=%v", obs.starts, obs.ends)
	}
}

func TestGraphCompileRejectsCycle(t *testing.T) {
	_, err := NewGraph().
		AddNode("a", AgentClass{Name: "a"}).
		AddNode("b", AgentClass{Name: "b"}).
		AddEdge("a", "b").
		AddEdge("b", "a").
		Compile()
	if err == nil {
		t.Fatal("expected cycle error")
	}
}

func TestGraphCompileRejectsDanglingEdge(t *testing.T) {
	_, err := NewGraph().
		AddNode("a", AgentClass{Name: "a"}).
		AddEdge("a", "ghost").
		Compile()
	if err == nil {
		t.Fatal("expected unknown-node error")
	}
}

func TestGraphCompileRejectsDuplicateNode(t *testing.T) {
	_, err := NewGraph().
		AddNode("a", AgentClass{Name: "a"}).
		AddNode("a", AgentClass{Name: "a"}).
		Compile()
	if err == nil {
		t.Fatal("expected duplicate-node error")
	}
}

func TestGraphCompileRejectsDuplicateEdge(t *testing.T) {
	_, err := NewGraph().
		AddNode("a", AgentClass{Name: "a"}).
		AddNode("b", AgentClass{Name: "b"}).
		AddEdge("a", "b").
		AddEdge("a", "b").
		Compile()
	if err == nil {
		t.Fatal("expected duplicate-edge error")
	}
}

func TestGraphCompileRejectsIncompatibleEdgeSchema(t *testing.T) {
	upstream := json.RawMessage(`{"type":"object","properties":{"foo":{"type":"string"}}}`)
	downstream := json.RawMessage(`{"type":"object","required":["bar"],"properties":{"bar":{"type":"string"}}}`)
	_, err := NewGraph().
		AddNode("a", AgentClass{Name: "a", OutputSchema: upstream}).
		AddNode("b", AgentClass{Name: "b", InputSchema: downstream}).
		AddEdge("a", "b").
		Compile()
	if err == nil {
		t.Fatal("expected schema-incompatibility error")
	}
}

func TestGraphCompileAcceptsCompatibleEdgeSchema(t *testing.T) {
	upstream := json.RawMessage(`{"type":"object","properties":{"bar":{"type":"string"}}}`)
	downstream := json.RawMessage(`{"type":"object","required":["bar"],"properties":{"bar":{"type":"string"}}}`)
	if _, err := NewGraph().
		AddNode("a", AgentClass{Name: "a", OutputSchema: upstream}).
		AddNode("b", AgentClass{Name: "b", InputSchema: downstream}).
		AddEdge("a", "b").
		Compile(); err != nil {
		t.Fatalf("Compile() unexpected error = %v", err)
	}
}

func TestGraphCompileRejectsNoEntry(t *testing.T) {
	// Only achievable via a cycle; ensure the message path is covered.
	_, err := NewGraph().
		AddNode("a", AgentClass{Name: "a"}).
		AddNode("b", AgentClass{Name: "b"}).
		AddEdge("a", "b").
		AddEdge("b", "a").
		Compile()
	if err == nil {
		t.Fatal("expected error for cyclic / no-entry graph")
	}
}

func TestGraphSurfacesExecutorError(t *testing.T) {
	boom := errors.New("node boom")
	exec := ExecutorFunc(func(_ context.Context, _ Dispatch) (api.TypedReport, error) {
		return api.TypedReport{}, boom
	})
	g := NewGraph().AddNode("a", AgentClass{Name: "a"})
	_, err := Drive(context.Background(), "run-1", mustCompile(t, g), exec, DriveOptions{})
	if !errors.Is(err, boom) {
		t.Fatalf("Drive error = %v, want node boom", err)
	}
}
