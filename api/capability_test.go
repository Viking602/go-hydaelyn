package api

import "testing"

func TestDefaultConfigIsEmpty(t *testing.T) {
	if DefaultConfig().StoreProvider != nil || DefaultConfig().PolicyEngine != nil {
		t.Fatal("DefaultConfig should be empty")
	}
}

func TestDefaultStoreCapabilitiesFailClosed(t *testing.T) {
	if got := DefaultStoreCapabilities(); got != (StoreCapabilities{}) {
		t.Fatalf("DefaultStoreCapabilities() = %+v, want all capabilities disabled", got)
	}
}

func TestDefinitionSnapshotsAreOptionalUnitOfWorkExtension(t *testing.T) {
	var required UnitOfWork = coreOnlyUnitOfWork{}
	if _, ok := required.(AgentDefinitionUnitOfWork); ok {
		t.Fatal("UnitOfWork unexpectedly requires AgentDefinitionUnitOfWork")
	}
}

type coreOnlyUnitOfWork struct{ UnitOfWork }

func TestToolCapabilityProjectionCopiesMutableMetadata(t *testing.T) {
	tool := Tool{
		Name:               "write_report",
		EffectType:         ToolEffectWrite,
		RequiresActionTask: true,
		RiskLevel:          "medium",
		Idempotent:         true,
		PolicyTags:         []string{"report", "write"},
		Metadata:           map[string]string{"owner": "runtime"},
	}

	capability := tool.AsCapability("agent-1")
	tool.PolicyTags[0] = "mutated"
	tool.Metadata["owner"] = "mutated"

	if capability.Name != tool.Name || capability.AgentID != "agent-1" || capability.EffectType != ToolEffectWrite {
		t.Fatalf("AsCapability() identity/effect = %#v", capability)
	}
	if !capability.RequiresApproval || !capability.RequiresLease || !capability.RequiresPolicy {
		t.Fatalf("AsCapability() requirement flags = %#v", capability)
	}
	if !capability.Idempotent || capability.RiskLevel != "medium" {
		t.Fatalf("AsCapability() policy fields = %#v", capability)
	}
	if capability.Tags[0] != "report" || capability.Metadata["owner"] != "runtime" {
		t.Fatalf("AsCapability() did not copy mutable fields: %#v", capability)
	}
}

func TestCapabilityToolProjectionCopiesMutableMetadata(t *testing.T) {
	capability := Capability{
		Name:             "fetch_context",
		EffectType:       ToolEffectReadOnly,
		RequiresApproval: true,
		RiskLevel:        "low",
		Idempotent:       true,
		Tags:             []string{"context", "read"},
		Metadata:         map[string]string{"source": "host"},
	}

	tool := capability.AsTool()
	capability.Tags[0] = "mutated"
	capability.Metadata["source"] = "mutated"

	if tool.Name != capability.Name || tool.EffectType != ToolEffectReadOnly || !tool.RequiresActionTask {
		t.Fatalf("AsTool() identity/effect = %#v", tool)
	}
	if !tool.Idempotent || tool.RiskLevel != "low" {
		t.Fatalf("AsTool() policy fields = %#v", tool)
	}
	if tool.PolicyTags[0] != "context" || tool.Metadata["source"] != "host" {
		t.Fatalf("AsTool() did not copy mutable fields: %#v", tool)
	}
}

func TestCapabilityAsToolORsRequirementFlags(t *testing.T) {
	tests := []struct {
		name       string
		capability Capability
		want       bool
	}{
		{name: "no requirement", capability: Capability{Name: "read"}, want: false},
		{name: "approval only", capability: Capability{Name: "read", RequiresApproval: true}, want: true},
		{name: "lease only", capability: Capability{Name: "read", RequiresLease: true}, want: true},
		{name: "policy only", capability: Capability{Name: "read", RequiresPolicy: true}, want: true},
		{
			name:       "all three",
			capability: Capability{Name: "read", RequiresApproval: true, RequiresLease: true, RequiresPolicy: true},
			want:       true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.capability.AsTool().RequiresActionTask; got != test.want {
				t.Fatalf("AsTool().RequiresActionTask = %t, want %t", got, test.want)
			}
		})
	}
}

func TestToolCapabilityRoundTripPreservesRequiresActionTask(t *testing.T) {
	for _, requires := range []bool{false, true} {
		tool := Tool{
			Name:               "write_report",
			EffectType:         ToolEffectWrite,
			RequiresActionTask: requires,
			RiskLevel:          "medium",
			Idempotent:         true,
			PolicyTags:         []string{"report"},
			Metadata:           map[string]string{"owner": "runtime"},
		}

		got := tool.AsCapability("agent-1").AsTool()
		if got.RequiresActionTask != requires {
			t.Fatalf("Tool→Capability→Tool RequiresActionTask = %t, want %t", got.RequiresActionTask, requires)
		}
		if got.Name != tool.Name || got.EffectType != tool.EffectType || got.RiskLevel != tool.RiskLevel {
			t.Fatalf("Tool→Capability→Tool lost identity fields: %#v", got)
		}
		if got.Idempotent != tool.Idempotent {
			t.Fatalf("Tool→Capability→Tool lost Idempotent: %#v", got)
		}
		if len(got.PolicyTags) != 1 || got.PolicyTags[0] != "report" {
			t.Fatalf("Tool→Capability→Tool lost PolicyTags: %#v", got.PolicyTags)
		}
		if got.Metadata["owner"] != "runtime" {
			t.Fatalf("Tool→Capability→Tool lost Metadata: %#v", got.Metadata)
		}
	}
}

// A Capability whose three requirement flags disagree cannot survive a trip
// through Tool: Tool carries a single RequiresActionTask bit. This test pins
// that documented loss so a future change to either projection is deliberate.
func TestCapabilityToolRoundTripFansOutRequirementFlags(t *testing.T) {
	capability := Capability{Name: "deploy", RequiresLease: true}

	got := capability.AsTool().AsCapability("")
	if !got.RequiresApproval || !got.RequiresLease || !got.RequiresPolicy {
		t.Fatalf("Capability→Tool→Capability requirement flags = %#v, want all three set", got)
	}
}

func TestAgentDefinitionAsProfileDefaultsRoleAndCopiesMetadata(t *testing.T) {
	definition := AgentDefinition{
		ID:       "agent-1",
		Name:     "Researcher",
		Metadata: map[string]string{"team": "alpha"},
	}

	profile := definition.AsProfile()
	definition.Metadata["team"] = "mutated"

	if profile.ID != "agent-1" || profile.Role != "Researcher" {
		t.Fatalf("AsProfile() = %#v", profile)
	}
	if profile.Metadata["team"] != "alpha" {
		t.Fatalf("AsProfile() did not copy metadata: %#v", profile.Metadata)
	}
}

func TestAgentDefinitionAsProfileUsesExplicitRole(t *testing.T) {
	profile := AgentDefinition{
		ID:       "agent-1",
		Name:     "Researcher",
		Metadata: map[string]string{"role": "reviewer"},
	}.AsProfile()

	if profile.Role != "reviewer" {
		t.Fatalf("AsProfile() role = %q, want reviewer", profile.Role)
	}
}
