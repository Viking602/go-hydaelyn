package security

import (
	"context"
	"testing"
)

func TestContextRoundTripAndGrants(t *testing.T) {
	ctx := WithContext(context.Background(), Context{
		Principal: "alice",
	})
	ctx = WithApprovalGrant(ctx, ApprovalGrant{Type: "tool", Name: "dangerous"})
	ctx = WithPermissionGrant(ctx, PermissionGrant{Name: "tool:dangerous"})
	ctx = WithIdempotencyKey(ctx, "idem-1")

	current, ok := FromContext(ctx)
	if !ok {
		t.Fatal("expected security context")
	}
	if current.Principal != "alice" {
		t.Fatalf("expected principal alice, got %#v", current)
	}
	if current.IdempotencyKey != "idem-1" {
		t.Fatalf("expected idempotency key, got %#v", current)
	}
	if _, ok := current.ApprovalGrants["tool/dangerous"]; !ok {
		t.Fatalf("expected approval grant, got %#v", current.ApprovalGrants)
	}
	if _, ok := current.Permissions["tool:dangerous"]; !ok {
		t.Fatalf("expected permission grant, got %#v", current.Permissions)
	}
}
