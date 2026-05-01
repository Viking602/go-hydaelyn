package model

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

// ValidateAddress returns an error if the address is malformed.
func ValidateAddress(a Address) error {
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
