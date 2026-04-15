package host

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Viking602/go-hydaelyn/agent"
	"github.com/Viking602/go-hydaelyn/auth"
	"github.com/Viking602/go-hydaelyn/hook"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/session"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
	"github.com/Viking602/go-hydaelyn/tool"
	"github.com/Viking602/go-hydaelyn/workflow"
)

var ErrProviderNotFound = errors.New("provider not found")
var ErrProfileNotFound = errors.New("profile not found")
var ErrPatternNotFound = errors.New("pattern not found")

type Config struct {
	Storage storage.Driver
	Auth    auth.Driver
}

type Runtime struct {
	storage     storage.Driver
	auth        auth.Driver
	tools       *tool.Bus
	workflows   *workflow.Registry
	hooks       hook.Chain
	providers   map[string]provider.Driver
	profiles    map[string]team.Profile
	patterns    map[string]team.Pattern
	mu          sync.RWMutex
	runSeq      uint64
	teamSeq     uint64
	activeRuns  map[string]context.CancelFunc
	activeTeams map[string]context.CancelFunc
}

func New(config Config) *Runtime {
	driver := config.Storage
	if driver == nil {
		driver = storage.NewMemoryDriver()
	}
	return &Runtime{
		storage:     driver,
		auth:        config.Auth,
		tools:       tool.NewBus(),
		workflows:   workflow.NewRegistry(),
		providers:   map[string]provider.Driver{},
		profiles:    map[string]team.Profile{},
		patterns:    map[string]team.Pattern{},
		activeRuns:  map[string]context.CancelFunc{},
		activeTeams: map[string]context.CancelFunc{},
	}
}

func (r *Runtime) RegisterProvider(name string, driver provider.Driver) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[name] = driver
}

func (r *Runtime) RegisterTool(driver tool.Driver) {
	r.tools.Register(driver)
}

func (r *Runtime) RegisterWorkflow(driver workflow.Driver) {
	r.workflows.Register(driver)
}

func (r *Runtime) RegisterHook(handler hook.Handler) {
	r.hooks = r.hooks.Append(handler)
}

func (r *Runtime) RegisterProfile(profile team.Profile) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.profiles[profile.Name] = profile
}

func (r *Runtime) RegisterPattern(pattern team.Pattern) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.patterns[pattern.Name()] = pattern
}

func (r *Runtime) CreateSession(ctx context.Context, params session.CreateParams) (session.Session, error) {
	return r.storage.Sessions().Create(ctx, params)
}

func (r *Runtime) GetSession(ctx context.Context, sessionID string) (session.Snapshot, error) {
	return r.storage.Sessions().Load(ctx, sessionID)
}

type PromptRequest struct {
	SessionID string
	Provider  string
	Model     string
	Messages  []message.Message
	ToolMode  tool.Mode
	Metadata  map[string]string
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
	currentProvider, err := r.lookupProvider(request.Provider)
	if err != nil {
		return PromptResponse{}, err
	}
	snapshot, err := r.storage.Sessions().Load(ctx, request.SessionID)
	if err != nil {
		return PromptResponse{}, err
	}
	if len(request.Messages) > 0 {
		if _, err := r.storage.Sessions().Append(ctx, request.SessionID, request.Messages...); err != nil {
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
		Hooks:    r.hooks,
	}
	result, err := engine.Run(runCtx, agent.Input{
		Model:         request.Model,
		Messages:      snapshot.Messages,
		Metadata:      request.Metadata,
		ToolMode:      request.ToolMode,
		MaxIterations: 6,
	})
	if err != nil {
		run.Status = storage.RunStatusFailed
		run.Error = err.Error()
		_ = r.storage.Runs().Save(ctx, run)
		return PromptResponse{}, err
	}
	generated := result.Messages[len(snapshot.Messages):]
	entries, err := r.storage.Sessions().Append(ctx, request.SessionID, generated...)
	if err != nil {
		return PromptResponse{}, err
	}
	run.Status = storage.RunStatusCompleted
	if err := r.storage.Runs().Save(ctx, run); err != nil {
		return PromptResponse{}, err
	}
	finalSnapshot, err := r.storage.Sessions().Load(ctx, request.SessionID)
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
}

func (r *Runtime) Continue(ctx context.Context, request ContinueRequest) (PromptResponse, error) {
	return r.Prompt(ctx, PromptRequest{
		SessionID: request.SessionID,
		Provider:  request.Provider,
		Model:     request.Model,
		ToolMode:  request.ToolMode,
		Metadata:  request.Metadata,
	})
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

type StartTeamRequest struct {
	TeamID            string
	Pattern           string
	SupervisorProfile string
	WorkerProfiles    []string
	Input             map[string]any
	Metadata          map[string]string
}

func (r *Runtime) StartTeam(ctx context.Context, request StartTeamRequest) (team.RunState, error) {
	pattern, err := r.lookupPattern(request.Pattern)
	if err != nil {
		return team.RunState{}, err
	}
	if request.TeamID == "" {
		request.TeamID = r.nextTeamID()
	}
	if _, err := r.lookupProfile(request.SupervisorProfile); err != nil {
		return team.RunState{}, err
	}
	for _, name := range request.WorkerProfiles {
		if _, err := r.lookupProfile(name); err != nil {
			return team.RunState{}, err
		}
	}
	state, err := pattern.Start(ctx, team.StartRequest{
		TeamID:            request.TeamID,
		Pattern:           request.Pattern,
		SupervisorProfile: request.SupervisorProfile,
		WorkerProfiles:    request.WorkerProfiles,
		Input:             request.Input,
		Metadata:          request.Metadata,
	})
	if err != nil {
		return team.RunState{}, err
	}
	if state.ID == "" {
		state.ID = request.TeamID
	}
	if state.Pattern == "" {
		state.Pattern = request.Pattern
	}
	teamSession, err := r.ensureTeamSession(ctx, state)
	if err != nil {
		return team.RunState{}, err
	}
	state.SessionID = teamSession.ID
	if err := r.storage.Teams().Save(ctx, state); err != nil {
		return team.RunState{}, err
	}
	teamCtx, cancel := context.WithCancel(ctx)
	r.mu.Lock()
	r.activeTeams[state.ID] = cancel
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.activeTeams, state.ID)
		r.mu.Unlock()
	}()
	return r.driveTeam(teamCtx, pattern, state)
}

