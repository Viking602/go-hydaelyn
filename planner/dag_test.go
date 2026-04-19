package planner

import (
	"slices"
	"strings"
	"testing"

	"github.com/Viking602/go-hydaelyn/team"
)

func TestDAGValidationRejectsCyclicDependencies(t *testing.T) {
	state := dagValidationState([]team.Task{
		{ID: "task-1", AssigneeAgentID: "worker-1", DependsOn: []string{"task-2"}, Status: team.TaskStatusPending},
		{ID: "task-2", AssigneeAgentID: "worker-1", DependsOn: []string{"task-1"}, Status: team.TaskStatusPending},
	})

	err := state.Validate()
	if err == nil || !strings.Contains(err.Error(), "cycle detected") {
		t.Fatalf("Validate() error = %v, want cycle detection", err)
	}
}

func TestDAGValidationRejectsMissingDependencies(t *testing.T) {
	state := dagValidationState([]team.Task{{
		ID:              "task-2",
		AssigneeAgentID: "worker-1",
		DependsOn:       []string{"task-1"},
		Status:          team.TaskStatusPending,
	}})

	err := state.Validate()
	if err == nil || !strings.Contains(err.Error(), "depends on missing task") {
		t.Fatalf("Validate() error = %v, want missing dependency failure", err)
	}
}

func TestDAGValidationMaintainsTopologicalOrdering(t *testing.T) {
	state := dagValidationState([]team.Task{
		{ID: "root", AssigneeAgentID: "worker-1", Status: team.TaskStatusPending},
		{ID: "left", AssigneeAgentID: "worker-1", DependsOn: []string{"root"}, Status: team.TaskStatusPending},
		{ID: "right", AssigneeAgentID: "worker-2", DependsOn: []string{"root"}, Status: team.TaskStatusPending},
		{ID: "leaf", AssigneeAgentID: "worker-1", DependsOn: []string{"left", "right"}, Status: team.TaskStatusPending},
	})

	if err := state.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	var layers [][]string
	for {
		runnable := state.RunnableTasks()
		if len(runnable) == 0 {
			break
		}
		layer := make([]string, 0, len(runnable))
		for _, task := range runnable {
			layer = append(layer, task.ID)
			for idx := range state.Tasks {
				if state.Tasks[idx].ID == task.ID {
					state.Tasks[idx].Status = team.TaskStatusCompleted
				}
			}
		}
		layers = append(layers, layer)
	}

	want := [][]string{{"root"}, {"left", "right"}, {"leaf"}}
	if len(layers) != len(want) {
		t.Fatalf("expected %d topological layers, got %#v", len(want), layers)
	}
	for idx := range want {
		if !slices.Equal(layers[idx], want[idx]) {
			t.Fatalf("layer %d = %#v, want %#v", idx, layers[idx], want[idx])
		}
	}
}

func dagValidationState(tasks []team.Task) team.RunState {
	return team.RunState{
		Supervisor: team.AgentInstance{ID: "supervisor", Role: team.RoleSupervisor, ProfileName: "supervisor"},
		Workers: []team.AgentInstance{
			{ID: "worker-1", Role: team.RoleResearcher, ProfileName: "researcher"},
			{ID: "worker-2", Role: team.RoleResearcher, ProfileName: "researcher"},
		},
		Tasks: tasks,
	}
}
