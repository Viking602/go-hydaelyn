package control

import (
	"context"
	"time"

	"github.com/Viking602/go-hydaelyn/host"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/internal/session"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
	"github.com/Viking602/go-hydaelyn/tool"
)

type API struct {
	runtime *host.Runtime
}

func New(runtime *host.Runtime) *API {
	return &API{runtime: runtime}
}

type CreateSessionRequest struct {
	Branch   string            `json:"branch"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type PromptRequest struct {
	Provider string            `json:"provider"`
	Model    string            `json:"model"`
	Messages []message.Message `json:"messages,omitempty"`
	ToolMode tool.Mode         `json:"toolMode,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
	Agent    host.AgentOptions `json:"agent,omitempty"`
}

type ContinueRequest struct {
	Provider string            `json:"provider"`
	Model    string            `json:"model"`
	ToolMode tool.Mode         `json:"toolMode,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
	Agent    host.AgentOptions `json:"agent,omitempty"`
}

type DrainSchedulerRequest struct {
	MaxTasks int `json:"maxTasks,omitempty"`
}

type DrainSchedulerResponse struct {
	Processed int `json:"processed"`
}

type RecoverSchedulerRequest struct {
	Now time.Time `json:"now,omitempty"`
}

type RecoverSchedulerResponse struct {
	Status string `json:"status"`
}

func (a *API) ListTeams(ctx context.Context) ([]team.RunState, error) {
	return a.runtime.ListTeams(ctx)
}

func (a *API) CreateSession(ctx context.Context, request CreateSessionRequest) (session.Session, error) {
	return a.runtime.CreateSession(ctx, session.CreateParams{
		Branch:   request.Branch,
		Metadata: request.Metadata,
	})
}

func (a *API) GetSession(ctx context.Context, sessionID string) (session.Snapshot, error) {
	return a.runtime.GetSession(ctx, sessionID)
}

func (a *API) Prompt(ctx context.Context, sessionID string, request PromptRequest) (host.PromptResponse, error) {
	return a.runtime.Prompt(ctx, host.PromptRequest{
		SessionID: sessionID,
		Provider:  request.Provider,
		Model:     request.Model,
		Messages:  request.Messages,
		ToolMode:  request.ToolMode,
		Metadata:  request.Metadata,
		Agent:     request.Agent,
	})
}

func (a *API) Continue(ctx context.Context, sessionID string, request ContinueRequest) (host.PromptResponse, error) {
	return a.runtime.Continue(ctx, host.ContinueRequest{
		SessionID: sessionID,
		Provider:  request.Provider,
		Model:     request.Model,
		ToolMode:  request.ToolMode,
		Metadata:  request.Metadata,
		Agent:     request.Agent,
	})
}

func (a *API) StreamPrompt(ctx context.Context, sessionID string, request PromptRequest, emit func(provider.Event) error) (host.PromptResponse, error) {
	return a.runtime.PromptStream(ctx, host.PromptRequest{
		SessionID: sessionID,
		Provider:  request.Provider,
		Model:     request.Model,
		Messages:  request.Messages,
		ToolMode:  request.ToolMode,
		Metadata:  request.Metadata,
		Agent:     request.Agent,
	}, emit)
}

func (a *API) GetTeam(ctx context.Context, teamID string) (team.RunState, error) {
	return a.runtime.GetTeam(ctx, teamID)
}

func (a *API) TeamEvents(ctx context.Context, teamID string) ([]storage.Event, error) {
	return a.runtime.TeamEvents(ctx, teamID)
}

func (a *API) ResumeTeam(ctx context.Context, teamID string) (team.RunState, error) {
	return a.runtime.ResumeTeam(ctx, teamID)
}

func (a *API) ReplayTeam(ctx context.Context, teamID string) (team.RunState, error) {
	return a.runtime.ReplayTeamState(ctx, teamID)
}

func (a *API) AbortTeam(ctx context.Context, teamID string) error {
	return a.runtime.AbortTeam(ctx, teamID)
}

func (a *API) DrainScheduler(ctx context.Context, request DrainSchedulerRequest) (DrainSchedulerResponse, error) {
	processed, err := a.runtime.RunQueueWorker(ctx, request.MaxTasks)
	if err != nil {
		return DrainSchedulerResponse{}, err
	}
	return DrainSchedulerResponse{Processed: processed}, nil
}

func (a *API) RecoverScheduler(ctx context.Context, request RecoverSchedulerRequest) (RecoverSchedulerResponse, error) {
	now := request.Now
	if now.IsZero() {
		now = time.Now()
	}
	if err := a.runtime.RecoverQueueLeases(ctx, now); err != nil {
		return RecoverSchedulerResponse{}, err
	}
	return RecoverSchedulerResponse{Status: "recovered"}, nil
}

func (a *API) AbortRun(ctx context.Context, runID string) error {
	return a.runtime.AbortRun(ctx, runID)
}
