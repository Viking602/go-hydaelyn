package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Viking602/venat"
	"github.com/Viking602/venat/agent"
	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/hook"
	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/tool"
	transporttrigger "github.com/Viking602/venat/transport/trigger"
)

func TestDefinitionDeploymentMaterializesAndPersistsDefinition(t *testing.T) {
	ctx := context.Background()
	runner := venat.NewDevelopment()
	modelDriver := &recordingProvider{}
	lookup := &recordingTool{definition: tool.Definition{Name: "lookup"}}
	createdAt := time.Date(2026, time.August, 4, 10, 30, 0, 0, time.FixedZone("offset", 3600))
	inputSchema := json.RawMessage(`{"type":"object","required":["query"]}`)
	outputSchema := json.RawMessage(`{"type":"object","required":["answer"]}`)
	definition := api.AgentDefinition{
		ID:                "researcher",
		Name:              "Researcher",
		Version:           "v2",
		Instructions:      "verify every claim",
		AvailableSkills:   []string{},
		Tools:             []string{"lookup"},
		InputSchema:       inputSchema,
		OutputSchema:      outputSchema,
		Capabilities:      []string{"search-capability"},
		Model:             api.ModelPolicy{Provider: "recording", Model: "primary", Temperature: 0.2, TopP: 0.8, MaxTokens: 900, FallbackModel: "fallback"},
		Triggers:          []api.Trigger{{ID: "manual", Type: api.TriggerManual, Enabled: true}},
		PreviousVersionID: "v1",
		Metadata:          map[string]string{"role": "analyst"},
	}
	deployed, err := (DefinitionDeployment{
		Runner: runner,
		BuildDeps: agent.BuildDeps{
			Providers: provider.Single(modelDriver),
			Tools:     tool.NewBus(lookup),
		},
		ToolMode:      tool.ModeSequential,
		MaxIterations: 7,
		TTL:           2 * time.Minute,
		Now:           func() time.Time { return createdAt },
	}).Deploy(ctx, definition)
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}
	defer func() { _ = deployed.Close() }()

	if deployed.Worker.AgentID != definition.ID || deployed.Worker.Model != definition.Model.Model {
		t.Fatalf("worker identity/model = %q/%q", deployed.Worker.AgentID, deployed.Worker.Model)
	}
	if deployed.Worker.Engine.Temperature != 0.2 || deployed.Worker.Engine.TopP != 0.8 || deployed.Worker.Engine.ModelMaxTokens != 900 {
		t.Fatalf("engine model policy = %#v", deployed.Worker.Engine)
	}
	if deployed.Spec.Provider != "recording" || deployed.Spec.FallbackModel != "fallback" ||
		len(deployed.Spec.Tools) != 1 || deployed.Spec.Tools[0] != "lookup" {
		t.Fatalf("materialized spec = %#v", deployed.Spec)
	}
	if string(deployed.Spec.InputSchema) != string(inputSchema) ||
		string(deployed.Spec.OutputSchema) != string(outputSchema) {
		t.Fatalf("materialized schemas = %s / %s", deployed.Spec.InputSchema, deployed.Spec.OutputSchema)
	}
	if deployed.Definition.ToolMode != api.ToolModeSequential ||
		deployed.Definition.MaxIterations != 7 ||
		deployed.Definition.TTL != 2*time.Minute {
		t.Fatalf("effective definition defaults = %#v", deployed.Definition)
	}
	if deployed.Worker.ToolMode != tool.ModeSequential ||
		deployed.Worker.MaxIterations != 7 ||
		deployed.Worker.TTL != 2*time.Minute {
		t.Fatalf("worker execution defaults = %#v", deployed.Worker)
	}
	if got := deployed.TriggerRegistrations(); len(got) != 0 {
		t.Fatalf("manual trigger registrations = %#v, want none", got)
	}

	stored, err := runner.LoadAgentDefinitionSnapshot(ctx, definition.ID, definition.Version)
	if err != nil {
		t.Fatalf("LoadAgentDefinitionSnapshot() error = %v", err)
	}
	encoded, err := json.Marshal(deployed.Definition)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(encoded)
	if stored.Digest != hex.EncodeToString(digest[:]) || !stored.CreatedAt.Equal(createdAt.UTC()) {
		t.Fatalf("stored snapshot = %#v", stored)
	}
	agents := runner.Agents()
	if len(agents) != 1 || agents[0].ID != definition.ID || agents[0].Role != "analyst" {
		t.Fatalf("registered agents = %#v", agents)
	}
	single := NewSingleRunner(deployed)
	task := single.taskCommand(StartSingleRunRequest{}, "run", "root")
	if string(task.InputSchema) != string(inputSchema) || string(task.OutputSchema) != string(outputSchema) {
		t.Fatalf("single-run definition schemas = %s / %s", task.InputSchema, task.OutputSchema)
	}
}

