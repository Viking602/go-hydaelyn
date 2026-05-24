package multiagent

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

// AgentInstance is the per-run materialization of an AgentClass. IDs are
// deterministic (see ComputeInstanceID) so reconstruction from the
// event stream produces the same ID — a load-bearing property for the
// runner's resume / replay surfaces.
//
// Spec anchor: docs/product-spec/v0.8.0/04-agent-class.md.
type AgentInstance struct {
	ID        string        `json:"id"`
	ClassName string        `json:"className"`
	RunID     string        `json:"runId"`
	TaskID    string        `json:"taskId,omitempty"`
	State     InstanceState `json:"state"`
	CreatedAt time.Time     `json:"createdAt"`
}

type InstanceState string

const (
	InstanceStatePending  InstanceState = "pending"
	InstanceStateRunning  InstanceState = "running"
	InstanceStateFinished InstanceState = "finished"
	InstanceStateFailed   InstanceState = "failed"
)

// ComputeInstanceID derives a deterministic AgentInstance ID from the
// AgentClass name, RunID, TaskID, and a stable suffix (typically a salt
// drawn from the scheduler decision that minted the instance).
// Reconstructing the same AgentInstance from the event stream produces
// the same ID — this is what lets the runner's three-surface
// reconstruction (boundaries Principle 5) tie events back to instances.
func ComputeInstanceID(className, runID, taskID, suffix string) string {
	parts := []string{
		strings.TrimSpace(className),
		strings.TrimSpace(runID),
		strings.TrimSpace(taskID),
		strings.TrimSpace(suffix),
	}
	h := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return "ai-" + hex.EncodeToString(h[:8])
}
