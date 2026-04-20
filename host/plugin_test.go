package host

import (
	"context"
	"testing"

	"github.com/Viking602/go-hydaelyn/capability"
	"github.com/Viking602/go-hydaelyn/hook"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/internal/middleware"
	"github.com/Viking602/go-hydaelyn/observe"
	"github.com/Viking602/go-hydaelyn/patterns/deepsearch"
	"github.com/Viking602/go-hydaelyn/internal/plugin"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/internal/session"
	"github.com/Viking602/go-hydaelyn/team"
	"github.com/Viking602/go-hydaelyn/tool"
	"github.com/Viking602/go-hydaelyn/tool/kit"
)

type observerSpy struct {
	beforeModelCalls int
}

func (o *observerSpy) TransformContext(_ context.Context, messages []message.Message) ([]message.Message, error) {
	return messages, nil
}

func (o *observerSpy) BeforeModelCall(_ context.Context, _ *provider.Request) error {
	o.beforeModelCalls++
	return nil
}

func (o *observerSpy) BeforeToolCall(_ context.Context, _ *tool.Call) error {
	return nil
}

func (o *observerSpy) AfterToolCall(_ context.Context, _ *tool.Result) error {
	return nil
}

func (o *observerSpy) OnEvent(_ context.Context, _ provider.Event) error {
	return nil
}

var _ hook.Handler = (*observerSpy)(nil)

func TestRegisterPluginExposesRegistryAndDumpConfig(t *testing.T) {
	runner := New(Config{
		Defaults: map[string]string{
			"timeout": "default",
			"retry":   "default",
		},
	})
	runner.RegisterProfile(team.Profile{
		Name: "researcher",
		Metadata: map[string]string{
			"timeout": "profile",
			"profile": "value",
		},
	})
	if err := runner.RegisterPlugin(plugin.Spec{
		Type:      plugin.TypeProvider,
		Name:      "fake",
		Component: fakeProvider{},
		Config: map[string]string{
			"timeout": "plugin",
			"region":  "plugin",
		},
	}); err != nil {
		t.Fatalf("RegisterPlugin() error = %v", err)
	}
	if _, ok := runner.Plugins().Lookup(plugin.TypeProvider, "fake"); !ok {
		t.Fatalf("expected provider plugin to be visible in registry")
	}
	dump, err := runner.DumpConfig(DumpConfigRequest{
		Plugins: []plugin.Ref{
			{Type: plugin.TypeProvider, Name: "fake"},
		},
		ProfileName: "researcher",
		TeamConfig: map[string]string{
			"timeout": "team",
			"team":    "value",
		},
		RunConfig: map[string]string{
			"timeout": "run",
			"run":     "value",
		},
	})
	if err != nil {
		t.Fatalf("DumpConfig() error = %v", err)
	}
	if dump.Values["timeout"] != "run" {
		t.Fatalf("expected run config to win, got %#v", dump.Values)
	}
	if dump.Values["region"] != "plugin" {
		t.Fatalf("expected plugin config to appear, got %#v", dump.Values)
	}
	if dump.Values["profile"] != "value" || dump.Values["team"] != "value" || dump.Values["run"] != "value" {
		t.Fatalf("expected merged values, got %#v", dump.Values)
	}
	if dump.Values["retry"] != "default" {
		t.Fatalf("expected default config to survive, got %#v", dump.Values)
	}
}

func TestObserverPluginAndMiddlewareAreInvoked(t *testing.T) {
	runner := New(Config{})
	trace := make([]string, 0, 4)
	observer := &observerSpy{}
	if err := runner.RegisterPlugin(plugin.Spec{
		Type:      plugin.TypeObserver,
		Name:      "audit",
		Component: observer,
	}); err != nil {
		t.Fatalf("RegisterPlugin(observer) error = %v", err)
	}
	if err := runner.RegisterPlugin(plugin.Spec{
		Type:      plugin.TypeProvider,
		Name:      "fake",
		Component: fakeProvider{},
	}); err != nil {
		t.Fatalf("RegisterPlugin(provider) error = %v", err)
	}
	driver, err := kit.Tool("answer", func(_ context.Context, input struct {
		Topic string `json:"topic"`
	}) (string, error) {
		return "topic:" + input.Topic, nil
	})
	if err != nil {
		t.Fatalf("Tool() error = %v", err)
	}
	if err := runner.RegisterPlugin(plugin.Spec{
		Type:      plugin.TypeTool,
		Name:      "answer",
		Component: driver,
	}); err != nil {
		t.Fatalf("RegisterPlugin(tool) error = %v", err)
	}
	runner.UseMiddleware(middleware.Func(func(ctx context.Context, envelope *middleware.Envelope, next middleware.Next) error {
		trace = append(trace, string(envelope.Stage)+":"+envelope.Operation)
		return next(ctx, envelope)
	}))
	sess, err := runner.CreateSession(context.Background(), session.CreateParams{Branch: "main"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if _, err := runner.Prompt(context.Background(), PromptRequest{
		SessionID: sess.ID,
		Provider:  "fake",
		Model:     "test",
		Messages:  []message.Message{message.NewText(message.RoleUser, "go")},
	}); err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}
	if observer.beforeModelCalls == 0 {
		t.Fatalf("expected observer plugin to receive before model call")
	}
	foundLLM := false
	foundTool := false
	for _, item := range trace {
		if item == "llm:before" {
			foundLLM = true
		}
		if item == "tool:before" || item == "tool:after" {
			foundTool = true
		}
	}
	if !foundLLM || !foundTool {
		t.Fatalf("expected llm and tool middleware stages, got %#v", trace)
	}
}

func TestTeamExecutionFlowsThroughRuntimeMiddlewareStages(t *testing.T) {
	runner := New(Config{})
	trace := make([]string, 0, 16)
	runner.UseMiddleware(middleware.Func(func(ctx context.Context, envelope *middleware.Envelope, next middleware.Next) error {
		trace = append(trace, string(envelope.Stage)+":"+envelope.Operation)
		return next(ctx, envelope)
	}))
	if err := runner.RegisterPlugin(plugin.Spec{
		Type:      plugin.TypeProvider,
		Name:      "team-fake",
		Component: teamFakeProvider{},
	}); err != nil {
		t.Fatalf("RegisterPlugin(provider) error = %v", err)
	}
	runner.RegisterPattern(deepsearch.New())
	runner.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "team-fake", Model: "test"})
	runner.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "team-fake", Model: "test"})
	state, err := runner.StartTeam(context.Background(), StartTeamRequest{
		Pattern:           "deepsearch",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher"},
		Input: map[string]any{
			"query":      "team middleware",
			"subqueries": []string{"branch"},
		},
	})
	if err != nil {
		t.Fatalf("StartTeam() error = %v", err)
	}
	if state.Status != team.StatusCompleted {
		t.Fatalf("expected completed team state, got %#v", state)
	}
	required := map[string]bool{
		"team:start":    false,
		"task:execute":  false,
		"agent:run":     false,
		"memory:create": false,
		"memory:append": false,
	}
	for _, item := range trace {
		if _, ok := required[item]; ok {
			required[item] = true
		}
	}
	for item, ok := range required {
		if !ok {
			t.Fatalf("expected middleware stage %q in trace, got %#v", item, trace)
		}
	}
}

