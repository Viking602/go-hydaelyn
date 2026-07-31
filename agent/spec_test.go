package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/message"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/skill"
	"github.com/Viking602/venat/tool"
	"github.com/Viking602/venat/tool/kit"
)

// modeledProvider is a single-turn driver that advertises a configurable name
// and model list, so Build's resolver tests can route by model and assert which
// driver a Spec resolved to.
type modeledProvider struct {
	name   string
	models []string
	answer string
}

func (p *modeledProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: p.name, Models: p.models}
}

func (p *modeledProvider) Stream(_ context.Context, _ provider.Request) (provider.Stream, error) {
	return provider.NewSliceStream([]provider.Event{
		{Kind: provider.EventTextDelta, Text: p.answer},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}), nil
}

func TestBuild_RequiresProviderResolver(t *testing.T) {
	if _, err := Build(Spec{Model: "m"}, BuildDeps{}); !errors.Is(err, ErrProviderResolverMissing) {
		t.Fatalf("Build without resolver err = %v, want ErrProviderResolverMissing", err)
	}
}

func TestBuild_UnservableModelFailsAtConstruction(t *testing.T) {
	deps := BuildDeps{Providers: provider.NewRegistry()} // empty registry
	_, err := Build(Spec{Model: "ghost"}, deps)
	if !errors.Is(err, provider.ErrNoDriverForModel) {
		t.Fatalf("Build with unservable model err = %v, want ErrNoDriverForModel", err)
	}
}

func TestBuild_ResolvesDriverAndCarriesModel(t *testing.T) {
	driver := &modeledProvider{name: "vendor", models: []string{"m"}, answer: "ok"}
	engine, err := Build(Spec{Model: "m"}, BuildDeps{Providers: provider.Single(driver)})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if engine.Model != "m" {
		t.Fatalf("Engine.Model = %q, want %q", engine.Model, "m")
	}
	if engine.Provider != driver {
		t.Fatalf("Engine.Provider = %v, want the resolved driver", engine.Provider)
	}
}

func TestBuild_CrossVendorSwitchSelectsPerModelDriver(t *testing.T) {
	anthropic := &modeledProvider{name: "anthropic", models: []string{"opus"}}
	openai := &modeledProvider{name: "openai", models: []string{"gpt"}}
	registry := provider.NewRegistry(anthropic, openai)

	cases := map[string]*modeledProvider{"opus": anthropic, "gpt": openai}
	for model, want := range cases {
		engine, err := Build(Spec{Model: model}, BuildDeps{Providers: registry})
		if err != nil {
			t.Fatalf("Build(%q) returned error: %v", model, err)
		}
		if engine.Provider != want {
			t.Fatalf("Build(%q) resolved %v, want %s driver", model, engine.Provider, want.name)
		}
	}
}

func TestBuild_ToolsNamedWithoutBus(t *testing.T) {
	driver := &modeledProvider{name: "vendor", models: []string{"m"}}
	_, err := Build(
		Spec{Model: "m", Tools: []string{"lookup"}},
		BuildDeps{Providers: provider.Single(driver)},
	)
	if err == nil {
		t.Fatal("Build with tools but no bus should fail")
	}
}

func TestBuild_UnknownToolFailsAtConstruction(t *testing.T) {
	driver := &modeledProvider{name: "vendor", models: []string{"m"}}
	lookup := mustTool(t, "lookup")
	_, err := Build(
		Spec{Model: "m", Tools: []string{"missing"}},
		BuildDeps{Providers: provider.Single(driver), Tools: tool.NewBus(lookup)},
	)
	if err == nil {
		t.Fatal("Build naming an unregistered tool should fail")
	}
}

func TestBuild_SelectsOnlyTheNamedToolSubset(t *testing.T) {
	driver := &modeledProvider{name: "vendor", models: []string{"m"}}
	master := tool.NewBus(mustTool(t, "lookup"), mustTool(t, "search"))
	engine, err := Build(
		Spec{Model: "m", Tools: []string{"lookup"}},
		BuildDeps{Providers: provider.Single(driver), Tools: master},
	)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if engine.Tools == nil {
		t.Fatal("Engine.Tools is nil, want the selected subset")
	}
	if _, ok := engine.Tools.Driver("lookup"); !ok {
		t.Fatal("subset is missing the named tool lookup")
	}
	if _, ok := engine.Tools.Driver("search"); ok {
		t.Fatal("subset leaked an unnamed tool search")
	}
}

func TestBuild_NoToolsYieldsNilBus(t *testing.T) {
	driver := &modeledProvider{name: "vendor", models: []string{"m"}}
	engine, err := Build(Spec{Model: "m"}, BuildDeps{Providers: provider.Single(driver)})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if engine.Tools != nil {
		t.Fatal("tool-less Spec should yield a nil tool bus")
	}
}

