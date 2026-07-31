package workflow_test

import (
	"context"
	"testing"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/multiagent"
	"github.com/Viking602/venat/workflow"
)

func TestEngineStartRunsCompiledWorkflow(t *testing.T) {
	def := workflow.New("linear").
		Step("a", multiagent.AgentClass{Name: "a"}).
		Step("b", multiagent.AgentClass{Name: "b"}).
		Then("a", "b").
		Definition()
	compiled, err := workflow.Compile(def)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	engine := workflow.Engine{
		Executor: multiagent.ExecutorFunc(func(_ context.Context, dispatch multiagent.Dispatch) (api.TypedReport, error) {
			return api.TypedReport{Status: api.ReportStatusSuccess, Summary: dispatch.Task.ID}, nil
		}),
	}
	run, err := engine.Start(context.Background(), workflow.StartRequest{
		RunID:    "run-1",
		Workflow: compiled,
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if run.RunID != "run-1" {
		t.Fatalf("RunID = %q, want run-1", run.RunID)
	}
	if len(run.Result.State.Instances) != 2 {
		t.Fatalf("instances = %d, want 2", len(run.Result.State.Instances))
	}
}

func TestEngineStartRejectsMissingExecutor(t *testing.T) {
	def := workflow.New("single").
		Step("a", multiagent.AgentClass{Name: "a"}).
		Definition()
	compiled, err := workflow.Compile(def)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	_, err = (workflow.Engine{}).Start(context.Background(), workflow.StartRequest{
		RunID:    "run-1",
		Workflow: compiled,
	})
	if err == nil {
		t.Fatal("expected missing executor error")
	}
}
