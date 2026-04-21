package kit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Viking602/go-hydaelyn/mailbox"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/team"
	"github.com/Viking602/go-hydaelyn/tool"
)

// MailboxProvider is the narrow interface the send_message tool needs from
// its host. Production code passes *host.Runtime; tests can supply a fake.
type MailboxProvider interface {
	Mailbox() mailbox.Mailbox
}

// SendMessageInput is the JSON shape accepted by the send_message tool.
type SendMessageInput struct {
	// Either `to` (simple shorthand) or `kind`+address field is accepted.
	// If `kind` is omitted we infer: role if `role` is set, group if `group`
	// is set, otherwise agent.
	Kind    string `json:"kind,omitempty"`
	AgentID string `json:"agentId,omitempty"`
	Role    string `json:"role,omitempty"`
	Group   string `json:"group,omitempty"`

	Subject       string         `json:"subject,omitempty"`
	Body          string         `json:"body"`
	Structured    map[string]any `json:"structured,omitempty"`
	ArtifactIDs   []string       `json:"artifactIds,omitempty"`
	Intent        string         `json:"intent,omitempty"`
	Priority      string         `json:"priority,omitempty"`
	CorrelationID string         `json:"correlationId,omitempty"`
	InReplyTo     string         `json:"inReplyTo,omitempty"`
	TTLSeconds    int            `json:"ttlSeconds,omitempty"`
}

// SendMessageOutput is returned to the LLM.
type SendMessageOutput struct {
	EnvelopeIDs []string `json:"envelopeIds"`
	Recipients  int      `json:"recipients"`
}

// sendMessageTool is a tool.Driver that lets an agent signal another agent
// through the mailbox.
type sendMessageTool struct {
	provider MailboxProvider
	def      tool.Definition
}

// NewSendMessageTool builds the send_message tool bound to the given provider.
// Call RegisterTool(NewSendMessageTool(runtime)) to enable it.
func NewSendMessageTool(provider MailboxProvider) tool.Driver {
	return &sendMessageTool{
		provider: provider,
		def: tool.Definition{
			Name:        "send_message",
			Description: "Send a letter to another agent (by id, role, or group) via the team mailbox. Use this for ask/answer/delegate/cancel/handoff signals that are not shared data.",
			InputSchema: message.JSONSchema{
				Type: "object",
				Properties: map[string]message.JSONSchema{
					"kind":          {Type: "string", Description: "Recipient kind: agent, role, or group. Inferred from which field is set if omitted."},
					"agentId":       {Type: "string", Description: "Recipient agent id (required when kind=agent)."},
					"role":          {Type: "string", Description: "Recipient role (required when kind=role)."},
					"group":         {Type: "string", Description: "Recipient group label (required when kind=group)."},
					"subject":       {Type: "string", Description: "One-line subject line."},
					"body":          {Type: "string", Description: "Free-text body of the letter."},
					"structured":    {Type: "object", AdditionalProperties: true, Description: "Optional structured payload."},
					"artifactIds":   {Type: "array", Items: &message.JSONSchema{Type: "string"}, Description: "References to artifacts already on the blackboard."},
					"intent":        {Type: "string", Description: "ask, answer, delegate, cancel, broadcast, or handoff."},
					"priority":      {Type: "string", Description: "low, normal, high, or urgent."},
					"correlationId": {Type: "string", Description: "Ties this letter to a thread."},
					"inReplyTo":     {Type: "string", Description: "Envelope id this letter replies to."},
					"ttlSeconds":    {Type: "integer", Description: "Optional per-letter TTL override."},
				},
				Required:             []string{"body"},
				AdditionalProperties: false,
			},
		},
	}
}

func (t *sendMessageTool) Definition() tool.Definition {
	return t.def
}

