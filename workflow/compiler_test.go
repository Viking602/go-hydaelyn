package workflow_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/multiagent"
	"github.com/Viking602/venat/workflow"
)

func TestCompileReturnsRunnableScheduler(t *testing.T) {
	def := workflow.New("triage").
		Step("intake", multiagent.AgentClass{Name: "intake"}).
		Step("respond", multiagent.AgentClass{Name: "respond"}).
		Then("intake", "respond").
		Definition()

	compiled, err := workflow.Compile(def)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	exec := multiagent.ExecutorFunc(func(_ context.Context, dispatch multiagent.Dispatch) (api.TypedReport, error) {
		return api.TypedReport{
			Status:     api.ReportStatusSuccess,
			Summary:    dispatch.Task.ID,
			Structured: map[string]any{"taskId": dispatch.Task.ID},
		}, nil
	})
	result, err := multiagent.Drive(context.Background(), "run-1", compiled.Scheduler(), exec, multiagent.DriveOptions{})
	if err != nil {
		t.Fatalf("Drive() error = %v", err)
	}
	if len(result.State.Instances) != 2 {
		t.Fatalf("instances = %d, want 2", len(result.State.Instances))
	}
}

func TestCompileFreezesMutableClassFields(t *testing.T) {
	inputSchema := json.RawMessage(`{"type":"object","properties":{"input":{"type":"string"}}}`)
	outputSchema := json.RawMessage(`{"type":"object","properties":{"output":{"type":"string"}}}`)
	wantInputSchema := string(inputSchema)
	wantOutputSchema := string(outputSchema)
	def := workflow.Definition{
		Name: "snapshot",
		Steps: []workflow.Step{{
			ID: "extract",
			Class: multiagent.AgentClass{
				Name:         "extract",
				InputSchema:  inputSchema,
				OutputSchema: outputSchema,
			},
		}},
	}

	compiled, err := workflow.Compile(def)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	def.Steps[0].Class.InputSchema[0] = '['
	def.Steps[0].Class.OutputSchema[0] = '['
	dispatches, err := compiled.Scheduler().Next(context.Background(), multiagent.TeamState{RunID: "run-1"})
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if len(dispatches) != 1 {
		t.Fatalf("dispatches = %d, want 1", len(dispatches))
	}
	if string(dispatches[0].Task.InputSchema) != wantInputSchema {
		t.Fatalf("input schema = %s, want %s", dispatches[0].Task.InputSchema, wantInputSchema)
	}
	if string(dispatches[0].Task.OutputSchema) != wantOutputSchema {
		t.Fatalf("output schema = %s, want %s", dispatches[0].Task.OutputSchema, wantOutputSchema)
	}
	if string(dispatches[0].OutputPolicy.Schema) != wantOutputSchema {
		t.Fatalf("output policy schema = %s, want %s", dispatches[0].OutputPolicy.Schema, wantOutputSchema)
	}
}

func TestCompilePropagatesConditionalBranches(t *testing.T) {
	high := func(report api.TypedReport) bool {
		return report.Structured["severity"] == "high"
	}
	def := workflow.New("branch").
		Step("classify", multiagent.AgentClass{Name: "classify"}).
		Step("contain", multiagent.AgentClass{Name: "contain"}).
		Branch("classify", "contain", high).
		Definition()

	compiled, err := workflow.Compile(def)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	exec := multiagent.ExecutorFunc(func(_ context.Context, dispatch multiagent.Dispatch) (api.TypedReport, error) {
		if dispatch.Task.ID == "run-1-classify" {
			return api.TypedReport{Status: api.ReportStatusSuccess, Structured: map[string]any{"severity": "high"}}, nil
		}
		return api.TypedReport{Status: api.ReportStatusSuccess}, nil
	})
	result, err := multiagent.Drive(context.Background(), "run-1", compiled.Scheduler(), exec, multiagent.DriveOptions{})
	if err != nil {
		t.Fatalf("Drive() error = %v", err)
	}
	if len(result.State.Instances) != 2 {
		t.Fatalf("branch should activate contain, got %d instances", len(result.State.Instances))
	}
}

func TestCompileForwardsFieldMappings(t *testing.T) {
	outputSchema := json.RawMessage(`{"type":"object","properties":{"summary":{"type":"string"}},"required":["summary"]}`)
	inputSchema := json.RawMessage(`{"type":"object","properties":{"draft":{"type":"string"}},"required":["draft"]}`)
	def := workflow.New("mapped").
		Step("extract", multiagent.AgentClass{Name: "extract", OutputSchema: outputSchema}).
		Step("write", multiagent.AgentClass{Name: "write", InputSchema: inputSchema}).
		Map("extract", "write", workflow.FieldMapping{From: "summary", To: "draft"}).
		Definition()

	compiled, err := workflow.Compile(def)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	var writeInput map[string]any
	exec := multiagent.ExecutorFunc(func(_ context.Context, dispatch multiagent.Dispatch) (api.TypedReport, error) {
		if dispatch.Task.ID == "run-1-write" {
			if err := json.Unmarshal(dispatch.Input, &writeInput); err != nil {
				t.Fatalf("write input unmarshal: %v", err)
			}
		}
		return api.TypedReport{Status: api.ReportStatusSuccess, Structured: map[string]any{"summary": "done"}}, nil
	})
	if _, err := multiagent.Drive(context.Background(), "run-1", compiled.Scheduler(), exec, multiagent.DriveOptions{}); err != nil {
		t.Fatalf("Drive() error = %v", err)
	}
	if writeInput["draft"] != "done" {
		t.Fatalf("mapped draft = %#v, want done", writeInput["draft"])
	}
}
