package workflow

import (
	"errors"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/multiagent"
)

var ErrCompiledWorkflowEmpty = errors.New("workflow: definition has no steps")

type Compiled struct {
	Name  string
	graph *multiagent.CompiledGraph
}

func Compile(def Definition) (Compiled, error) {
	if len(def.Steps) == 0 {
		return Compiled{}, ErrCompiledWorkflowEmpty
	}
	g := multiagent.NewGraph()
	for _, step := range def.Steps {
		g.AddNode(step.ID, cloneAgentClass(step.Class))
	}
	for _, edge := range def.Edges {
		opts := make([]multiagent.EdgeOption, 0, 2)
		if edge.Condition != nil {
			condition := edge.Condition
			opts = append(opts, multiagent.WithPredicate(func(report api.TypedReport) bool {
				return condition(report)
			}))
		}
		if len(edge.Mappings) > 0 {
			mappings := append([]FieldMapping(nil), edge.Mappings...)
			opts = append(opts, multiagent.WithFieldMapping(mappings...))
		}
		g.AddEdgeWith(edge.From, edge.To, opts...)
	}
	compiled, err := g.Compile()
	if err != nil {
		return Compiled{}, err
	}
	return Compiled{Name: def.Name, graph: compiled}, nil
}

func (c Compiled) Scheduler() multiagent.Scheduler {
	return c.graph
}

func (c Compiled) Graph() *multiagent.CompiledGraph {
	return c.graph
}
