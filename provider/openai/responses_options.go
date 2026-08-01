package openai

import "fmt"

// PromptCacheMode selects whether OpenAI adds an implicit cache breakpoint in
// addition to explicit message boundaries supplied by the caller.
type PromptCacheMode string

const (
	PromptCacheModeImplicit PromptCacheMode = "implicit"
	PromptCacheModeExplicit PromptCacheMode = "explicit"
)

// PromptCacheTTL is the minimum lifetime requested for prompt-cache entries.
type PromptCacheTTL string

const PromptCacheTTL30Minutes PromptCacheTTL = "30m"

// ReasoningEffort selects the amount of reasoning work requested from a model.
// Individual models support different subsets of these values.
type ReasoningEffort string

const (
	ReasoningEffortNone    ReasoningEffort = "none"
	ReasoningEffortMinimal ReasoningEffort = "minimal"
	ReasoningEffortLow     ReasoningEffort = "low"
	ReasoningEffortMedium  ReasoningEffort = "medium"
	ReasoningEffortHigh    ReasoningEffort = "high"
	ReasoningEffortXHigh   ReasoningEffort = "xhigh"
	ReasoningEffortMax     ReasoningEffort = "max"
)

// ReasoningSummary selects the detail level of the model's reasoning summary.
type ReasoningSummary string

const (
	ReasoningSummaryAuto     ReasoningSummary = "auto"
	ReasoningSummaryConcise  ReasoningSummary = "concise"
	ReasoningSummaryDetailed ReasoningSummary = "detailed"
)

// ReasoningMode selects a model-specific reasoning mode.
type ReasoningMode string

const ReasoningModePro ReasoningMode = "pro"

// ReasoningContext selects which prior reasoning items remain relevant.
type ReasoningContext string

const (
	ReasoningContextAuto        ReasoningContext = "auto"
	ReasoningContextCurrentTurn ReasoningContext = "current_turn"
	ReasoningContextAllTurns    ReasoningContext = "all_turns"
)

// TextVerbosity controls the requested detail level of model text output.
type TextVerbosity string

const (
	TextVerbosityLow    TextVerbosity = "low"
	TextVerbosityMedium TextVerbosity = "medium"
	TextVerbosityHigh   TextVerbosity = "high"
)

// PromptCacheOptions configures OpenAI's request-wide prompt-cache policy.
type PromptCacheOptions struct {
	Mode PromptCacheMode
	TTL  PromptCacheTTL
}

// ResponsesReasoningOptions configures Responses API reasoning behavior.
type ResponsesReasoningOptions struct {
	Effort  ReasoningEffort
	Summary ReasoningSummary
	Mode    ReasoningMode
	Context ReasoningContext
}

// ResponsesTextOptions configures Responses API text generation behavior.
type ResponsesTextOptions struct {
	Verbosity TextVerbosity
}

// ResponsesOptions is the typed subset of per-request Responses API options
// that Venat coordinates with its agent loop. ExtraBody remains available for
// forward-compatible OpenAI fields not represented here.
type ResponsesOptions struct {
	MaxOutputTokens int
	// Store controls provider-side response retention. Nil keeps Venat's
	// explicit store:false default; true opts into retention.
	Store              *bool
	PromptCacheKey     string
	PromptCacheOptions *PromptCacheOptions
	Reasoning          *ResponsesReasoningOptions
	Text               *ResponsesTextOptions
}

// ChatCompletionsOptions is the typed subset of per-request Chat Completions
// options used for prompt caching.
type ChatCompletionsOptions struct {
	PromptCacheKey     string
	PromptCacheOptions *PromptCacheOptions
}

// ExtraBody converts options to the provider-specific map accepted by
// agent.Input.ExtraBody and agent.Spec.ExtraBody.
func (options ChatCompletionsOptions) ExtraBody() (map[string]any, error) {
	if err := validatePromptCacheOptions(options.PromptCacheOptions); err != nil {
		return nil, err
	}
	body := make(map[string]any, 2)
	appendPromptCacheOptions(body, options.PromptCacheKey, options.PromptCacheOptions)
	if len(body) == 0 {
		return nil, nil
	}
	return body, nil
}

