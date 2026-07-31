package multiagent

import (
	"context"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/stream"
)

// Executor wiring for graphs lives here: subgraph delegation and the
// observability decorator. Both compose without touching Drive — the graph
// rides the existing Drive -> Executor -> Runner path.

// Executor returns an Executor that runs this graph's nodes against leaf:
// leaf nodes are delegated to leaf directly, while subgraph nodes run a
// nested Drive over their compiled subgraph (with a derived run id and the
// same opts, so a parent's MaxConcurrency/MaxTicks bound the whole subgraph
// tree) and fold the nested result into a single TypedReport. Drive is
// unchanged; the subgraph mechanism is pure executor composition. The returned
// executor also implements StreamingExecutor, so a subgraph node's frames flow
// to the parent's DriveOptions.Sink, labeled by their nested node ids.
func (c *CompiledGraph) Executor(leaf Executor, opts DriveOptions) Executor {
	return &compiledGraphExecutor{graph: c, leaf: leaf, opts: opts}
}

type compiledGraphExecutor struct {
	graph *CompiledGraph
	leaf  Executor
	opts  DriveOptions
}

func (g *compiledGraphExecutor) Execute(ctx context.Context, dispatch Dispatch) (api.TypedReport, error) {
	sub := g.subgraphFor(dispatch)
	if sub == nil {
		return g.leaf.Execute(ctx, dispatch)
	}
	// Execute is the non-streaming entry: never propagate a Sink into the
	// nested Drive (ExecuteStream is the path that threads frames). This keeps
	// streaming strictly opt-in through ExecuteStream.
	subOpts := g.opts
	subOpts.Sink = nil
	result, err := Drive(ctx, g.subRunID(dispatch), sub, sub.Executor(g.leaf, subOpts), subOpts)
	if err != nil {
		return api.TypedReport{}, err
	}
	return foldSubgraphResult(result), nil
}

func (g *compiledGraphExecutor) ExecuteStream(ctx context.Context, dispatch Dispatch, sink stream.Sink) (api.TypedReport, error) {
	sub := g.subgraphFor(dispatch)
	if sub == nil {
		// Leaf node: delegate to the leaf's streaming path when it has one.
		if streamer, ok := g.leaf.(StreamingExecutor); ok {
			return streamer.ExecuteStream(ctx, dispatch, sink)
		}
		return g.leaf.Execute(ctx, dispatch)
	}
	// Subgraph node: thread the sink through the nested Drive so nested-node
	// frames reach the same consumer, labeled by their node ids.
	subOpts := g.opts
	subOpts.Sink = sink
	result, err := Drive(ctx, g.subRunID(dispatch), sub, sub.Executor(g.leaf, subOpts), subOpts)
	if err != nil {
		return api.TypedReport{}, err
	}
	return foldSubgraphResult(result), nil
}

func (g *compiledGraphExecutor) subgraphFor(dispatch Dispatch) *CompiledGraph {
	nodeID := dispatchClassName(dispatch)
	node, ok := g.graph.nodes[nodeID]
	if !ok || node.sub == nil {
		return nil
	}
	return node.sub
}

func (g *compiledGraphExecutor) subRunID(dispatch Dispatch) string {
	return dispatch.Task.RunID + "/" + dispatchClassName(dispatch)
}

func dispatchClassName(dispatch Dispatch) string {
	if dispatch.ClassName != "" {
		return dispatch.ClassName
	}
	return classNameFromTaskID(dispatch.Task.RunID, dispatch.Task.ID)
}

var _ StreamingExecutor = (*compiledGraphExecutor)(nil)

// foldSubgraphResult collapses a nested DriveResult into one TypedReport,
// keying each finished node's report by its node id under Structured.
func foldSubgraphResult(result DriveResult) api.TypedReport {
	structured := make(map[string]any)
	for _, instance := range result.State.Instances {
		if instance.State != InstanceStateFinished {
			continue
		}
		if report := result.State.reportForClass(instance.ClassName); report != nil {
			structured[instance.ClassName] = report
		}
	}
	return api.TypedReport{
		Status:     api.ReportStatusSuccess,
		Summary:    "subgraph completed",
		Structured: structured,
	}
}

// NodeObserver receives node-execution lifecycle callbacks. It is the
// aspect/injection point for logging, metrics, and tracing. Implementations
// must be safe for concurrent use when Drive runs nodes in parallel.
type NodeObserver interface {
	OnStart(dispatch Dispatch)
	OnEnd(dispatch Dispatch, report api.TypedReport)
	OnError(dispatch Dispatch, err error)
}

// ObservedExecutor wraps inner so obs sees every node's start, success, and
// failure. It composes with CompiledGraph.Executor and can itself wrap a
// hook.Chain-backed Executor.
func ObservedExecutor(inner Executor, obs NodeObserver) Executor {
	if obs == nil {
		return inner
	}
	return ExecutorFunc(func(ctx context.Context, dispatch Dispatch) (api.TypedReport, error) {
		obs.OnStart(dispatch)
		report, err := inner.Execute(ctx, dispatch)
		if err != nil {
			obs.OnError(dispatch, err)
			return report, err
		}
		obs.OnEnd(dispatch, report)
		return report, nil
	})
}
