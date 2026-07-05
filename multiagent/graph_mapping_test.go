package multiagent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Viking602/go-hydaelyn/api"
)

// mappingExecutor returns a fixed Structured payload per node and captures the
// Input handed to the named sink node, so tests can assert the fan-in shape.
func mappingExecutor(runID, capture string, captured *json.RawMessage, byNode map[string]map[string]any) Executor {
	return ExecutorFunc(func(_ context.Context, dispatch Dispatch) (api.TypedReport, error) {
		node := classNameFromTaskID(runID, dispatch.Task.ID)
		if node == capture {
			*captured = dispatch.Input
		}
		return api.TypedReport{Status: api.ReportStatusSuccess, Structured: byNode[node]}, nil
	})
}

func TestGraphFieldMappingFlattensFanInInput(t *testing.T) {
	var captured json.RawMessage
	exec := mappingExecutor("run-1", "c", &captured, map[string]map[string]any{
		"a": {"title": "T"},
		"b": {"body": "B"},
	})
	g := NewGraph().
		AddNode("a", AgentClass{Name: "a"}).
		AddNode("b", AgentClass{Name: "b"}).
		AddNode("c", AgentClass{Name: "c"}).
		AddEdgeWith("a", "c", WithFieldMapping(FieldMapping{From: "title", To: "headline"})).
		AddEdgeWith("b", "c", WithFieldMapping(FieldMapping{From: "body", To: "content"}))

	if _, err := Drive(context.Background(), "run-1", mustCompile(t, g), exec, DriveOptions{}); err != nil {
		t.Fatalf("Drive error = %v", err)
	}
	var flat map[string]any
	if err := json.Unmarshal(captured, &flat); err != nil {
		t.Fatalf("mapped fan-in input not a flat object: %v (raw=%s)", err, captured)
	}
	if flat["headline"] != "T" || flat["content"] != "B" {
		t.Fatalf("mapped input = %#v, want {headline:T, content:B}", flat)
	}
	if _, keyed := flat["a"]; keyed {
		t.Fatalf("mapped fan-in must be flat, not keyed by node id: %s", captured)
	}
}

func TestGraphSingleMappedEdgeProjectsOnlyMappedFields(t *testing.T) {
	var captured json.RawMessage
	exec := mappingExecutor("run-1", "b", &captured, map[string]map[string]any{
		"a": {"x": "1", "y": "2"},
	})
	g := NewGraph().
		AddNode("a", AgentClass{Name: "a"}).
		AddNode("b", AgentClass{Name: "b"}).
		AddEdgeWith("a", "b", WithFieldMapping(FieldMapping{From: "x", To: "key"}))

	if _, err := Drive(context.Background(), "run-1", mustCompile(t, g), exec, DriveOptions{}); err != nil {
		t.Fatalf("Drive error = %v", err)
	}
	var flat map[string]any
	if err := json.Unmarshal(captured, &flat); err != nil {
		t.Fatalf("mapped input not a flat object: %v (raw=%s)", err, captured)
	}
	if flat["key"] != "1" {
		t.Fatalf("mapped input = %#v, want key=1", flat)
	}
	if _, leaked := flat["y"]; leaked {
		t.Fatalf("unmapped field y must not be projected: %s", captured)
	}
	// A mapped single edge must project fields, not forward the whole report
	// (which would carry a top-level "status").
	if _, forwarded := flat["status"]; forwarded {
		t.Fatalf("single mapped edge must project fields, not forward the whole report: %s", captured)
	}
}

