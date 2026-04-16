package team

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Viking602/go-hydaelyn/blackboard"
)

type Role string

const (
	RoleSupervisor  Role = "supervisor"
	RoleResearcher  Role = "researcher"
	RoleVerifier    Role = "verifier"
	RoleSynthesizer Role = "synthesizer"
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusAborted   Status = "aborted"
	StatusPaused    Status = "paused"
)

type Phase string

const (
	PhasePlanning   Phase = "planning"
	PhaseResearch   Phase = "research"
	PhaseVerify     Phase = "verify"
	PhaseSynthesize Phase = "synthesize"
	PhaseComplete   Phase = "complete"
)

type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusAborted   TaskStatus = "aborted"
	TaskStatusSkipped   TaskStatus = "skipped"
)

type TaskKind string

const (
	TaskKindResearch   TaskKind = "research"
	TaskKindVerify     TaskKind = "verify"
	TaskKindSynthesize TaskKind = "synthesize"
)

type FailurePolicy string

const (
	FailurePolicyFailFast     FailurePolicy = "fail_fast"
	FailurePolicyRetry        FailurePolicy = "retry"
	FailurePolicyDegrade      FailurePolicy = "degrade"
	FailurePolicySkipOptional FailurePolicy = "skip_optional"
)

type Budget struct {
	Tokens    int `json:"tokens,omitempty"`
	ToolCalls int `json:"toolCalls,omitempty"`
}

