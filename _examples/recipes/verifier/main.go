// verifier demonstrates feedback-loop engineering on the Venat agent layer:
// a writer agent drafts an answer, a verifier subagent checks it against a
// rubric, and the writer revises on the verifier's feedback until it passes.
// The verify-revise cycle IS the loop — built entirely from existing primitives
// (agent.AsTool for the verifier subagent), with no change to the core engine.
//
// A StepPolicy bounds how many verify rounds the loop may run, so a verifier
// that never accepts can never spin the loop forever — the loop-engineering
// control that makes an autonomous feedback loop safe to run unattended.
//
// Both models are scripted, so the example is deterministic and runs offline:
// the verifier rejects the first draft and accepts the revision.
//
//	go run ./_examples/recipes/verifier
package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/Viking602/venat/agent"
	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/tool"
)

const (
	modelWriter     = "writer"
	modelVerifier   = "verifier"
	maxVerifyRounds = 5
)

func main() {
	ctx := context.Background()

	// The verifier subagent: a plain Engine that judges a draft against the
	// rubric and replies REJECT (naming the gap) or ACCEPT. Scripted to reject
	// the first draft, accept the revision.
	verifierDriver := &turnDriver{
		name:   "verifier-vendor",
		models: []string{modelVerifier},
		turns: [][]provider.Event{
			{say("REJECT: the draft never covers failure-mode handling."), done(provider.StopReasonComplete)},
			{say("ACCEPT: the revision covers the rubric."), done(provider.StopReasonComplete)},
		},
	}
	verifier, err := agent.Build(agent.Spec{
		Instructions: "You verify a draft against the rubric and reply ACCEPT or REJECT with the missing point.",
		Model:        modelVerifier,
	}, agent.BuildDeps{Providers: provider.Single(verifierDriver)})
	must(err)

	// Wrap the verifier as a tool the writer calls from inside its own loop:
	// agent-as-tool, so a verification is just one of the writer's tool calls.
	verifyTool := agent.AsTool(verifier, agent.SubagentDef{
		Name:        "verify",
		Description: "Check the current draft against the rubric; returns ACCEPT or REJECT with the gap.",
		InputSchema: tool.Schema{
			Type:       "object",
			Properties: map[string]tool.Schema{"input": {Type: "string", Description: "the draft to verify"}},
			Required:   []string{"input"},
		},
	})

	// The writer drafts, calls verify, and revises on the feedback until it is
	// accepted — then returns a final answer with no further tool call.
	writerDriver := &turnDriver{
		name:   "writer-vendor",
		models: []string{modelWriter},
		turns: [][]provider.Event{
			{
				say("Draft v1: an agent is a model in a loop."),
				callVerify("v1", "an agent is a model in a loop."),
				done(provider.StopReasonToolUse),
			},
			{
				say("Draft v2: an agent is a model in a loop that also handles tool failures."),
				callVerify("v2", "an agent is a model in a loop that also handles tool failures."),
				done(provider.StopReasonToolUse),
			},
			{
				say("Final answer: an agent is a model in a loop that handles tool failures (verified)."),
				done(provider.StopReasonComplete),
			},
		},
	}
	writer, err := agent.Build(agent.Spec{
		Instructions: "You draft an answer, call verify, and revise until it is accepted.",
		Model:        modelWriter,
		Tools:        []string{"verify"},
	}, agent.BuildDeps{
		Providers: provider.Single(writerDriver),
		Tools:     tool.NewBus(verifyTool),
	})
	must(err)

	// Loop-engineering control: bound the verify-revise cycles. The StepPolicy is
	// consulted at every continue boundary and fails the run if the writer asks
	// to verify more than maxVerifyRounds times without converging.
	writer.StepPolicy = boundVerifyRounds(maxVerifyRounds)

	result := writer.Run(ctx, api.Task{Goal: "Define an AI agent in one sentence, verified against the rubric."}, agent.OutputPolicy{})
	if result.Failure != nil {
		panic(result.Failure)
	}

	fmt.Println("feedback loop (writer ⇄ verifier subagent):")
	rounds := 0
	for _, msg := range result.Messages {
		switch {
		case msg.Role == message.RoleAssistant && strings.TrimSpace(msg.Text) != "":
			fmt.Printf("  writer:   %s\n", strings.TrimSpace(msg.Text))
		case msg.Role == message.RoleTool && msg.ToolResult != nil:
			rounds++
			fmt.Printf("  verifier: %s\n", strings.TrimSpace(msg.ToolResult.Content))
		}
	}
	fmt.Printf("\nconverged after %d verify round(s) (bound: %d)\nfinal answer: %s\n", rounds, maxVerifyRounds, result.Text)
}

// boundVerifyRounds returns a StepPolicy that fails the loop once the writer has
// issued more than limit verify tool calls without finishing — the safety bound
// that keeps a never-satisfied verifier from looping unbounded.
func boundVerifyRounds(limit int) agent.StepPolicy {
	return stepPolicyFunc(func(s agent.LoopSnapshot) (agent.StepDecision, error) {
		verifies := 0
		for _, step := range s.Steps {
			for _, tc := range step.ToolCalls {
				if tc.Name == "verify" {
					verifies++
				}
			}
		}
		if verifies > limit {
			return agent.StepDecisionFail, nil
		}
		return agent.StepDecisionContinue, nil
	})
}

type stepPolicyFunc func(agent.LoopSnapshot) (agent.StepDecision, error)

func (f stepPolicyFunc) Next(s agent.LoopSnapshot) (agent.StepDecision, error) { return f(s) }

func must(err error) {
	if err != nil {
		panic(err)
	}
}
