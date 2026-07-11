package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Viking602/go-hydaelyn/agent"
	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/skill"
)

func main() {
	root, err := os.MkdirTemp("", "hydaelyn-skills-")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(root)
	writeDemoSkill(root)

	discovered, err := skill.Discover(skill.DiscoveryOptions{AdditionalDirs: []string{root}})
	if err != nil {
		panic(err)
	}
	registry := skill.NewRegistry()
	for _, current := range discovered.Skills {
		if err := skill.Register(registry, current); err != nil {
			panic(err)
		}
	}

	driver := &activationDemoProvider{}
	engine, err := agent.Build(agent.Spec{
		Model:           "scripted",
		Skills:          []string{"baseline-review"},
		AvailableSkills: []string{"release-review"},
		LoopPolicy:      agent.LoopPolicy{MaxIterations: 3},
	}, agent.BuildDeps{
		Providers: provider.Single(driver),
		Skills:    registry,
	})
	if err != nil {
		panic(err)
	}
	result := engine.Run(context.Background(), api.Task{Goal: "review the release"}, agent.OutputPolicy{})
	if result.Failure != nil {
		panic(result.Failure)
	}
	fmt.Println(result.Text)
}

func writeDemoSkill(root string) {
	baseline := filepath.Join(root, "baseline-review")
	if err := os.MkdirAll(baseline, 0o755); err != nil {
		panic(err)
	}
	baselineContent := "---\nname: baseline-review\ndescription: Apply baseline review checks to every release.\n---\nAlways confirm tests pass before publication.\n"
	if err := os.WriteFile(filepath.Join(baseline, "SKILL.md"), []byte(baselineContent), 0o644); err != nil {
		panic(err)
	}

	dir := filepath.Join(root, "release-review")
	if err := os.MkdirAll(filepath.Join(dir, "references"), 0o755); err != nil {
		panic(err)
	}
	content := "---\nname: release-review\ndescription: Review a release before publication. Use for release readiness.\ncompatibility: Requires a git checkout\n---\nRead references/checklist.md only when detailed checks are needed.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "references", "checklist.md"), []byte("Run tests and inspect release notes.\n"), 0o644); err != nil {
		panic(err)
	}
}

type activationDemoProvider struct{ turn int }

func (*activationDemoProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "scripted", Models: []string{"scripted"}}
}

func (p *activationDemoProvider) Stream(_ context.Context, _ provider.Request) (provider.Stream, error) {
	p.turn++
	if p.turn == 1 {
		return provider.NewSliceStream([]provider.Event{
			{Kind: provider.EventToolCall, ToolCall: &message.ToolCall{
				ID:        "activate",
				Name:      agent.SkillActivationToolName,
				Arguments: json.RawMessage(`{"name":"release-review"}`),
			}},
			{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
		}), nil
	}
	if p.turn == 2 {
		return provider.NewSliceStream([]provider.Event{
			{Kind: provider.EventToolCall, ToolCall: &message.ToolCall{
				ID:        "read-resource",
				Name:      agent.SkillResourceToolName,
				Arguments: json.RawMessage(`{"skill":"release-review","path":"references/checklist.md"}`),
			}},
			{Kind: provider.EventDone, StopReason: provider.StopReasonToolUse},
		}), nil
	}
	return provider.NewSliceStream([]provider.Event{
		{Kind: provider.EventTextDelta, Text: "release skill and checklist loaded"},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}), nil
}
