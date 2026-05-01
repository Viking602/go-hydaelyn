package model

type Flow struct {
	Name                     string `json:"name"`
	PlannerPreset            string `json:"plannerPreset,omitempty"`
	RouterPreset             string `json:"routerPreset,omitempty"`
	PolicyPreset             string `json:"policyPreset,omitempty"`
	ProjectorPreset          string `json:"projectorPreset,omitempty"`
	BypassTaskStore          bool   `json:"bypassTaskStore,omitempty"`
	BypassPolicyEngine       bool   `json:"bypassPolicyEngine,omitempty"`
	BypassTaskExecutionLease bool   `json:"bypassTaskExecutionLease,omitempty"`
	BypassHandoff            bool   `json:"bypassHandoff,omitempty"`
	BypassResponseLayer      bool   `json:"bypassResponseLayer,omitempty"`
	BypassOutputGateway      bool   `json:"bypassOutputGateway,omitempty"`
}
