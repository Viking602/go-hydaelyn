package workflow

import (
	"encoding/json"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/multiagent"
)

type Condition func(api.TypedReport) bool

type FieldMapping = multiagent.FieldMapping

type Definition struct {
	Name  string `json:"name"`
	Steps []Step `json:"steps,omitempty"`
	Edges []Edge `json:"edges,omitempty"`
}

type Step struct {
	ID    string                `json:"id"`
	Class multiagent.AgentClass `json:"class"`
}

type Edge struct {
	From      string         `json:"from"`
	To        string         `json:"to"`
	Condition Condition      `json:"-"`
	Mappings  []FieldMapping `json:"mappings,omitempty"`
}

type Builder struct {
	def Definition
}

func New(name string) *Builder {
	return &Builder{def: Definition{Name: name}}
}

func (b *Builder) Step(id string, class multiagent.AgentClass) *Builder {
	b.def.Steps = append(b.def.Steps, Step{ID: id, Class: cloneAgentClass(class)})
	return b
}

func (b *Builder) Then(from, to string) *Builder {
	b.def.Edges = append(b.def.Edges, Edge{From: from, To: to})
	return b
}

func (b *Builder) Branch(from, to string, condition Condition) *Builder {
	b.def.Edges = append(b.def.Edges, Edge{From: from, To: to, Condition: condition})
	return b
}

func (b *Builder) Map(from, to string, mappings ...FieldMapping) *Builder {
	b.def.Edges = append(b.def.Edges, Edge{From: from, To: to, Mappings: append([]FieldMapping(nil), mappings...)})
	return b
}

func (b *Builder) Definition() Definition {
	out := Definition{Name: b.def.Name}
	out.Steps = make([]Step, len(b.def.Steps))
	for i, step := range b.def.Steps {
		out.Steps[i] = Step{ID: step.ID, Class: cloneAgentClass(step.Class)}
	}
	out.Edges = append([]Edge(nil), b.def.Edges...)
	for i := range out.Edges {
		out.Edges[i].Mappings = append([]FieldMapping(nil), out.Edges[i].Mappings...)
	}
	return out
}

func cloneAgentClass(in multiagent.AgentClass) multiagent.AgentClass {
	out := in
	out.Skills = append([]string(nil), in.Skills...)
	out.AvailableSkills = append([]string(nil), in.AvailableSkills...)
	out.Tools = append([]string(nil), in.Tools...)
	out.InputSchema = append([]byte(nil), in.InputSchema...)
	out.OutputSchema = append([]byte(nil), in.OutputSchema...)
	out.Capabilities = cloneCapabilities(in.Capabilities)
	return out
}

func cloneCapabilities(in []api.Capability) []api.Capability {
	if len(in) == 0 {
		return nil
	}
	out := make([]api.Capability, len(in))
	for i, capability := range in {
		out[i] = capability
		out[i].InputSchema = cloneAnyMap(capability.InputSchema)
		out[i].OutputSchema = cloneAnyMap(capability.OutputSchema)
		out[i].Tags = append([]string(nil), capability.Tags...)
		out[i].Metadata = cloneStringMap(capability.Metadata)
	}
	return out
}

func cloneAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
