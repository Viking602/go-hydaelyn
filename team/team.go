package team

import (
	"context"
	"time"
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
)

type TaskKind string

const (
	TaskKindResearch   TaskKind = "research"
	TaskKindVerify     TaskKind = "verify"
	TaskKindSynthesize TaskKind = "synthesize"
)

type Profile struct {
	Name           string            `json:"name"`
	Role           Role              `json:"role"`
	Provider       string            `json:"provider"`
	Model          string            `json:"model"`
	Prompt         string            `json:"prompt,omitempty"`
	Program        string            `json:"program,omitempty"`
	ToolNames      []string          `json:"toolNames,omitempty"`
	MaxTurns       int               `json:"maxTurns,omitempty"`
	MaxConcurrency int               `json:"maxConcurrency,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

type Member struct {
	ID      string `json:"id"`
	Role    Role   `json:"role"`
	Profile string `json:"profile"`
}

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

type Task struct {
	ID         string     `json:"id"`
	Kind       TaskKind   `json:"kind"`
	Title      string     `json:"title,omitempty"`
	Input      string     `json:"input,omitempty"`
	Assignee   string     `json:"assignee"`
	DependsOn  []string   `json:"dependsOn,omitempty"`
	Status     TaskStatus `json:"status"`
	SessionID  string     `json:"sessionId,omitempty"`
	Result     *Result    `json:"result,omitempty"`
	Error      string     `json:"error,omitempty"`
	StartedAt  time.Time  `json:"startedAt,omitempty"`
	FinishedAt time.Time  `json:"finishedAt,omitempty"`
}

type RunState struct {
	ID                  string            `json:"id"`
	Pattern             string            `json:"pattern"`
	SessionID           string            `json:"sessionId,omitempty"`
	Status              Status            `json:"status"`
	Phase               Phase             `json:"phase"`
	Supervisor          Member            `json:"supervisor"`
	Workers             []Member          `json:"workers,omitempty"`
	Tasks               []Task            `json:"tasks,omitempty"`
	Result              *Result           `json:"result,omitempty"`
	Input               map[string]any    `json:"input,omitempty"`
	Metadata            map[string]string `json:"metadata,omitempty"`
	RequireVerification bool              `json:"requireVerification,omitempty"`
	CreatedAt           time.Time         `json:"createdAt"`
	UpdatedAt           time.Time         `json:"updatedAt"`
}

type StartRequest struct {
	TeamID            string            `json:"teamId,omitempty"`
	Pattern           string            `json:"pattern"`
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
	return s.Status == StatusCompleted || s.Status == StatusFailed || s.Status == StatusAborted
}

func (s RunState) RunnableTasks() []Task {
	runnable := make([]Task, 0, len(s.Tasks))
	for _, task := range s.Tasks {
		if task.Status != TaskStatusPending {
			continue
		}
		depsReady := true
		for _, dep := range task.DependsOn {
			if !s.isTaskCompleted(dep) {
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
