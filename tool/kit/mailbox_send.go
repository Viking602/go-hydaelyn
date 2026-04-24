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

	Subject       string           `json:"subject,omitempty"`
	Body          string           `json:"body"`
	Structured    map[string]any   `json:"structured,omitempty"`
	ArtifactIDs   []string         `json:"artifactIds,omitempty"`
	ThreadID      string           `json:"threadId,omitempty"`
	References    []team.Reference `json:"references,omitempty"`
	Intent        string           `json:"intent,omitempty"`
	Priority      string           `json:"priority,omitempty"`
	CorrelationID string           `json:"correlationId,omitempty"`
	InReplyTo     string           `json:"inReplyTo,omitempty"`
	TTLSeconds    int              `json:"ttlSeconds,omitempty"`
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
					"kind":        {Type: "string", Description: "Recipient kind: agent, role, or group. Inferred from which field is set if omitted."},
					"agentId":     {Type: "string", Description: "Recipient agent id (required when kind=agent)."},
					"role":        {Type: "string", Description: "Recipient role (required when kind=role)."},
					"group":       {Type: "string", Description: "Recipient group label (required when kind=group)."},
					"subject":     {Type: "string", Description: "One-line subject line."},
					"body":        {Type: "string", Description: "Free-text body of the letter."},
					"structured":  {Type: "object", AdditionalProperties: true, Description: "Optional structured payload."},
					"artifactIds": {Type: "array", Items: &message.JSONSchema{Type: "string"}, Description: "References to artifacts already on the blackboard."},
					"threadId":    {Type: "string", Description: "Conversation thread id for user-visible collaboration."},
					"references": {
						Type: "array",
						Items: &message.JSONSchema{
							Type: "object",
							Properties: map[string]message.JSONSchema{
								"kind": {Type: "string", Description: "Referenced object kind: task, todo, claim, evidence, finding, artifact, or message."},
								"id":   {Type: "string", Description: "Referenced object id."},
							},
							Required: []string{"kind", "id"},
						},
						Description: "Structured references that make the natural-language message auditable.",
					},
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
	box := t.mailbox()
	if box == nil {
		return errorResult(call, "mailbox unavailable"), nil
	}
	caller, ok := tool.CallerFromContext(ctx)
	if !ok || strings.TrimSpace(caller.TeamRunID) == "" || strings.TrimSpace(caller.AgentID) == "" {
		return errorResult(call, "send_message can only be invoked inside a team task"), nil
	}

	input, err := parseSendMessageInput(call.Arguments)
	if err != nil {
		return errorResult(call, fmt.Sprintf("invalid send_message arguments: %v", err)), nil
	}
	if !input.HasPayload() {
		return errorResult(call, "send_message requires body, structured, or artifactIds"), nil
	}
	send, err := input.SendInput(caller)
	if err != nil {
		return errorResult(call, err.Error()), nil
	}

	ids, sendErr := box.Send(ctx, send)
	if sendErr != nil {
		if sendMessageUserError(sendErr) {
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

func (t *sendMessageTool) mailbox() mailbox.Mailbox {
	if t.provider == nil {
		return nil
	}
	return t.provider.Mailbox()
}

func parseSendMessageInput(args json.RawMessage) (SendMessageInput, error) {
	var input SendMessageInput
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return SendMessageInput{}, err
	}
	return input, nil
}

func (in SendMessageInput) HasPayload() bool {
	return strings.TrimSpace(in.Body) != "" || len(in.Structured) > 0 || len(in.ArtifactIDs) > 0
}

func (in SendMessageInput) SendInput(caller tool.CallerInfo) (mailbox.SendInput, error) {
	addr, err := resolveSendAddress(caller.TeamRunID, in)
	if err != nil {
		return mailbox.SendInput{}, err
	}
	send := mailbox.SendInput{
		TeamRunID: caller.TeamRunID,
		From: mailbox.Address{
			Kind:      mailbox.AddressKindAgent,
			TeamRunID: caller.TeamRunID,
			AgentID:   caller.AgentID,
		},
		To:            addr,
		Letter:        in.Letter(),
		CorrelationID: in.CorrelationIDOrThread(),
		InReplyTo:     in.InReplyTo,
	}
	if in.TTLSeconds > 0 {
		send.TTL = time.Duration(in.TTLSeconds) * time.Second
	}
	return send, nil
}

func (in SendMessageInput) Letter() mailbox.Letter {
	return mailbox.Letter{
		Subject:     in.Subject,
		Body:        in.Body,
		Structured:  in.StructuredWithCollaborationRefs(),
		ArtifactIDs: append([]string{}, in.ArtifactIDs...),
		Intent:      normalizeIntent(in.Intent),
		Priority:    normalizePriority(in.Priority),
	}
}

func (in SendMessageInput) CorrelationIDOrThread() string {
	if in.CorrelationID != "" {
		return in.CorrelationID
	}
	return in.ThreadID
}

func (in SendMessageInput) StructuredWithCollaborationRefs() map[string]any {
	structured := cloneStructured(in.Structured)
	if strings.TrimSpace(in.ThreadID) != "" {
		if structured == nil {
			structured = map[string]any{}
		}
		structured["threadId"] = in.ThreadID
	}
	if len(in.References) > 0 {
		if structured == nil {
			structured = map[string]any{}
		}
		structured["references"] = referencesPayload(in.References)
	}
	return structured
}

func sendMessageUserError(err error) bool {
	return errors.Is(err, mailbox.ErrNoRecipients) ||
		errors.Is(err, mailbox.ErrInvalidAddress) ||
		errors.Is(err, mailbox.ErrRateLimited) ||
		errors.Is(err, mailbox.ErrOverSize) ||
		errors.Is(err, mailbox.ErrHopLimit) ||
		errors.Is(err, mailbox.ErrMailboxFull)
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
	case "challenge":
		return mailbox.IntentChallenge
	case "review":
		return mailbox.IntentReview
	default:
		return mailbox.LetterIntent(strings.ToLower(strings.TrimSpace(v)))
	}
}

func cloneStructured(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	clone := make(map[string]any, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

func referencesPayload(refs []team.Reference) []map[string]string {
	items := make([]map[string]string, 0, len(refs))
	for _, ref := range refs {
		items = append(items, map[string]string{
			"kind": string(ref.Kind),
			"id":   ref.ID,
		})
	}
	return items
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
