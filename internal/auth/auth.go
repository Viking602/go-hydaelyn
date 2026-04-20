package auth

import "context"

type Identity struct {
	Subject string            `json:"subject"`
	Claims  map[string]string `json:"claims,omitempty"`
}

type Credentials struct {
	APIKey  string            `json:"apiKey,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

type Driver interface {
	ResolveProviderCredentials(ctx context.Context, providerName string) (Credentials, error)
	ResolveRuntimeIdentity(ctx context.Context) (Identity, error)
}

type StaticDriver struct {
	Identity    Identity
	Credentials map[string]Credentials
}

func (d StaticDriver) ResolveProviderCredentials(_ context.Context, providerName string) (Credentials, error) {
	if d.Credentials == nil {
		return Credentials{}, nil
	}
	return d.Credentials[providerName], nil
}

func (d StaticDriver) ResolveRuntimeIdentity(_ context.Context) (Identity, error) {
	return d.Identity, nil
}