func (r *Runtime) ResumeTeam(ctx context.Context, teamID string) (team.RunState, error) {
	state, err := r.storage.Teams().Load(ctx, teamID)
	if err != nil {
		return team.RunState{}, err
	}
	pattern, err := r.lookupPattern(state.Pattern)
	if err != nil {
		return team.RunState{}, err
	}
	teamCtx, cancel := context.WithCancel(ctx)
	r.mu.Lock()
	r.activeTeams[state.ID] = cancel
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.activeTeams, state.ID)
		r.mu.Unlock()
	}()
	return r.driveTeam(teamCtx, pattern, state)
}

func (r *Runtime) AbortTeam(_ context.Context, teamID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cancel, ok := r.activeTeams[teamID]
	if ok {
		cancel()
		delete(r.activeTeams, teamID)
	}
	return nil
}

func (r *Runtime) StartWorkflow(ctx context.Context, name string, input map[string]any) (workflow.State, error) {
	driver, ok := r.workflows.Driver(name)
	if !ok {
		return workflow.State{}, fmt.Errorf("workflow not found: %s", name)
	}
	state, err := driver.Start(ctx, input)
	if err != nil {
		return workflow.State{}, err
	}
	if err := r.storage.Workflows().Save(ctx, state); err != nil {
		return workflow.State{}, err
	}
	return state, nil
}

func (r *Runtime) ResumeWorkflow(ctx context.Context, workflowID string) (workflow.State, error) {
	current, err := r.storage.Workflows().Load(ctx, workflowID)
	if err != nil {
		return workflow.State{}, err
	}
	driver, ok := r.workflows.Driver(current.Name)
	if !ok {
		return workflow.State{}, fmt.Errorf("workflow not found: %s", current.Name)
	}
	next, err := driver.Resume(ctx, current)
	if err != nil {
		return workflow.State{}, err
	}
	if err := r.storage.Workflows().Save(ctx, next); err != nil {
		return workflow.State{}, err
	}
	return next, nil
}

func (r *Runtime) AbortWorkflow(ctx context.Context, workflowID string) (workflow.State, error) {
	current, err := r.storage.Workflows().Load(ctx, workflowID)
	if err != nil {
		return workflow.State{}, err
	}
	driver, ok := r.workflows.Driver(current.Name)
	if !ok {
		return workflow.State{}, fmt.Errorf("workflow not found: %s", current.Name)
	}
	next, err := driver.Abort(ctx, current)
	if err != nil {
		return workflow.State{}, err
	}
	if err := r.storage.Workflows().Save(ctx, next); err != nil {
		return workflow.State{}, err
	}
	return next, nil
}