func TestSingleRunnerResumeUsesRecordedDefinitionRevision(t *testing.T) {
	ctx := context.Background()
	runner := venat.NewDevelopment()
	v1Driver := &recordingProvider{}
	v1Definition := api.AgentDefinition{
		ID: "researcher", Name: "Researcher", Version: "v1",
		Instructions: "follow the original instructions",
		Model:        api.ModelPolicy{Provider: "recording", Model: "model-v1"},
	}
	v1Deployment := DefinitionDeployment{
		Runner: runner, BuildDeps: agent.BuildDeps{Providers: provider.Single(v1Driver)},
		ToolMode: tool.ModeParallel, MaxIterations: 3, TTL: time.Minute,
	}
	v1, err := v1Deployment.Deploy(ctx, v1Definition)
	if err != nil {
		t.Fatalf("Deploy(v1) error = %v", err)
	}
	started, err := NewSingleRunner(v1).Start(ctx, StartSingleRunRequest{
		RunID: "definition-revision-resume", Request: "finish with the recorded revision",
	})
	if err != nil {
		t.Fatalf("Start(v1) error = %v", err)
	}
	if started.Run.Metadata[singleRunDefinitionIDMetadata] != v1Definition.ID ||
		started.Run.Metadata[singleRunDefinitionVersionMetadata] != v1Definition.Version {
		t.Fatalf("run definition metadata = %#v", started.Run.Metadata)
	}

	v2Driver := &recordingProvider{}
	v2Definition := v1Definition
	v2Definition.Version = "v2"
	v2Definition.Instructions = "follow replacement instructions"
	v2Definition.Model.Model = "model-v2"
	v2Deployment := DefinitionDeployment{
		Runner: runner, BuildDeps: agent.BuildDeps{Providers: provider.Single(v2Driver)},
		ToolMode: tool.ModeSequential, MaxIterations: 9, TTL: 5 * time.Minute,
	}
	v2, err := v2Deployment.Deploy(ctx, v2Definition)
	if err != nil {
		t.Fatalf("Deploy(v2) error = %v", err)
	}
	restored, err := v2.restoreWorker(ctx, v1Definition.ID, v1Definition.Version)
	if err != nil {
		t.Fatalf("restoreWorker(v1) error = %v", err)
	}
	if restored.ToolMode != tool.ModeParallel || restored.MaxIterations != 3 || restored.TTL != time.Minute {
		t.Fatalf("restored v1 execution defaults = %#v", restored)
	}
	resumer := NewSingleRunner(v2)
	if _, err := resumer.Resume(ctx, started.Run.ID); err != nil {
		t.Fatalf("Resume(v1 with v2 coordinator) error = %v", err)
	}
	result, err := resumer.Execute(ctx, ExecuteSingleRunRequest{RunID: started.Run.ID})
	if err != nil {
		t.Fatalf("Execute(resumed v1) error = %v", err)
	}
	if result.Execution.State != ExecutionCompleted {
		t.Fatalf("execution state = %q", result.Execution.State)
	}
	if len(v1Driver.requests) != 0 {
		t.Fatalf("v1 deployment driver received %d requests after replacement", len(v1Driver.requests))
	}
	if len(v2Driver.requests) != 1 {
		t.Fatalf("replacement driver requests = %d, want 1", len(v2Driver.requests))
	}
	request := v2Driver.requests[0]
	if request.Model != "model-v1" {
		t.Fatalf("resumed model = %q, want model-v1", request.Model)
	}
	if len(request.Messages) == 0 || request.Messages[0].Text != v1Definition.Instructions {
		t.Fatalf("resumed messages = %#v, want original instructions", request.Messages)
	}
}