func (t *sendMessageTool) Execute(ctx context.Context, call tool.Call, _ tool.UpdateSink) (tool.Result, error) {
	if t.provider == nil {
		return errorResult(call, "mailbox unavailable"), nil
	}
	box := t.provider.Mailbox()
	if box == nil {
		return errorResult(call, "mailbox unavailable"), nil
	}
	caller, ok := tool.CallerFromContext(ctx)
	if !ok || strings.TrimSpace(caller.TeamRunID) == "" || strings.TrimSpace(caller.AgentID) == "" {
		return errorResult(call, "send_message can only be invoked inside a team task"), nil
	}

	var input SendMessageInput
	raw := call.Arguments
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return errorResult(call, fmt.Sprintf("invalid send_message arguments: %v", err)), nil
	}
	if strings.TrimSpace(input.Body) == "" && len(input.Structured) == 0 && len(input.ArtifactIDs) == 0 {
		return errorResult(call, "send_message requires body, structured, or artifactIds"), nil
	}

	addr, err := resolveSendAddress(caller.TeamRunID, input)
	if err != nil {
		return errorResult(call, err.Error()), nil
	}

	letter := mailbox.Letter{
		Subject:     input.Subject,
		Body:        input.Body,
		Structured:  input.Structured,
		ArtifactIDs: append([]string{}, input.ArtifactIDs...),
		Intent:      normalizeIntent(input.Intent),
		Priority:    normalizePriority(input.Priority),
	}

	send := mailbox.SendInput{
		TeamRunID: caller.TeamRunID,
		From: mailbox.Address{
			Kind:      mailbox.AddressKindAgent,
			TeamRunID: caller.TeamRunID,
			AgentID:   caller.AgentID,
		},
		To:            addr,
		Letter:        letter,
		CorrelationID: input.CorrelationID,
		InReplyTo:     input.InReplyTo,
	}
	if input.TTLSeconds > 0 {
		send.TTL = time.Duration(input.TTLSeconds) * time.Second
	}

	ids, sendErr := box.Send(ctx, send)
	if sendErr != nil {
		if errors.Is(sendErr, mailbox.ErrNoRecipients) ||
			errors.Is(sendErr, mailbox.ErrInvalidAddress) ||
			errors.Is(sendErr, mailbox.ErrRateLimited) ||
			errors.Is(sendErr, mailbox.ErrOverSize) ||
			errors.Is(sendErr, mailbox.ErrHopLimit) ||
			errors.Is(sendErr, mailbox.ErrMailboxFull) {
			return errorResult(call, sendErr.Error()), nil
		}
		return tool.Result{}, sendErr
	}

	out := SendMessageOutput{EnvelopeIDs: ids, Recipients: len(ids)}
	payload, _ := json.Marshal(out)
	return tool.Result{
		ToolCallID: call.ID,
		Name:       call.Name,
		Content:    fmt.Sprintf("delivered to %d recipient(s)", len(ids)),
		Structured: payload,
	}, nil
}

func resolveSendAddress(teamRunID string, in SendMessageInput) (mailbox.Address, error) {
	kind := strings.ToLower(strings.TrimSpace(in.Kind))
	if kind == "" {
		switch {
		case strings.TrimSpace(in.AgentID) != "":
			kind = string(mailbox.AddressKindAgent)
		case strings.TrimSpace(in.Role) != "":
			kind = string(mailbox.AddressKindRole)
		case strings.TrimSpace(in.Group) != "":
			kind = string(mailbox.AddressKindGroup)
		default:
			return mailbox.Address{}, errors.New("send_message requires agentId, role, or group")
		}
	}
	switch mailbox.AddressKind(kind) {
	case mailbox.AddressKindAgent:
		if strings.TrimSpace(in.AgentID) == "" {
			return mailbox.Address{}, errors.New("agentId is required when kind=agent")
		}
		return mailbox.Address{Kind: mailbox.AddressKindAgent, TeamRunID: teamRunID, AgentID: in.AgentID}, nil
	case mailbox.AddressKindRole:
		if strings.TrimSpace(in.Role) == "" {
			return mailbox.Address{}, errors.New("role is required when kind=role")
		}
		return mailbox.Address{Kind: mailbox.AddressKindRole, TeamRunID: teamRunID, Role: team.Role(in.Role)}, nil
	case mailbox.AddressKindGroup:
		if strings.TrimSpace(in.Group) == "" {
			return mailbox.Address{}, errors.New("group is required when kind=group")
		}
		return mailbox.Address{Kind: mailbox.AddressKindGroup, TeamRunID: teamRunID, Group: in.Group}, nil
	default:
		return mailbox.Address{}, fmt.Errorf("unsupported recipient kind %q", kind)
	}
}

func normalizeIntent(v string) mailbox.LetterIntent {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "ask":
		return mailbox.IntentAsk
	case "answer":
		return mailbox.IntentAnswer
	case "delegate":
		return mailbox.IntentDelegate
	case "cancel":
		return mailbox.IntentCancel
	case "broadcast":
		return mailbox.IntentBroadcast
	case "handoff":
		return mailbox.IntentHandoff
	default:
		return mailbox.LetterIntent(strings.ToLower(strings.TrimSpace(v)))
	}
}

func normalizePriority(v string) mailbox.LetterPriority {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "low":
		return mailbox.PriorityLow
	case "high":
		return mailbox.PriorityHigh
	case "urgent":
		return mailbox.PriorityUrgent
	case "", "normal":
		return mailbox.PriorityNormal
	default:
		return mailbox.PriorityNormal
	}
}

func errorResult(call tool.Call, reason string) tool.Result {
	return tool.Result{
		ToolCallID: call.ID,
		Name:       call.Name,
		Content:    reason,
		IsError:    true,
	}
}
