package team

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type InteractionMode string

const (
	InteractionModeDAGOnly InteractionMode = "dag_only"
	InteractionModePanel   InteractionMode = "panel"
	InteractionModeDebate  InteractionMode = "debate"
)

type TodoStatus string

const (
	TodoStatusOpen      TodoStatus = "open"
	TodoStatusClaimed   TodoStatus = "claimed"
	TodoStatusRunning   TodoStatus = "running"
	TodoStatusReviewing TodoStatus = "reviewing"
	TodoStatusVerified  TodoStatus = "verified"
	TodoStatusBlocked   TodoStatus = "blocked"
	TodoStatusCompleted TodoStatus = "completed"
)

type TodoPriority string

const (
	TodoPriorityLow    TodoPriority = "low"
	TodoPriorityNormal TodoPriority = "normal"
	TodoPriorityHigh   TodoPriority = "high"
	TodoPriorityUrgent TodoPriority = "urgent"
)

type TodoVerificationPolicy struct {
	Required      bool    `json:"required,omitempty"`
	Mode          string  `json:"mode,omitempty"`
	MinConfidence float64 `json:"minConfidence,omitempty"`
	Reviewers     int     `json:"reviewers,omitempty"`
}

type TodoItem struct {
	ID                   string                 `json:"id"`
	Title                string                 `json:"title,omitempty"`
	Input                string                 `json:"input,omitempty"`
	Domain               string                 `json:"domain,omitempty"`
	RequiredCapabilities []string               `json:"requiredCapabilities,omitempty"`
	Priority             TodoPriority           `json:"priority,omitempty"`
	Dependencies         []string               `json:"dependencies,omitempty"`
	ExpectedReportKind   ReportKind             `json:"expectedReportKind,omitempty"`
	VerificationPolicy   TodoVerificationPolicy `json:"verificationPolicy,omitempty"`
	Status               TodoStatus             `json:"status,omitempty"`
	PrimaryAgentID       string                 `json:"primaryAgentId,omitempty"`
	ReviewerAgentIDs     []string               `json:"reviewerAgentIds,omitempty"`
	TaskID               string                 `json:"taskId,omitempty"`
	ReviewTaskIDs        []string               `json:"reviewTaskIds,omitempty"`
	VerificationTaskID   string                 `json:"verificationTaskId,omitempty"`
	ClaimedAt            time.Time              `json:"claimedAt,omitempty"`
	UpdatedAt            time.Time              `json:"updatedAt,omitempty"`
}

type TodoPlan struct {
	ID        string     `json:"id,omitempty"`
	Goal      string     `json:"goal,omitempty"`
	Items     []TodoItem `json:"items,omitempty"`
	CreatedAt time.Time  `json:"createdAt,omitempty"`
	UpdatedAt time.Time  `json:"updatedAt,omitempty"`
}

type TaskBoard struct {
	Plan TodoPlan `json:"plan"`
}

type AgentCapability struct {
	AgentID        string   `json:"agentId"`
	Roles          []Role   `json:"roles,omitempty"`
	Domains        []string `json:"domains,omitempty"`
	Tools          []string `json:"tools,omitempty"`
	CostProfile    string   `json:"costProfile,omitempty"`
	LatencyClass   string   `json:"latencyClass,omitempty"`
	Reliability    float64  `json:"reliability,omitempty"`
	MaxConcurrency int      `json:"maxConcurrency,omitempty"`
}

type ClaimOptions struct {
	RequireDomainMatch bool
	MaxActivePerAgent  int
}

var (
	ErrTodoNotFound       = errors.New("todo not found")
	ErrTodoAlreadyClaimed = errors.New("todo already claimed")
	ErrTodoCapability     = errors.New("todo capability mismatch")
	ErrTodoConcurrency    = errors.New("todo agent concurrency limit reached")
)

func (b *TaskBoard) Claim(todoID string, agent AgentCapability, opts ClaimOptions) (TodoItem, error) {
	if b == nil {
		return TodoItem{}, ErrTodoNotFound
	}
	idx := b.todoIndex(todoID)
	if idx < 0 {
		return TodoItem{}, ErrTodoNotFound
	}
	item := b.Plan.Items[idx]
	if item.Status == "" {
		item.Status = TodoStatusOpen
	}
	if item.Status != TodoStatusOpen {
		return TodoItem{}, fmt.Errorf("%w: %s status=%s", ErrTodoAlreadyClaimed, todoID, item.Status)
	}
	if strings.TrimSpace(agent.AgentID) == "" {
		return TodoItem{}, fmt.Errorf("%w: empty agent id", ErrTodoCapability)
	}
	if opts.RequireDomainMatch && !agentCoversDomain(agent, item.Domain) {
		return TodoItem{}, fmt.Errorf("%w: domain %s", ErrTodoCapability, item.Domain)
	}
	if !agentHasCapabilities(agent, item.RequiredCapabilities) {
		return TodoItem{}, fmt.Errorf("%w: required %v", ErrTodoCapability, item.RequiredCapabilities)
	}
	if opts.MaxActivePerAgent > 0 && b.activeClaims(agent.AgentID) >= opts.MaxActivePerAgent {
		return TodoItem{}, ErrTodoConcurrency
	}
	now := time.Now().UTC()
	item.Status = TodoStatusClaimed
	item.PrimaryAgentID = agent.AgentID
	item.ClaimedAt = now
	item.UpdatedAt = now
	b.Plan.Items[idx] = item
	b.Plan.UpdatedAt = now
	return item, nil
}

