package team

import "testing"

func TestRunStateRunnableTasksSupportsCommonDAGShapes(t *testing.T) {
	tests := []struct {
		name  string
		tasks []Task
		want  []string
	}{
		{
			name: "linear",
			tasks: []Task{
				{ID: "task-1", AssigneeAgentID: "worker-1", Status: TaskStatusPending},
				{ID: "task-2", AssigneeAgentID: "worker-1", DependsOn: []string{"task-1"}, Status: TaskStatusPending},
			},
			want: []string{"task-1"},
		},
		{
			name: "parallel",
			tasks: []Task{
				{ID: "task-1", AssigneeAgentID: "worker-1", Status: TaskStatusPending},
				{ID: "task-2", AssigneeAgentID: "worker-2", Status: TaskStatusPending},
				{ID: "task-3", AssigneeAgentID: "worker-1", DependsOn: []string{"task-1", "task-2"}, Status: TaskStatusPending},
			},
			want: []string{"task-1", "task-2"},
		},
		{
			name: "diamond",
			tasks: []Task{
				{ID: "root", AssigneeAgentID: "worker-1", Status: TaskStatusCompleted},
				{ID: "left", AssigneeAgentID: "worker-1", DependsOn: []string{"root"}, Status: TaskStatusPending},
				{ID: "right", AssigneeAgentID: "worker-2", DependsOn: []string{"root"}, Status: TaskStatusPending},
				{ID: "leaf", AssigneeAgentID: "worker-1", DependsOn: []string{"left", "right"}, Status: TaskStatusPending},
			},
			want: []string{"left", "right"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := RunState{
				Supervisor: AgentInstance{ID: "supervisor", Role: RoleSupervisor, ProfileName: "supervisor"},
				Workers: []AgentInstance{
					{ID: "worker-1", Role: RoleResearcher, ProfileName: "research"},
					{ID: "worker-2", Role: RoleResearcher, ProfileName: "research"},
				},
				Tasks: tt.tasks,
			}
			runnable := state.RunnableTasks()
			if len(runnable) != len(tt.want) {
				t.Fatalf("expected %d runnable tasks, got %#v", len(tt.want), runnable)
			}
			for idx, task := range runnable {
				if task.ID != tt.want[idx] {
					t.Fatalf("expected runnable task %q at %d, got %q", tt.want[idx], idx, task.ID)
				}
			}
		})
	}
}

func TestRunStateValidateRejectsInvalidDAG(t *testing.T) {
	base := RunState{
		Supervisor: AgentInstance{ID: "supervisor", Role: RoleSupervisor, ProfileName: "supervisor"},
		Workers: []AgentInstance{
			{ID: "worker-1", Role: RoleResearcher, ProfileName: "research"},
		},
	}

	t.Run("duplicate task id", func(t *testing.T) {
		state := base
		state.Tasks = []Task{
			{ID: "task-1", AssigneeAgentID: "worker-1", Status: TaskStatusPending},
			{ID: "task-1", AssigneeAgentID: "worker-1", Status: TaskStatusPending},
		}
		if err := state.Validate(); err == nil {
			t.Fatalf("expected duplicate task id validation failure")
		}
	})

	t.Run("missing dependency", func(t *testing.T) {
		state := base
		state.Tasks = []Task{
			{ID: "task-1", AssigneeAgentID: "worker-1", DependsOn: []string{"missing"}, Status: TaskStatusPending},
		}
		if err := state.Validate(); err == nil {
			t.Fatalf("expected missing dependency validation failure")
		}
	})

	t.Run("cycle", func(t *testing.T) {
		state := base
		state.Tasks = []Task{
			{ID: "task-1", AssigneeAgentID: "worker-1", DependsOn: []string{"task-2"}, Status: TaskStatusPending},
			{ID: "task-2", AssigneeAgentID: "worker-1", DependsOn: []string{"task-1"}, Status: TaskStatusPending},
		}
		if err := state.Validate(); err == nil {
			t.Fatalf("expected cycle validation failure")
		}
	})
}
