// mailbox_pingpong shows the mailbox dispatch + ack primitive. Two agents
// trade tasks: A asks B, B acks, then B replies with a follow-up task.
//
//	go run ./_examples/mailbox_pingpong
package main

import (
	"context"
	"fmt"
	"time"

	hydaelyn "github.com/Viking602/go-hydaelyn"
)

func main() {
	ctx := context.Background()
	runner := hydaelyn.New()
	runner.RegisterAgent(hydaelyn.AgentProfile{ID: "alice"})
	runner.RegisterAgent(hydaelyn.AgentProfile{ID: "bob"})

	run, _, err := runner.StartRun(ctx, hydaelyn.StartRunCommand{Request: "ping pong"})
	must(err)

	// Alice dispatches a question task to Bob.
	ask, err := runner.CreateTask(ctx, hydaelyn.CreateTaskCommand{
		RunID: run.ID, TaskID: "ask", OwnerAgentID: "bob",
		Goal: "verify alpha-cohort effect",
	})
	must(err)
	askEnv, err := runner.DispatchTask(ctx, hydaelyn.DispatchTaskCommand{
		RunID: run.ID, TaskID: ask.ID, TargetAgentID: "bob",
		Payload: map[string]any{"from": "alice", "subject": "verify claim"},
	})
	must(err)
	fmt.Printf("alice → bob: envelope %s\n", askEnv.ID)

	// Bob acquires the lease, acks the envelope, and submits a typed report.
	bobLease, _, err := runner.AcquireTaskExecution(ctx, hydaelyn.AcquireTaskExecutionCommand{
		RunID: run.ID, TaskID: ask.ID, EnvelopeID: askEnv.ID,
		HolderType: hydaelyn.HolderAgent, HolderID: "bob", TTL: time.Minute,
	})
	must(err)
	must(runner.AckEnvelope(ctx, hydaelyn.AckEnvelopeCommand{
		EnvelopeID: askEnv.ID, HolderID: "bob",
	}))
	must(runner.SubmitTypedReport(ctx, hydaelyn.SubmitTypedReportCommand{
		RunID: run.ID, TaskID: ask.ID, LeaseID: bobLease.ID,
		HolderType: hydaelyn.HolderAgent, HolderID: "bob",
		TaskVersion: ask.Version,
		Report:      hydaelyn.TypedReport{Status: hydaelyn.ReportStatusSuccess, Summary: "p=0.012, d=0.41"},
	}))
	fmt.Println("bob acked + reported")

	// Bob replies by dispatching a fresh task back to Alice.
	reply, err := runner.CreateTask(ctx, hydaelyn.CreateTaskCommand{
		RunID: run.ID, TaskID: "answer", OwnerAgentID: "alice",
	})
	must(err)
	replyEnv, err := runner.DispatchTask(ctx, hydaelyn.DispatchTaskCommand{
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
