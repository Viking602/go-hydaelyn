// mailbox_pingpong shows the mailbox dispatch + ack primitive. Two agents
// trade tasks: A asks B, B acks, then B replies with a follow-up task.
//
//	go run ./_examples/mailbox_pingpong
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/Viking602/go-hydaelyn/orchestrator"
)

func main() {
	ctx := context.Background()
	rt := orchestrator.NewRuntime(orchestrator.Config{})
	rt.RegisterAgent(orchestrator.AgentProfile{ID: "alice"})
	rt.RegisterAgent(orchestrator.AgentProfile{ID: "bob"})

	run, _, err := rt.StartRun(ctx, orchestrator.StartRunCommand{Request: "ping pong"})
	must(err)

	// Alice dispatches a question task to Bob.
	ask, err := rt.CreateTask(ctx, orchestrator.CreateTaskCommand{
		RunID: run.ID, TaskID: "ask", OwnerAgentID: "bob",
		Goal: "verify alpha-cohort effect",
	})
	must(err)
	askEnv, err := rt.DispatchTask(ctx, orchestrator.DispatchTaskCommand{
		RunID: run.ID, TaskID: ask.ID, TargetAgentID: "bob",
		Payload: map[string]any{"from": "alice", "subject": "verify claim"},
	})
	must(err)
	fmt.Printf("alice → bob: envelope %s\n", askEnv.ID)

	// Bob acquires the lease, acks the envelope, and submits a typed report.
	bobLease, _, err := rt.AcquireTaskExecution(ctx, orchestrator.AcquireTaskExecutionCommand{
		RunID: run.ID, TaskID: ask.ID, EnvelopeID: askEnv.ID,
		HolderType: orchestrator.HolderAgent, HolderID: "bob", TTL: time.Minute,
	})
	must(err)
	must(rt.AckEnvelope(ctx, orchestrator.AckEnvelopeCommand{
		EnvelopeID: askEnv.ID, HolderID: "bob",
	}))
	must(rt.SubmitTypedReport(ctx, orchestrator.SubmitTypedReportCommand{
		RunID: run.ID, TaskID: ask.ID, LeaseID: bobLease.ID,
		HolderType: orchestrator.HolderAgent, HolderID: "bob",
		TaskVersion: ask.Version,
		Report:      orchestrator.TypedReport{Status: orchestrator.ReportStatusSuccess, Summary: "p=0.012, d=0.41"},
	}))
	fmt.Println("bob acked + reported")

	// Bob replies by dispatching a fresh task back to Alice.
	reply, err := rt.CreateTask(ctx, orchestrator.CreateTaskCommand{
		RunID: run.ID, TaskID: "answer", OwnerAgentID: "alice",
	})
	must(err)
	replyEnv, err := rt.DispatchTask(ctx, orchestrator.DispatchTaskCommand{
		RunID: run.ID, TaskID: reply.ID, TargetAgentID: "alice",
		Payload: map[string]any{"from": "bob", "in_reply_to": askEnv.ID, "summary": "confirmed"},
	})
	must(err)
	fmt.Printf("bob → alice: envelope %s (in_reply_to=%s)\n", replyEnv.ID, askEnv.ID)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
