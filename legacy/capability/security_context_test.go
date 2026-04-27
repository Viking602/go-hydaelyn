package capability

import (
	"context"
	"errors"
	"testing"
)

func TestRequirePermissionsIgnoresCallGrantedFlagWithoutTrustedContext(t *testing.T) {
	t.Parallel()

	invoker := NewInvoker()
	invoker.Use(RequirePermissions())
	handlerCalls := 0
	invoker.Register(TypeTool, "deploy", func(context.Context, Call) (Result, error) {
		handlerCalls++
		return Result{Output: "ok"}, nil
	})

	_, err := invoker.Invoke(context.Background(), Call{
		Type: TypeTool,
		Name: "deploy",
		Permissions: []Permission{{
			Name:    "tool:deploy",
			Granted: true,
		}},
		Metadata: map[string]string{
			"permission": "tool:deploy",
			"granted":    "true",
		},
	})
	if err == nil {
		t.Fatal("expected trusted-context permission denial")
	}
	var capErr *Error
	if !errors.As(err, &capErr) || capErr.Kind != ErrorKindPermission {
		t.Fatalf("expected permission error, got %v", err)
	}
	if handlerCalls != 0 {
		t.Fatalf("expected denied call to skip handler, got %d", handlerCalls)
	}
}

func TestRequirePermissionsAcceptsTrustedSecurityContextGrant(t *testing.T) {
	t.Parallel()

	invoker := NewInvoker()
	invoker.Use(RequirePermissions())
	invoker.Register(TypeTool, "deploy", func(context.Context, Call) (Result, error) {
		return Result{Output: "ok"}, nil
	})

	ctx := WithPermissionGrants(context.Background(), PermissionGrant{Name: "tool:deploy", GrantedBy: "policy"})
	result, err := invoker.Invoke(ctx, Call{
		Type: TypeTool,
		Name: "deploy",
		Permissions: []Permission{{
			Name: "tool:deploy",
		}},
	})
	if err != nil {
		t.Fatalf("expected trusted grant to pass, got %v", err)
	}
	if result.Output != "ok" {
		t.Fatalf("unexpected output %#v", result)
	}
}

func TestRequireApprovalAcceptsTrustedSecurityContextGrant(t *testing.T) {
	t.Parallel()

	invoker := NewInvoker()
	invoker.Use(RequireApproval())
	invoker.Register(TypeTool, "deploy", func(context.Context, Call) (Result, error) {
		return Result{Output: "approved"}, nil
	})

	ctx := WithApprovalGrant(context.Background(), ApprovalGrant{
		Type:       TypeTool,
		Name:       "deploy",
		ApprovedBy: "runtime",
	})
	result, err := invoker.Invoke(ctx, Call{Type: TypeTool, Name: "deploy"})
	if err != nil {
		t.Fatalf("expected trusted approval to pass, got %v", err)
	}
	if result.Output != "approved" {
		t.Fatalf("unexpected output %#v", result)
	}
}
