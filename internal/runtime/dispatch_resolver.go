package runtime

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrInvalidAddress = errors.New("orchestrator: invalid address")
	ErrNoRecipients   = errors.New("orchestrator: address resolved to zero recipients")
)

type AddressKind string

const (
	AddressKindAgent AddressKind = "agent"
	AddressKindRole  AddressKind = "role"
	AddressKindGroup AddressKind = "group"
)

// Address selects one or more agents for fan-out dispatch. Exactly one of
// AgentID, Role, or Group must be set, matching Kind.
type Address struct {
	Kind    AddressKind `json:"kind"`
	AgentID string      `json:"agentId,omitempty"`
	Role    string      `json:"role,omitempty"`
	Group   string      `json:"group,omitempty"`
}

// AgentProfile is the framework-level identity of an agent participating in
// runs. Role and Groups are opaque developer-defined labels used solely for
// fan-out routing; the runtime ascribes no semantics to their values.
type AgentProfile struct {
	ID       string            `json:"id"`
	Role     string            `json:"role,omitempty"`
	Groups   []string          `json:"groups,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// ResolveRecipients expands an Address into the set of concrete agent IDs
// among the supplied profiles. The result is deduplicated and stable in input
// order. Returns ErrNoRecipients when zero agents match a well-formed address.
func ResolveRecipients(agents []AgentProfile, to Address) ([]string, error) {
	if err := validateAddress(to); err != nil {
		return nil, err
	}
	switch to.Kind {
	case AddressKindAgent:
		for _, a := range agents {
			if a.ID == to.AgentID {
				return []string{a.ID}, nil
			}
		}
		return nil, fmt.Errorf("%w: agent %q not found", ErrNoRecipients, to.AgentID)
	case AddressKindRole:
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
			return nil, fmt.Errorf("%w: no agents with role %q", ErrNoRecipients, to.Role)
		}
		return out, nil
	case AddressKindGroup:
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
			return nil, fmt.Errorf("%w: no agents in group %q", ErrNoRecipients, to.Group)
		}
		return out, nil
	}
	return nil, fmt.Errorf("%w: unknown kind %q", ErrInvalidAddress, to.Kind)
}

func validateAddress(a Address) error {
	switch a.Kind {
	case AddressKindAgent:
		if strings.TrimSpace(a.AgentID) == "" {
			return fmt.Errorf("%w: agent address requires AgentID", ErrInvalidAddress)
		}
	case AddressKindRole:
		if strings.TrimSpace(a.Role) == "" {
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