func TestDefinitionDeploymentOwnsEffectiveDefinitionData(t *testing.T) {
	ctx := context.Background()
	runner := venat.NewDevelopment()
	schemaText := `{"type":"string"}`
	schema := json.RawMessage(schemaText)
	definition := api.AgentDefinition{
		ID: "owned", Name: "Owned", Version: "v1",
		Model:        api.ModelPolicy{Model: "primary"},
		Tools:        []string{"lookup"},
		InputSchema:  schema,
		Capabilities: []string{"search"},
		Triggers: []api.Trigger{{
			ID: "manual", Type: api.TriggerManual, Enabled: true,
			Config: map[string]string{"source": "original"},
		}},
		Metadata: map[string]string{"role": "original"},
	}
	deployed, err := (DefinitionDeployment{
		Runner: runner,
		BuildDeps: agent.BuildDeps{
			Providers: provider.Single(&recordingProvider{}),
			Tools:     tool.NewBus(&recordingTool{definition: tool.Definition{Name: "lookup"}}),
		},
	}).Deploy(ctx, definition)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = deployed.Close() }()

	definition.Tools[0] = "caller-mutated"
	definition.InputSchema[2] = 'X'
	definition.Capabilities[0] = "caller-mutated"
	definition.Triggers[0].Config["source"] = "caller-mutated"
	definition.Metadata["role"] = "caller-mutated"
	if deployed.Definition.Tools[0] != "lookup" ||
		string(deployed.Definition.InputSchema) != schemaText ||
		deployed.Definition.Capabilities[0] != "search" ||
		deployed.Definition.Triggers[0].Config["source"] != "original" ||
		deployed.Definition.Metadata["role"] != "original" {
		t.Fatalf("effective definition aliases caller data: %#v", deployed.Definition)
	}

	deployed.Definition.Tools[0] = "returned-mutated"
	deployed.Definition.Triggers[0].Config["source"] = "returned-mutated"
	deployed.Definition.Metadata["role"] = "returned-mutated"
	if deployed.Snapshot.Definition.Tools[0] != "lookup" ||
		deployed.Snapshot.Definition.Triggers[0].Config["source"] != "original" ||
		deployed.Snapshot.Definition.Metadata["role"] != "original" {
		t.Fatalf("snapshot aliases returned definition: %#v", deployed.Snapshot.Definition)
	}
	stored, err := runner.LoadAgentDefinitionSnapshot(ctx, "owned", "v1")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Definition.Tools[0] != "lookup" ||
		stored.Definition.Triggers[0].Config["source"] != "original" ||
		stored.Definition.Metadata["role"] != "original" {
		t.Fatalf("stored snapshot was mutated: %#v", stored.Definition)
	}
}

