package multiagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/Viking602/go-hydaelyn/agent"
	"github.com/Viking602/go-hydaelyn/api"
)

// Graph is a declarative, acyclic orchestration graph. Nodes reference an
// AgentClass (or a nested CompiledGraph); edges declare typed data flow
// with an optional activation predicate. Build it with the chained
// AddNode/AddEdge methods, then Compile() into a CompiledGraph that
// implements Scheduler and runs on the existing Drive loop.
//
// The graph holds no execution state: CompiledGraph.Next is a pure
// function of TeamState, so it is replay-safe exactly like the reference
// Schedulers (ADR-016 hard rule 4) and inherits the durable Runner's
// three-surface reconstruction without a dedicated checkpoint store.
//
// Spec anchor: docs/superpowers/specs/2026-05-29-orchestration-typed-dag-design.md.
type Graph struct {
	nodes     map[string]*graphNode
	order     []string
	edges     []graphEdge
	buildErrs []error
}

// EdgePredicate gates a conditional edge: the edge activates only when the
// predicate returns true for the upstream node's report. It must be a pure
// function — Next may call it during replay.
type EdgePredicate func(upstream api.TypedReport) bool

type graphNode struct {
	id    string
	class AgentClass
	sub   *CompiledGraph // nil for a leaf node
}

type graphEdge struct {
	from string
	to   string
	pred EdgePredicate // nil means unconditional
}

// NewGraph returns an empty Graph.
func NewGraph() *Graph {
	return &Graph{nodes: map[string]*graphNode{}}
}

// AddNode registers a leaf node: a unique nodeID playing the given
// AgentClass. The same AgentClass may back multiple nodes; identity keys on
// nodeID, not the class name.
func (g *Graph) AddNode(nodeID string, class AgentClass) *Graph {
	return g.addNode(nodeID, class, nil)
}

// AddSubgraph registers a node backed by a compiled subgraph. At execution
// the node runs a nested Drive over sub (see CompiledGraph.Executor).
func (g *Graph) AddSubgraph(nodeID string, sub *CompiledGraph) *Graph {
	if sub == nil {
		g.buildErrs = append(g.buildErrs, fmt.Errorf("graph: subgraph node %q is nil", nodeID))
		return g
	}
	return g.addNode(nodeID, AgentClass{Name: nodeID}, sub)
}

func (g *Graph) addNode(nodeID string, class AgentClass, sub *CompiledGraph) *Graph {
	if nodeID == "" {
		g.buildErrs = append(g.buildErrs, errors.New("graph: node id must not be empty"))
		return g
	}
	if _, exists := g.nodes[nodeID]; exists {
		g.buildErrs = append(g.buildErrs, fmt.Errorf("graph: duplicate node id %q", nodeID))
		return g
	}
	g.nodes[nodeID] = &graphNode{id: nodeID, class: class, sub: sub}
	g.order = append(g.order, nodeID)
	return g
}

// AddEdge declares an unconditional dependency: to runs after from, and
// from's OutputSchema must be compatible with to's InputSchema (checked at
// Compile).
func (g *Graph) AddEdge(from, to string) *Graph {
	g.edges = append(g.edges, graphEdge{from: from, to: to})
	return g
}

// AddConditionalEdge declares an edge that activates only when pred returns
// true for from's report. Inactive edges are pruned without blocking the
// target (see the readiness rules in Next).
func (g *Graph) AddConditionalEdge(from, to string, pred EdgePredicate) *Graph {
	g.edges = append(g.edges, graphEdge{from: from, to: to, pred: pred})
	return g
}

// CompiledGraph is the validated, runnable form of a Graph. It implements
// Scheduler so it can be driven by Drive.
type CompiledGraph struct {
	nodes    map[string]*graphNode
	incoming map[string][]graphEdge
	topo     []string
}

type nodeStatus int

const (
	statusBlocked nodeStatus = iota
	statusReady
	statusFinished
	statusDead
)

