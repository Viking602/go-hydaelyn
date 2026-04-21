package mailbox

import (
	"fmt"
	"strings"

	"github.com/Viking602/go-hydaelyn/team"
)

// ResolveRecipients expands an Address into concrete agent IDs within a
// team run. The returned slice is deduplicated and stable in declaration order
// (supervisor first, then workers). An empty result is returned with
// ErrNoRecipients so callers can treat it as a semantic error.
func ResolveRecipients(state team.RunState, to Address) ([]string, error) {
	if err := validateTargetAddress(to); err != nil {
		return nil, err
	}
	if strings.TrimSpace(to.TeamRunID) != "" && strings.TrimSpace(state.ID) != "" && to.TeamRunID != state.ID {
		return nil, fmt.Errorf("%w: address team-run %q does not match state %q", ErrInvalidAddress, to.TeamRunID, state.ID)
	}

	snapshot := state
	snapshot.Normalize()
	agents := allAgents(snapshot)

	switch to.Kind {
	case AddressKindAgent:
		for _, a := range agents {
			if a.ID == to.AgentID {
				return []string{a.ID}, nil
			}
		}
		return nil, fmt.Errorf("%w: agent %q not found", ErrNoRecipients, to.AgentID)

	case AddressKindRole:
		out := make([]string, 0, len(agents))
		seen := map[string]struct{}{}
		for _, a := range agents {
			if a.Role != to.Role {
				continue
			}
			if _, ok := seen[a.ID]; ok {
				continue
			}
			seen[a.ID] = struct{}{}
			out = append(out, a.ID)
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("%w: no agents with role %q", ErrNoRecipients, to.Role)
		}
		return out, nil

	case AddressKindGroup:
		out := make([]string, 0, len(agents))
		seen := map[string]struct{}{}
		for _, a := range agents {
			if a.Metadata == nil {
				continue
			}
			if a.Metadata["group"] != to.Group {
				continue
			}
			if _, ok := seen[a.ID]; ok {
				continue
			}
			seen[a.ID] = struct{}{}
			out = append(out, a.ID)
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("%w: no agents in group %q", ErrNoRecipients, to.Group)
		}
		return out, nil
	}

	return nil, fmt.Errorf("%w: unknown kind %q", ErrInvalidAddress, to.Kind)
}

// validateTargetAddress rejects Address values whose discriminator fields do
// not match the declared Kind. Sender-side validation so storage never sees
// ambiguous rows.
func validateTargetAddress(a Address) error {
	switch a.Kind {
	case AddressKindAgent:
		if strings.TrimSpace(a.AgentID) == "" {
			return fmt.Errorf("%w: agent address requires AgentID", ErrInvalidAddress)
		}
	case AddressKindRole:
		if strings.TrimSpace(string(a.Role)) == "" {
			return fmt.Errorf("%w: role address requires Role", ErrInvalidAddress)
		}
	case AddressKindGroup:
		if strings.TrimSpace(a.Group) == "" {
			return fmt.Errorf("%w: group address requires Group", ErrInvalidAddress)
		}
	default:
		return fmt.Errorf("%w: unknown kind %q", ErrInvalidAddress, a.Kind)
	}
	return nil
}

// validateSenderAddress ensures senders are concrete agents with a team-run.
func validateSenderAddress(a Address) error {
	if a.Kind != AddressKindAgent {
		return fmt.Errorf("%w: sender must be kind=agent", ErrInvalidAddress)
	}
	if strings.TrimSpace(a.AgentID) == "" {
		return fmt.Errorf("%w: sender requires AgentID", ErrInvalidAddress)
	}
	if strings.TrimSpace(a.TeamRunID) == "" {
		return fmt.Errorf("%w: sender requires TeamRunID", ErrInvalidAddress)
	}
	return nil
}

func allAgents(state team.RunState) []team.AgentInstance {
	out := make([]team.AgentInstance, 0, 1+len(state.Workers))
	if strings.TrimSpace(state.Supervisor.ID) != "" {
		out = append(out, state.Supervisor)
	}
	out = append(out, state.Workers...)
	return out
}
