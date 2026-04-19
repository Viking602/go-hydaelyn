package capability

import (
	"context"
	"maps"
	"time"
)

type ApprovalGrant struct {
	Type       Type      `json:"type"`
	Name       string    `json:"name"`
	ApprovedBy string    `json:"approvedBy,omitempty"`
	Reason     string    `json:"reason,omitempty"`
	GrantedAt  time.Time `json:"grantedAt,omitempty"`
}

type PermissionGrant struct {
	Name      string    `json:"name"`
	GrantedBy string    `json:"grantedBy,omitempty"`
	Scope     string    `json:"scope,omitempty"`
	GrantedAt time.Time `json:"grantedAt,omitempty"`
}

type SecurityContext struct {
	Principal      string                     `json:"principal,omitempty"`
	TrustedClaims  map[string]any             `json:"trustedClaims,omitempty"`
	UserMetadata   map[string]any             `json:"userMetadata,omitempty"`
	ModelMetadata  map[string]any             `json:"modelMetadata,omitempty"`
	ApprovalGrants map[string]ApprovalGrant   `json:"approvalGrants,omitempty"`
	Permissions    map[string]PermissionGrant `json:"permissions,omitempty"`
	IdempotencyKey string                     `json:"idempotencyKey,omitempty"`
}

type securityContextKey struct{}

func WithSecurityContext(ctx context.Context, security SecurityContext) context.Context {
	return context.WithValue(ctx, securityContextKey{}, cloneSecurityContext(security))
}

func SecurityContextFromContext(ctx context.Context) (SecurityContext, bool) {
	security, ok := ctx.Value(securityContextKey{}).(SecurityContext)
	if !ok {
		return SecurityContext{}, false
	}
	return cloneSecurityContext(security), true
}

func WithApprovalGrant(ctx context.Context, grant ApprovalGrant) context.Context {
	security, _ := SecurityContextFromContext(ctx)
	if security.ApprovalGrants == nil {
		security.ApprovalGrants = map[string]ApprovalGrant{}
	}
	if grant.GrantedAt.IsZero() {
		grant.GrantedAt = time.Now().UTC()
	}
	security.ApprovalGrants[key(grant.Type, grant.Name)] = grant
	return WithSecurityContext(ctx, security)
}

func WithPermissionGrant(ctx context.Context, grant PermissionGrant) context.Context {
	security, _ := SecurityContextFromContext(ctx)
	if security.Permissions == nil {
		security.Permissions = map[string]PermissionGrant{}
	}
	if grant.GrantedAt.IsZero() {
		grant.GrantedAt = time.Now().UTC()
	}
	security.Permissions[grant.Name] = grant
	return WithSecurityContext(ctx, security)
}

func WithPermissionGrants(ctx context.Context, grants ...PermissionGrant) context.Context {
	for _, grant := range grants {
		ctx = WithPermissionGrant(ctx, grant)
	}
	return ctx
}

func WithIdempotencyKey(ctx context.Context, idempotencyKey string) context.Context {
	security, _ := SecurityContextFromContext(ctx)
	security.IdempotencyKey = idempotencyKey
	return WithSecurityContext(ctx, security)
}

func hasPermissionGrant(ctx context.Context, permission string) bool {
	security, ok := SecurityContextFromContext(ctx)
	if !ok || len(security.Permissions) == 0 {
		return false
	}
	_, ok = security.Permissions[permission]
	return ok
}

func cloneSecurityContext(security SecurityContext) SecurityContext {
	security.TrustedClaims = cloneAnyMap(security.TrustedClaims)
	security.UserMetadata = cloneAnyMap(security.UserMetadata)
	security.ModelMetadata = cloneAnyMap(security.ModelMetadata)
	if len(security.ApprovalGrants) > 0 {
		cloned := make(map[string]ApprovalGrant, len(security.ApprovalGrants))
		maps.Copy(cloned, security.ApprovalGrants)
		security.ApprovalGrants = cloned
	}
	if len(security.Permissions) > 0 {
		cloned := make(map[string]PermissionGrant, len(security.Permissions))
		maps.Copy(cloned, security.Permissions)
		security.Permissions = cloned
	}
	return security
}

func cloneAnyMap(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(values))
	maps.Copy(cloned, values)
	return cloned
}
