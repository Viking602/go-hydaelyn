package api

import (
	"errors"
	"testing"
)

func TestValidateCapabilityName_RejectsReservedSelfNamespace(t *testing.T) {
	for _, name := range []string{
		CapabilityNameSelfProfile,
		CapabilityNameSelfMemoryRead,
		CapabilityNameSelfHistory,
		CapabilityNameSelfSummarizeHistory,
		HydaelynSelfNamespace + "extra",
	} {
		if err := ValidateCapabilityName(name); !errors.Is(err, ErrCapabilityNameReserved) {
			t.Fatalf("ValidateCapabilityName(%q) = %v, want ErrCapabilityNameReserved", name, err)
		}
	}
}

func TestValidateCapabilityName_AllowsApplicationNames(t *testing.T) {
	if err := ValidateCapabilityName("research.search"); err != nil {
		t.Fatalf("ValidateCapabilityName(application) = %v", err)
	}
	if err := ValidateCapabilityName(""); !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("ValidateCapabilityName(empty) = %v, want ErrInvalidCommand", err)
	}
}

func TestSelfNamespaceConstants_StableStrings(t *testing.T) {
	cases := map[string]string{
		CapabilityNameSelfProfile:          "hydaelyn.self.profile",
		CapabilityNameSelfMemoryRead:       "hydaelyn.self.memory.read",
		CapabilityNameSelfHistory:          "hydaelyn.self.history",
		CapabilityNameSelfSummarizeHistory: "hydaelyn.self.summarize_history",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("reserved name %q, want %q", got, want)
		}
	}
	if HydaelynSelfNamespace != "hydaelyn.self." {
		t.Fatalf("HydaelynSelfNamespace = %q", HydaelynSelfNamespace)
	}
}
