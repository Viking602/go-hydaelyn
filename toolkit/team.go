package toolkit

import "hydaelyn/team"

type ProfileOption func(*team.Profile)

func WithRole(role team.Role) ProfileOption {
	return func(profile *team.Profile) {
		profile.Role = role
	}
}

func WithModel(providerName, model string) ProfileOption {
	return func(profile *team.Profile) {
		profile.Provider = providerName
		profile.Model = model
	}
}

func WithToolNames(names ...string) ProfileOption {
	return func(profile *team.Profile) {
		profile.ToolNames = append([]string{}, names...)
	}
}

func WithPrompt(prompt string) ProfileOption {
	return func(profile *team.Profile) {
		profile.Prompt = prompt
	}
}

func WithMaxTurns(limit int) ProfileOption {
	return func(profile *team.Profile) {
		profile.MaxTurns = limit
	}
}

func WithMaxConcurrency(limit int) ProfileOption {
	return func(profile *team.Profile) {
		profile.MaxConcurrency = limit
	}
}

func Profile(name string, options ...ProfileOption) team.Profile {
	profile := team.Profile{Name: name}
	for _, option := range options {
		option(&profile)
	}
	return profile
}

type TeamSpec struct {
	request team.StartRequest
}

func Team(pattern, supervisor string, workers ...string) *TeamSpec {
	return &TeamSpec{
		request: team.StartRequest{
			Pattern:           pattern,
			SupervisorProfile: supervisor,
			WorkerProfiles:    append([]string{}, workers...),
		},
	}
}

func (s *TeamSpec) Input(input map[string]any) *TeamSpec {
	s.request.Input = input
	return s
}

func (s *TeamSpec) Metadata(metadata map[string]string) *TeamSpec {
	s.request.Metadata = metadata
	return s
}

func (s *TeamSpec) Build() team.StartRequest {
	return s.request
}

func Pattern(pattern team.Pattern) team.Pattern {
	return pattern
}
