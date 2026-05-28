package multiagent

import (
	"context"

	"github.com/Viking602/go-hydaelyn/api"
)

// Executor wiring for graphs lives here: subgraph delegation and the
// observability decorator. Both compose without touching Drive — the graph
// rides the existing Drive -> Executor -> Runner path.

// Executor returns an Executor that runs this graph's nodes against leaf:
// leaf nodes are delegated to leaf directly, while subgraph nodes run a
// nested Drive over their compiled subgraph (with a derived run id and the
// same opts, so a parent's MaxConcurrency/MaxTicks bound the whole subgraph
// tree) and fold the nested result into a single TypedReport. Drive is
// unchanged; the subgraph mechanism is pure executor composition.
func (c *CompiledGraph) Executor(leaf Executor, opts DriveOptions) Executor {
	return ExecutorFunc(func(ctx context.Context, dispatch Dispatch) (api.TypedReport, error) {
		nodeID := classNameFromTaskID(dispatch.Task.RunID, dispatch.Task.ID)
		node, ok := c.nodes[nodeID]
		if !ok || node.sub == nil {
			return leaf.Execute(ctx, dispatch)
		}
		subRunID := dispatch.Task.RunID + "/" + nodeID
		result, err := Drive(ctx, subRunID, node.sub, node.sub.Executor(leaf, opts), opts)
		if err != nil {
			return api.TypedReport{}, err
		}
		return foldSubgraphResult(result), nil
	})
}

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
