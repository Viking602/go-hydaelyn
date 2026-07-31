// subagent demonstrates the self-sufficient agent layer (ADR-018): one parent
// agent delegates to two subagents, each running on a different model served by
// a different vendor, through a single provider.Registry and a single
// agent.Build path.
//
// The parent is an ordinary agent.Engine. Each subagent is another Engine
// wrapped with agent.AsTool, so from the parent's point of view a delegation is
// just one tool call — agent-as-tool, not a multi-agent team member. The whole
// example never touches the multiagent layer.
//
//	go run ./_examples/subagent
package main

import (
	"context"
	"fmt"

	"github.com/Viking602/venat/agent"
	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/tool"
)

// Three models, three vendors. The Registry indexes each driver by the model
// names it advertises, so agent.Build resolves each Spec.Model to the right
// driver — that is the only thing that makes per-agent, cross-vendor model
// selection work.
const (
	modelOrchestrator = "anthropic:claude-opus"
	modelSummarizer   = "openai:gpt-4o-mini"
	modelCritic       = "google:gemini-pro"
)

func main() {
	ctx := context.Background()

	// One resolver routes every model name to its vendor's driver.
	registry := provider.NewRegistry(
		newOrchestratorDriver(),
		newSummarizerDriver(),
		newCriticDriver(),
	)
	deps := agent.BuildDeps{Providers: registry}

	// Two subagents, each materialized on its own model through the shared
	// Build path. They are plain Engines until AsTool gives them a parent-facing
	// tool identity.
	summarizer, err := agent.Build(agent.Spec{
		Instructions: "You summarize a report into three crisp bullet points.",
		Model:        modelSummarizer,
	}, deps)
	must(err)

	critic, err := agent.Build(agent.Spec{
		Instructions: "You critique a summary, naming what it omits.",
		Model:        modelCritic,
	}, deps)
	must(err)

	// A subagent's input contract: one "input" string the parent fills with the
	// task it is delegating. AsTool validates the parent's arguments against it
	// before the child runs and maps the "input" field to the child task goal.
	inputSchema := tool.Schema{
		Type:       "object",
		Properties: map[string]tool.Schema{"input": {Type: "string", Description: "the task to delegate"}},
		Required:   []string{"input"},
	}
	summarizeTool := agent.AsTool(summarizer, agent.SubagentDef{
		Name:        "summarize",
		Description: "Delegate summarizing the report to a fast subagent.",
		InputSchema: inputSchema,
	})
	critiqueTool := agent.AsTool(critic, agent.SubagentDef{
		Name:        "critique",
		Description: "Delegate critiquing the summary to a deep-reasoning subagent.",
		InputSchema: inputSchema,
	})

	// The parent agent gets the two subagents as tools. Build selects them from
	// the master bus by name, exactly like any other tool.
	deps.Tools = tool.NewBus(summarizeTool, critiqueTool)
	parent, err := agent.Build(agent.Spec{
		Instructions: "You orchestrate subagents to produce a reviewed report.",
		Model:        modelOrchestrator,
		Tools:        []string{"summarize", "critique"},
	}, deps)
	must(err)

	fmt.Println("topology (model → vendor):")
	for _, model := range []string{modelOrchestrator, modelSummarizer, modelCritic} {
		driver, derr := registry.Driver(model)
		must(derr)
		fmt.Printf("  %-22s → %s\n", model, driver.Metadata().Name)
	}
	fmt.Println()

	result := parent.Run(ctx, api.Task{Goal: "Produce a reviewed summary of the agent-runtime report."}, agent.OutputPolicy{})
	if result.Failure != nil {
		panic(result.Failure)
	}

	// Each delegation is one of the parent's tool calls. The subagent's answer
	// comes back as a tool-result message in the parent's history — the same
	// shape any tool result takes, which is the whole point of agent-as-tool.
	fmt.Println("parent delegated through these subagent tool results:")
	for _, msg := range result.Messages {
		if msg.Role == message.RoleTool && msg.ToolResult != nil {
			fmt.Printf("  → %-10s %s\n", msg.ToolResult.Name, msg.ToolResult.Content)
		}
	}
	fmt.Printf("\nparent final answer:\n  %s\n", result.Text)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
