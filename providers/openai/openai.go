package openai

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
			"gpt-5.4",
			"gpt-5.4-mini",
			"gpt-5.2",
		}
	}
	if config.BaseURL == "" {
		config.BaseURL = "https://api.openai.com/v1"
	}
	return Driver{config: config}
}

func (d Driver) Metadata() provider.Metadata {
	return provider.Metadata{
		Name:    "openai",
		Models:  d.config.Models,
		Version: "v1",
	}
}

func (d Driver) Stream(context.Context, provider.Request) (provider.Stream, error) {
	return nil, fmt.Errorf("openai provider adapter is scaffolded but not wired to the remote API yet: %w", provider.ErrNotImplemented)
}
