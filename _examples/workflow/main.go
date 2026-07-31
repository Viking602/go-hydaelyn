package main

import (
	"context"
	"fmt"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/multiagent"
	"github.com/Viking602/venat/workflow"
)

func main() {
	def := workflow.New("support-triage").
		Step("intake", multiagent.AgentClass{Name: "intake", Instructions: "classify the request"}).
		Step("reply", multiagent.AgentClass{Name: "reply", Instructions: "draft the response"}).
		Then("intake", "reply").
		Definition()

	compiled, err := workflow.Compile(def)
	if err != nil {
		panic(err)
	}

	engine := workflow.Engine{
		Executor: multiagent.ExecutorFunc(func(_ context.Context, dispatch multiagent.Dispatch) (api.TypedReport, error) {
			return api.TypedReport{
				Status:  api.ReportStatusSuccess,
				Summary: "completed " + dispatch.Task.ID,
			}, nil
		}),
	}
	run, err := engine.Start(context.Background(), workflow.StartRequest{
		RunID:    "example-run",
		Workflow: compiled,
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(run.RunID, len(run.Result.State.Instances))
}
