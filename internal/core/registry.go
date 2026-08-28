package core

import (
	"fmt"
	"maps"
	"slices"

	"github.com/Viking602/venat/api"
)

type toolHolderKey struct {
	runID      string
	taskID     string
	holderType api.HolderType
	holderID   string
}

func (r *Runtime) RegisterToolForInvocation(runID, taskID string, holderType api.HolderType, holderID string, tool api.Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if runID == "" || taskID == "" || holderType == "" || holderID == "" || tool.Name == "" {
		return fmt.Errorf("%w: scoped tool registration requires run, task, holder, and tool name", api.ErrInvalidCommand)
	}
	if tool.EffectType == "" {
		tool.EffectType = api.ToolEffectReadOnly
	}
	key := toolHolderKey{runID: runID, taskID: taskID, holderType: holderType, holderID: holderID}
	if r.scopedTools[key] == nil {
		r.scopedTools[key] = make(map[string]api.Tool)
	}
	r.scopedTools[key][tool.Name] = cloneTool(tool)
	return nil
}

func (r *Runtime) RemoveToolsForInvocation(runID, taskID string, holderType api.HolderType, holderID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.scopedTools, toolHolderKey{runID: runID, taskID: taskID, holderType: holderType, holderID: holderID})
}

func (r *Runtime) toolForInvocation(runID, taskID string, holderType api.HolderType, holderID, name string) (api.Tool, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	tools := r.scopedTools[toolHolderKey{runID: runID, taskID: taskID, holderType: holderType, holderID: holderID}]
	tool, ok := tools[name]
	return cloneTool(tool), ok
}

func (r *Runtime) RegisterAgent(profile AgentProfile) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := profile.Validate(); err != nil {
		return err
	}
	if _, exists := r.agents[profile.ID]; !exists {
		r.agentOrder = append(r.agentOrder, profile.ID)
	}
	r.agents[profile.ID] = cloneAgentProfile(profile)
	return nil
}

func (r *Runtime) Agents() []AgentProfile {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]AgentProfile, 0, len(r.agentOrder))
	for _, id := range r.agentOrder {
		if profile, ok := r.agents[id]; ok {
			out = append(out, cloneAgentProfile(profile))
		}
	}
	return out
}

func cloneAgentProfile(profile AgentProfile) AgentProfile {
	clone := profile
	clone.Groups = slices.Clone(profile.Groups)
	clone.Metadata = maps.Clone(profile.Metadata)
	return clone
}

func (r *Runtime) RegisterTool(tool api.Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if tool.Name == "" {
		return fmt.Errorf("%w: tool name is required", api.ErrInvalidCommand)
	}
	if tool.EffectType == "" {
		tool.EffectType = api.ToolEffectReadOnly
	}
	r.tools[tool.Name] = cloneTool(tool)
	return nil
}

func (r *Runtime) tool(name string) (api.Tool, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	tool, ok := r.tools[name]
	return cloneTool(tool), ok
}

func cloneTool(tool api.Tool) api.Tool {
	clone := tool
	clone.PolicyTags = slices.Clone(tool.PolicyTags)
	clone.Metadata = maps.Clone(tool.Metadata)
	return clone
}

// SetMessagePolicy installs the message-scoped checker. It is independent of
// SetPolicyEngine: a message request must satisfy both, and the stricter
// decision wins. Passing nil clears only the message checker and leaves any
// installed policy engine in force.
func (r *Runtime) SetMessagePolicy(policy api.MessagePolicyChecker) {
	r.configMu.Lock()
	defer r.configMu.Unlock()
	r.messagePolicy = policy
}

// SetPolicyEngine installs the engine that authorizes every operation, message
// requests included. Passing nil clears only the engine and leaves any checker
// installed by SetMessagePolicy in force.
func (r *Runtime) SetPolicyEngine(policy PolicyEngine) {
	r.configMu.Lock()
	defer r.configMu.Unlock()
	r.policyEngine = policy
}

func (r *Runtime) SetOutputGateway(gateway OutputGateway) {
	r.configMu.Lock()
	defer r.configMu.Unlock()
	if gateway == nil {
		r.outputGateway = memoryOutputGateway{}
		return
	}
	r.outputGateway = gateway
}

func (r *Runtime) SetPipeline(components PipelineComponents) {
	r.configMu.Lock()
	defer r.configMu.Unlock()
	r.pipeline = defaultPipeline(components)
}

// currentPolicyEngine composes the configured engine and message checker into
// the single engine the authorization path consults. Both are optional; with
// neither configured every request is allowed, as before.
func (r *Runtime) currentPolicyEngine() PolicyEngine {
	r.configMu.RLock()
	defer r.configMu.RUnlock()
	switch {
	case r.policyEngine == nil && r.messagePolicy == nil:
		return allowPolicyEngine{}
	case r.messagePolicy == nil:
		return r.policyEngine
	case r.policyEngine == nil:
		return messagePolicyAdapter{check: r.messagePolicy}
	default:
		return combinedPolicyEngine{engine: r.policyEngine, message: r.messagePolicy}
	}
}

func (r *Runtime) currentTaskMonitor() TaskMonitor {
	r.configMu.RLock()
	defer r.configMu.RUnlock()
	if r.pipeline.TaskMonitor == nil {
		return defaultPipeline(PipelineComponents{}).TaskMonitor
	}
	return r.pipeline.TaskMonitor
}

func (r *Runtime) currentPipeline() PipelineComponents {
	r.configMu.RLock()
	defer r.configMu.RUnlock()
	return defaultPipeline(r.pipeline)
}