// Compile validates the graph and returns its runnable form. It is the
// single quality gate: it rejects duplicate/empty node ids, edges to
// unknown nodes, cycles, graphs with no entry node, and edges whose
// upstream output schema is incompatible with the downstream input schema.
func (g *Graph) Compile() (*CompiledGraph, error) {
	if len(g.buildErrs) > 0 {
		return nil, errors.Join(g.buildErrs...)
	}
	if len(g.nodes) == 0 {
		return nil, errors.New("graph: no nodes")
	}

	incoming := make(map[string][]graphEdge, len(g.nodes))
	outgoing := make(map[string][]graphEdge, len(g.nodes))
	seen := make(map[[2]string]bool, len(g.edges))
	for _, e := range g.edges {
		if _, ok := g.nodes[e.from]; !ok {
			return nil, fmt.Errorf("graph: edge from unknown node %q", e.from)
		}
		if _, ok := g.nodes[e.to]; !ok {
			return nil, fmt.Errorf("graph: edge to unknown node %q", e.to)
		}
		key := [2]string{e.from, e.to}
		if seen[key] {
			return nil, fmt.Errorf("graph: duplicate edge %q -> %q", e.from, e.to)
		}
		seen[key] = true
		incoming[e.to] = append(incoming[e.to], e)
		outgoing[e.from] = append(outgoing[e.from], e)
	}

	topo, err := topoSort(g.order, outgoing)
	if err != nil {
		return nil, err
	}

	entries := 0
	for _, id := range g.order {
		if len(incoming[id]) == 0 {
			entries++
		}
	}
	if entries == 0 {
		return nil, errors.New("graph: no entry node (every node has an incoming edge — graph is cyclic or empty)")
	}

	if err := validateEdgeSchemas(g.nodes, incoming); err != nil {
		return nil, err
	}

	return &CompiledGraph{nodes: g.nodes, incoming: incoming, topo: topo}, nil
}

// Next implements Scheduler. It classifies every node from the TeamState
// snapshot in topological order and returns the Dispatch for each node that
// has become ready this tick (sorted by node id for deterministic output).
// Returning no dispatches is the terminal signal Drive waits for.
func (c *CompiledGraph) Next(_ context.Context, state TeamState) ([]Dispatch, error) {
	if state.hasActiveInstance() || state.hasFailedInstance() {
		return nil, nil
	}
	finished := state.finishedClasses()
	status := make(map[string]nodeStatus, len(c.topo))
	activeParents := make(map[string][]string)
	var ready []string

	for _, id := range c.topo {
		if finished[id] {
			status[id] = statusFinished
			continue
		}
		inc := c.incoming[id]
		if len(inc) == 0 {
			status[id] = statusReady
			ready = append(ready, id)
			continue
		}
		settled := true
		var active []string
		for _, e := range inc {
			switch status[e.from] {
			case statusFinished:
				if edgeActive(e, state) {
					active = append(active, e.from)
				}
			case statusDead:
				// settled but contributes no activation.
			default:
				settled = false
			}
			if !settled {
				break
			}
		}
		switch {
		case !settled:
			status[id] = statusBlocked
		case len(active) > 0:
			status[id] = statusReady
			activeParents[id] = active
			ready = append(ready, id)
		default:
			status[id] = statusDead
		}
	}

	sort.Strings(ready)
	out := make([]Dispatch, 0, len(ready))
	for _, id := range ready {
		node := c.nodes[id]
		out = append(out, buildGraphDispatch(state.RunID, id, node.class, inputFor(activeParents[id], state)))
	}
	return out, nil
}

// edgeActive reports whether a finished parent's edge activates its target.
// Unconditional edges always activate; conditional edges consult the
// parent's report through the predicate.
func edgeActive(e graphEdge, state TeamState) bool {
	if e.pred == nil {
		return true
	}
	report := state.reportForClass(e.from)
	if report == nil {
		return false
	}
	return e.pred(*report)
}

// inputFor threads the already-classified active parents (Next evaluates
// edge activation once, in the status pass, so predicates are not
// re-evaluated here) into a node's Input: a single active parent's report is
// forwarded directly (matching RouterScheduler); several active parents are
// marshaled as a {nodeID: report} object.
func inputFor(active []string, state TeamState) json.RawMessage {
	switch len(active) {
	case 0:
		return nil
	case 1:
		return state.reportInput(active[0])
	default:
		sort.Strings(active)
		merged := make(map[string]*api.TypedReport, len(active))
		for _, parent := range active {
			merged[parent] = state.reportForClass(parent)
		}
		raw, err := json.Marshal(merged)
		if err != nil {
			return nil
		}
		return raw
	}
}