func (r *Runtime) driveTeam(ctx context.Context, pattern team.Pattern, state team.RunState) (team.RunState, error) {
	current := state
	for step := 0; step < 24; step++ {
		if current.IsTerminal() {
			if err := r.storage.Teams().Save(ctx, current); err != nil {
				return team.RunState{}, err
			}
			return current, nil
		}
		runnable := current.RunnableTasks()
		if len(runnable) > 0 {
			next, err := r.executeTasks(ctx, current)
			if err != nil {
				return team.RunState{}, err
			}
			current = next
			if err := r.storage.Teams().Save(ctx, current); err != nil {
				return team.RunState{}, err
			}
			continue
		}
		next, err := pattern.Advance(ctx, current)
		if err != nil {
			return team.RunState{}, err
		}
		if next.ID == "" {
			next.ID = current.ID
		}
		if next.SessionID == "" {
			next.SessionID = current.SessionID
		}
		current = next
		if err := r.storage.Teams().Save(ctx, current); err != nil {
			return team.RunState{}, err
		}
	}
	current.Status = team.StatusFailed
	if current.Result == nil {
		current.Result = &team.Result{Error: "team exceeded execution steps"}
	}
	_ = r.storage.Teams().Save(ctx, current)
	return current, nil
}

func (r *Runtime) executeTasks(ctx context.Context, state team.RunState) (team.RunState, error) {
	type outcome struct {
		index int
		task  team.Task
		err   error
	}
	current := state
	semByProfile := map[string]chan struct{}{}
	for _, task := range current.Tasks {
		if task.Status != team.TaskStatusPending {
			continue
		}
		profile, err := r.lookupProfile(task.Assignee)
		if err != nil {
			return team.RunState{}, err
		}
		if profile.MaxConcurrency > 0 {
			if _, ok := semByProfile[profile.Name]; !ok {
				semByProfile[profile.Name] = make(chan struct{}, profile.MaxConcurrency)
			}
		}
	}
	results := make(chan outcome, len(current.Tasks))
	var wg sync.WaitGroup
	for idx, task := range current.Tasks {
		if task.Status != team.TaskStatusPending {
			continue
		}
		wg.Add(1)
		go func(index int, original team.Task) {
			defer wg.Done()
			profile, err := r.lookupProfile(original.Assignee)
			if err != nil {
				results <- outcome{index: index, task: original, err: err}
				return
			}
			if sem, ok := semByProfile[profile.Name]; ok {
				sem <- struct{}{}
				defer func() { <-sem }()
			}
			item, err := r.executeTask(ctx, current, original, profile)
			results <- outcome{index: index, task: item, err: err}
		}(idx, task)
	}
	wg.Wait()
	close(results)
	for result := range results {
		current.Tasks[result.index] = result.task
		if result.err != nil {
			current.Status = team.StatusRunning
		}
	}
	current.UpdatedAt = time.Now().UTC()
	return current, nil
}

func (r *Runtime) executeTask(ctx context.Context, state team.RunState, task team.Task, profile team.Profile) (team.Task, error) {
	task.Status = team.TaskStatusRunning
	task.StartedAt = time.Now().UTC()
	if task.SessionID == "" {
		workerSession, err := r.storage.Sessions().Create(ctx, session.CreateParams{
			TeamID:  state.ID,
			AgentID: task.Assignee,
			Scope:   message.VisibilityPrivate,
			Branch:  task.ID,
			Metadata: map[string]string{
				"kind":    string(task.Kind),
				"profile": profile.Name,
			},
		})
		if err != nil {
			task.Status = team.TaskStatusFailed
			task.Error = err.Error()
			return task, err
		}
		task.SessionID = workerSession.ID
	}
	snapshot, err := r.storage.Sessions().Load(ctx, task.SessionID)
	if err != nil {
		task.Status = team.TaskStatusFailed
		task.Error = err.Error()
		return task, err
	}
	initialMessages := snapshot.Messages
	if len(initialMessages) == 0 {
		initialMessages = r.initialTaskMessages(state, task, profile)
		if _, err := r.storage.Sessions().Append(ctx, task.SessionID, initialMessages...); err != nil {
			task.Status = team.TaskStatusFailed
			task.Error = err.Error()
			return task, err
		}
	}
	currentProvider, err := r.lookupProvider(profile.Provider)
	if err != nil {
		task.Status = team.TaskStatusFailed
		task.Error = err.Error()
		return task, err
	}
	engine := agent.Engine{
		Provider: currentProvider,
		Tools:    r.tools.Subset(profile.ToolNames),
		Hooks:    r.hooks,
	}
	result, err := engine.Run(ctx, agent.Input{
		Model:         profile.Model,
		Messages:      initialMessages,
		ToolMode:      tool.ModeParallel,
		MaxIterations: max(1, profile.MaxTurns),
		Metadata: map[string]string{
			"teamId":  state.ID,
			"taskId":  task.ID,
			"profile": profile.Name,
		},
	})
	if err != nil {
		task.Status = team.TaskStatusFailed
		task.Error = err.Error()
		task.Result = &team.Result{Error: err.Error()}
		task.FinishedAt = time.Now().UTC()
		return task, err
	}
	generated := result.Messages[len(initialMessages):]
	if len(generated) > 0 {
		if _, err := r.storage.Sessions().Append(ctx, task.SessionID, generated...); err != nil {
			task.Status = team.TaskStatusFailed
			task.Error = err.Error()
			return task, err
		}
	}
	task.Status = team.TaskStatusCompleted
	task.Result = r.buildTaskResult(task, generated)
	task.FinishedAt = time.Now().UTC()
	shared := message.NewText(message.RoleAssistant, task.Result.Summary)
	shared.TeamID = state.ID
	shared.AgentID = task.Assignee
	shared.Visibility = message.VisibilityShared
	shared.Metadata = map[string]string{
		"taskId": string(task.Kind),
	}
	_, _ = r.storage.Sessions().Append(ctx, state.SessionID, shared)
	return task, nil
}

