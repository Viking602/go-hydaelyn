package core

import (
	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/internal/mailbox"
)

var (
	ErrInvalidAddress = api.ErrInvalidAddress
	ErrNoRecipients   = api.ErrNoRecipients
)

type (
	AddressKind  = api.AddressKind
	Address      = api.Address
	AgentProfile = api.AgentProfile
)

const (
	AddressKindAgent = api.AddressKindAgent
	AddressKindRole  = api.AddressKindRole
	AddressKindGroup = api.AddressKindGroup
)

// ResolveRecipients expands an Address into the set of concrete agent IDs
// among the supplied profiles. The result is deduplicated and stable in input
// order. Returns ErrNoRecipients when zero agents match a well-formed address.
func ResolveRecipients(agents []AgentProfile, to Address) ([]string, error) {
	return mailbox.ResolveRecipients(agents, to)
}
