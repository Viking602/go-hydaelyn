package anthropic

import (
	"net/http"

	"github.com/Viking602/go-hydaelyn/provider"
)

type Config struct {
	APIKey    string
	BaseURL   string
	Models    []string
	Client    *http.Client
	Version   string
	MaxTokens int
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
	if config.Version == "" {
		config.Version = "2023-06-01"
	}
	if config.MaxTokens <= 0 {
		config.MaxTokens = 1024
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
