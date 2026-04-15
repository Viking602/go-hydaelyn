package session

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Viking602/go-hydaelyn/message"
)

var ErrSessionNotFound = errors.New("session not found")

type Session struct {
	ID          string             `json:"id"`
	ParentID    string             `json:"parentId,omitempty"`
	Branch      string             `json:"branch,omitempty"`
	HeadEntryID string             `json:"headEntryId,omitempty"`
	TeamID      string             `json:"teamId,omitempty"`
	AgentID     string             `json:"agentId,omitempty"`
	Scope       message.Visibility `json:"scope,omitempty"`
	Metadata    map[string]string  `json:"metadata,omitempty"`
	CreatedAt   time.Time          `json:"createdAt"`
	UpdatedAt   time.Time          `json:"updatedAt"`
}

type Entry struct {
	ID        string          `json:"id"`
	SessionID string          `json:"sessionId"`
	ParentID  string          `json:"parentId,omitempty"`
	Message   message.Message `json:"message"`
	CreatedAt time.Time       `json:"createdAt"`
}

type Snapshot struct {
	Session  Session           `json:"session"`
	Entries  []Entry           `json:"entries"`
	Messages []message.Message `json:"messages"`
}

type CreateParams struct {
	ID       string             `json:"id,omitempty"`
	ParentID string             `json:"parentId,omitempty"`
	Branch   string             `json:"branch,omitempty"`
	TeamID   string             `json:"teamId,omitempty"`
	AgentID  string             `json:"agentId,omitempty"`
	Scope    message.Visibility `json:"scope,omitempty"`
	Metadata map[string]string  `json:"metadata,omitempty"`
}

type Store interface {
	Create(ctx context.Context, params CreateParams) (Session, error)
	Append(ctx context.Context, sessionID string, messages ...message.Message) ([]Entry, error)
	Load(ctx context.Context, sessionID string) (Snapshot, error)
	Branch(ctx context.Context, sessionID string, branch string) (Session, error)
	List(ctx context.Context) ([]Session, error)
}

type MemoryStore struct {
	mu       sync.RWMutex
	sessions map[string]Session
	entries  map[string][]Entry
	seq      uint64
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		sessions: map[string]Session{},
		entries:  map[string][]Entry{},
	}
}

func (s *MemoryStore) nextID(prefix string) string {
	s.seq++
	return fmt.Sprintf("%s-%d", prefix, s.seq)
}

func (s *MemoryStore) Create(_ context.Context, params CreateParams) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := params.ID
	if id == "" {
		id = s.nextID("session")
	}
	now := time.Now().UTC()
	session := Session{
		ID:        id,
		ParentID:  params.ParentID,
		Branch:    params.Branch,
		TeamID:    params.TeamID,
		AgentID:   params.AgentID,
		Scope:     params.Scope,
		Metadata:  params.Metadata,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if session.Scope == "" {
		session.Scope = message.VisibilityShared
	}
	s.sessions[id] = session
	return session, nil
}

func (s *MemoryStore) Append(_ context.Context, sessionID string, messages ...message.Message) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.sessions[sessionID]
	if !ok {
		return nil, ErrSessionNotFound
	}
	existing := s.entries[sessionID]
	parentID := current.HeadEntryID
	items := make([]Entry, 0, len(messages))
	for _, msg := range messages {
		entry := Entry{
			ID:        s.nextID("entry"),
			SessionID: sessionID,
			ParentID:  parentID,
			Message:   msg,
			CreatedAt: time.Now().UTC(),
		}
		items = append(items, entry)
		existing = append(existing, entry)
		parentID = entry.ID
	}
	current.HeadEntryID = parentID
	current.UpdatedAt = time.Now().UTC()
	s.entries[sessionID] = existing
	s.sessions[sessionID] = current
	return items, nil
}

func (s *MemoryStore) Load(_ context.Context, sessionID string) (Snapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	current, ok := s.sessions[sessionID]
	if !ok {
		return Snapshot{}, ErrSessionNotFound
	}
	entries := append([]Entry{}, s.entries[sessionID]...)
	messages := make([]message.Message, 0, len(entries))
	for _, entry := range entries {
		messages = append(messages, entry.Message)
	}
	return Snapshot{
		Session:  current,
		Entries:  entries,
		Messages: messages,
	}, nil
}

func (s *MemoryStore) Branch(ctx context.Context, sessionID string, branch string) (Session, error) {
	original, err := s.Load(ctx, sessionID)
	if err != nil {
		return Session{}, err
	}
	child, err := s.Create(ctx, CreateParams{
		ParentID: sessionID,
		Branch:   branch,
		TeamID:   original.Session.TeamID,
		AgentID:  original.Session.AgentID,
		Scope:    original.Session.Scope,
		Metadata: original.Session.Metadata,
	})
	if err != nil {
		return Session{}, err
	}
	if len(original.Messages) > 0 {
		if _, err := s.Append(ctx, child.ID, original.Messages...); err != nil {
			return Session{}, err
		}
	}
	return child, nil
}

func (s *MemoryStore) List(_ context.Context) ([]Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]Session, 0, len(s.sessions))
	for _, current := range s.sessions {
		items = append(items, current)
	}
	return items, nil
}
