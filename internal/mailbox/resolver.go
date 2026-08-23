package mailbox

import (
	"fmt"

	"github.com/Viking602/venat/api"
)

// ResolveRecipients expands an Address into the set of concrete agent IDs
// among the supplied profiles. The result is deduplicated and stable in input
// order. Returns ErrNoRecipients when zero agents match a well-formed address.
func ResolveRecipients(agents []api.AgentProfile, to api.Address) ([]string, error) {
	if err := api.ValidateAddress(to); err != nil {
		return nil, err
	}
	switch to.Kind {
	case api.AddressKindAgent:
		for _, a := range agents {
			if a.ID == to.AgentID {
				return []string{a.ID}, nil
			}
		}
		return nil, fmt.Errorf("%w: agent %q not found", api.ErrNoRecipients, to.AgentID)
	case api.AddressKindRole:
		out, seen := make([]string, 0, len(agents)), map[string]struct{}{}
		for _, a := range agents {
			if a.Role != to.Role {
				continue
			}
			if _, dup := seen[a.ID]; dup {
				continue
			}
			seen[a.ID] = struct{}{}
			out = append(out, a.ID)
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("%w: no agents with role %q", api.ErrNoRecipients, to.Role)
		}
		return out, nil
	case api.AddressKindGroup:
		out, seen := make([]string, 0, len(agents)), map[string]struct{}{}
		for _, a := range agents {
			for _, g := range a.Groups {
				if g != to.Group {
					continue
				}
				if _, dup := seen[a.ID]; dup {
					continue
				}
				seen[a.ID] = struct{}{}
				out = append(out, a.ID)
			}
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("%w: no agents in group %q", api.ErrNoRecipients, to.Group)
		}
		return out, nil
	}
	return nil, fmt.Errorf("%w: unknown kind %q", api.ErrInvalidAddress, to.Kind)
}
