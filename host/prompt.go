package host

import (
	"context"
	"fmt"
	"time"

	"github.com/Viking602/go-hydaelyn/agent"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/middleware"
	"github.com/Viking602/go-hydaelyn/plugin"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/session"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/tool"
)

type PromptRequest struct {
	SessionID string
	Provider  string
	Model     string
	Messages  []message.Message
	ToolMode  tool.Mode
	Metadata  map[string]string
	Agent     AgentOptions
}

type PromptResponse struct {
	Run        storage.Run         `json:"run"`
	Session    session.Session     `json:"session"`
	NewEntries []session.Entry     `json:"newEntries"`
	Messages   []message.Message   `json:"messages"`
	Usage      provider.Usage      `json:"usage"`
	StopReason provider.StopReason `json:"stopReason"`
}

func (r *Runtime) Prompt(ctx context.Context, request PromptRequest) (PromptResponse, error) {
	return r.promptCore(ctx, request, nil)
}

func (r *Runtime) PromptStream(ctx context.Context, request PromptRequest, onEvent func(provider.Event) error) (PromptResponse, error) {
	return r.promptCore(ctx, request, onEvent)
}

func (r *Runtime) promptCore(ctx context.Context, request PromptRequest, onEvent func(provider.Event) error) (PromptResponse, error) {
	currentProvider, err := r.lookupProvider(request.Provider)
	if err != nil {
		return PromptResponse{}, err
	}
	snapshot, err := r.loadSession(ctx, request.SessionID)
	if err != nil {
		return PromptResponse{}, err
	}
	if len(request.Messages) > 0 {
		if _, err := r.appendSessionMessages(ctx, request.SessionID, request.Messages...); err != nil {
			return PromptResponse{}, err
		}
		snapshot.Messages = append(snapshot.Messages, request.Messages...)
	}
	run := storage.Run{
		ID:        r.nextRunID(),
		SessionID: request.SessionID,
		Status:    storage.RunStatusRunning,
		Provider:  request.Provider,
		Model:     request.Model,
		Metadata:  cloneStringMap(request.Metadata),
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := r.storage.Runs().Save(ctx, run); err != nil {
		return PromptResponse{}, err
	}
	runCtx, cancel := context.WithCancel(ctx)
	r.mu.Lock()
	r.activeRuns[run.ID] = cancel
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.activeRuns, run.ID)
		r.mu.Unlock()
	}()
	engine := agent.Engine{
		Provider: currentProvider,
		Tools:    r.tools,
		Hooks:    r.engineHooks(),
	}
	var result agent.Result
	runMetadata := cloneStringMap(request.Metadata)
	if runMetadata == nil {
		runMetadata = map[string]string{}
	}
	runMetadata["runId"] = run.ID
	outputGuardrails, err := r.resolveOutputGuardrails(request.Agent)
	if err != nil {
		run.Status = storage.RunStatusFailed
		run.Error = err.Error()
		_ = r.storage.Runs().Save(ctx, run)
		return PromptResponse{}, err
	}
	compactedMessages := r.compactMessages(runCtx, snapshot.Messages)
	err = r.runStage(runCtx, &middleware.Envelope{
		Stage:     middleware.StageAgent,
		Operation: "prompt",
		Metadata:  cloneStringMap(runMetadata),
		Request:   request,
	}, func(ctx context.Context, envelope *middleware.Envelope) error {
		runResult, runErr := engine.Run(ctx, agent.Input{
			Model:            request.Model,
			Messages:         compactedMessages,
			Metadata:         runMetadata,
			ToolMode:         request.ToolMode,
			MaxIterations:    max(1, request.Agent.maxIterationsOrDefault(6)),
			OnEvent:          onEvent,
			StopSequences:    append([]string{}, request.Agent.StopSequences...),
			ThinkingBudget:   request.Agent.ThinkingBudget,
			OutputGuardrails: outputGuardrails,
			OutputRecorder:   r,
		})
		if runErr != nil {
			return runErr
		}
		result = runResult
		envelope.Response = runResult
		return nil
	})
	if err != nil {
		run.Status = storage.RunStatusFailed
		run.Error = err.Error()
		_ = r.storage.Runs().Save(ctx, run)
		return PromptResponse{}, err
	}
	generated := result.Messages[len(compactedMessages):]
	entries, err := r.appendSessionMessages(ctx, request.SessionID, generated...)
	if err != nil {
		return PromptResponse{}, err
	}
	run.Status = storage.RunStatusCompleted
	if err := r.storage.Runs().Save(ctx, run); err != nil {
		return PromptResponse{}, err
	}
	finalSnapshot, err := r.loadSession(ctx, request.SessionID)
	if err != nil {
		return PromptResponse{}, err
	}
	return PromptResponse{
		Run:        run,
		Session:    finalSnapshot.Session,
		NewEntries: entries,
		Messages:   generated,
		Usage:      result.Usage,
		StopReason: result.StopReason,
	}, nil
}

type ContinueRequest struct {
	SessionID string
	Provider  string
	Model     string
	ToolMode  tool.Mode
	Metadata  map[string]string
	Agent     AgentOptions
}

type DumpConfigRequest struct {
	Plugins     []plugin.Ref
	ProfileName string
	TeamConfig  map[string]string
	RunConfig   map[string]string
}

type DumpConfigResponse struct {
	Values map[string]string            `json:"values"`
	Layers map[string]map[string]string `json:"layers,omitempty"`
}

func (r *Runtime) Continue(ctx context.Context, request ContinueRequest) (PromptResponse, error) {
	return r.Prompt(ctx, PromptRequest{
		SessionID: request.SessionID,
		Provider:  request.Provider,
		Model:     request.Model,
		ToolMode:  request.ToolMode,
		Metadata:  request.Metadata,
		Agent:     request.Agent,
	})
}

func (r *Runtime) DumpConfig(request DumpConfigRequest) (DumpConfigResponse, error) {
	values := map[string]string{}
	mergeStringMap(values, r.defaults)
	layers := map[string]map[string]string{
		"default": cloneStringMap(r.defaults),
	}
	pluginLayer := map[string]string{}
	for _, ref := range request.Plugins {
		spec, ok := r.plugins.Lookup(ref.Type, ref.Name)
		if !ok {
			return DumpConfigResponse{}, fmt.Errorf("plugin not found: %s/%s", ref.Type, ref.Name)
		}
		mergeStringMap(pluginLayer, spec.Config)
		mergeStringMap(values, spec.Config)
	}
	if len(pluginLayer) > 0 {
		layers["plugin"] = pluginLayer
	}
	if request.ProfileName != "" {
		profile, err := r.lookupProfile(request.ProfileName)
		if err != nil {
			return DumpConfigResponse{}, err
		}
		layers["profile"] = cloneStringMap(profile.Metadata)
		mergeStringMap(values, profile.Metadata)
	}
	layers["team"] = cloneStringMap(request.TeamConfig)
	mergeStringMap(values, request.TeamConfig)
	layers["run"] = cloneStringMap(request.RunConfig)
	mergeStringMap(values, request.RunConfig)
	return DumpConfigResponse{
		Values: values,
		Layers: layers,
	}, nil
}

func (r *Runtime) AbortRun(_ context.Context, runID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cancel, ok := r.activeRuns[runID]
	if !ok {
		return nil
	}
	cancel()
	delete(r.activeRuns, runID)
	return nil
}