type AgentProfile struct {
	Name           string            `json:"name"`
	Role           Role              `json:"role"`
	Provider       string            `json:"provider"`
	Model          string            `json:"model"`
	DefaultBudget  Budget            `json:"defaultBudget,omitempty"`
	Prompt         string            `json:"prompt,omitempty"`
	Program        string            `json:"program,omitempty"`
	ToolNames      []string          `json:"toolNames,omitempty"`
	MaxTurns       int               `json:"maxTurns,omitempty"`
	MaxConcurrency int               `json:"maxConcurrency,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

type Profile = AgentProfile

type AgentInstance struct {
	ID          string            `json:"id"`
	Role        Role              `json:"role"`
	ProfileName string            `json:"profileName,omitempty"`
	Profile     string            `json:"profile,omitempty"`
	SessionID   string            `json:"sessionId,omitempty"`
	Budget      Budget            `json:"budget,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type Member = AgentInstance

type Evidence struct {
	Source  string `json:"source,omitempty"`
	Snippet string `json:"snippet,omitempty"`
}

type Finding struct {
	Summary    string     `json:"summary"`
	Evidence   []Evidence `json:"evidence,omitempty"`
	Confidence float64    `json:"confidence,omitempty"`
}

type Result struct {
	Summary    string     `json:"summary"`
	Findings   []Finding  `json:"findings,omitempty"`
	Evidence   []Evidence `json:"evidence,omitempty"`
	Confidence float64    `json:"confidence,omitempty"`
	Error      string     `json:"error,omitempty"`
}

type PlanningState struct {
	PlannerName      string   `json:"plannerName,omitempty"`
	Goal             string   `json:"goal,omitempty"`
	SuccessCriteria  []string `json:"successCriteria,omitempty"`
	ReviewCount      int      `json:"reviewCount,omitempty"`
	LastAction       string   `json:"lastAction,omitempty"`
	LastActionReason string   `json:"lastActionReason,omitempty"`
	PlanVersion      int      `json:"planVersion,omitempty"`
}

type Task struct {
	ID                   string        `json:"id"`
	Kind                 TaskKind      `json:"kind"`
	Title                string        `json:"title,omitempty"`
	Input                string        `json:"input,omitempty"`
	RequiredRole         Role          `json:"requiredRole,omitempty"`
	RequiredCapabilities []string      `json:"requiredCapabilities,omitempty"`
	Budget               Budget        `json:"budget,omitempty"`
	AssigneeAgentID      string        `json:"assigneeAgentId,omitempty"`
	Assignee             string        `json:"assignee,omitempty"`
	DependsOn            []string      `json:"dependsOn,omitempty"`
	FailurePolicy        FailurePolicy `json:"failurePolicy,omitempty"`
	MaxAttempts          int           `json:"maxAttempts,omitempty"`
	Attempts             int           `json:"attempts,omitempty"`
	Status               TaskStatus    `json:"status"`
	SessionID            string        `json:"sessionId,omitempty"`
	Result               *Result       `json:"result,omitempty"`
	Error                string        `json:"error,omitempty"`
	StartedAt            time.Time     `json:"startedAt,omitempty"`
	FinishedAt           time.Time     `json:"finishedAt,omitempty"`
}

type RunState struct {
	ID                  string            `json:"id"`
	Pattern             string            `json:"pattern"`
	SessionID           string            `json:"sessionId,omitempty"`
	Status              Status            `json:"status"`
	Phase               Phase             `json:"phase"`
	Supervisor          AgentInstance     `json:"supervisor"`
	Workers             []AgentInstance   `json:"workers,omitempty"`
	Tasks               []Task            `json:"tasks,omitempty"`
	Result              *Result           `json:"result,omitempty"`
	Planning            *PlanningState    `json:"planning,omitempty"`
	Blackboard          *blackboard.State `json:"blackboard,omitempty"`
	Input               map[string]any    `json:"input,omitempty"`
	Metadata            map[string]string `json:"metadata,omitempty"`
	RequireVerification bool              `json:"requireVerification,omitempty"`
	CreatedAt           time.Time         `json:"createdAt"`
	UpdatedAt           time.Time         `json:"updatedAt"`
}

type StartRequest struct {
	TeamID            string            `json:"teamId,omitempty"`
	Pattern           string            `json:"pattern"`
	Planner           string            `json:"planner,omitempty"`
	SupervisorProfile string            `json:"supervisorProfile"`
	WorkerProfiles    []string          `json:"workerProfiles"`
	Input             map[string]any    `json:"input,omitempty"`
	Metadata          map[string]string `json:"metadata,omitempty"`
}

type Pattern interface {
	Name() string
	Start(ctx context.Context, request StartRequest) (RunState, error)
	Advance(ctx context.Context, state RunState) (RunState, error)
}

func (s RunState) IsTerminal() bool {
	return s.Status == StatusCompleted || s.Status == StatusFailed || s.Status == StatusAborted || s.Status == StatusPaused
}

func (s RunState) RunnableTasks() []Task {
	current := s
	current.Normalize()
	runnable := make([]Task, 0, len(s.Tasks))
	for _, task := range current.Tasks {
		if task.Status != TaskStatusPending {
			continue
		}
		depsReady := true
		for _, dep := range task.DependsOn {
			if !current.isTaskCompleted(dep) {
				depsReady = false
				break
			}
		}
		if depsReady {
			runnable = append(runnable, task)
		}
	}
	return runnable
}

func (s RunState) isTaskCompleted(taskID string) bool {
	for _, task := range s.Tasks {
		if task.ID == taskID {
			return task.Status == TaskStatusCompleted
		}
	}
	return false
}

func (p AgentInstance) EffectiveProfileName() string {
	if strings.TrimSpace(p.ProfileName) != "" {
		return p.ProfileName
	}
	return p.Profile
}

func (p *AgentInstance) Normalize() {
	if p.ProfileName == "" {
		p.ProfileName = p.Profile
	}
	if p.Profile == "" {
		p.Profile = p.ProfileName
	}
}

func (t Task) EffectiveAssigneeAgentID() string {
	if strings.TrimSpace(t.AssigneeAgentID) != "" {
		return t.AssigneeAgentID
	}
	return t.Assignee
}

func (t *Task) Normalize() {
	if t.AssigneeAgentID == "" {
		t.AssigneeAgentID = t.Assignee
	}
	if t.Assignee == "" {
		t.Assignee = t.AssigneeAgentID
	}
	if t.FailurePolicy == "" {
		t.FailurePolicy = FailurePolicyFailFast
	}
}

func (t Task) EffectiveMaxAttempts() int {
	if t.MaxAttempts > 0 {
		return t.MaxAttempts
	}
	if t.FailurePolicy == FailurePolicyRetry {
		return 2
	}
	return 1
}

func (b Budget) Covers(required Budget) bool {
	if required.Tokens > 0 && b.Tokens > 0 && b.Tokens < required.Tokens {
		return false
	}
	if required.ToolCalls > 0 && b.ToolCalls > 0 && b.ToolCalls < required.ToolCalls {
		return false
	}
	if required.Tokens > 0 && b.Tokens == 0 {
		return false
	}
	if required.ToolCalls > 0 && b.ToolCalls == 0 {
		return false
	}
	return true
}

func (t Task) CanRetry() bool {
	return t.FailurePolicy == FailurePolicyRetry && t.Attempts < t.EffectiveMaxAttempts()
}

func (t Task) BlocksTeamOnFailure() bool {
	switch t.FailurePolicy {
	case FailurePolicyDegrade, FailurePolicySkipOptional:
		return false
	case FailurePolicyRetry:
		return !t.CanRetry()
	default:
		return true
	}
}

func (s *RunState) Normalize() {
	s.Supervisor.Normalize()
	for idx := range s.Workers {
		s.Workers[idx].Normalize()
	}
	for idx := range s.Tasks {
		s.Tasks[idx].Normalize()
	}
}

func (s RunState) Agent(agentID string) (AgentInstance, bool) {
	current := s
	current.Normalize()
	for _, agent := range current.allAgents() {
		if agent.ID == agentID {
			return agent, true
		}
	}
	return AgentInstance{}, false
}

func (s RunState) Validate() error {
	current := s
	current.Normalize()
	agents, err := current.validateAgents()
	if err != nil {
		return err
	}
	return current.validateTasks(agents)
}

func (s RunState) ResolveBlockedTasks() (RunState, bool) {
	current := s
	current.Normalize()
	statusByTask := map[string]TaskStatus{}
	for _, task := range current.Tasks {
		statusByTask[task.ID] = task.Status
	}
	changed := false
	now := time.Now().UTC()
	for idx, task := range current.Tasks {
		if task.Status != TaskStatusPending {
			continue
		}
		dependencyID, dependencyStatus, blocked := blockedDependency(task, statusByTask)
		if !blocked {
			continue
		}
		task = resolveBlockedTask(task, dependencyID, dependencyStatus, now)
		current.Tasks[idx] = task
		statusByTask[task.ID] = task.Status
		changed = true
	}
	return current, changed
}

func (s RunState) FirstBlockingFailure() *Task {
	current := s
	current.Normalize()
	for _, task := range current.Tasks {
		if task.Status == TaskStatusFailed && task.BlocksTeamOnFailure() {
			failed := task
			return &failed
		}
	}
	return nil
}

func (s RunState) allAgents() []AgentInstance {
	agents := make([]AgentInstance, 0, 1+len(s.Workers))
	agents = append(agents, s.Supervisor)
	agents = append(agents, s.Workers...)
	return agents
}

func (s RunState) validateAgents() (map[string]AgentInstance, error) {
	agents := map[string]AgentInstance{}
	for _, agent := range s.allAgents() {
		if strings.TrimSpace(agent.ID) == "" {
			return nil, fmt.Errorf("agent id is required")
		}
		if strings.TrimSpace(agent.EffectiveProfileName()) == "" {
			return nil, fmt.Errorf("agent %s is missing profile name", agent.ID)
		}
		if _, exists := agents[agent.ID]; exists {
			return nil, fmt.Errorf("duplicate agent id: %s", agent.ID)
		}
		agents[agent.ID] = agent
	}
	return agents, nil
}

func (s RunState) validateTasks(agents map[string]AgentInstance) error {
	taskIndex := map[string]Task{}
	for _, task := range s.Tasks {
		if err := validateTaskAssignment(task, agents); err != nil {
			return err
		}
		if _, exists := taskIndex[task.ID]; exists {
			return fmt.Errorf("duplicate task id: %s", task.ID)
		}
		taskIndex[task.ID] = task
	}
	if err := validateTaskDependencies(taskIndex); err != nil {
		return err
	}
	return validateTaskCycles(taskIndex)
}

func validateTaskAssignment(task Task, agents map[string]AgentInstance) error {
	if strings.TrimSpace(task.ID) == "" {
		return fmt.Errorf("task id is required")
	}
	if strings.TrimSpace(task.EffectiveAssigneeAgentID()) == "" {
		return fmt.Errorf("task %s is missing assignee agent id", task.ID)
	}
	agent, ok := agents[task.EffectiveAssigneeAgentID()]
	if !ok {
		return fmt.Errorf("task %s references unknown agent %s", task.ID, task.EffectiveAssigneeAgentID())
	}
	if task.RequiredRole != "" && agent.Role != task.RequiredRole {
		return fmt.Errorf("task %s requires role %s but agent %s has role %s", task.ID, task.RequiredRole, agent.ID, agent.Role)
	}
	return nil
}

func validateTaskDependencies(tasks map[string]Task) error {
	for _, task := range tasks {
		for _, dep := range task.DependsOn {
			if _, ok := tasks[dep]; !ok {
				return fmt.Errorf("task %s depends on missing task %s", task.ID, dep)
			}
		}
	}
	return nil
}

func validateTaskCycles(tasks map[string]Task) error {
	visiting := map[string]bool{}
	visited := map[string]bool{}
	var visit func(string) error
	visit = func(taskID string) error {
		if visited[taskID] {
			return nil
		}
		if visiting[taskID] {
			return fmt.Errorf("cycle detected at task %s", taskID)
		}
		visiting[taskID] = true
		task := tasks[taskID]
		for _, dep := range task.DependsOn {
			if err := visit(dep); err != nil {
				return err
			}
		}
		visiting[taskID] = false
		visited[taskID] = true
		return nil
	}
	for taskID := range tasks {
		if err := visit(taskID); err != nil {
			return err
		}
	}
	return nil
}

func blockedDependency(task Task, statusByTask map[string]TaskStatus) (string, TaskStatus, bool) {
	for _, dep := range task.DependsOn {
		status := statusByTask[dep]
		if status == TaskStatusPending || status == TaskStatusRunning || status == TaskStatusCompleted {
			continue
		}
		return dep, status, true
	}
	return "", "", false
}

func resolveBlockedTask(task Task, dependencyID string, dependencyStatus TaskStatus, now time.Time) Task {
	task.Error = fmt.Sprintf("dependency %s ended with status %s", dependencyID, dependencyStatus)
	task.FinishedAt = now
	switch task.FailurePolicy {
	case FailurePolicyDegrade, FailurePolicySkipOptional:
		task.Status = TaskStatusSkipped
	default:
		task.Status = TaskStatusFailed
		if task.FailurePolicy == FailurePolicyRetry && task.Attempts == 0 {
			task.Attempts = task.EffectiveMaxAttempts()
		}
	}
	return task
}