func TestGraphMappedInputErrorsWhenNoMappedFieldsExist(t *testing.T) {
	var captured json.RawMessage
	exec := mappingExecutor("run-1", "b", &captured, map[string]map[string]any{
		"a": {"other": "1"},
	})
	g := NewGraph().
		AddNode("a", AgentClass{Name: "a"}).
		AddNode("b", AgentClass{Name: "b"}).
		AddEdgeWith("a", "b", WithFieldMapping(FieldMapping{From: "missing", To: "key"}))

	_, err := Drive(context.Background(), "run-1", mustCompile(t, g), exec, DriveOptions{})
	if err == nil {
		t.Fatal("Drive() expected mapped-input error, got nil")
	}
	if !strings.Contains(err.Error(), "input resolution failed") {
		t.Fatalf("Drive() error = %q, want scheduler input-resolution context", err.Error())
	}
	if !strings.Contains(err.Error(), "mapped fields") {
		t.Fatalf("Drive() error = %q, want missing mapped-field detail", err.Error())
	}
	if captured != nil {
		t.Fatalf("downstream node should not run with empty mapped input, got %s", captured)
	}
}

func TestGraphCompileRejectsMixedMappedUnmapped(t *testing.T) {
	_, err := NewGraph().
		AddNode("a", AgentClass{Name: "a"}).
		AddNode("b", AgentClass{Name: "b"}).
		AddNode("c", AgentClass{Name: "c"}).
		AddEdgeWith("a", "c", WithFieldMapping(FieldMapping{From: "x", To: "p"})).
		AddEdge("b", "c").
		Compile()
	if err == nil {
		t.Fatal("expected mixed mapped/unmapped fan-in error")
	}
}

func TestGraphCompileRejectsDuplicateMappingTarget(t *testing.T) {
	_, err := NewGraph().
		AddNode("a", AgentClass{Name: "a"}).
		AddNode("b", AgentClass{Name: "b"}).
		AddNode("c", AgentClass{Name: "c"}).
		AddEdgeWith("a", "c", WithFieldMapping(FieldMapping{From: "x", To: "out"})).
		AddEdgeWith("b", "c", WithFieldMapping(FieldMapping{From: "y", To: "out"})).
		Compile()
	if err == nil {
		t.Fatal("expected duplicate mapping-target error")
	}
}

func TestGraphCompileRejectsDeepFieldTypeMismatch(t *testing.T) {
	// bar is present in upstream (so the old shallow check passed), but the
	// per-field type differs: string upstream vs number downstream.
	up := json.RawMessage(`{"type":"object","properties":{"bar":{"type":"string"}}}`)
	down := json.RawMessage(`{"type":"object","required":["bar"],"properties":{"bar":{"type":"number"}}}`)
	_, err := NewGraph().
		AddNode("a", AgentClass{Name: "a", OutputSchema: up}).
		AddNode("b", AgentClass{Name: "b", InputSchema: down}).
		AddEdge("a", "b").
		Compile()
	if err == nil {
		t.Fatal("expected deep field type-mismatch error (bar: string vs number)")
	}
}

func TestGraphCompileAcceptsDeepNestedMatch(t *testing.T) {
	up := json.RawMessage(`{"type":"object","properties":{"cfg":{"type":"object","properties":{"n":{"type":"number"}}}}}`)
	down := json.RawMessage(`{"type":"object","required":["cfg"],"properties":{"cfg":{"type":"object","required":["n"],"properties":{"n":{"type":"number"}}}}}`)
	if _, err := NewGraph().
		AddNode("a", AgentClass{Name: "a", OutputSchema: up}).
		AddNode("b", AgentClass{Name: "b", InputSchema: down}).
		AddEdge("a", "b").
		Compile(); err != nil {
		t.Fatalf("Compile() unexpected error on deep nested match = %v", err)
	}
}

func TestGraphCompileRejectsMappingFieldTypeMismatch(t *testing.T) {
	up := json.RawMessage(`{"type":"object","properties":{"n":{"type":"number"}}}`)
	down := json.RawMessage(`{"type":"object","properties":{"label":{"type":"string"}}}`)
	_, err := NewGraph().
		AddNode("a", AgentClass{Name: "a", OutputSchema: up}).
		AddNode("b", AgentClass{Name: "b", InputSchema: down}).
		AddEdgeWith("a", "b", WithFieldMapping(FieldMapping{From: "n", To: "label"})).
		Compile()
	if err == nil {
		t.Fatal("expected mapping field type-mismatch (n:number -> label:string)")
	}
}
