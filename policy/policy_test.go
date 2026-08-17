package policy

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Viking602/venat/api"
)

func TestDenySideEffectsByDefault(t *testing.T) {
	engine := DenySideEffectsByDefault()
	decision, err := engine.Authorize(context.Background(), Request{
		Operation: OperationToolCall,
		Tool:      &api.Tool{Name: "write", EffectType: api.ToolEffectWrite},
	})
	if err != nil {
		t.Fatalf("Authorize() error = %v", err)
	}
	if decision.Effect != EffectDeny {
		t.Fatalf("expected deny, got %#v", decision)
	}
	decision, err = engine.Authorize(context.Background(), Request{
		Operation: OperationToolCall,
		Tool:      &api.Tool{Name: "read", EffectType: api.ToolEffectReadOnly},
	})
	if err != nil {
		t.Fatalf("Authorize(read) error = %v", err)
	}
	if decision.Effect != EffectAllow {
		t.Fatalf("expected allow, got %#v", decision)
	}
}

func TestRequireApprovalForSideEffects(t *testing.T) {
	engine := RequireApprovalForSideEffects()
	decision, err := engine.Authorize(context.Background(), Request{
		Operation: OperationAction,
	})
	if err != nil {
		t.Fatalf("Authorize() error = %v", err)
	}
	if decision.Effect != EffectRequireApproval {
		t.Fatalf("expected require approval, got %#v", decision)
	}
}

func TestChainCombinesEveryDecisionInStableOrder(t *testing.T) {
	calls := make([]string, 0, 4)
	expiresLater := time.Date(2026, time.August, 4, 14, 0, 0, 0, time.UTC)
	expiresSooner := expiresLater.Add(-time.Hour)
	engine := NewChain(
		EngineFunc(func(context.Context, Request) (Decision, error) {
			calls = append(calls, "allow")
			return Decision{
				Effect:      EffectAllow,
				Obligations: []Obligation{{Kind: ObligationRedactFields, Target: TargetResponse}},
				Redactions:  []string{"email"},
				ExpiresAt:   expiresLater,
				Metadata:    map[string]string{"owner": "first"},
			}, nil
		}),
		EngineFunc(func(context.Context, Request) (Decision, error) {
			calls = append(calls, "pause")
			return Decision{
				DecisionID:  "pause-decision",
				Effect:      EffectPause,
				Reason:      "maintenance",
				Obligations: []Obligation{{Kind: ObligationRedactFields, Target: TargetResponse}},
				Redactions:  []string{"email", "token"},
				ExpiresAt:   expiresSooner,
				Metadata:    map[string]string{"owner": "later", "region": "local"},
			}, nil
		}),
		EngineFunc(func(context.Context, Request) (Decision, error) {
			calls = append(calls, "deny")
			return Decision{
				DecisionID:  "deny-decision",
				Effect:      EffectDeny,
				Reason:      "blocked",
				Obligations: []Obligation{{Kind: ObligationHideInternalTrace, Target: TargetTrace}},
			}, nil
		}),
		EngineFunc(func(context.Context, Request) (Decision, error) {
			calls = append(calls, "approval")
			return Decision{Effect: EffectRequireApproval}, nil
		}),
	)

	decision, err := engine.Authorize(context.Background(), Request{Operation: OperationDispatch})
	if err != nil {
		t.Fatalf("Authorize() error = %v", err)
	}
	if !reflect.DeepEqual(calls, []string{"allow", "pause", "deny", "approval"}) {
		t.Fatalf("engine calls = %v", calls)
	}
	if decision.Effect != EffectDeny || decision.DecisionID != "deny-decision" || decision.Reason != "blocked" {
		t.Fatalf("combined effect = %#v", decision)
	}
	if !reflect.DeepEqual(decision.Obligations, []Obligation{
		{Kind: ObligationRedactFields, Target: TargetResponse},
		{Kind: ObligationHideInternalTrace, Target: TargetTrace},
	}) {
		t.Fatalf("combined obligations = %#v", decision.Obligations)
	}
	if !reflect.DeepEqual(decision.Redactions, []string{"email", "token"}) {
		t.Fatalf("combined redactions = %#v", decision.Redactions)
	}
	if !decision.ExpiresAt.Equal(expiresSooner) {
		t.Fatalf("combined expiry = %v, want %v", decision.ExpiresAt, expiresSooner)
	}
	if !reflect.DeepEqual(decision.Metadata, map[string]string{"owner": "first", "region": "local"}) {
		t.Fatalf("combined metadata = %#v", decision.Metadata)
	}
}

func TestChainPreservesDistinctSelectorObligations(t *testing.T) {
	first := api.BlackboardSelector{Tags: []string{"public"}}
	second := api.BlackboardSelector{SourceIDs: []string{"reviewer"}}
	engine := NewChain(
		EngineFunc(func(context.Context, Request) (Decision, error) {
			return Decision{
				Effect: EffectAllow,
				Obligations: []Obligation{{
					Kind:     ObligationSelectorOnly,
					Target:   TargetBlackboardRead,
					Selector: &first,
				}},
			}, nil
		}),
		EngineFunc(func(context.Context, Request) (Decision, error) {
			return Decision{
				Effect: EffectAllow,
				Obligations: []Obligation{{
					Kind:     ObligationSelectorOnly,
					Target:   TargetBlackboardRead,
					Selector: &second,
				}},
			}, nil
		}),
	)

	decision, err := engine.Authorize(context.Background(), Request{Operation: OperationBlackboardRead})
	if err != nil {
		t.Fatalf("Authorize() error = %v", err)
	}
	if len(decision.Obligations) != 2 {
		t.Fatalf("combined obligations = %#v, want both selector restrictions", decision.Obligations)
	}
}

func TestChainFailsClosedOnEmptyEffect(t *testing.T) {
	engine := NewChain(EngineFunc(func(context.Context, Request) (Decision, error) {
		return Decision{}, nil
	}))

	decision, err := engine.Authorize(context.Background(), Request{Operation: OperationToolCall})
	if err == nil {
		t.Fatal("Authorize() error = nil, want unknown-effect failure")
	}
	if !strings.Contains(err.Error(), `unknown effect ""`) {
		t.Fatalf("Authorize() error = %v, want unknown empty effect", err)
	}
	if decision.Effect != EffectDeny {
		t.Fatalf("decision effect = %q, want deny", decision.Effect)
	}
}

func TestChainFailsClosedOnEngineError(t *testing.T) {
	wantErr := errors.New("policy unavailable")
	engine := NewChain(EngineFunc(func(context.Context, Request) (Decision, error) {
		return Decision{Effect: EffectAllow}, wantErr
	}))

	decision, err := engine.Authorize(context.Background(), Request{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Authorize() error = %v, want %v", err, wantErr)
	}
	if decision.Effect != EffectDeny {
		t.Fatalf("decision effect = %q, want deny", decision.Effect)
	}
}

func TestChainUsesDocumentedEffectPrecedence(t *testing.T) {
	effects := []Effect{EffectPause, EffectRequireApproval, EffectDeny, EffectAbort}
	engine := NewChain()
	for _, effect := range effects {
		effect := effect
		engine.Engines = append(engine.Engines, EngineFunc(func(context.Context, Request) (Decision, error) {
			return Decision{Effect: effect}, nil
		}))
	}

	decision, err := engine.Authorize(context.Background(), Request{})
	if err != nil {
		t.Fatalf("Authorize() error = %v", err)
	}
	if decision.Effect != EffectAbort {
		t.Fatalf("decision effect = %q, want abort", decision.Effect)
	}
}
