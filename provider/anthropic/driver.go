package anthropic

import (
	"net/http"
	"time"

	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/provider/shared"
)

const defaultClientTimeout = 30 * time.Second

type Config struct {
	APIKey    string
	BaseURL   string
	Models    []string
	Client    *http.Client
	Version   string
	MaxTokens int
	// Betas sets the anthropic-beta request header (comma-joined). Leave empty
	// for the GA path; extended thinking combined with tool use needs no beta
	// header on current models. Set e.g. "interleaved-thinking-2025-05-14"
	// only when opting into a beta feature.
	Betas []string
	// Retry bounds stream-initiation retries for transient HTTP failures
	// (429/5xx/transport errors). The zero value retries up to 3 total
	// attempts with exponential backoff; MaxAttempts: -1 disables retrying.
	Retry shared.RetryPolicy
}

type Driver struct {
	config Config
}

func New(config Config) Driver {
	if len(config.Models) == 0 {
		config.Models = []string{
			"claude-opus-4-8",
			"claude-sonnet-4-6",
			"claude-haiku-4-5",
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
	if config.Client == nil {
		config.Client = &http.Client{Timeout: defaultClientTimeout}
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
