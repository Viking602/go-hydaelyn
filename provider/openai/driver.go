package openai

import (
	"net/http"
	"time"

	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/provider/shared"
)

const defaultClientTimeout = 30 * time.Second

type Config struct {
	APIKey  string
	BaseURL string
	Models  []string
	Client  *http.Client
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
			"gpt-5.4",
			"gpt-5.4-mini",
			"gpt-5.2",
		}
	}
	if config.BaseURL == "" {
		config.BaseURL = "https://api.openai.com/v1"
	}
	if config.Client == nil {
		config.Client = &http.Client{Timeout: defaultClientTimeout}
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
