package webhook_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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

func TestDriver_LegacyVerifyTokenStillWorks(t *testing.T) {
	d := webhook.New(webhook.Options{
		VerifyToken: func(r *http.Request, expected string) bool {
			return r.Header.Get("X-Token") == expected
		},
	})
	_, err := d.Register(
		api.Trigger{ID: "legacy", Type: api.TriggerWebhook, Config: map[string]string{"path": "/legacy", "secret": "hunter2"}},
		"agent",
		trigger.HandlerFunc(func(ctx context.Context, t trigger.TriggerContext) error { return nil }),
	)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	srv := httptest.NewServer(d.Handler())
	defer srv.Close()

	// No token -> 401.
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/legacy", strings.NewReader(""))
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing token: expected 401, got %d", resp.StatusCode)
	}

	// With token -> 202. Body is preserved (still readable by handler).
	var seenBody string
	d2 := webhook.New(webhook.Options{
		VerifyToken: func(r *http.Request, expected string) bool {
			return r.Header.Get("X-Token") == expected
		},
	})
	_, err = d2.Register(
		api.Trigger{ID: "legacy2", Type: api.TriggerWebhook, Config: map[string]string{"path": "/legacy", "secret": "hunter2"}},
		"agent",
		trigger.HandlerFunc(func(ctx context.Context, tc trigger.TriggerContext) error {
			seenBody = string(tc.Payload)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("Register d2: %v", err)
	}
	srv2 := httptest.NewServer(d2.Handler())
	defer srv2.Close()

	req2, _ := http.NewRequest(http.MethodPost, srv2.URL+"/legacy", strings.NewReader(`{"x":1}`))
	req2.Header.Set("X-Token", "hunter2")
	resp2, _ := http.DefaultClient.Do(req2)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusAccepted {
		t.Fatalf("with token: expected 202, got %d", resp2.StatusCode)
	}
	if seenBody != `{"x":1}` {
		t.Fatalf("legacy path dropped body: got %q", seenBody)
	}
}

func TestDriver_VerifyRequestRunsAfterBodyRead(t *testing.T) {
	// VerifyRequest must receive the raw body bytes; this proves it runs
	// AFTER the body read, not before.
	var gotBody []byte
	var called bool
	d := webhook.New(webhook.Options{
		VerifyRequest: func(r *http.Request, body []byte, expected string) bool {
			called = true
			gotBody = body
			return string(body) == expected
		},
	})
	const secret = `{"x":1}`
	_, err := d.Register(
		api.Trigger{ID: "vr", Type: api.TriggerWebhook, Config: map[string]string{"path": "/vr", "secret": secret}},
		"agent",
		trigger.HandlerFunc(func(ctx context.Context, t trigger.TriggerContext) error { return nil }),
	)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	srv := httptest.NewServer(d.Handler())
	defer srv.Close()

	// Mismatched body -> verifier returns false -> 401.
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/vr", strings.NewReader(`{"x":2}`))
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if !called {
		t.Fatal("VerifyRequest was not invoked")
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("tampered body: expected 401, got %d", resp.StatusCode)
	}
	if string(gotBody) != `{"x":2}` {
		t.Fatalf("verifier did not receive raw body: got %q", gotBody)
	}

	// Matching body -> 202.
	called = false
	req2, _ := http.NewRequest(http.MethodPost, srv.URL+"/vr", strings.NewReader(secret))
	resp2, _ := http.DefaultClient.Do(req2)
	resp2.Body.Close()
	if !called {
		t.Fatal("VerifyRequest not invoked on second request")
	}
	if resp2.StatusCode != http.StatusAccepted {
		t.Fatalf("matching body: expected 202, got %d", resp2.StatusCode)
	}
}

func TestDriver_VerifyHMACAcceptsValidSignature(t *testing.T) {
	const secret = "shared-secret"
	d := webhook.New(webhook.Options{
		VerifyRequest: webhook.VerifyHMAC(secret, "X-Signature"),
	})
	var seenBody string
	_, err := d.Register(
		api.Trigger{ID: "hmac-ok", Type: api.TriggerWebhook, Config: map[string]string{"path": "/hmac", "secret": secret}},
		"agent",
		trigger.HandlerFunc(func(ctx context.Context, tc trigger.TriggerContext) error {
			seenBody = string(tc.Payload)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	srv := httptest.NewServer(d.Handler())
	defer srv.Close()

	body := []byte(`{"event":"push","ref":"refs/heads/main"}`)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/hmac", bytes.NewReader(body))
	req.Header.Set("X-Signature", sig)
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("valid HMAC: expected 202, got %d", resp.StatusCode)
	}
	if seenBody != string(body) {
		t.Fatalf("body not forwarded to handler: got %q", seenBody)
	}
}

func TestDriver_VerifyHMACUsesTriggerSecretAndGitHubPrefix(t *testing.T) {
	const secret = "trigger-secret"
	d := webhook.New(webhook.Options{
		VerifyRequest: webhook.VerifyHMAC("", "X-Hub-Signature-256"),
	})
	_, err := d.Register(
		api.Trigger{ID: "hmac-prefix", Type: api.TriggerWebhook, Config: map[string]string{"path": "/hmac-prefix", "secret": secret}},
		"agent",
		trigger.HandlerFunc(func(ctx context.Context, tc trigger.TriggerContext) error {
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	srv := httptest.NewServer(d.Handler())
	defer srv.Close()

	body := []byte(`{"event":"push"}`)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/hmac-prefix", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", sig)
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("prefixed HMAC: expected 202, got %d", resp.StatusCode)
	}
}

func TestDriver_VerifyHMACRejectsTamperedBody(t *testing.T) {
	const secret = "shared-secret"
	d := webhook.New(webhook.Options{
		VerifyRequest: webhook.VerifyHMAC(secret, "X-Signature"),
	})
	var handlerCalled bool
	_, err := d.Register(
		api.Trigger{ID: "hmac-tamper", Type: api.TriggerWebhook, Config: map[string]string{"path": "/hmac", "secret": secret}},
		"agent",
		trigger.HandlerFunc(func(ctx context.Context, tc trigger.TriggerContext) error {
			handlerCalled = true
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	srv := httptest.NewServer(d.Handler())
	defer srv.Close()

	// Compute signature for the original body, then send a DIFFERENT body.
	orig := []byte(`{"event":"push"}`)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(orig)
	sig := hex.EncodeToString(mac.Sum(nil))

	tampered := []byte(`{"event":"push","evil":true}`)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/hmac", bytes.NewReader(tampered))
	req.Header.Set("X-Signature", sig)
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("tampered body: expected 401, got %d", resp.StatusCode)
	}
	if handlerCalled {
		t.Fatal("handler must not run when HMAC verification fails")
	}
}
