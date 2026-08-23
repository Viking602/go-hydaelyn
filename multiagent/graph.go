package multiagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Viking602/venat/agent"
	"github.com/Viking602/venat/api"
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

// FieldMapping declares field-level fan-in: the upstream report's
// Structured[From] value is projected into the downstream input object under
// the To name. Both are top-level field names (not JSON paths), matching the
// discriminator convention in discriminatorValue.
type FieldMapping struct {
	From string
	To   string
}

// EdgeOption configures an edge declared via AddEdgeWith.
type EdgeOption func(*graphEdge)

// WithPredicate makes an edge conditional: it activates only when pred returns
// true for the upstream node's report. AddConditionalEdge is a thin wrapper
// over AddEdgeWith with this option.
func WithPredicate(pred EdgePredicate) EdgeOption {
	return func(e *graphEdge) { e.pred = pred }
}

// WithFieldMapping declares field-level fan-in on an edge: instead of threading
// the whole upstream report, the listed Structured fields are projected into
// the downstream input object under their To names. Mixing mapped and unmapped
// incoming edges on one node, or two mappings targeting the same To field, is
// rejected at Compile.
func WithFieldMapping(mappings ...FieldMapping) EdgeOption {
	return func(e *graphEdge) { e.mappings = append(e.mappings, mappings...) }
}

type graphNode struct {
	id    string
	class AgentClass
	sub   *CompiledGraph // nil for a leaf node
}

