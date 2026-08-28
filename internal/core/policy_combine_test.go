package core

import (
	"testing"
	"time"

	"github.com/Viking602/venat/api"
)

func TestStrictestPolicyDecision_KeepsEarliestExpiry(t *testing.T) {
	early := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	late := early.Add(time.Hour)
	tests := []struct {
		name    string
		engine  api.PolicyDecision
		message api.PolicyDecision
		want    time.Time
	}{
		{
			name:    "winner carries no expiry",
			engine:  api.PolicyDecision{Effect: api.PolicyEffectAllow, ExpiresAt: early},
			message: api.PolicyDecision{Effect: api.PolicyEffectPause},
			want:    early,
		},
		{
			name:    "loser carries no expiry",
			engine:  api.PolicyDecision{Effect: api.PolicyEffectAllow},
			message: api.PolicyDecision{Effect: api.PolicyEffectPause, ExpiresAt: late},
			want:    late,
		},
		{
			name:    "both carry an expiry",
			engine:  api.PolicyDecision{Effect: api.PolicyEffectAllow, ExpiresAt: early},
			message: api.PolicyDecision{Effect: api.PolicyEffectPause, ExpiresAt: late},
			want:    early,
		},
		{
			name:    "neither carries an expiry",
			engine:  api.PolicyDecision{Effect: api.PolicyEffectAllow},
			message: api.PolicyDecision{Effect: api.PolicyEffectPause},
			want:    time.Time{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := strictestPolicyDecision(test.engine, test.message)
			if !got.ExpiresAt.Equal(test.want) {
				t.Fatalf("ExpiresAt = %v, want %v", got.ExpiresAt, test.want)
			}
		})
	}
}

// An expired engine decision must still fail closed after the merge, even when
// the message policy wins the effect comparison with a decision of its own that
// never expires.
func TestStrictestPolicyDecision_ExpiredAllowNormalizesToDeny(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	combined := strictestPolicyDecision(
		api.PolicyDecision{Effect: api.PolicyEffectAllow, ExpiresAt: now.Add(-time.Minute)},
		api.PolicyDecision{Effect: api.PolicyEffectPause},
	)
	if combined.Effect != api.PolicyEffectPause {
		t.Fatalf("merged effect = %q, want the stricter Pause before normalization", combined.Effect)
	}
	request := api.PolicyRequest{Operation: api.PolicyOperationResponsePublish}
	if err := normalizePolicyDecision(request, &combined, now); err != nil {
		t.Fatalf("normalizePolicyDecision() error = %v", err)
	}
	if combined.Effect != api.PolicyEffectDeny {
		t.Fatalf("normalized effect = %q, want Deny for an expired decision", combined.Effect)
	}
}

func TestMergePolicyObligations_Deduplicates(t *testing.T) {
	redact := api.PolicyObligation{Kind: api.ObligationRedactFields, Target: api.PolicyTargetResponse}
	hideTrace := api.PolicyObligation{Kind: api.ObligationHideInternalTrace, Target: api.PolicyTargetResponse}
	selectorOnly := func() api.PolicyObligation {
		return api.PolicyObligation{
			Kind:     api.ObligationSelectorOnly,
			Target:   api.PolicyTargetBlackboardRead,
			Selector: &api.BlackboardSelector{Keys: []string{"summary"}},
		}
	}
	tests := []struct {
		name    string
		engine  []api.PolicyObligation
		message []api.PolicyObligation
		want    int
	}{
		{
			name:    "identical obligation appears once",
			engine:  []api.PolicyObligation{redact},
			message: []api.PolicyObligation{redact},
			want:    1,
		},
		{
			name:    "distinct obligations are both kept",
			engine:  []api.PolicyObligation{redact},
			message: []api.PolicyObligation{hideTrace},
			want:    2,
		},
		{
			name:    "equal selectors behind different pointers collapse",
			engine:  []api.PolicyObligation{selectorOnly()},
			message: []api.PolicyObligation{selectorOnly()},
			want:    1,
		},
		{
			name:   "only one side carries obligations",
			engine: []api.PolicyObligation{redact, hideTrace},
			want:   2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := mergePolicyObligations(test.engine, test.message); len(got) != test.want {
				t.Fatalf("mergePolicyObligations() = %#v, want %d entries", got, test.want)
			}
		})
	}
}

func TestStrictestPolicyDecision_MergesMetadataWinnerFirst(t *testing.T) {
	combined := strictestPolicyDecision(
		api.PolicyDecision{
			Effect:   api.PolicyEffectAllow,
			Metadata: map[string]string{"source": "engine", "engineOnly": "yes"},
		},
		api.PolicyDecision{
			Effect:   api.PolicyEffectRequireApproval,
			Metadata: map[string]string{"source": "message", "messageOnly": "yes"},
		},
	)
	want := map[string]string{"source": "message", "engineOnly": "yes", "messageOnly": "yes"}
	if len(combined.Metadata) != len(want) {
		t.Fatalf("Metadata = %#v, want %#v", combined.Metadata, want)
	}
	for key, value := range want {
		if combined.Metadata[key] != value {
			t.Fatalf("Metadata[%q] = %q, want %q", key, combined.Metadata[key], value)
		}
	}
}
