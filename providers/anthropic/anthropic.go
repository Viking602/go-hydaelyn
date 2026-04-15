package anthropic

import (
	"context"
	"fmt"

	"github.com/Viking602/go-hydaelyn/provider"
)

type Config struct {
	APIKey  string
	BaseURL string
	Models  []string
}

type Driver struct {
	config Config
}

func New(config Config) Driver {
	if len(config.Models) == 0 {
		config.Models = []string{
			"claude-opus-4.1",
			"claude-sonnet-4",
			"claude-3.7-sonnet",
		}
	}
	if config.BaseURL == "" {
		config.BaseURL = "https://api.anthropic.com/v1"
	}
	return Driver{config: config}
}

func (d Driver) Metadata() provider.Metadata {
	return provider.Metadata{
		Name:    "anthropic",
		Models:  d.config.Models,
		Version: "v1",
	}
}

func (d Driver) Stream(context.Context, provider.Request) (provider.Stream, error) {
	return nil, fmt.Errorf("anthropic provider adapter is scaffolded but not wired to the remote API yet: %w", provider.ErrNotImplemented)
}
