package host

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/internal/session"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/provider/openai"
)

func TestPromptLiveOpenAICompatibleExtraBodyEnablesThinking(t *testing.T) {
	cfg := liveOpenAIThinkingConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()

	requests := &liveRequestRecorder{base: http.DefaultTransport}
	runner := New(Config{})
	runner.RegisterProvider("live-openai", openai.New(openai.Config{
		APIKey:  cfg.apiKey,
		BaseURL: cfg.baseURL,
		Client:  &http.Client{Transport: requests},
	}))
	current, err := runner.CreateSession(ctx, session.CreateParams{Branch: "live-openai-thinking"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	response, err := runner.Prompt(ctx, PromptRequest{
		SessionID: current.ID,
		Provider:  "live-openai",
		Model:     cfg.model,
		Messages: []message.Message{
			message.NewText(message.RoleUser, cfg.prompt),
		},
		Agent: AgentOptions{
			MaxIterations: 1,
			ExtraBody:     cfg.extraBody,
		},
	})
	if err != nil {
		t.Fatalf("Prompt() error = %v; request body=%s", err, previewRunes(requests.body, 800))
	}
	requireLiveExtraBodyForwarded(t, requests.body, cfg.extraBody)

	thinking := responseThinking(response)
	if strings.TrimSpace(thinking) == "" {
		t.Fatalf("extra body was forwarded, but no thinking was emitted; request body=%s; answer preview=%q", previewRunes(requests.body, 800), previewRunes(response.UserFacingAnswer, 240))
	}
	t.Logf("thinking chars=%d answer preview=%q", len([]rune(thinking)), previewRunes(response.UserFacingAnswer, 240))
}

type liveRequestRecorder struct {
	base http.RoundTripper
	body string
}

func (r *liveRequestRecorder) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Body != nil {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		r.body = string(body)
		request.Body = io.NopCloser(bytes.NewReader(body))
		request.ContentLength = int64(len(body))
	}
	base := r.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(request)
}

type openAIThinkingConfig struct {
	baseURL   string
	apiKey    string
	model     string
	prompt    string
	timeout   time.Duration
	extraBody map[string]any
}

func liveOpenAIThinkingConfig(t *testing.T) openAIThinkingConfig {
	t.Helper()
	cfg := openAIThinkingConfig{
		baseURL: normalizeOpenAIBaseURL(os.Getenv("HYDAELYN_OPENAI_THINKING_BASE_URL")),
		apiKey:  strings.TrimSpace(os.Getenv("HYDAELYN_OPENAI_THINKING_API_KEY")),
		model:   strings.TrimSpace(os.Getenv("HYDAELYN_OPENAI_THINKING_MODEL")),
		prompt:  strings.TrimSpace(os.Getenv("HYDAELYN_OPENAI_THINKING_PROMPT")),
		timeout: 90 * time.Second,
		extraBody: map[string]any{
			"chat_template_kwargs": map[string]any{
				"thinking": true,
			},
		},
	}
	if cfg.apiKey == "" {
		cfg.apiKey = strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	}
	if cfg.prompt == "" {
		cfg.prompt = "请用中文简短回答：37 * 43 等于多少？"
	}
	if raw := strings.TrimSpace(os.Getenv("HYDAELYN_OPENAI_THINKING_EXTRA_BODY_JSON")); raw != "" {
		cfg.extraBody = decodeLiveExtraBody(t, raw)
	}
	if cfg.baseURL == "" || cfg.model == "" || cfg.apiKey == "" {
		t.Skip("set HYDAELYN_OPENAI_THINKING_BASE_URL, HYDAELYN_OPENAI_THINKING_MODEL, and HYDAELYN_OPENAI_THINKING_API_KEY to run this live test")
	}
	return cfg
}

func decodeLiveExtraBody(t *testing.T, raw string) map[string]any {
	t.Helper()
	var extraBody map[string]any
	if err := json.Unmarshal([]byte(raw), &extraBody); err != nil {
		t.Fatalf("HYDAELYN_OPENAI_THINKING_EXTRA_BODY_JSON must be a JSON object: %v", err)
	}
	return extraBody
}

func normalizeOpenAIBaseURL(value string) string {
	baseURL := strings.TrimRight(strings.TrimSpace(value), "/")
	return strings.TrimSuffix(baseURL, "/chat/completions")
}

func requireLiveExtraBodyForwarded(t *testing.T, requestBody string, extraBody map[string]any) {
	t.Helper()
	if strings.TrimSpace(requestBody) == "" {
		t.Fatal("expected to capture live OpenAI-compatible request body")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(requestBody), &payload); err != nil {
		t.Fatalf("live request body is not JSON: %v; body=%s", err, previewRunes(requestBody, 800))
	}
	for key, want := range extraBody {
		got, ok := payload[key]
		if !ok {
			t.Fatalf("expected live request body to include extra field %q; body=%s", key, previewRunes(requestBody, 800))
		}
		if !reflect.DeepEqual(normalizeJSONValue(t, want), got) {
			t.Fatalf("unexpected live request body field %q: got %#v want %#v; body=%s", key, got, want, previewRunes(requestBody, 800))
		}
	}
}

func normalizeJSONValue(t *testing.T, value any) any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("cannot marshal expected JSON value: %v", err)
	}
	var normalized any
	if err := json.Unmarshal(data, &normalized); err != nil {
		t.Fatalf("cannot normalize expected JSON value: %v", err)
	}
	return normalized
}

func responseThinking(response PromptResponse) string {
	var thinking strings.Builder
	for _, msg := range response.Messages {
		thinking.WriteString(msg.Thinking)
	}
	return thinking.String()
}

func previewRunes(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "..."
}
