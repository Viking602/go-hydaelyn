package run

import (
	"encoding/json"

	"github.com/Viking602/go-hydaelyn/internal/core/model"
)

type StartRunCommand struct {
	RunID      string
	RootTaskID string
	Request    string
	Metadata   map[string]string
}

type CreateTaskCommand struct {
	RunID              string
	TaskID             string
	ParentTaskID       string
	Type               model.TaskType
	Goal               string
	AssignedAgentID    string
	OwnerAgentID       string
	OwnerComponent     string
	AllowsAction       bool
	Tags               []string
	CompletionCriteria []string
	DependsOn          []string
	AwaitMode          model.AwaitMode
	AwaitQuorum        int
	OnDependencyFailed model.OnDependencyFailed
	ReadSelectors      []model.BlackboardSelector
	WriteTargets       []string
	RetryPolicy        model.RetryPolicy
	PolicyDecisions    []model.PolicyDecision
	InputSchema        json.RawMessage
	OutputSchema       json.RawMessage
}

func (StartRunCommand) CommandName() string   { return "run.start" }
func (CreateTaskCommand) CommandName() string { return "task.create" }

type AdvanceRunCommand struct {
	RunID string
}

func (AdvanceRunCommand) CommandName() string { return "run.advance" }
