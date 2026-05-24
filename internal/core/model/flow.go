package model

type Flow struct {
	Name            string `json:"name"`
	PlannerPreset   string `json:"plannerPreset,omitempty"`
	RouterPreset    string `json:"routerPreset,omitempty"`
	PolicyPreset    string `json:"policyPreset,omitempty"`
	ProjectorPreset string `json:"projectorPreset,omitempty"`
}
