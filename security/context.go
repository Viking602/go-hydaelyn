package security

import (
	"context"
	"maps"
	"time"
)

type ApprovalGrant struct {
	Type       string    `json:"type"`
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

type Context struct {
	Principal      string                     `json:"principal,omitempty"`
	TrustedClaims  map[string]any             `json:"trustedClaims,omitempty"`
	UserMetadata   map[string]any             `json:"userMetadata,omitempty"`
	ModelMetadata  map[string]any             `json:"modelMetadata,omitempty"`
	ApprovalGrants map[string]ApprovalGrant   `json:"approvalGrants,omitempty"`
	Permissions    map[string]PermissionGrant `json:"permissions,omitempty"`
	IdempotencyKey string                     `json:"idempotencyKey,omitempty"`
}

type contextKey struct{}

func WithContext(ctx context.Context, security Context) context.Context {
	return context.WithValue(ctx, contextKey{}, cloneContext(security))
}

func FromContext(ctx context.Context) (Context, bool) {
	security, ok := ctx.Value(contextKey{}).(Context)
	if !ok {
		return Context{}, false
	}
	return cloneContext(security), true
}

func WithApprovalGrant(ctx context.Context, grant ApprovalGrant) context.Context {
	security, _ := FromContext(ctx)
	if security.ApprovalGrants == nil {
		security.ApprovalGrants = map[string]ApprovalGrant{}
	}
	if grant.GrantedAt.IsZero() {
		grant.GrantedAt = time.Now().UTC()
	}
	security.ApprovalGrants[grant.Type+"/"+grant.Name] = grant
	return WithContext(ctx, security)
}

func WithPermissionGrant(ctx context.Context, grant PermissionGrant) context.Context {
	security, _ := FromContext(ctx)
	if security.Permissions == nil {
		security.Permissions = map[string]PermissionGrant{}
	}
	if grant.GrantedAt.IsZero() {
		grant.GrantedAt = time.Now().UTC()
	}
	security.Permissions[grant.Name] = grant
	return WithContext(ctx, security)
}

func WithIdempotencyKey(ctx context.Context, key string) context.Context {
	security, _ := FromContext(ctx)
	security.IdempotencyKey = key
	return WithContext(ctx, security)
}

func cloneContext(security Context) Context {
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
