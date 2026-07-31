package assertions

import (
	"context"
	"fmt"

	"github.com/Viking602/venat/api"
	"github.com/Viking602/venat/eval"
)

// PolicyDecisionAllowedBy asserts that some policy decision recorded on the
// run's tasks both identifies as the named policy and allowed the operation
// (Effect == allow). A decision is matched by name against its DecisionID or a
// Metadata "policy"/"name" label, falling back to its Reason — whichever the
// PolicyEngine populated.
type PolicyDecisionAllowedBy struct {
	// Policy is the name (DecisionID, Metadata label, or Reason) the allowing
	// decision must identify as.
	Policy string
}

// Name returns the assertion's stable identifier.
func (a PolicyDecisionAllowedBy) Name() string { return "PolicyDecisionAllowedBy" }

// Check reports whether the named policy allowed an operation on the run.
func (a PolicyDecisionAllowedBy) Check(ctx context.Context, run api.Run, harness eval.Harness) error {
	return checkPolicyDecision(ctx, run, harness, a.Policy, allowEffects(), "allow")
}

// PolicyDecisionDeniedBy asserts that some policy decision recorded on the
// run's tasks both identifies as the named policy and denied the operation
// (Effect == deny or abort). The name is resolved the same way as
// PolicyDecisionAllowedBy.
type PolicyDecisionDeniedBy struct {
	// Policy is the name (DecisionID, Metadata label, or Reason) the denying
	// decision must identify as.
	Policy string
}

// Name returns the assertion's stable identifier.
func (a PolicyDecisionDeniedBy) Name() string { return "PolicyDecisionDeniedBy" }

// Check reports whether the named policy denied an operation on the run.
func (a PolicyDecisionDeniedBy) Check(ctx context.Context, run api.Run, harness eval.Harness) error {
	return checkPolicyDecision(ctx, run, harness, a.Policy, denyEffects(), "deny")
}

func allowEffects() map[api.PolicyEffect]bool {
	return map[api.PolicyEffect]bool{api.PolicyEffectAllow: true}
}

func denyEffects() map[api.PolicyEffect]bool {
	return map[api.PolicyEffect]bool{api.PolicyEffectDeny: true, api.PolicyEffectAbort: true}
}

func checkPolicyDecision(ctx context.Context, run api.Run, harness eval.Harness, policy string, effects map[api.PolicyEffect]bool, verb string) error {
	runner := harness.Runner()
	if runner == nil {
		return fmt.Errorf("harness returned a nil runner")
	}
	tasks, err := runner.ListTasks(ctx, run.ID)
	if err != nil {
		return fmt.Errorf("list tasks: %w", err)
	}
	var sawPolicy bool
	for _, task := range tasks {
		for _, decision := range task.PolicyDecisions {
			if !policyDecisionMatchesName(decision, policy) {
				continue
			}
			sawPolicy = true
			if effects[decision.Effect] {
				return nil
			}
		}
	}
	if sawPolicy {
		return fmt.Errorf("policy %q recorded a decision but none with effect %q", policy, verb)
	}
	return fmt.Errorf("no policy decision identified as %q was recorded", policy)
}

// policyDecisionMatchesName reports whether decision identifies as the named
// policy. The framework does not reserve a single canonical name field, so the
// DecisionID and the Metadata "policy"/"name" labels are accepted first. The
// Reason is matched only as a last resort, since it is free-form prose that may
// over-match; prefer a DecisionID or a Metadata "policy"/"name" label in your
// PolicyEngine so the assertion does not have to fall back to it.
func policyDecisionMatchesName(decision api.PolicyDecision, name string) bool {
	if decision.DecisionID == name {
		return true
	}
	if decision.Metadata != nil {
		if decision.Metadata["policy"] == name || decision.Metadata["name"] == name {
			return true
		}
	}
	return decision.Reason == name
}
