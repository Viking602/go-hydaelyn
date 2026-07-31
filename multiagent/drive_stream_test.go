package multiagent

import (
	"context"
	"sync"
	"testing"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/stream"
)

// collectSink records every frame; safe for concurrent Emit so it can sit
// under the bounded-concurrency Drive path.
type collectSink struct {
	mu     sync.Mutex
	frames []stream.Frame
}

func (c *collectSink) Emit(_ context.Context, frame stream.Frame) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.frames = append(c.frames, frame)
	return nil
}

func (c *collectSink) textBySource() map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]string, len(c.frames))
	for _, f := range c.frames {
		if f.Kind == stream.FrameText {
			out[f.Source] = f.Text
		}
	}
	return out
}

func (c *collectSink) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.frames)
}

// streamingNodeExecutor emits one text frame per node then returns success. It
// implements StreamingExecutor; its plain Execute path emits nothing, mirroring
// how a real executor only streams when asked.
type streamingNodeExecutor struct{}

func (streamingNodeExecutor) Execute(_ context.Context, _ Dispatch) (api.TypedReport, error) {
	return api.TypedReport{Status: api.ReportStatusSuccess}, nil
}

func (e streamingNodeExecutor) ExecuteStream(ctx context.Context, dispatch Dispatch, sink stream.Sink) (api.TypedReport, error) {
	node := classNameFromTaskID(dispatch.Task.RunID, dispatch.Task.ID)
	if err := sink.Emit(ctx, stream.Frame{Kind: stream.FrameText, Text: "hello-" + node}); err != nil {
		return api.TypedReport{}, err
	}
	return e.Execute(ctx, dispatch)
}

func TestDriveStreamsFramesLabeledByNode(t *testing.T) {
	g := NewGraph().
		AddNode("a", AgentClass{Name: "a"}).
		AddNode("b", AgentClass{Name: "b"}).
		AddEdge("a", "b")
	sink := &collectSink{}
	if _, err := Drive(context.Background(), "run-1", mustCompile(t, g), streamingNodeExecutor{}, DriveOptions{Sink: sink}); err != nil {
		t.Fatalf("Drive error = %v", err)
	}
	got := sink.textBySource()
	if got["a"] != "hello-a" || got["b"] != "hello-b" {
		t.Fatalf("frames by source = %#v, want a:hello-a b:hello-b", got)
	}
}

func TestDriveStreamingFoldMatchesNonStreaming(t *testing.T) {
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
	ids := func(opts DriveOptions) []string {
		result, err := Drive(context.Background(), "run-1", build(), streamingNodeExecutor{}, opts)
		if err != nil {
			t.Fatalf("Drive error = %v", err)
		}
		out := make([]string, 0, len(result.State.Instances))
		for _, inst := range result.State.Instances {
			out = append(out, inst.ID)
		}
		return out
	}
	// Attaching a Sink must not change the folded snapshot: frames are a
	// transient side-channel, so the concurrent diamond folds identically with
	// and without a consumer attached.
	want := ids(DriveOptions{MaxConcurrency: 4})
	got := ids(DriveOptions{MaxConcurrency: 4, Sink: &collectSink{}})
	if len(got) != len(want) {
		t.Fatalf("instance count drift with Sink: %v vs %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Sink changed folded order at %d: %v != %v", i, got, want)
		}
	}
}

func TestDriveSinkWithNonStreamingExecutorRunsWithoutFrames(t *testing.T) {
	g := NewGraph().AddNode("a", AgentClass{Name: "a"})
	sink := &collectSink{}
	// graphExecutor is a plain ExecutorFunc — not a StreamingExecutor — so the
	// run completes but produces no frames (documented degraded mode).
	if _, err := Drive(context.Background(), "run-1", mustCompile(t, g), graphExecutor("run-1", nil), DriveOptions{Sink: sink}); err != nil {
		t.Fatalf("Drive error = %v", err)
	}
	if n := sink.count(); n != 0 {
		t.Fatalf("non-streaming executor must produce no frames, got %d", n)
	}
}

func TestDriveSubgraphFramesFlowToParentSink(t *testing.T) {
	inner := mustCompile(t, NewGraph().
		AddNode("inner1", AgentClass{Name: "inner1"}).
		AddNode("inner2", AgentClass{Name: "inner2"}).
		AddEdge("inner1", "inner2"))
	parent := mustCompile(t, NewGraph().AddSubgraph("mid", inner))
	sink := &collectSink{}
	opts := DriveOptions{Sink: sink}
	if _, err := Drive(context.Background(), "run-1", parent, parent.Executor(streamingNodeExecutor{}, opts), opts); err != nil {
		t.Fatalf("Drive error = %v", err)
	}
	got := sink.textBySource()
	if got["inner1"] != "hello-inner1" || got["inner2"] != "hello-inner2" {
		t.Fatalf("subgraph frames by source = %#v, want inner1/inner2 labels", got)
	}
}