func TestDefinitionDeploymentResolvesNamedHooks(t *testing.T) {
	handler := &resumeToolLifecycleHook{}
	deployed, err := (DefinitionDeployment{
		Runner:       venat.NewDevelopment(),
		BuildDeps:    agent.BuildDeps{Providers: provider.Single(&recordingProvider{})},
		HookRegistry: map[string]hook.Handler{"audit": handler},
	}).Deploy(context.Background(), api.AgentDefinition{
		ID: "hooked", Name: "Hooked", Version: "v1",
		Model: api.ModelPolicy{Model: "primary"},
		Hooks: []string{"audit"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = deployed.Close() }()
	if deployed.Worker.Engine.Hooks.Len() != 1 {
		t.Fatalf("materialized hook count = %d, want 1", deployed.Worker.Engine.Hooks.Len())
	}
}

func TestDefinitionDeploymentRejectsUnresolvedExecutionBindings(t *testing.T) {
	handler := &resumeToolLifecycleHook{}
	tests := []struct {
		name       string
		deployment DefinitionDeployment
		definition api.AgentDefinition
		want       error
	}{
		{
			name: "missing named hook",
			deployment: DefinitionDeployment{
				Runner:    venat.NewDevelopment(),
				BuildDeps: agent.BuildDeps{Providers: provider.Single(&recordingProvider{})},
			},
			definition: api.AgentDefinition{
				ID: "missing-hook", Name: "Missing Hook", Version: "v1",
				Model: api.ModelPolicy{Model: "primary"}, Hooks: []string{"missing"},
			},
			want: ErrDefinitionInvalid,
		},
		{
			name: "unversioned hook chain",
			deployment: DefinitionDeployment{
				Runner: venat.NewDevelopment(),
				BuildDeps: agent.BuildDeps{
					Providers: provider.Single(&recordingProvider{}),
					Hooks:     hook.NewChain(handler),
				},
			},
			definition: api.AgentDefinition{
				ID: "unversioned-hook", Name: "Unversioned Hook", Version: "v1",
				Model: api.ModelPolicy{Model: "primary"},
			},
			want: ErrDefinitionUnsupported,
		},
		{
			name: "malformed schema",
			deployment: DefinitionDeployment{
				Runner:    venat.NewDevelopment(),
				BuildDeps: agent.BuildDeps{Providers: provider.Single(&recordingProvider{})},
			},
			definition: api.AgentDefinition{
				ID: "malformed-schema", Name: "Malformed Schema", Version: "v1",
				Model: api.ModelPolicy{Model: "primary"}, InputSchema: json.RawMessage(`{`),
			},
			want: ErrDefinitionInvalid,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := test.deployment.Deploy(context.Background(), test.definition); !errors.Is(err, test.want) {
				t.Fatalf("Deploy() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestDefinitionDeploymentOwnsTriggerLifecycle(t *testing.T) {
	registrar := &deploymentRegistrar{}
	deployed, err := (DefinitionDeployment{
		Runner:     venat.NewDevelopment(),
		BuildDeps:  agent.BuildDeps{Providers: provider.Single(&recordingProvider{})},
		Registrars: transporttrigger.Registrars{api.TriggerEvent: registrar},
		TriggerHandler: transporttrigger.HandlerFunc(func(context.Context, transporttrigger.TriggerContext) error {
			return nil
		}),
	}).Deploy(context.Background(), api.AgentDefinition{
		ID:      "event-agent",
		Name:    "Event Agent",
		Version: "v1",
		Model:   api.ModelPolicy{Model: "primary"},
		Triggers: []api.Trigger{{
			ID: "incident", Type: api.TriggerEvent, Enabled: true,
		}},
	})
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}
	if len(deployed.TriggerRegistrations()) != 1 {
		t.Fatalf("trigger registrations = %#v", deployed.TriggerRegistrations())
	}
	if err := deployed.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if len(registrar.removed) != 1 || registrar.removed[0] != "incident" {
		t.Fatalf("removed triggers = %v", registrar.removed)
	}
}

func TestDefinitionDeploymentRejectsUnsupportedFieldsBeforePublishing(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*api.AgentDefinition)
	}{
		{name: "context", mutate: func(definition *api.AgentDefinition) {
			definition.Context = []api.ContextSource{{Name: "memory", Type: "vector"}}
		}},
		{name: "definition authorization policy", mutate: func(definition *api.AgentDefinition) {
			definition.Governance.AllowedCapabilities = []string{"lookup"}
		}},
		{name: "action call budget", mutate: func(definition *api.AgentDefinition) {
			definition.Governance.Budget.MaxActionCalls = 1
		}},
		{name: "credit budget", mutate: func(definition *api.AgentDefinition) {
			definition.Governance.Budget.MaxCredits = 1
		}},
		{name: "unknown trigger", mutate: func(definition *api.AgentDefinition) {
			definition.Triggers = []api.Trigger{{ID: "future", Type: "future", Enabled: true}}
		}},
		{name: "unknown tool mode", mutate: func(definition *api.AgentDefinition) {
			definition.ToolMode = "future"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := venat.NewDevelopment()
			definition := api.AgentDefinition{ID: "agent", Name: "Agent", Version: "v1", Model: api.ModelPolicy{Model: "primary"}}
			test.mutate(&definition)
			_, err := (DefinitionDeployment{
				Runner:    runner,
				BuildDeps: agent.BuildDeps{Providers: provider.Single(&recordingProvider{})},
			}).Deploy(context.Background(), definition)
			if !errors.Is(err, ErrDefinitionUnsupported) {
				t.Fatalf("Deploy() error = %v, want ErrDefinitionUnsupported", err)
			}
			if _, loadErr := runner.LoadAgentDefinitionSnapshot(context.Background(), definition.ID, definition.Version); loadErr == nil {
				t.Fatal("unsupported definition was published")
			}
		})
	}
}

func TestDefinitionDeploymentRequiresAndCarriesAdmissionController(t *testing.T) {
	runner := venat.NewDevelopment()
	definition := api.AgentDefinition{
		ID: "governed", Name: "Governed", Version: "v1", Model: api.ModelPolicy{Model: "primary"},
		Governance: api.GovernancePolicy{MaxConcurrentRuns: 1},
	}
	deployment := DefinitionDeployment{
		Runner: runner, BuildDeps: agent.BuildDeps{Providers: provider.Single(&recordingProvider{})},
	}
	if _, err := deployment.Deploy(context.Background(), definition); !errors.Is(err, ErrAdmissionControllerMissing) {
		t.Fatalf("Deploy() error = %v, want ErrAdmissionControllerMissing", err)
	}
	controller := StandardAdmissionController{Runner: runner}
	deployment.Admission = controller
	deployed, err := deployment.Deploy(context.Background(), definition)
	if err != nil {
		t.Fatalf("Deploy() with admission error = %v", err)
	}
	defer func() { _ = deployed.Close() }()
	if deployed.Admission == nil {
		t.Fatal("deployment dropped admission controller")
	}
}

type deploymentRegistrar struct {
	removed []string
}

func (r *deploymentRegistrar) Register(configured api.Trigger, agentID string, handler transporttrigger.Handler) (transporttrigger.Registration, error) {
	return transporttrigger.Registration{Trigger: configured, AgentID: agentID, Handler: handler}, nil
}

func (r *deploymentRegistrar) Deregister(triggerID string) bool {
	r.removed = append(r.removed, triggerID)
	return true
}
