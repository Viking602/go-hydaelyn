package capability

import (
	"context"
	"time"

	securityctx "github.com/Viking602/go-hydaelyn/security"
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
	return securityctx.WithContext(ctx, toSecurityContext(security))
}

func SecurityContextFromContext(ctx context.Context) (SecurityContext, bool) {
	security, ok := securityctx.FromContext(ctx)
	if !ok {
		return SecurityContext{}, false
	}
	return fromSecurityContext(security), true
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
	security, ok := securityctx.FromContext(ctx)
	if !ok || len(security.Permissions) == 0 {
		return false
	}
	_, ok = security.Permissions[permission]
	return ok
}

func cloneSecurityContext(security SecurityContext) SecurityContext {
	return fromSecurityContext(toSecurityContext(security))
}

func cloneAnyMap(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func toSecurityContext(security SecurityContext) securityctx.Context {
	current := securityctx.Context{
		Principal:      security.Principal,
		TrustedClaims:  cloneAnyMap(security.TrustedClaims),
		UserMetadata:   cloneAnyMap(security.UserMetadata),
		ModelMetadata:  cloneAnyMap(security.ModelMetadata),
		IdempotencyKey: security.IdempotencyKey,
	}
	if len(security.ApprovalGrants) > 0 {
		current.ApprovalGrants = make(map[string]securityctx.ApprovalGrant, len(security.ApprovalGrants))
		for key, grant := range security.ApprovalGrants {
			current.ApprovalGrants[key] = securityctx.ApprovalGrant{
				Type:       string(grant.Type),
				Name:       grant.Name,
				ApprovedBy: grant.ApprovedBy,
				Reason:     grant.Reason,
				GrantedAt:  grant.GrantedAt,
			}
		}
	}
	if len(security.Permissions) > 0 {
		current.Permissions = make(map[string]securityctx.PermissionGrant, len(security.Permissions))
		for key, grant := range security.Permissions {
			current.Permissions[key] = securityctx.PermissionGrant{
				Name:      grant.Name,
				GrantedBy: grant.GrantedBy,
				Scope:     grant.Scope,
				GrantedAt: grant.GrantedAt,
			}
		}
	}
	return current
}

func fromSecurityContext(security securityctx.Context) SecurityContext {
	current := SecurityContext{
		Principal:      security.Principal,
		TrustedClaims:  cloneAnyMap(security.TrustedClaims),
		UserMetadata:   cloneAnyMap(security.UserMetadata),
		ModelMetadata:  cloneAnyMap(security.ModelMetadata),
		IdempotencyKey: security.IdempotencyKey,
	}
	if len(security.ApprovalGrants) > 0 {
		current.ApprovalGrants = make(map[string]ApprovalGrant, len(security.ApprovalGrants))
		for key, grant := range security.ApprovalGrants {
			current.ApprovalGrants[key] = ApprovalGrant{
				Type:       Type(grant.Type),
				Name:       grant.Name,
				ApprovedBy: grant.ApprovedBy,
				Reason:     grant.Reason,
				GrantedAt:  grant.GrantedAt,
			}
		}
	}
	if len(security.Permissions) > 0 {
		current.Permissions = make(map[string]PermissionGrant, len(security.Permissions))
		for key, grant := range security.Permissions {
			current.Permissions[key] = PermissionGrant{
				Name:      grant.Name,
				GrantedBy: grant.GrantedBy,
				Scope:     grant.Scope,
				GrantedAt: grant.GrantedAt,
			}
		}
	}
	return current
}
