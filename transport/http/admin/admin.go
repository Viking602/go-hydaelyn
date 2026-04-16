package admin

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Viking602/go-hydaelyn/host"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/session"
	"github.com/Viking602/go-hydaelyn/tool"
)

type Server struct {
	runtime *host.Runtime
}

func New(runtime *host.Runtime) *Server {
	return &Server{runtime: runtime}
}

func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/teams":
			handleListTeams(s, writer, request)
		case request.Method == http.MethodPost && request.URL.Path == "/sessions":
			handleCreateSession(s, writer, request)
		case strings.HasPrefix(request.URL.Path, "/sessions/"):
			handleSessionRoute(s, writer, request)
		case strings.HasPrefix(request.URL.Path, "/teams/"):
			handleTeamRoute(s, writer, request)
		case request.Method == http.MethodPost && request.URL.Path == "/scheduler/drain":
			handleSchedulerDrain(s, writer, request)
		case request.Method == http.MethodPost && request.URL.Path == "/scheduler/recover":
			handleSchedulerRecover(s, writer, request)
		case request.Method == http.MethodPost && strings.HasPrefix(request.URL.Path, "/runs/"):
			handleAbortRun(s, writer, request)
		default:
			writer.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(writer).Encode(map[string]string{"error": "not found"})
		}
	})
}

func handleListTeams(s *Server, writer http.ResponseWriter, request *http.Request) {
	items, err := s.runtime.ListTeams(request.Context())
	writeJSON(writer, items, err)
}

func handleCreateSession(s *Server, writer http.ResponseWriter, request *http.Request) {
	var body struct {
		Branch   string            `json:"branch"`
		Metadata map[string]string `json:"metadata"`
	}
	_ = json.NewDecoder(request.Body).Decode(&body)
	current, err := s.runtime.CreateSession(request.Context(), session.CreateParams{
		Branch:   body.Branch,
		Metadata: body.Metadata,
	})
	writeJSON(writer, current, err)
}

func handleSessionRoute(s *Server, writer http.ResponseWriter, request *http.Request) {
	parts := strings.Split(strings.TrimPrefix(request.URL.Path, "/sessions/"), "/")
	if len(parts) == 1 {
		if request.Method != http.MethodGet {
			writer.WriteHeader(http.StatusMethodNotAllowed)
			_ = json.NewEncoder(writer).Encode(map[string]string{"error": "method not allowed"})
			return
		}
		current, err := s.runtime.GetSession(request.Context(), parts[0])
		writeJSON(writer, current, err)
		return
	}
	handleSessionAction(s, writer, request, parts)
}

func handleTeamRoute(s *Server, writer http.ResponseWriter, request *http.Request) {
	parts := strings.Split(strings.TrimPrefix(request.URL.Path, "/teams/"), "/")
	handleTeamAction(s, writer, request, parts)
}

func handleSchedulerDrain(s *Server, writer http.ResponseWriter, request *http.Request) {
	processed, err := s.runtime.RunQueueWorker(request.Context(), 100)
	writeJSON(writer, map[string]any{"processed": processed}, err)
}

func handleSchedulerRecover(s *Server, writer http.ResponseWriter, request *http.Request) {
	err := s.runtime.RecoverQueueLeases(request.Context(), time.Now())
	writeJSON(writer, map[string]string{"status": "recovered"}, err)
}

func handleAbortRun(s *Server, writer http.ResponseWriter, request *http.Request) {
	runID := strings.TrimSuffix(strings.TrimPrefix(request.URL.Path, "/runs/"), "/abort")
	err := s.runtime.AbortRun(request.Context(), runID)
	writeJSON(writer, map[string]string{"status": "aborted"}, err)
}

func handleTeamAction(s *Server, writer http.ResponseWriter, request *http.Request, parts []string) {
	teamID := parts[0]
	switch {
	case request.Method == http.MethodGet && len(parts) == 1:
		handleGetTeam(s, writer, request, teamID)
	case request.Method == http.MethodGet && len(parts) == 2 && parts[1] == "events":
		handleGetTeamEvents(s, writer, request, teamID)
	case request.Method == http.MethodPost && len(parts) == 2 && parts[1] == "resume":
		handleResumeTeam(s, writer, request, teamID)
	case request.Method == http.MethodPost && len(parts) == 2 && parts[1] == "replay":
		handleReplayTeam(s, writer, request, teamID)
	case request.Method == http.MethodPost && len(parts) == 2 && parts[1] == "abort":
		handleAbortTeam(s, writer, request, teamID)
	default:
		writer.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(writer).Encode(map[string]string{"error": "not found"})
	}
}

func handleGetTeam(s *Server, writer http.ResponseWriter, request *http.Request, teamID string) {
	current, err := s.runtime.GetTeam(request.Context(), teamID)
	writeJSON(writer, current, err)
}

func handleGetTeamEvents(s *Server, writer http.ResponseWriter, request *http.Request, teamID string) {
	events, err := s.runtime.TeamEvents(request.Context(), teamID)
	writeJSON(writer, events, err)
}

func handleResumeTeam(s *Server, writer http.ResponseWriter, request *http.Request, teamID string) {
	current, err := s.runtime.ResumeTeam(request.Context(), teamID)
	writeJSON(writer, current, err)
}

func handleReplayTeam(s *Server, writer http.ResponseWriter, request *http.Request, teamID string) {
	current, err := s.runtime.ReplayTeamState(request.Context(), teamID)
	writeJSON(writer, current, err)
}

func handleAbortTeam(s *Server, writer http.ResponseWriter, request *http.Request, teamID string) {
	err := s.runtime.AbortTeam(request.Context(), teamID)
	writeJSON(writer, map[string]string{"status": "aborted"}, err)
}

func handleSessionAction(s *Server, writer http.ResponseWriter, request *http.Request, parts []string) {
	sessionID := parts[0]
	switch {
	case request.Method == http.MethodPost && len(parts) == 2 && parts[1] == "prompt":
		var body struct {
			Provider string            `json:"provider"`
			Model    string            `json:"model"`
			Messages []message.Message `json:"messages"`
			ToolMode tool.Mode         `json:"toolMode"`
			Metadata map[string]string `json:"metadata"`
		}
		_ = json.NewDecoder(request.Body).Decode(&body)
		response, err := s.runtime.Prompt(request.Context(), host.PromptRequest{
			SessionID: sessionID,
			Provider:  body.Provider,
			Model:     body.Model,
			Messages:  body.Messages,
			ToolMode:  body.ToolMode,
			Metadata:  body.Metadata,
		})
		writeJSON(writer, response, err)
	case request.Method == http.MethodPost && len(parts) == 2 && parts[1] == "continue":
		var body struct {
			Provider string            `json:"provider"`
			Model    string            `json:"model"`
			ToolMode tool.Mode         `json:"toolMode"`
			Metadata map[string]string `json:"metadata"`
		}
		_ = json.NewDecoder(request.Body).Decode(&body)
		response, err := s.runtime.Continue(request.Context(), host.ContinueRequest{
			SessionID: sessionID,
			Provider:  body.Provider,
			Model:     body.Model,
			ToolMode:  body.ToolMode,
			Metadata:  body.Metadata,
		})
		writeJSON(writer, response, err)
	default:
		writer.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(writer).Encode(map[string]string{"error": "not found"})
	}
}

func writeJSON(writer http.ResponseWriter, payload any, err error) {
	if err != nil {
		writer.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(writer).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(writer).Encode(payload)
}