// buildGraphDispatch assembles a Dispatch keyed on nodeID rather than class
// name, so the same AgentClass can back multiple nodes. Drive recovers the
// nodeID into AgentInstance.ClassName via classNameFromTaskID, which is what
// finishedClasses (and therefore Next) keys on.
func buildGraphDispatch(runID, nodeID string, class AgentClass, input json.RawMessage) Dispatch {
	taskID := taskIDForClass(runID, nodeID)
	goal := class.Instructions
	if goal == "" {
		goal = class.Description
	}
	return Dispatch{
		To: ComputeInstanceID(nodeID, runID, taskID, "graph"),
		Task: api.Task{
			ID:           taskID,
			RunID:        runID,
			Type:         api.TaskTypeWorker,
			Goal:         goal,
			Status:       api.TaskStatusCreated,
			InputSchema:  class.InputSchema,
			OutputSchema: class.OutputSchema,
		},
		Input: input,
		OutputPolicy: agent.OutputPolicy{
			Schema:   class.OutputSchema,
			Validate: len(class.OutputSchema) > 0,
		},
	}
}

// topoSort returns a topological ordering of the nodes using Kahn's
// algorithm over insertion order for stable output, and reports a cycle as
// an error.
func topoSort(order []string, outgoing map[string][]graphEdge) ([]string, error) {
	indegree := make(map[string]int, len(order))
	for _, id := range order {
		indegree[id] = 0
	}
	for _, edges := range outgoing {
		for _, e := range edges {
			indegree[e.to]++
		}
	}
	queue := make([]string, 0, len(order))
	for _, id := range order {
		if indegree[id] == 0 {
			queue = append(queue, id)
		}
	}
	sorted := make([]string, 0, len(order))
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		sorted = append(sorted, id)
		for _, e := range outgoing[id] {
			indegree[e.to]--
			if indegree[e.to] == 0 {
				queue = append(queue, e.to)
			}
		}
	}
	if len(sorted) != len(order) {
		return nil, errors.New("graph: cycle detected (graph must be acyclic)")
	}
	return sorted, nil
}

// validateEdgeSchemas runs the construction-time compatibility check. A node
// with exactly one incoming edge threads that parent's report directly, so
// the parent OutputSchema must satisfy the node InputSchema. Fan-in nodes
// receive a keyed object and are relaxed. The check is shallow structural
// only (top-level type match + required ⊆ properties); deep JSON Schema
// subtyping is future work.
func validateEdgeSchemas(nodes map[string]*graphNode, incoming map[string][]graphEdge) error {
	tos := make([]string, 0, len(incoming))
	for to := range incoming {
		tos = append(tos, to)
	}
	sort.Strings(tos) // deterministic: report the same incompatible edge every compile.
	for _, to := range tos {
		edges := incoming[to]
		if len(edges) != 1 {
			continue
		}
		from := edges[0].from
		if err := shallowSchemaCompatible(nodes[from].class.OutputSchema, nodes[to].class.InputSchema); err != nil {
			return fmt.Errorf("graph: edge %q -> %q schema incompatible: %w", from, to, err)
		}
	}
	return nil
}

func shallowSchemaCompatible(upstream, downstream json.RawMessage) error {
	if len(downstream) == 0 {
		return nil
	}
	var down map[string]any
	if err := json.Unmarshal(downstream, &down); err != nil {
		return fmt.Errorf("downstream input schema is not a JSON object: %w", err)
	}
	if len(upstream) == 0 {
		return errors.New("downstream declares an input schema but upstream has no output schema")
	}
	var up map[string]any
	if err := json.Unmarshal(upstream, &up); err != nil {
		return fmt.Errorf("upstream output schema is not a JSON object: %w", err)
	}

	if dt, ok := down["type"].(string); ok {
		if ut, ok := up["type"].(string); ok && ut != dt {
			return fmt.Errorf("type %q is not compatible with %q", ut, dt)
		}
	}

	required, _ := down["required"].([]any)
	if len(required) == 0 {
		return nil
	}
	props, _ := up["properties"].(map[string]any)
	for _, r := range required {
		name, ok := r.(string)
		if !ok {
			continue
		}
		if _, present := props[name]; !present {
			return fmt.Errorf("required field %q missing from upstream properties", name)
		}
	}
	return nil
}

// ensure CompiledGraph satisfies Scheduler at compile time.
var _ Scheduler = (*CompiledGraph)(nil)
