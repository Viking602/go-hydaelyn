package auth

import (
	"context"
	"reflect"
	"testing"
)

func TestStaticDriver_ResolveProviderCredentials(t *testing.T) {
	tests := []struct {
		name         string
		driver       StaticDriver
		providerName string
		want         Credentials
	}{
		{
			name:         "existing provider",
			driver:       StaticDriver{Credentials: map[string]Credentials{"test": {APIKey: "key123"}}},
			providerName: "test",
			want:         Credentials{APIKey: "key123"},
		},
		{
			name:         "non-existing provider",
			driver:       StaticDriver{Credentials: map[string]Credentials{"test": {APIKey: "key123"}}},
			providerName: "other",
			want:         Credentials{},
		},
		{
			name:         "nil credentials map",
			driver:       StaticDriver{},
			providerName: "any",
			want:         Credentials{},
		},
		{
			name: "with headers",
			driver: StaticDriver{Credentials: map[string]Credentials{
				"api": {APIKey: "secret", Headers: map[string]string{"X-Custom": "value"}},
			}},
			providerName: "api",
			want:         Credentials{APIKey: "secret", Headers: map[string]string{"X-Custom": "value"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			got, err := tt.driver.ResolveProviderCredentials(ctx, tt.providerName)
			if err != nil {
				t.Errorf("ResolveProviderCredentials() error = %v", err)
				return
			}
			if got.APIKey != tt.want.APIKey {
				t.Errorf("APIKey = %v, want %v", got.APIKey, tt.want.APIKey)
			}
		if !reflect.DeepEqual(got.Headers, tt.want.Headers) {
			t.Errorf("Headers = %v, want %v", got.Headers, tt.want.Headers)
		}
		})
	}
}

func TestStaticDriver_ResolveRuntimeIdentity(t *testing.T) {
	tests := []struct {
		name   string
		driver StaticDriver
		want   Identity
	}{
		{
			name:   "empty identity",
			driver: StaticDriver{},
			want:   Identity{},
		},
		{
			name:   "with subject",
			driver: StaticDriver{Identity: Identity{Subject: "user123"}},
			want:   Identity{Subject: "user123"},
		},
		{
			name: "with claims",
			driver: StaticDriver{Identity: Identity{
				Subject: "user123",
				Claims:  map[string]string{"role": "admin"},
			}},
			want: Identity{Subject: "user123", Claims: map[string]string{"role": "admin"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			got, err := tt.driver.ResolveRuntimeIdentity(ctx)
			if err != nil {
				t.Errorf("ResolveRuntimeIdentity() error = %v", err)
				return
			}
			if got.Subject != tt.want.Subject {
				t.Errorf("Subject = %v, want %v", got.Subject, tt.want.Subject)
			}
		if !reflect.DeepEqual(got.Claims, tt.want.Claims) {
			t.Errorf("Claims = %v, want %v", got.Claims, tt.want.Claims)
		}
		})
	}
}

func TestIdentity_Struct(t *testing.T) {
	identity := Identity{
		Subject: "test-subject",
		Claims: map[string]string{
			"key1": "value1",
			"key2": "value2",
		},
	}

	if identity.Subject != "test-subject" {
		t.Errorf("Subject = %v, want test-subject", identity.Subject)
	}

	if len(identity.Claims) != 2 {
		t.Errorf("len(Claims) = %v, want 2", len(identity.Claims))
	}
}

func TestCredentials_Struct(t *testing.T) {
	creds := Credentials{
		APIKey: "test-api-key",
		Headers: map[string]string{
			"Authorization": "Bearer token",
		},
	}

	if creds.APIKey != "test-api-key" {
		t.Errorf("APIKey = %v, want test-api-key", creds.APIKey)
	}

	if creds.Headers["Authorization"] != "Bearer token" {
		t.Errorf("Authorization header = %v, want Bearer token", creds.Headers["Authorization"])
	}
}