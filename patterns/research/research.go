package research

import (
	"context"
	"fmt"
	"time"

	"github.com/Viking602/go-hydaelyn/program"
	"github.com/Viking602/go-hydaelyn/internal/workflow"
)

type Decision string

const (
	DecisionKeep     Decision = "keep"
	DecisionDiscard  Decision = "discard"
	DecisionAdvance  Decision = "advance"
	DecisionComplete Decision = "complete"
)

type Iteration struct {
	Index    int      `json:"index"`
	Topic    string   `json:"topic,omitempty"`
	Decision Decision `json:"decision"`
	Notes    string   `json:"notes,omitempty"`
}

type State struct {
	Objective  string      `json:"objective"`
	Budget     int         `json:"budget"`
	Used       int         `json:"used"`
	Iterations []Iteration `json:"iterations,omitempty"`
	Program    string      `json:"program,omitempty"`
}

type Driver struct {
	ProgramLoader program.Loader
}

func (Driver) Name() string {
	return "research"
}

func (d Driver) Start(ctx context.Context, input map[string]any) (workflow.State, error) {
	objective, _ := input["objective"].(string)
	budget, _ := input["budget"].(int)
	if budget == 0 {
		if cast, ok := input["budget"].(float64); ok {
			budget = int(cast)
		}
	}
	programName, _ := input["program"].(string)
	state := State{
		Objective: objective,
		Budget:    budget,
		Program:   programName,
	}
	if d.ProgramLoader != nil && programName != "" {
		document, err := d.ProgramLoader.Load(ctx, programName)
		if err != nil {
			return workflow.State{}, err
		}
		state.Program = document.Body
	}
	return encode("research-1", d.Name(), workflow.StatusRunning, "plan", state), nil
}

func (Driver) Resume(_ context.Context, current workflow.State) (workflow.State, error) {
	state := decode(current)
	if state.Used >= state.Budget && state.Budget > 0 {
		current.Status = workflow.StatusCompleted
		current.Step = "complete"
		return current, nil
	}
	state.Used++
	state.Iterations = append(state.Iterations, Iteration{
		Index:    state.Used,
		Decision: DecisionAdvance,
		Notes:    fmt.Sprintf("completed research iteration %d", state.Used),
	})
	next := encode(current.ID, current.Name, workflow.StatusRunning, "evaluate", state)
	if state.Used >= state.Budget && state.Budget > 0 {
		next.Status = workflow.StatusCompleted
		next.Step = "complete"
	}
	return next, nil
}

func (Driver) Abort(_ context.Context, current workflow.State) (workflow.State, error) {
	current.Status = workflow.StatusAborted
	current.Step = "aborted"
	current.UpdatedAt = time.Now().UTC()
	return current, nil
}

func decode(current workflow.State) State {
	state := State{}
	if objective, ok := current.Data["objective"].(string); ok {
		state.Objective = objective
	}
	if programText, ok := current.Data["program"].(string); ok {
		state.Program = programText
	}
	if budget, ok := current.Data["budget"].(int); ok {
		state.Budget = budget
	}
	if used, ok := current.Data["used"].(int); ok {
		state.Used = used
	}
	if budget, ok := current.Data["budget"].(float64); ok {
		state.Budget = int(budget)
	}
	if used, ok := current.Data["used"].(float64); ok {
		state.Used = int(used)
	}
	return state
}

func encode(id, name string, status workflow.Status, step string, state State) workflow.State {
	return workflow.State{
		ID:     id,
		Name:   name,
		Status: status,
		Step:   step,
		Data: map[string]any{
			"objective": state.Objective,
			"budget":    state.Budget,
			"used":      state.Used,
			"program":   state.Program,
		},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
}
