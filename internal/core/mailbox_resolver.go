package core

import (
	"github.com/Viking602/go-hydaelyn/internal/core/mailbox"
	"github.com/Viking602/go-hydaelyn/internal/core/model"
)

var (
	ErrInvalidAddress = model.ErrInvalidAddress
	ErrNoRecipients   = model.ErrNoRecipients
)

type AddressKind = model.AddressKind
type Address = model.Address
type AgentProfile = model.AgentProfile

const (
	AddressKindAgent AddressKind = model.AddressKindAgent
	AddressKindRole  AddressKind = model.AddressKindRole
	AddressKindGroup AddressKind = model.AddressKindGroup
)

// ResolveRecipients expands an Address into the set of concrete agent IDs
// among the supplied profiles. The result is deduplicated and stable in input
// order. Returns ErrNoRecipients when zero agents match a well-formed address.
func ResolveRecipients(agents []AgentProfile, to Address) ([]string, error) {
	return mailbox.ResolveRecipients(agents, to)
}
