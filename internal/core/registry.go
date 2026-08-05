package core

import (
	"maps"
	"slices"

	"github.com/Viking602/venat/internal/core/model"
)

type toolHolderKey struct {
	runID      string
	taskID     string
	holderType model.HolderType
	holderID   string
}

func (r *Runtime) RegisterToolForInvocation(runID, taskID string, holderType model.HolderType, holderID string, tool model.Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if runID == "" || taskID == "" || holderType == "" || holderID == "" || tool.Name == "" {
		return
	}
	if tool.EffectType == "" {
		tool.EffectType = model.ToolEffectReadOnly
	}
	key := toolHolderKey{runID: runID, taskID: taskID, holderType: holderType, holderID: holderID}
	if r.scopedTools[key] == nil {
		r.scopedTools[key] = make(map[string]model.Tool)
	}
	r.scopedTools[key][tool.Name] = cloneTool(tool)
}

func (r *Runtime) RemoveToolsForInvocation(runID, taskID string, holderType model.HolderType, holderID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.scopedTools, toolHolderKey{runID: runID, taskID: taskID, holderType: holderType, holderID: holderID})
}

func (r *Runtime) toolForInvocation(runID, taskID string, holderType model.HolderType, holderID, name string) (model.Tool, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	tools := r.scopedTools[toolHolderKey{runID: runID, taskID: taskID, holderType: holderType, holderID: holderID}]
	tool, ok := tools[name]
	return cloneTool(tool), ok
}

func (r *Runtime) RegisterAgent(profile AgentProfile) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if profile.ID == "" {
		return
	}
	if _, exists := r.agents[profile.ID]; !exists {
		r.agentOrder = append(r.agentOrder, profile.ID)
	}
	r.agents[profile.ID] = cloneAgentProfile(profile)
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

func (r *Runtime) RegisterTool(tool model.Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if tool.Name == "" {
		return
	}
	if tool.EffectType == "" {
		tool.EffectType = model.ToolEffectReadOnly
	}
	r.tools[tool.Name] = cloneTool(tool)
}

func (r *Runtime) tool(name string) (model.Tool, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	tool, ok := r.tools[name]
	return cloneTool(tool), ok
}

func cloneTool(tool model.Tool) model.Tool {
	clone := tool
	clone.PolicyTags = slices.Clone(tool.PolicyTags)
	clone.Metadata = maps.Clone(tool.Metadata)
	return clone
}

func (r *Runtime) SetMessagePolicy(policy model.MessagePolicyChecker) {
	r.configMu.Lock()
	defer r.configMu.Unlock()
	if policy == nil {
		r.policy = allowPolicyEngine{}
		return
	}
	r.policy = messagePolicyAdapter{check: policy}
}

func (r *Runtime) SetPolicyEngine(policy PolicyEngine) {
	r.configMu.Lock()
	defer r.configMu.Unlock()
	if policy == nil {
		r.policy = allowPolicyEngine{}
		return
	}
	r.policy = policy
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

func (r *Runtime) currentPolicyEngine() PolicyEngine {
	r.configMu.RLock()
	defer r.configMu.RUnlock()
	if r.policy == nil {
		return allowPolicyEngine{}
	}
	return r.policy
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
