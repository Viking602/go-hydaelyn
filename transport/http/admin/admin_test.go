package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Viking602/go-hydaelyn/host"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/patterns/deepsearch"
	"github.com/Viking602/go-hydaelyn/plugin"
	"github.com/Viking602/go-hydaelyn/provider"
	"github.com/Viking602/go-hydaelyn/scheduler"
	"github.com/Viking602/go-hydaelyn/session"
	"github.com/Viking602/go-hydaelyn/team"
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

func TestAdminHandlerTeamRoutes(t *testing.T) {
	runtime := host.New(host.Config{})
	queue := scheduler.NewMemoryQueue()
	if err := runtime.RegisterPlugin(plugin.Spec{
		Type:      plugin.TypeScheduler,
		Name:      "memory",
		Component: queue,
	}); err != nil {
		t.Fatalf("RegisterPlugin() error = %v", err)
	}
	runtime.RegisterProvider("fake", fakeProvider{})
	runtime.RegisterPattern(deepsearch.New())
	runtime.RegisterProfile(team.Profile{Name: "supervisor", Role: team.RoleSupervisor, Provider: "fake", Model: "test"})
	runtime.RegisterProfile(team.Profile{Name: "researcher", Role: team.RoleResearcher, Provider: "fake", Model: "test"})
	state, err := runtime.StartTeam(context.Background(), host.StartTeamRequest{
		Pattern:           "deepsearch",
		SupervisorProfile: "supervisor",
		WorkerProfiles:    []string{"researcher"},
		Input:             map[string]any{"query": "hello"},
	})
	if err != nil {
		t.Fatalf("StartTeam() error = %v", err)
	}
	server := New(runtime)

	listReq := httptest.NewRequest(http.MethodGet, "/teams", nil)
	listRes := httptest.NewRecorder()
	server.Handler().ServeHTTP(listRes, listReq)
	if listRes.Code != http.StatusOK {
		t.Fatalf("expected 200 on list teams, got %d: %s", listRes.Code, listRes.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/teams/"+state.ID, nil)
	getRes := httptest.NewRecorder()
	server.Handler().ServeHTTP(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("expected 200 on get team, got %d: %s", getRes.Code, getRes.Body.String())
	}

	replayReq := httptest.NewRequest(http.MethodPost, "/teams/"+state.ID+"/replay", nil)
	replayRes := httptest.NewRecorder()
	server.Handler().ServeHTTP(replayRes, replayReq)
	if replayRes.Code != http.StatusOK {
		t.Fatalf("expected 200 on replay team, got %d: %s", replayRes.Code, replayRes.Body.String())
	}

	eventsReq := httptest.NewRequest(http.MethodGet, "/teams/"+state.ID+"/events", nil)
	eventsRes := httptest.NewRecorder()
	server.Handler().ServeHTTP(eventsRes, eventsReq)
	if eventsRes.Code != http.StatusOK {
		t.Fatalf("expected 200 on team events, got %d: %s", eventsRes.Code, eventsRes.Body.String())
	}

	abortReq := httptest.NewRequest(http.MethodPost, "/teams/"+state.ID+"/abort", nil)
	abortRes := httptest.NewRecorder()
	server.Handler().ServeHTTP(abortRes, abortReq)
	if abortRes.Code != http.StatusOK {
		t.Fatalf("expected 200 on abort team, got %d: %s", abortRes.Code, abortRes.Body.String())
	}

	_ = queue.Enqueue(context.Background(), scheduler.TaskLease{
		TaskID:    "task-x",
		TeamID:    state.ID,
		OwnerID:   "worker",
		ExpiresAt: time.Now().Add(-time.Minute),
	})
	recoverReq := httptest.NewRequest(http.MethodPost, "/scheduler/recover", nil)
	recoverRes := httptest.NewRecorder()
	server.Handler().ServeHTTP(recoverRes, recoverReq)
	if recoverRes.Code != http.StatusOK {
		t.Fatalf("expected 200 on scheduler recover, got %d: %s", recoverRes.Code, recoverRes.Body.String())
	}

	drainReq := httptest.NewRequest(http.MethodPost, "/scheduler/drain", nil)
	drainRes := httptest.NewRecorder()
	server.Handler().ServeHTTP(drainRes, drainReq)
	if drainRes.Code != http.StatusOK {
		t.Fatalf("expected 200 on scheduler drain, got %d: %s", drainRes.Code, drainRes.Body.String())
	}
}
