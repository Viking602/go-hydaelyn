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

func oneBatchScheduler(classes ...AgentClass) Scheduler {
	frozen := append([]AgentClass(nil), classes...)
	return SchedulerFunc(func(_ context.Context, state TeamState) ([]Dispatch, error) {
		if len(state.Instances) > 0 {
			return nil, nil
		}
		dispatches := make([]Dispatch, 0, len(frozen))
		for index, class := range frozen {
			dispatches = append(dispatches, buildDispatch(state.RunID, class, index, nil))
		}
		return dispatches, nil
	})
}

func TestDriveStreamsFramesLabeledByNode(t *testing.T) {
	scheduler := SequentialScheduler{Classes: []AgentClass{{Name: "a"}, {Name: "b"}}}
	sink := &collectSink{}
	if _, err := Drive(context.Background(), "run-1", scheduler, streamingNodeExecutor{}, DriveOptions{Sink: sink}); err != nil {
		t.Fatalf("Drive error = %v", err)
	}
	got := sink.textBySource()
	if got["a"] != "hello-a" || got["b"] != "hello-b" {
		t.Fatalf("frames by source = %#v, want a:hello-a b:hello-b", got)
	}
}

func TestDriveStreamingFoldMatchesNonStreaming(t *testing.T) {
	build := func() Scheduler {
		return oneBatchScheduler(
			AgentClass{Name: "a"},
			AgentClass{Name: "b"},
			AgentClass{Name: "c"},
			AgentClass{Name: "d"},
		)
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
	// transient side-channel, so the concurrent batch folds identically with
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
	scheduler := SequentialScheduler{Classes: []AgentClass{{Name: "a"}}}
	sink := &collectSink{}
	plain := ExecutorFunc(func(context.Context, Dispatch) (api.TypedReport, error) {
		return api.TypedReport{Status: api.ReportStatusSuccess}, nil
	})
	// A plain ExecutorFunc is not a StreamingExecutor, so the run completes
	// without frames.
	if _, err := Drive(context.Background(), "run-1", scheduler, plain, DriveOptions{Sink: sink}); err != nil {
		t.Fatalf("Drive error = %v", err)
	}
	if n := sink.count(); n != 0 {
		t.Fatalf("non-streaming executor must produce no frames, got %d", n)
	}
}