type graphEdge struct {
	from     string
	to       string
	pred     EdgePredicate // nil means unconditional
	mappings []FieldMapping
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

// AddEdgeWith declares an edge from -> to configured by opts (WithPredicate,
// WithFieldMapping). AddEdge and AddConditionalEdge are thin wrappers over it.
func (g *Graph) AddEdgeWith(from, to string, opts ...EdgeOption) *Graph {
	e := graphEdge{from: from, to: to}
	for _, opt := range opts {
		opt(&e)
	}
	g.edges = append(g.edges, e)
	return g
}

// AddEdge declares an unconditional dependency: to runs after from, and
// from's OutputSchema must be compatible with to's InputSchema (checked at
// Compile).
func (g *Graph) AddEdge(from, to string) *Graph {
	return g.AddEdgeWith(from, to)
}

// AddConditionalEdge declares an edge that activates only when pred returns
// true for from's report. Inactive edges are pruned without blocking the
// target (see the readiness rules in Next).
func (g *Graph) AddConditionalEdge(from, to string, pred EdgePredicate) *Graph {
	return g.AddEdgeWith(from, to, WithPredicate(pred))
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
	activeParents := make(map[string][]graphEdge)
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
		var active []graphEdge
		for _, e := range inc {
			switch status[e.from] {
			case statusFinished:
				if edgeActive(e, state) {
					active = append(active, e)
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
		input, err := inputFor(activeParents[id], state)
		if err != nil {
			return nil, fmt.Errorf("graph: node %q input resolution failed: %w", id, err)
		}
		out = append(out, buildGraphDispatch(state.RunID, id, node.class, input))
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

func inputFor(active []graphEdge, state TeamState) (json.RawMessage, error) {
	if len(active) == 0 {
		return nil, nil
	}
	// Compile enforces all-or-none mapping per target, so the first active
	// edge's mapping presence classifies the whole fan-in.
	if len(active[0].mappings) > 0 {
		return mappedInput(active, state)
	}
	if len(active) == 1 {
		return state.reportInput(active[0].from)
	}
	froms := make([]string, 0, len(active))
	for _, e := range active {
		froms = append(froms, e.from)
	}
	sort.Strings(froms)
	merged := make(map[string]*api.TypedReport, len(froms))
	for _, from := range froms {
		merged[from] = state.reportForClass(from)
	}
	raw, err := json.Marshal(merged)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

// mappedInput builds the flat input object for field-mapped fan-in: each
// active edge projects its declared upstream Structured fields into the
// downstream input under their To names. Edges are processed in from order so
// the marshaled bytes are deterministic; Compile rejects duplicate To targets,
// so distinct edges never contend for the same key.
func mappedInput(active []graphEdge, state TeamState) (json.RawMessage, error) {
	sorted := append([]graphEdge(nil), active...)
	sort.Slice(sorted, func(a, b int) bool { return sorted[a].from < sorted[b].from })
	out := make(map[string]any)
	for _, e := range sorted {
		report := state.reportForClass(e.from)
		if report == nil || report.Structured == nil {
			continue
		}
		for _, m := range e.mappings {
			if value, ok := report.Structured[m.From]; ok {
				out[m.To] = value
			}
		}
	}
	if len(out) == 0 {
		// Every active parent finished without any of the mapped fields
		// in its Structured payload — a runtime schema drift. Returning
		// nil here would dispatch the downstream node with empty input
		// and no signal, likely producing a confusing failure inside the
		// agent. Surface a scheduler-level error instead so the cause is
		// attributable.
		froms := make([]string, 0, len(sorted))
		for _, e := range sorted {
			froms = append(froms, e.from)
		}
		return nil, fmt.Errorf("graph: node input is empty — mapped fields %q not present in any active parent report %s", mappedFieldNames(sorted), strings.Join(froms, ", "))
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

// mappedFieldNames returns the distinct To-names a set of mapped edges
// project, sorted for deterministic error messages.
func mappedFieldNames(edges []graphEdge) []string {
	seen := make(map[string]struct{})
	for _, e := range edges {
		for _, m := range e.mappings {
			seen[m.To] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// buildGraphDispatch assembles a Dispatch keyed on nodeID rather than class
// name, so the same AgentClass can back multiple nodes. Drive recovers the
// nodeID into AgentInstance.ClassName via classNameFromTaskID, which is what
// graph schedulers need to correlate reports back to nodes.
func buildGraphDispatch(runID, nodeID string, class AgentClass, input json.RawMessage) Dispatch {
	taskID := taskIDForClass(runID, nodeID)
	goal := class.Instructions
	if goal == "" {
		goal = class.Description
	}
	return Dispatch{
		To:             ComputeInstanceID(nodeID, runID, taskID, "graph"),
		ClassName:      nodeID,
		AgentClassName: class.Name,
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

// validateEdgeSchemas runs the construction-time compatibility check. Each
// fan-in target's incoming edges must be either all field-mapped or all
// unmapped (mixing is rejected). For mapped targets, every declared mapping is
// type-checked per field and no two mappings may target the same downstream
// field. For unmapped targets, a node with exactly one incoming edge threads
// that parent's report directly, so the parent OutputSchema must satisfy the
// node InputSchema; unmapped multi-parent fan-in receives a keyed object and
// is relaxed.
func validateEdgeSchemas(nodes map[string]*graphNode, incoming map[string][]graphEdge) error {
	tos := make([]string, 0, len(incoming))
	for to := range incoming {
		tos = append(tos, to)
	}
	sort.Strings(tos) // deterministic: report the same incompatible edge every compile.
	for _, to := range tos {
		edges := incoming[to]
		mapped, unmapped := 0, 0
		for _, e := range edges {
			if len(e.mappings) > 0 {
				mapped++
			} else {
				unmapped++
			}
		}
		if mapped > 0 && unmapped > 0 {
			return fmt.Errorf("graph: node %q mixes mapped and unmapped incoming edges; make them all mapped or all unmapped", to)
		}
		if mapped > 0 {
			if err := validateMappedEdges(nodes, to, edges); err != nil {
				return err
			}
			continue
		}
		if len(edges) != 1 {
			continue
		}
		from := edges[0].from
		if err := schemaCompatible(nodes[from].class.OutputSchema, nodes[to].class.InputSchema); err != nil {
			return fmt.Errorf("graph: edge %q -> %q schema incompatible: %w", from, to, err)
		}
	}
	return nil
}

// validateMappedEdges checks a single mapped fan-in target: no two mappings
// (across any of its incoming edges) target the same downstream field, and
// each mapping's upstream field type is compatible with the downstream field
// it feeds.
func validateMappedEdges(nodes map[string]*graphNode, to string, edges []graphEdge) error {
	seenTo := make(map[string]string) // downstream field -> upstream node that already claimed it
	for _, e := range edges {
		for _, m := range e.mappings {
			if prev, dup := seenTo[m.To]; dup {
				return fmt.Errorf("graph: node %q has duplicate field mapping target %q (from %q and %q)", to, m.To, prev, e.from)
			}
			seenTo[m.To] = e.from
			if err := fieldSchemaCompatible(nodes[e.from].class.OutputSchema, nodes[to].class.InputSchema, m.From, m.To); err != nil {
				return fmt.Errorf("graph: edge %q -> %q mapping %s->%s incompatible: %w", e.from, to, m.From, m.To, err)
			}
		}
	}
	return nil
}

// maxSchemaDepth bounds the recursive schema walk so a pathological or
// self-referential schema cannot loop forever; nesting beyond it is reported
// as an error rather than silently accepted.
const maxSchemaDepth = 32

// schemaCompatible reports whether an upstream output schema can satisfy a
// downstream input schema. It checks the structural `type` dimension and
// recurses through object properties (for downstream required fields) and
// array items. It is intentionally not a full JSON Schema validator:
// combinators ($ref/allOf/oneOf/anyOf/not) and keyword constraints
// (format/pattern/enum) are treated as undecidable and pass.
func schemaCompatible(upstream, downstream json.RawMessage) error {
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
	return schemaNodeCompatible(up, down, 0)
}

func schemaNodeCompatible(up, down map[string]any, depth int) error {
	if depth > maxSchemaDepth {
		return fmt.Errorf("schema nesting exceeds supported depth (%d)", maxSchemaDepth)
	}
	if hasSchemaCombinator(down) || hasSchemaCombinator(up) {
		return nil // undecidable here — accept and stop descending.
	}
	dt, dtOK := down["type"].(string)
	ut, utOK := up["type"].(string)
	if dtOK && utOK && ut != dt {
		return fmt.Errorf("type %q is not compatible with %q", ut, dt)
	}
	switch dt {
	case "object":
		return schemaObjectCompatible(up, down, depth)
	case "array":
		return schemaArrayCompatible(up, down, depth)
	default:
		return nil
	}
}

func schemaObjectCompatible(up, down map[string]any, depth int) error {
	required, _ := down["required"].([]any)
	if len(required) == 0 {
		return nil
	}
	upProps, _ := up["properties"].(map[string]any)
	downProps, _ := down["properties"].(map[string]any)
	for _, r := range required {
		name, ok := r.(string)
		if !ok {
			continue
		}
		raw, present := upProps[name]
		if !present {
			return fmt.Errorf("required field %q missing from upstream properties", name)
		}
		upField, ok := raw.(map[string]any)
		if !ok {
			continue // present but not a sub-schema object; name presence suffices.
		}
		downField, ok := downProps[name].(map[string]any)
		if !ok {
			continue // downstream gives no sub-schema for this field.
		}
		if err := schemaNodeCompatible(upField, downField, depth+1); err != nil {
			return fmt.Errorf("field %q: %w", name, err)
		}
	}
	return nil
}

func schemaArrayCompatible(up, down map[string]any, depth int) error {
	downItems, ok := down["items"].(map[string]any)
	if !ok {
		return nil
	}
	upItems, ok := up["items"].(map[string]any)
	if !ok {
		return nil // upstream items unconstrained; accept.
	}
	if err := schemaNodeCompatible(upItems, downItems, depth+1); err != nil {
		return fmt.Errorf("items: %w", err)
	}
	return nil
}

func hasSchemaCombinator(node map[string]any) bool {
	for _, key := range []string{"$ref", "allOf", "oneOf", "anyOf", "not"} {
		if _, ok := node[key]; ok {
			return true
		}
	}
	return false
}

// fieldSchemaCompatible type-checks a single FieldMapping: the upstream output
// field `from` must be able to satisfy the downstream input field `to`. When
// the downstream schema does not constrain `to`, the mapping is accepted; when
// the upstream schema declares properties but lacks `from`, it is rejected.
func fieldSchemaCompatible(upstream, downstream json.RawMessage, from, to string) error {
	downField, downOK := propertySchema(downstream, to)
	if !downOK {
		return nil // downstream does not constrain the target field.
	}
	upField, upOK := propertySchema(upstream, from)
	if !upOK {
		return fmt.Errorf("upstream output schema has no field %q to map", from)
	}
	return schemaNodeCompatible(upField, downField, 0)
}

// propertySchema extracts the named property's sub-schema from a JSON Schema
// object, reporting false when the schema is absent, unparseable, declares no
// properties, or the property is not itself a schema object.
func propertySchema(raw json.RawMessage, field string) (map[string]any, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, false
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		return nil, false
	}
	sub, ok := props[field].(map[string]any)
	if !ok {
		return nil, false
	}
	return sub, true
}

// ensure CompiledGraph satisfies Scheduler at compile time.
var _ Scheduler = (*CompiledGraph)(nil)
