package webhook_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/transport/trigger"
	"github.com/Viking602/go-hydaelyn/transport/webhook"
)

func TestDriver_RegisterAndDispatch(t *testing.T) {
	d := webhook.New(webhook.Options{})
	var seenBody string
	var seenAgent string
	_, err := d.Register(
		api.Trigger{ID: "hook", Type: api.TriggerWebhook, Config: map[string]string{"path": "/hooks/incoming"}},
		"agent-1",
		trigger.HandlerFunc(func(ctx context.Context, tc trigger.TriggerContext) error {
			seenBody = string(tc.Payload)
			seenAgent = tc.AgentID
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	srv := httptest.NewServer(d.Handler())
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/hooks/incoming", "application/json", strings.NewReader(`{"x":1}`))
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}
	if seenBody != `{"x":1}` {
		t.Fatalf("body = %q", seenBody)
	}
	if seenAgent != "agent-1" {
		t.Fatalf("agent = %q", seenAgent)
	}
}

func TestDriver_NotFoundForUnknownPath(t *testing.T) {
	d := webhook.New(webhook.Options{})
	srv := httptest.NewServer(d.Handler())
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/nope", "text/plain", strings.NewReader(""))
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestDriver_RejectsWrongTriggerType(t *testing.T) {
	d := webhook.New(webhook.Options{})
	_, err := d.Register(
		api.Trigger{ID: "x", Type: api.TriggerSchedule, Config: map[string]string{"cron": "* * * * * *"}},
		"agent-1",
		trigger.HandlerFunc(func(ctx context.Context, t trigger.TriggerContext) error { return nil }),
	)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDriver_VerifyTokenGatesRequest(t *testing.T) {
	d := webhook.New(webhook.Options{
		VerifyToken: func(r *http.Request, expected string) bool {
			return r.Header.Get("X-Token") == expected
		},
	})
	_, err := d.Register(
		api.Trigger{
			ID:     "secret",
			Type:   api.TriggerWebhook,
			Config: map[string]string{"path": "/secret", "secret": "hunter2"},
		},
		"agent",
		trigger.HandlerFunc(func(ctx context.Context, t trigger.TriggerContext) error { return nil }),
	)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	srv := httptest.NewServer(d.Handler())
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/secret", strings.NewReader(""))
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing token: expected 401, got %d", resp.StatusCode)
	}

	req2, _ := http.NewRequest(http.MethodPost, srv.URL+"/secret", strings.NewReader(""))
	req2.Header.Set("X-Token", "hunter2")
	resp2, _ := http.DefaultClient.Do(req2)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusAccepted {
		t.Fatalf("with token: expected 202, got %d", resp2.StatusCode)
	}
}

func TestDriver_RejectsSecretWithoutVerifier(t *testing.T) {
	d := webhook.New(webhook.Options{})
	_, err := d.Register(
		api.Trigger{
			ID:     "secret",
			Type:   api.TriggerWebhook,
			Config: map[string]string{"path": "/secret", "secret": "hunter2"},
		},
		"agent",
		trigger.HandlerFunc(func(ctx context.Context, t trigger.TriggerContext) error { return nil }),
	)
	if err == nil {
		t.Fatal("expected secret registration without verifier to fail")
	}
}

func TestDriver_OversizedBodyReturnsTooLarge(t *testing.T) {
	d := webhook.New(webhook.Options{MaxBodyBytes: 4})
	var called bool
	_, err := d.Register(
		api.Trigger{ID: "hook", Type: api.TriggerWebhook, Config: map[string]string{"path": "/hooks/incoming"}},
		"agent-1",
		trigger.HandlerFunc(func(ctx context.Context, tc trigger.TriggerContext) error {
			called = true
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	srv := httptest.NewServer(d.Handler())
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/hooks/incoming", "application/json", strings.NewReader("12345"))
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d", resp.StatusCode)
	}
	if called {
		t.Fatal("handler should not be called for oversized body")
	}
}

func TestDriver_HandlerErrorDoesNotExposeSecretDetails(t *testing.T) {
	d := webhook.New(webhook.Options{})
	_, err := d.Register(
		api.Trigger{ID: "hook", Type: api.TriggerWebhook, Config: map[string]string{"path": "/hooks/incoming"}},
		"agent-1",
		trigger.HandlerFunc(func(ctx context.Context, tc trigger.TriggerContext) error {
			return errors.New("internal secret token leaked")
		}),
	)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	srv := httptest.NewServer(d.Handler())
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/hooks/incoming", "application/json", strings.NewReader(`{"x":1}`))
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
	if strings.Contains(string(body), "internal secret token") {
		t.Fatalf("handler error leaked in response body: %q", body)
	}
}
