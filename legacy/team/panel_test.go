package team

import "testing"

func TestTaskBoardClaimTodoRequiresMatchingCapability(t *testing.T) {
	board := TaskBoard{
		Plan: TodoPlan{
			ID:   "plan-1",
			Goal: "ship panel collaboration",
			Items: []TodoItem{{
				ID:                   "todo-security",
				Title:                "review threat model",
				Domain:               "security",
				RequiredCapabilities: []string{"threat_model"},
				Priority:             TodoPriorityHigh,
				ExpectedReportKind:   ReportKindResearch,
				VerificationPolicy:   TodoVerificationPolicy{Required: true, Mode: "cross_review"},
				Status:               TodoStatusOpen,
			}},
		},
	}

	_, err := board.Claim("todo-security", AgentCapability{
		AgentID: "frontend-expert",
		Domains: []string{"frontend"},
		Tools:   []string{"browser"},
	}, ClaimOptions{RequireDomainMatch: true})
	if err == nil {
		t.Fatalf("expected domain/capability mismatch to reject claim")
	}

	claimed, err := board.Claim("todo-security", AgentCapability{
		AgentID: "security-expert",
		Domains: []string{"security"},
		Tools:   []string{"threat_model", "search"},
	}, ClaimOptions{RequireDomainMatch: true, MaxActivePerAgent: 1})
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if claimed.Status != TodoStatusClaimed || claimed.PrimaryAgentID != "security-expert" {
		t.Fatalf("expected claimed todo assigned to expert, got %#v", claimed)
	}

	_, err = board.Claim("todo-security", AgentCapability{
		AgentID: "security-expert-2",
		Domains: []string{"security"},
		Tools:   []string{"threat_model"},
	}, ClaimOptions{RequireDomainMatch: true})
	if err == nil {
		t.Fatalf("expected duplicate claim to be rejected")
	}
}

func TestConversationMessageRequiresVisibleBodyAndReferences(t *testing.T) {
	msg := ConversationMessage{
		ID:          "msg-1",
		ThreadID:    "thread-1",
		TeamID:      "team-1",
		FromAgentID: "verifier-1",
		Intent:      ConversationIntentChallenge,
		Body:        "claim-2 only cites one weak source",
		References: []Reference{{
			Kind: ReferenceKindClaim,
			ID:   "claim-2",
		}},
	}
	if err := msg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	msg.References = nil
	if err := msg.Validate(); err == nil {
		t.Fatalf("expected challenge message without references to fail validation")
	}
}