// ExtraBody converts options to the provider-specific map accepted by
// agent.Input.ExtraBody and agent.Spec.ExtraBody. The returned map is newly
// allocated and safe for the caller to extend.
func (options ResponsesOptions) ExtraBody() (map[string]any, error) {
	if options.MaxOutputTokens < 0 {
		return nil, fmt.Errorf("openai responses max output tokens must not be negative")
	}
	if err := validatePromptCacheOptions(options.PromptCacheOptions); err != nil {
		return nil, err
	}
	if options.Reasoning != nil {
		if err := validateReasoningOptions(*options.Reasoning); err != nil {
			return nil, err
		}
	}
	if options.Text != nil && !validTextVerbosity(options.Text.Verbosity) {
		return nil, fmt.Errorf("openai responses text verbosity %q is invalid", options.Text.Verbosity)
	}

	body := make(map[string]any, 6)
	if options.MaxOutputTokens > 0 {
		body["max_output_tokens"] = options.MaxOutputTokens
	}
	if options.Store != nil {
		body["store"] = *options.Store
	}
	appendPromptCacheOptions(body, options.PromptCacheKey, options.PromptCacheOptions)
	if options.Reasoning != nil {
		reasoning := make(map[string]any, 4)
		if options.Reasoning.Effort != "" {
			reasoning["effort"] = options.Reasoning.Effort
		}
		if options.Reasoning.Summary != "" {
			reasoning["summary"] = options.Reasoning.Summary
		}
		if options.Reasoning.Mode != "" {
			reasoning["mode"] = options.Reasoning.Mode
		}
		if options.Reasoning.Context != "" {
			reasoning["context"] = options.Reasoning.Context
		}
		if len(reasoning) > 0 {
			body["reasoning"] = reasoning
		}
	}
	if options.Text != nil && options.Text.Verbosity != "" {
		body["text"] = map[string]any{"verbosity": options.Text.Verbosity}
	}
	if len(body) == 0 {
		return nil, nil
	}
	return body, nil
}

func appendPromptCacheOptions(body map[string]any, key string, options *PromptCacheOptions) {
	if key != "" {
		body["prompt_cache_key"] = key
	}
	if options == nil {
		return
	}
	cache := make(map[string]any, 2)
	if options.Mode != "" {
		cache["mode"] = options.Mode
	}
	if options.TTL != "" {
		cache["ttl"] = options.TTL
	}
	if len(cache) > 0 {
		body["prompt_cache_options"] = cache
	}
}

func validatePromptCacheOptions(options *PromptCacheOptions) error {
	if options == nil {
		return nil
	}
	switch options.Mode {
	case "", PromptCacheModeImplicit, PromptCacheModeExplicit:
	default:
		return fmt.Errorf("openai prompt cache mode %q is invalid", options.Mode)
	}
	switch options.TTL {
	case "", PromptCacheTTL30Minutes:
		return nil
	default:
		return fmt.Errorf("openai prompt cache TTL %q is invalid", options.TTL)
	}
}

func validateReasoningOptions(options ResponsesReasoningOptions) error {
	switch options.Effort {
	case "", ReasoningEffortNone, ReasoningEffortMinimal, ReasoningEffortLow,
		ReasoningEffortMedium, ReasoningEffortHigh, ReasoningEffortXHigh,
		ReasoningEffortMax:
	default:
		return fmt.Errorf("openai responses reasoning effort %q is invalid", options.Effort)
	}
	switch options.Summary {
	case "", ReasoningSummaryAuto, ReasoningSummaryConcise, ReasoningSummaryDetailed:
	default:
		return fmt.Errorf("openai responses reasoning summary %q is invalid", options.Summary)
	}
	switch options.Mode {
	case "", ReasoningModePro:
	default:
		return fmt.Errorf("openai responses reasoning mode %q is invalid", options.Mode)
	}
	switch options.Context {
	case "", ReasoningContextAuto, ReasoningContextCurrentTurn, ReasoningContextAllTurns:
		return nil
	default:
		return fmt.Errorf("openai responses reasoning context %q is invalid", options.Context)
	}
}

func validTextVerbosity(verbosity TextVerbosity) bool {
	switch verbosity {
	case "", TextVerbosityLow, TextVerbosityMedium, TextVerbosityHigh:
		return true
	default:
		return false
	}
}