func (b *TaskBoard) SetStatus(todoID string, status TodoStatus) error {
	if b == nil {
		return ErrTodoNotFound
	}
	idx := b.todoIndex(todoID)
	if idx < 0 {
		return ErrTodoNotFound
	}
	b.Plan.Items[idx].Status = status
	b.Plan.Items[idx].UpdatedAt = time.Now().UTC()
	b.Plan.UpdatedAt = b.Plan.Items[idx].UpdatedAt
	return nil
}

func (b *TaskBoard) SetReviewers(todoID string, reviewerIDs []string, taskIDs []string) error {
	if b == nil {
		return ErrTodoNotFound
	}
	idx := b.todoIndex(todoID)
	if idx < 0 {
		return ErrTodoNotFound
	}
	b.Plan.Items[idx].ReviewerAgentIDs = append([]string{}, reviewerIDs...)
	b.Plan.Items[idx].ReviewTaskIDs = append([]string{}, taskIDs...)
	b.Plan.Items[idx].Status = TodoStatusReviewing
	b.Plan.Items[idx].UpdatedAt = time.Now().UTC()
	b.Plan.UpdatedAt = b.Plan.Items[idx].UpdatedAt
	return nil
}

func (b *TaskBoard) Items() []TodoItem {
	if b == nil {
		return nil
	}
	out := make([]TodoItem, len(b.Plan.Items))
	copy(out, b.Plan.Items)
	return out
}

func (b *TaskBoard) todoIndex(todoID string) int {
	for idx, item := range b.Plan.Items {
		if item.ID == todoID {
			return idx
		}
	}
	return -1
}

func (b *TaskBoard) activeClaims(agentID string) int {
	count := 0
	for _, item := range b.Plan.Items {
		if item.PrimaryAgentID != agentID {
			continue
		}
		switch item.Status {
		case TodoStatusClaimed, TodoStatusRunning, TodoStatusReviewing:
			count++
		}
	}
	return count
}

func agentCoversDomain(agent AgentCapability, domain string) bool {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return true
	}
	for _, current := range agent.Domains {
		current = strings.TrimSpace(current)
		if current == "*" || strings.EqualFold(current, domain) {
			return true
		}
	}
	return false
}

func agentHasCapabilities(agent AgentCapability, required []string) bool {
	if len(required) == 0 {
		return true
	}
	available := map[string]struct{}{}
	for _, item := range agent.Tools {
		available[strings.TrimSpace(item)] = struct{}{}
	}
	for _, item := range agent.Domains {
		available[strings.TrimSpace(item)] = struct{}{}
	}
	for _, item := range required {
		if _, ok := available[strings.TrimSpace(item)]; !ok {
			return false
		}
	}
	return true
}

type ReferenceKind string

const (
	ReferenceKindTask     ReferenceKind = "task"
	ReferenceKindTodo     ReferenceKind = "todo"
	ReferenceKindClaim    ReferenceKind = "claim"
	ReferenceKindEvidence ReferenceKind = "evidence"
	ReferenceKindFinding  ReferenceKind = "finding"
	ReferenceKindArtifact ReferenceKind = "artifact"
	ReferenceKindMessage  ReferenceKind = "message"
)

type Reference struct {
	Kind ReferenceKind `json:"kind"`
	ID   string        `json:"id"`
}

type ConversationIntent string

const (
	ConversationIntentAsk       ConversationIntent = "ask"
	ConversationIntentAnswer    ConversationIntent = "answer"
	ConversationIntentChallenge ConversationIntent = "challenge"
	ConversationIntentDefend    ConversationIntent = "defend"
	ConversationIntentPropose   ConversationIntent = "propose"
	ConversationIntentSummarize ConversationIntent = "summarize"
	ConversationIntentHandoff   ConversationIntent = "handoff"
)

type ConversationMessage struct {
	ID          string             `json:"id"`
	ThreadID    string             `json:"threadId"`
	TeamID      string             `json:"teamId,omitempty"`
	FromAgentID string             `json:"fromAgentId,omitempty"`
	ToAgentIDs  []string           `json:"toAgentIds,omitempty"`
	Intent      ConversationIntent `json:"intent,omitempty"`
	Body        string             `json:"body"`
	References  []Reference        `json:"references,omitempty"`
	Structured  map[string]any     `json:"structured,omitempty"`
	CreatedAt   time.Time          `json:"createdAt,omitempty"`
}

type ConversationThread struct {
	ID        string                `json:"id"`
	TeamID    string                `json:"teamId,omitempty"`
	Topic     string                `json:"topic,omitempty"`
	Mode      InteractionMode       `json:"mode,omitempty"`
	Round     int                   `json:"round,omitempty"`
	Messages  []ConversationMessage `json:"messages,omitempty"`
	CreatedAt time.Time             `json:"createdAt,omitempty"`
	UpdatedAt time.Time             `json:"updatedAt,omitempty"`
}

func (m ConversationMessage) Validate() error {
	if strings.TrimSpace(m.ID) == "" {
		return errors.New("conversation message missing id")
	}
	if strings.TrimSpace(m.ThreadID) == "" {
		return errors.New("conversation message missing thread id")
	}
	if strings.TrimSpace(m.Body) == "" {
		return errors.New("conversation message missing body")
	}
	if m.Intent == ConversationIntentChallenge && len(m.References) == 0 {
		return errors.New("challenge message must reference the challenged object")
	}
	for idx, ref := range m.References {
		if strings.TrimSpace(string(ref.Kind)) == "" || strings.TrimSpace(ref.ID) == "" {
			return fmt.Errorf("conversation message reference[%d] is incomplete", idx)
		}
	}
	return nil
}
