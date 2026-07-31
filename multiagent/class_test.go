package multiagent

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Viking602/venat/agent"
	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/tool"
	"github.com/Viking602/venat/tool/kit"
)

func TestAgentClass_ToSpec_CarriesExecutableFields(t *testing.T) {
	class := AgentClass{
		Name:            "Researcher",
		Description:     "looks things up",
		Instructions:    "You research.",
		Model:           "opus",
		Skills:          []string{"code-review"},
		AvailableSkills: []string{"pdf-processing"},
		Tools:           []string{"lookup", "search"},
		InputSchema:     json.RawMessage(`{"type":"object"}`),
		OutputSchema:    json.RawMessage(`{"type":"string"}`),
		LoopPolicy:      agent.LoopPolicy{MaxIterations: 7},
		Capabilities:    []api.Capability{{Name: "research"}},
	}

	spec := class.ToSpec()

	if spec.Instructions != class.Instructions {
		t.Errorf("Instructions = %q, want %q", spec.Instructions, class.Instructions)
	}
	if spec.Model != class.Model {
		t.Errorf("Model = %q, want %q", spec.Model, class.Model)
	}
	if !reflect.DeepEqual(spec.Skills, class.Skills) {
		t.Errorf("Skills = %v, want %v", spec.Skills, class.Skills)
	}
	if !reflect.DeepEqual(spec.AvailableSkills, class.AvailableSkills) {
		t.Errorf("AvailableSkills = %v, want %v", spec.AvailableSkills, class.AvailableSkills)
	}
	if !reflect.DeepEqual(spec.Tools, class.Tools) {
		t.Errorf("Tools = %v, want %v", spec.Tools, class.Tools)
	}
	if spec.LoopPolicy.MaxIterations != 7 {
		t.Errorf("LoopPolicy.MaxIterations = %d, want 7", spec.LoopPolicy.MaxIterations)
	}
	if string(spec.InputSchema) != `{"type":"object"}` {
		t.Errorf("InputSchema = %s, want the class schema", spec.InputSchema)
	}
	if string(spec.OutputSchema) != `{"type":"string"}` {
		t.Errorf("OutputSchema = %s, want the class schema", spec.OutputSchema)
	}
}

// TestAgentClass_ToSpec_IgnoresRolePositioningFields pins the discipline that
// ToSpec is materialization, not positioning: two classes that differ only in
// their Team-facing ontology (Name, Description, Capabilities) must project to
// an identical, byte-for-byte equal Spec.
func TestAgentClass_ToSpec_IgnoresRolePositioningFields(t *testing.T) {
	base := AgentClass{Instructions: "i", Skills: []string{"code-review"}, Model: "m", Tools: []string{"t"}}

	first := base
	first.Name = "Alpha"
	first.Description = "the alpha role"
	first.Capabilities = []api.Capability{{Name: "alpha"}}

	second := base
	second.Name = "Beta"
	second.Description = "the beta role"
	second.Capabilities = []api.Capability{{Name: "beta"}}

	if !reflect.DeepEqual(first.ToSpec(), second.ToSpec()) {
		t.Fatalf("ToSpec leaked role-positioning fields:\n first = %#v\nsecond = %#v", first.ToSpec(), second.ToSpec())
	}
}

// TestAgentClass_ToSpec_BuildEquivalence pins switch consistency: a role used as
// a Team member (built from class.ToSpec()) and the same role used as a
// standalone agent (built from the same Spec) resolve to engines bound to the
// same model and the same tool subset. Positioning is the caller's choice; the
// materialized Engine is identical either way.
func TestAgentClass_ToSpec_BuildEquivalence(t *testing.T) {
	driver := &classTestProvider{models: []string{"m"}}
	lookup := mustClassTool(t, "lookup")
	other := mustClassTool(t, "other")
	deps := agent.BuildDeps{
		Providers: provider.Single(driver),
		Tools:     tool.NewBus(lookup, other),
	}
	class := AgentClass{
		Name:         "Researcher",
		Instructions: "You research.",
		Model:        "m",
		Tools:        []string{"lookup"},
	}

	memberEngine, err := agent.Build(class.ToSpec(), deps)
	if err != nil {
		t.Fatalf("build as team member: %v", err)
	}
	standaloneEngine, err := agent.Build(class.ToSpec(), deps)
	if err != nil {
		t.Fatalf("build as standalone agent: %v", err)
	}

	if memberEngine.Model != standaloneEngine.Model || memberEngine.Model != "m" {
		t.Fatalf("models diverged: member=%q standalone=%q", memberEngine.Model, standaloneEngine.Model)
	}
	for _, engine := range []agent.Engine{memberEngine, standaloneEngine} {
		if engine.Tools == nil {
			t.Fatal("engine missing its tool subset")
		}
		if _, ok := engine.Tools.Driver("lookup"); !ok {
			t.Fatal("engine subset missing the declared tool")
		}
		if _, ok := engine.Tools.Driver("other"); ok {
			t.Fatal("engine subset leaked an undeclared tool")
		}
	}
}

type classTestProvider struct {
	models []string
}

func (p *classTestProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "class-test", Models: p.models}
}

func (p *classTestProvider) Stream(_ context.Context, _ provider.Request) (provider.Stream, error) {
	return provider.NewSliceStream([]provider.Event{
		{Kind: provider.EventTextDelta, Text: "ok"},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}), nil
}

func mustClassTool(t *testing.T, name string) tool.Driver {
	t.Helper()
	driver, err := kit.Tool(name, func(_ context.Context, _ struct {
		Query string `json:"query"`
	}) (string, error) {
		return name + ":ok", nil
	})
	if err != nil {
		t.Fatalf("tool %q setup: %v", name, err)
	}
	return driver
}
