package workflow

import "testing"

func TestStateCarriesExplicitTeamExecutionMetadata(t *testing.T) {
	state := State{
		ID:     "wf-1",
		Name:   "deepsearch",
		Status: StatusRunning,
		Phase:  "research",
		Tasks: []TaskState{
			{ID: "task-1", Kind: "research", Assignee: "research-a", Status: "completed"},
		},
		ChildRuns: []ChildRunState{
			{TaskID: "task-1", AgentID: "worker-1", SessionID: "session-1", Status: "completed"},
		},
		Retry: RetryPolicy{MaxAttempts: 2},
		Abort: &AbortState{Reason: "manual"},
	}
	if state.Tasks[0].Assignee != "research-a" {
		t.Fatalf("unexpected task metadata: %#v", state.Tasks[0])
	}
	if state.ChildRuns[0].SessionID != "session-1" {
		t.Fatalf("unexpected child run metadata: %#v", state.ChildRuns[0])
	}
}