func (r *Runtime) buildTaskResult(task team.Task, generated []message.Message) *team.Result {
	summary := task.Input
	for idx := len(generated) - 1; idx >= 0; idx-- {
		if strings.TrimSpace(generated[idx].Text) != "" {
			summary = generated[idx].Text
			break
		}
	}
	return &team.Result{
		Summary: summary,
		Findings: []team.Finding{
			{
				Summary:    summary,
				Confidence: 0.75,
			},
		},
		Evidence: []team.Evidence{
			{Source: task.Title, Snippet: summary},
		},
		Confidence: 0.75,
	}
}

func (r *Runtime) initialTaskMessages(state team.RunState, task team.Task, profile team.Profile) []message.Message {
	messages := make([]message.Message, 0, 2)
	if strings.TrimSpace(profile.Prompt) != "" {
		system := message.NewText(message.RoleSystem, profile.Prompt)
		system.TeamID = state.ID
		system.AgentID = task.Assignee
		system.Visibility = message.VisibilityPrivate
		messages = append(messages, system)
	}
	user := message.NewText(message.RoleUser, task.Input)
	user.TeamID = state.ID
	user.AgentID = task.Assignee
	user.Visibility = message.VisibilityPrivate
	messages = append(messages, user)
	return messages
}

func (r *Runtime) ensureTeamSession(ctx context.Context, state team.RunState) (session.Session, error) {
	if state.SessionID != "" {
		snapshot, err := r.storage.Sessions().Load(ctx, state.SessionID)
		if err == nil {
			return snapshot.Session, nil
		}
	}
	current, err := r.storage.Sessions().Create(ctx, session.CreateParams{
		ID:     fmt.Sprintf("%s-session", state.ID),
		TeamID: state.ID,
		Scope:  message.VisibilityShared,
		Metadata: map[string]string{
			"pattern": state.Pattern,
		},
	})
	if err != nil {
		return session.Session{}, err
	}
	if query, ok := state.Input["query"].(string); ok && strings.TrimSpace(query) != "" {
		root := message.NewText(message.RoleUser, query)
		root.TeamID = state.ID
		root.Visibility = message.VisibilityShared
		_, _ = r.storage.Sessions().Append(ctx, current.ID, root)
	}
	return current, nil
}

func (r *Runtime) lookupProvider(name string) (provider.Driver, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	driver, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrProviderNotFound, name)
	}
	return driver, nil
}

func (r *Runtime) lookupProfile(name string) (team.Profile, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	profile, ok := r.profiles[name]
	if !ok {
		return team.Profile{}, fmt.Errorf("%w: %s", ErrProfileNotFound, name)
	}
	return profile, nil
}

func (r *Runtime) lookupPattern(name string) (team.Pattern, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	pattern, ok := r.patterns[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrPatternNotFound, name)
	}
	return pattern, nil
}

func (r *Runtime) nextRunID() string {
	return fmt.Sprintf("run-%d", atomic.AddUint64(&r.runSeq, 1))
}

func (r *Runtime) nextTeamID() string {
	return fmt.Sprintf("team-%d", atomic.AddUint64(&r.teamSeq, 1))
}

func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}