func TestProviderAndToolPluginsFlowThroughCapabilityMiddleware(t *testing.T) {
	runner := New(Config{})
	trace := make([]string, 0, 4)
	runner.UseCapabilityMiddleware(capability.Func(func(ctx context.Context, call capability.Call, next capability.Next) (capability.Result, error) {
		trace = append(trace, string(call.Type)+":"+call.Name)
		return next(ctx, call)
	}))
	if err := runner.RegisterPlugin(plugin.Spec{
		Type:      plugin.TypeProvider,
		Name:      "fake",
		Component: fakeProvider{},
	}); err != nil {
		t.Fatalf("RegisterPlugin(provider) error = %v", err)
	}
	driver, err := kit.Tool("answer", func(_ context.Context, input struct {
		Topic string `json:"topic"`
	}) (string, error) {
		return "topic:" + input.Topic, nil
	})
	if err != nil {
		t.Fatalf("Tool() error = %v", err)
	}
	if err := runner.RegisterPlugin(plugin.Spec{
		Type:      plugin.TypeTool,
		Name:      "answer",
		Component: driver,
	}); err != nil {
		t.Fatalf("RegisterPlugin(tool) error = %v", err)
	}
	sess, err := runner.CreateSession(context.Background(), session.CreateParams{Branch: "main"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if _, err := runner.Prompt(context.Background(), PromptRequest{
		SessionID: sess.ID,
		Provider:  "fake",
		Model:     "test",
		Messages:  []message.Message{message.NewText(message.RoleUser, "go")},
	}); err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}
	foundLLM := false
	foundTool := false
	for _, item := range trace {
		if item == "llm:fake" {
			foundLLM = true
		}
		if item == "tool:answer" {
			foundTool = true
		}
	}
	if !foundLLM || !foundTool {
		t.Fatalf("expected capability trace for plugin-registered provider and tool, got %#v", trace)
	}
}

func TestObserverPluginAcceptsObserveObserver(t *testing.T) {
	runner := New(Config{})
	observer := observe.NewMemoryObserver()
	if err := runner.RegisterPlugin(plugin.Spec{
		Type:      plugin.TypeObserver,
		Name:      "memory-observer",
		Component: observer,
	}); err != nil {
		t.Fatalf("RegisterPlugin(observer) error = %v", err)
	}
	if err := runner.RegisterPlugin(plugin.Spec{
		Type:      plugin.TypeProvider,
		Name:      "fake",
		Component: fakeProvider{},
	}); err != nil {
		t.Fatalf("RegisterPlugin(provider) error = %v", err)
	}
	driver, err := kit.Tool("answer", func(_ context.Context, input struct {
		Topic string `json:"topic"`
	}) (string, error) {
		return "topic:" + input.Topic, nil
	})
	if err != nil {
		t.Fatalf("Tool() error = %v", err)
	}
	if err := runner.RegisterPlugin(plugin.Spec{
		Type:      plugin.TypeTool,
		Name:      "answer",
		Component: driver,
	}); err != nil {
		t.Fatalf("RegisterPlugin(tool) error = %v", err)
	}
	sess, err := runner.CreateSession(context.Background(), session.CreateParams{Branch: "main"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if _, err := runner.Prompt(context.Background(), PromptRequest{
		SessionID: sess.ID,
		Provider:  "fake",
		Model:     "test",
		Messages:  []message.Message{message.NewText(message.RoleUser, "go")},
	}); err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}
	if len(observer.Spans()) == 0 {
		t.Fatalf("expected observer plugin spans")
	}
	counters := observer.Counters()
	if counters["llm.calls"] == 0 || counters["tool.calls"] == 0 {
		t.Fatalf("expected llm/tool counters, got %#v", counters)
	}
}
