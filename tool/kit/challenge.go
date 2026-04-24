package kit

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Viking602/go-hydaelyn/mailbox"
	"github.com/Viking602/go-hydaelyn/message"
	"github.com/Viking602/go-hydaelyn/team"
	"github.com/Viking602/go-hydaelyn/tool"
)

type ChallengeClaimInput struct {
	ClaimID    string `json:"claimId"`
	Reason     string `json:"reason"`
	AgentID    string `json:"agentId,omitempty"`
	Role       string `json:"role,omitempty"`
	Group      string `json:"group,omitempty"`
	ThreadID   string `json:"threadId,omitempty"`
	Priority   string `json:"priority,omitempty"`
	TTLSeconds int    `json:"ttlSeconds,omitempty"`
}

func NewChallengeClaimTool(provider MailboxProvider) tool.Driver {
	return challengeClaimTool{provider: provider}
}

type challengeClaimTool struct {
	provider MailboxProvider
}

func (t challengeClaimTool) Definition() tool.Definition {
	return tool.Definition{
		Name:        "challenge_claim",
		Description: "Challenge a specific claim and send the challenge to another panel expert via the team mailbox.",
		InputSchema: message.JSONSchema{
			Type: "object",
			Properties: map[string]message.JSONSchema{
				"claimId":    {Type: "string", Description: "Claim id being challenged."},
				"reason":     {Type: "string", Description: "Natural-language challenge reason."},
				"agentId":    {Type: "string", Description: "Target agent id."},
				"role":       {Type: "string", Description: "Target role when agentId is not used."},
				"group":      {Type: "string", Description: "Target group when agentId/role are not used."},
				"threadId":   {Type: "string", Description: "Conversation thread id."},
				"priority":   {Type: "string", Description: "low, normal, high, or urgent."},
				"ttlSeconds": {Type: "integer", Description: "Optional per-letter TTL override."},
			},
			Required:             []string{"claimId", "reason"},
			AdditionalProperties: false,
		},
	}
}

func (t challengeClaimTool) Execute(ctx context.Context, call tool.Call, _ tool.UpdateSink) (tool.Result, error) {
	if t.provider == nil || t.provider.Mailbox() == nil {
		return errorResult(call, "mailbox unavailable"), nil
	}
	caller, ok := tool.CallerFromContext(ctx)
	if !ok || strings.TrimSpace(caller.TeamRunID) == "" || strings.TrimSpace(caller.AgentID) == "" {
		return errorResult(call, "challenge_claim can only be invoked inside a team task"), nil
	}
	var input ChallengeClaimInput
	if len(call.Arguments) > 0 {
		if err := json.Unmarshal(call.Arguments, &input); err != nil {
			return errorResult(call, fmt.Sprintf("invalid challenge_claim arguments: %v", err)), nil
		}
	}
	if strings.TrimSpace(input.ClaimID) == "" || strings.TrimSpace(input.Reason) == "" {
		return errorResult(call, "challenge_claim requires claimId and reason"), nil
	}
	addr, err := resolveSendAddress(caller.TeamRunID, SendMessageInput{
		AgentID: input.AgentID,
		Role:    input.Role,
		Group:   input.Group,
	})
	if err != nil {
		return errorResult(call, err.Error()), nil
	}
	refs := []team.Reference{{Kind: team.ReferenceKindClaim, ID: input.ClaimID}}
	send := mailbox.SendInput{
		TeamRunID: caller.TeamRunID,
		From: mailbox.Address{
			Kind:      mailbox.AddressKindAgent,
			TeamRunID: caller.TeamRunID,
			AgentID:   caller.AgentID,
		},
		To: addr,
		Letter: mailbox.Letter{
			Subject:  "challenge claim " + input.ClaimID,
			Body:     input.Reason,
			Intent:   mailbox.IntentChallenge,
			Priority: normalizePriority(input.Priority),
			Structured: map[string]any{
				"threadId":   input.ThreadID,
				"claimId":    input.ClaimID,
				"references": referencesPayload(refs),
			},
		},
		CorrelationID: input.ThreadID,
	}
	if input.TTLSeconds > 0 {
		send.TTL = time.Duration(input.TTLSeconds) * time.Second
	}
	ids, err := t.provider.Mailbox().Send(ctx, send)
	if err != nil {
		return errorResult(call, err.Error()), nil
	}
	payload, _ := json.Marshal(SendMessageOutput{EnvelopeIDs: ids, Recipients: len(ids)})
	return tool.Result{
		ToolCallID: call.ID,
		Name:       call.Name,
		Content:    fmt.Sprintf("challenge delivered to %d recipient(s)", len(ids)),
		Structured: payload,
	}, nil
}
