package mailbox

import (
	"errors"
	"testing"

	"github.com/Viking602/go-hydaelyn/team"
)

func fakeState() team.RunState {
	return team.RunState{
		ID: "run-1",
		Supervisor: team.AgentInstance{
			ID:          "sup-1",
			Role:        team.RoleSupervisor,
			ProfileName: "sup",
		},
		Workers: []team.AgentInstance{
			{ID: "res-1", Role: team.RoleResearcher, ProfileName: "res", Metadata: map[string]string{"group": "alpha"}},
			{ID: "res-2", Role: team.RoleResearcher, ProfileName: "res", Metadata: map[string]string{"group": "beta"}},
			{ID: "ver-1", Role: team.RoleVerifier, ProfileName: "ver", Metadata: map[string]string{"group": "alpha"}},
		},
	}
}

func TestResolveRecipients_Agent(t *testing.T) {
	ids, err := ResolveRecipients(fakeState(), Address{Kind: AddressKindAgent, TeamRunID: "run-1", AgentID: "res-2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 1 || ids[0] != "res-2" {
		t.Fatalf("want [res-2]; got %v", ids)
	}
}

func TestResolveRecipients_AgentMissing(t *testing.T) {
	_, err := ResolveRecipients(fakeState(), Address{Kind: AddressKindAgent, TeamRunID: "run-1", AgentID: "nope"})
	if !errors.Is(err, ErrNoRecipients) {
		t.Fatalf("want ErrNoRecipients, got %v", err)
	}
}

func TestResolveRecipients_Role(t *testing.T) {
	ids, err := ResolveRecipients(fakeState(), Address{Kind: AddressKindRole, TeamRunID: "run-1", Role: team.RoleResearcher})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("want 2 researchers, got %d: %v", len(ids), ids)
	}
}

func TestResolveRecipients_RoleEmpty(t *testing.T) {
	_, err := ResolveRecipients(fakeState(), Address{Kind: AddressKindRole, TeamRunID: "run-1", Role: team.RoleSynthesizer})
	if !errors.Is(err, ErrNoRecipients) {
		t.Fatalf("want ErrNoRecipients, got %v", err)
	}
}

func TestResolveRecipients_Group(t *testing.T) {
	ids, err := ResolveRecipients(fakeState(), Address{Kind: AddressKindGroup, TeamRunID: "run-1", Group: "alpha"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("want 2 alpha members, got %d: %v", len(ids), ids)
	}
}

func TestResolveRecipients_TeamMismatch(t *testing.T) {
	_, err := ResolveRecipients(fakeState(), Address{Kind: AddressKindAgent, TeamRunID: "other", AgentID: "res-1"})
	if !errors.Is(err, ErrInvalidAddress) {
		t.Fatalf("want ErrInvalidAddress, got %v", err)
	}
}

func TestResolveRecipients_InvalidKind(t *testing.T) {
	_, err := ResolveRecipients(fakeState(), Address{Kind: "bogus", TeamRunID: "run-1"})
	if !errors.Is(err, ErrInvalidAddress) {
		t.Fatalf("want ErrInvalidAddress, got %v", err)
	}
}

func TestValidateSenderAddress(t *testing.T) {
	if err := validateSenderAddress(Address{Kind: AddressKindRole, TeamRunID: "run", Role: team.RoleSupervisor}); !errors.Is(err, ErrInvalidAddress) {
		t.Fatalf("role sender must be rejected, got %v", err)
	}
	if err := validateSenderAddress(Address{Kind: AddressKindAgent, AgentID: "a"}); !errors.Is(err, ErrInvalidAddress) {
		t.Fatalf("missing TeamRunID must be rejected, got %v", err)
	}
	if err := validateSenderAddress(Address{Kind: AddressKindAgent, AgentID: "a", TeamRunID: "r"}); err != nil {
		t.Fatalf("valid sender rejected: %v", err)
	}
}