func TestBuild_DefaultContextSeedsInstructionsAndGoal(t *testing.T) {
	driver := &scriptedProvider{turns: [][]provider.Event{{
		{Kind: provider.EventTextDelta, Text: "ok"},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}}}
	engine, err := Build(
		Spec{Model: "m", Instructions: "You are a router."},
		BuildDeps{Providers: provider.Single(driver)},
	)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	result := engine.Run(context.Background(), api.Task{Goal: "route the request"}, OutputPolicy{})
	if result.Failure != nil {
		t.Fatalf("Run failed: %v", result.Failure)
	}
	if len(driver.requests) == 0 {
		t.Fatal("provider received no request")
	}
	got := driver.requests[0].Messages
	if len(got) != 2 {
		t.Fatalf("seeded %d messages, want 2 (system + user)", len(got))
	}
	if got[0].Role != message.RoleSystem || got[0].Text != "You are a router." {
		t.Fatalf("system message = %+v, want instructions", got[0])
	}
	if got[1].Role != message.RoleUser || got[1].Text != "route the request" {
		t.Fatalf("user message = %+v, want task goal", got[1])
	}
}

func TestBuild_ResolvesSkillsIntoContext(t *testing.T) {
	driver := &scriptedProvider{turns: [][]provider.Event{{
		{Kind: provider.EventTextDelta, Text: "ok"},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}}}
	registry := skill.NewRegistry()
	if err := skill.Register(registry, skill.Skill{
		Name:        "code-review",
		Description: "Review code",
		Body:        "Review diffs before editing.",
		SourceDir:   "skills/code-review",
	}); err != nil {
		t.Fatalf("register skill: %v", err)
	}
	engine, err := Build(
		Spec{Model: "m", Instructions: "base", Skills: []string{"code-review"}},
		BuildDeps{Providers: provider.Single(driver), Skills: registry},
	)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	result := engine.Run(context.Background(), api.Task{Goal: "review this change"}, OutputPolicy{})
	if result.Failure != nil {
		t.Fatalf("Run failed: %v", result.Failure)
	}
	if len(driver.requests) == 0 {
		t.Fatal("provider received no request")
	}
	got := driver.requests[0].Messages
	if len(got) != 3 {
		t.Fatalf("seeded %d messages, want 3 (system + skills + user): %+v", len(got), got)
	}
	if got[0].Role != message.RoleSystem || got[0].Text != "base" {
		t.Fatalf("first message = %+v, want base system instructions", got[0])
	}
	if got[1].Role != message.RoleSystem ||
		!strings.Contains(got[1].Text, "--- skill: code-review ---") ||
		!strings.Contains(got[1].Text, "Review diffs before editing.") {
		t.Fatalf("second message = %+v, want rendered code-review skill", got[1])
	}
	if got[2].Role != message.RoleUser || got[2].Text != "review this change" {
		t.Fatalf("third message = %+v, want task goal", got[2])
	}
}

func TestBuild_SkillsRequireRegistry(t *testing.T) {
	driver := &modeledProvider{name: "vendor", models: []string{"m"}}
	_, err := Build(
		Spec{Model: "m", Skills: []string{"code-review"}},
		BuildDeps{Providers: provider.Single(driver)},
	)
	if !errors.Is(err, ErrSkillRegistryMissing) {
		t.Fatalf("Build with skills but no registry err = %v, want ErrSkillRegistryMissing", err)
	}
}

func TestBuild_AvailableSkillsRequireRegistry(t *testing.T) {
	driver := &modeledProvider{name: "vendor", models: []string{"m"}}
	_, err := Build(
		Spec{Model: "m", AvailableSkills: []string{"code-review"}},
		BuildDeps{Providers: provider.Single(driver)},
	)
	if !errors.Is(err, ErrSkillRegistryMissing) {
		t.Fatalf("Build with available skills but no registry err = %v, want ErrSkillRegistryMissing", err)
	}
}

func TestBuild_MissingAvailableSkillFailsConstruction(t *testing.T) {
	driver := &modeledProvider{name: "vendor", models: []string{"m"}}
	_, err := Build(
		Spec{Model: "m", AvailableSkills: []string{"missing"}},
		BuildDeps{Providers: provider.Single(driver), Skills: skill.NewRegistry()},
	)
	var missing *skill.NotFoundError
	if !errors.As(err, &missing) || missing.Name != "missing" {
		t.Fatalf("Build with unknown available skill err = %v, want missing NotFoundError", err)
	}
}

func TestBuild_MissingSkillFailsConstruction(t *testing.T) {
	driver := &modeledProvider{name: "vendor", models: []string{"m"}}
	_, err := Build(
		Spec{Model: "m", Skills: []string{"missing"}},
		BuildDeps{Providers: provider.Single(driver), Skills: skill.NewRegistry()},
	)
	var missing *skill.NotFoundError
	if !errors.As(err, &missing) {
		t.Fatalf("Build with unknown skill err = %v, want *skill.NotFoundError", err)
	}
	if missing.Name != "missing" {
		t.Fatalf("missing skill name = %q, want missing", missing.Name)
	}
}

