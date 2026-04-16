package openai

import (
	"net/http"

	"github.com/Viking602/go-hydaelyn/provider"
)

type Config struct {
	APIKey  string
	BaseURL string
	Models  []string
	Client  *http.Client
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
