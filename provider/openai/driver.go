package openai

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/Viking602/venat/provider"
	"github.com/Viking602/venat/provider/shared"
)

const defaultResponseHeaderTimeout = 30 * time.Second

// WireAPI selects the OpenAI HTTP protocol used for streamed requests.
type WireAPI string

const (
	// WireChatCompletions uses the backward-compatible /chat/completions route.
	WireChatCompletions WireAPI = "chat_completions"
	// WireResponses uses the /responses route required by Codex models.
	WireResponses WireAPI = "responses"
)

type Config struct {
	APIKey  string
	BaseURL string
	Models  []string
	Client  *http.Client
	WireAPI WireAPI
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
			"gpt-5.6-sol",
			"gpt-5.6-terra",
			"gpt-5.6-luna",
			"gpt-5.3-codex",
		}
	}
	if config.BaseURL == "" {
		config.BaseURL = "https://api.openai.com/v1"
	}
	if config.WireAPI == "" {
		config.WireAPI = WireResponses
	}
	if config.Client == nil {
		config.Client = shared.NewHTTPClient(defaultResponseHeaderTimeout)
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

func (d Driver) Stream(ctx context.Context, request provider.Request) (provider.Stream, error) {
	switch d.config.WireAPI {
	case WireChatCompletions:
		return d.streamChatCompletions(ctx, request)
	case WireResponses:
		return d.streamResponses(ctx, request)
	default:
		return nil, fmt.Errorf("openai: unsupported wire API %q", d.config.WireAPI)
	}
}