func TestBuild_ContextManagerOverrideStillGetsSkills(t *testing.T) {
	driver := &scriptedProvider{turns: [][]provider.Event{{
		{Kind: provider.EventTextDelta, Text: "ok"},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}}}
	registry := skill.NewRegistry()
	if err := skill.Register(registry, skill.Skill{
		Name:        "code-review",
		Description: "Review code",
		Body:        "Use the checklist.",
	}); err != nil {
		t.Fatalf("register skill: %v", err)
	}
	engine, err := Build(
		Spec{Model: "m", Instructions: "ignored when overridden", Skills: []string{"code-review"}},
		BuildDeps{Providers: provider.Single(driver), Skills: registry, ContextManager: markerPairContext{}},
	)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	if result := engine.Run(context.Background(), api.Task{Goal: "ignored by marker pair"}, OutputPolicy{}); result.Failure != nil {
		t.Fatalf("Run failed: %v", result.Failure)
	}
	got := driver.requests[0].Messages
	if len(got) != 3 {
		t.Fatalf("seeded %d messages, want marker system + skills + marker user: %+v", len(got), got)
	}
	if got[0].Role != message.RoleSystem || got[0].Text != "marker-system" {
		t.Fatalf("first message = %+v, want marker system", got[0])
	}
	if got[1].Role != message.RoleSystem ||
		!strings.Contains(got[1].Text, "--- skill: code-review ---") ||
		!strings.Contains(got[1].Text, "Use the checklist.") {
		t.Fatalf("second message = %+v, want active skill after marker system", got[1])
	}
	if got[2].Role != message.RoleUser || got[2].Text != "marker-user" {
		t.Fatalf("third message = %+v, want marker user", got[2])
	}
}

func TestBuild_ContextManagerOverrideWins(t *testing.T) {
	driver := &scriptedProvider{turns: [][]provider.Event{{
		{Kind: provider.EventTextDelta, Text: "ok"},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}}}
	engine, err := Build(
		Spec{Model: "m", Instructions: "ignored when overridden"},
		BuildDeps{Providers: provider.Single(driver), ContextManager: markerContext{}},
	)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	if result := engine.Run(context.Background(), api.Task{Goal: "g"}, OutputPolicy{}); result.Failure != nil {
		t.Fatalf("Run failed: %v", result.Failure)
	}
	got := driver.requests[0].Messages
	if len(got) != 1 || got[0].Text != "marker" {
		t.Fatalf("override context not used, got %+v", got)
	}
}

func TestBuild_ForwardsTuningFields(t *testing.T) {
	driver := &modeledProvider{name: "vendor", models: []string{"m"}}
	spec := Spec{
		Model:          "m",
		ThinkingBudget: 256,
		StopSequences:  []string{"STOP"},
		ExtraBody:      map[string]any{"k": "v"},
	}
	engine, err := Build(spec, BuildDeps{Providers: provider.Single(driver)})
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if engine.ThinkingBudget != 256 {
		t.Fatalf("ThinkingBudget = %d, want 256", engine.ThinkingBudget)
	}
	if len(engine.StopSequences) != 1 || engine.StopSequences[0] != "STOP" {
		t.Fatalf("StopSequences = %v, want [STOP]", engine.StopSequences)
	}
	if engine.ExtraBody["k"] != "v" {
		t.Fatalf("ExtraBody = %v, want {k:v}", engine.ExtraBody)
	}
}

// markerContext is a ContextManager whose Build emits a single recognizable
// message, so a test can prove Build wired the override rather than the default
// instructions context.
type markerContext struct{}

func (markerContext) Build(context.Context, api.Task) ([]message.Message, error) {
	return []message.Message{message.NewText(message.RoleSystem, "marker")}, nil
}

func (markerContext) Compact(_ context.Context, history []message.Message) ([]message.Message, error) {
	return history, nil
}

type markerPairContext struct{}

func (markerPairContext) Build(context.Context, api.Task) ([]message.Message, error) {
	return []message.Message{
		message.NewText(message.RoleSystem, "marker-system"),
		message.NewText(message.RoleUser, "marker-user"),
	}, nil
}

func (markerPairContext) Compact(_ context.Context, history []message.Message) ([]message.Message, error) {
	return history, nil
}

func mustTool(t *testing.T, name string) tool.Driver {
	t.Helper()
	driver, err := kit.Tool(name, func(_ context.Context, _ struct {
		Query string `json:"query"`
	}) (string, error) {
		return name + ":ok", nil
	})
	if err != nil {
		t.Fatalf("tool %q setup: %v", name, err)
	}
	return driver
}
