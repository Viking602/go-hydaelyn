package workflow_test

import (
	"encoding/json"
	"testing"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/multiagent"
	"github.com/Viking602/go-hydaelyn/workflow"
)

func TestBuilderDefinesNamedStepsAndEdges(t *testing.T) {
	def := workflow.New("triage").
		Step("intake", multiagent.AgentClass{Name: "intake"}).
		Step("respond", multiagent.AgentClass{Name: "respond"}).
		Then("intake", "respond").
		Definition()

	if def.Name != "triage" {
		t.Fatalf("Definition.Name = %q", def.Name)
	}
	if len(def.Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(def.Steps))
	}
	if len(def.Edges) != 1 {
		t.Fatalf("edges = %d, want 1", len(def.Edges))
	}
	if def.Edges[0].From != "intake" || def.Edges[0].To != "respond" {
		t.Fatalf("unexpected edge: %+v", def.Edges[0])
	}
}

func TestBuilderRecordsFieldMappings(t *testing.T) {
	def := workflow.New("mapped").
		Step("extract", multiagent.AgentClass{Name: "extract"}).
		Step("write", multiagent.AgentClass{Name: "write"}).
		Map("extract", "write", workflow.FieldMapping{From: "summary", To: "draft"}).
		Definition()

	if got := def.Edges[0].Mappings; len(got) != 1 || got[0].From != "summary" || got[0].To != "draft" {
		t.Fatalf("unexpected mappings: %+v", got)
	}
}

func TestBuilderDefinitionCopiesMutableClassFields(t *testing.T) {
	inputSchema := json.RawMessage(`{"type":"object"}`)
	class := multiagent.AgentClass{
		Name:        "extract",
		Tools:       []string{"lookup"},
		InputSchema: inputSchema,
		Capabilities: []api.Capability{{
			Name:        "cap",
			Tags:        []string{"tag"},
			Metadata:    map[string]string{"owner": "workflow"},
			InputSchema: map[string]any{"type": "object"},
		}},
	}

	def := workflow.New("snapshot").Step("extract", class).Definition()

	class.Tools[0] = "mutated"
	inputSchema[0] = '['
	class.Capabilities[0].Tags[0] = "mutated"
	class.Capabilities[0].Metadata["owner"] = "mutated"
	class.Capabilities[0].InputSchema["type"] = "mutated"

	got := def.Steps[0].Class
	if got.Tools[0] != "lookup" {
		t.Fatalf("tools were aliased: %#v", got.Tools)
	}
	if string(got.InputSchema) != `{"type":"object"}` {
		t.Fatalf("input schema was aliased: %s", got.InputSchema)
	}
	if got.Capabilities[0].Tags[0] != "tag" ||
		got.Capabilities[0].Metadata["owner"] != "workflow" ||
		got.Capabilities[0].InputSchema["type"] != "object" {
		t.Fatalf("capability fields were aliased: %#v", got.Capabilities[0])
	}
}
