package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"hydaelyn/host"
	"hydaelyn/message"
	"hydaelyn/provider"
	"hydaelyn/session"
)

type fakeProvider struct{}

func (fakeProvider) Metadata() provider.Metadata {
	return provider.Metadata{Name: "fake"}
}

func (fakeProvider) Stream(_ context.Context, _ provider.Request) (provider.Stream, error) {
	return provider.NewSliceStream([]provider.Event{
		{Kind: provider.EventTextDelta, Text: "hello"},
		{Kind: provider.EventDone, StopReason: provider.StopReasonComplete},
	}), nil
}

func TestAdminHandlerPrompt(t *testing.T) {
	runtime := host.New(host.Config{})
	runtime.RegisterProvider("fake", fakeProvider{})
	sess, err := runtime.CreateSession(context.Background(), session.CreateParams{Branch: "main"})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	server := New(runtime)
	body, _ := json.Marshal(map[string]any{
		"provider": "fake",
		"model":    "test",
		"messages": []message.Message{message.NewText(message.RoleUser, "hello")},
	})
	request := httptest.NewRequest(http.MethodPost, "/sessions/"+sess.ID+"/prompt", bytes.NewReader(body))
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
}
