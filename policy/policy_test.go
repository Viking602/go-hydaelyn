package policy

import (
	"context"
	"testing"

	"github.com/Viking602/go-hydaelyn/api"
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
