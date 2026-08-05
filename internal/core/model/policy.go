package model

import "time"

type PolicyEffect string

const (
	PolicyEffectAllow           PolicyEffect = "allow"
	PolicyEffectDeny            PolicyEffect = "deny"
	PolicyEffectRequireApproval PolicyEffect = "require_approval"
	PolicyEffectPause           PolicyEffect = "pause"
	PolicyEffectAbort           PolicyEffect = "abort"
)

type ObligationKind string

type PolicyObligationTarget string

const (
	ObligationRedactFields           ObligationKind = "redact_fields"
	ObligationSelectorOnly           ObligationKind = "selector_only"
	ObligationRequireHumanApproval   ObligationKind = "require_human_approval"
	ObligationHideInternalTrace      ObligationKind = "hide_internal_trace"
	ObligationMaskToolOutput         ObligationKind = "mask_tool_output"
	ObligationRestrictHandoffContext ObligationKind = "restrict_handoff_context"
)

const (
	PolicyTargetBlackboardRead  PolicyObligationTarget = "blackboard_read"
	PolicyTargetBlackboardWrite PolicyObligationTarget = "blackboard_write"
	PolicyTargetToolResult      PolicyObligationTarget = "tool_result"
	PolicyTargetHandoff         PolicyObligationTarget = "handoff"
	PolicyTargetResponse        PolicyObligationTarget = "response"
	PolicyTargetTrace           PolicyObligationTarget = "trace"
)

type PolicyDecision struct {
	DecisionID       string             `json:"decisionId"`
	Effect           PolicyEffect       `json:"effect"`
	Reason           string             `json:"reason,omitempty"`
	Obligations      []PolicyObligation `json:"obligations,omitempty"`
	Redactions       []string           `json:"redactions,omitempty"`
	ApprovalRequired bool               `json:"approvalRequired,omitempty"`
	ExpiresAt        time.Time          `json:"expiresAt,omitempty"`
	Metadata         map[string]string  `json:"metadata,omitempty"`
}

type PolicyObligation struct {
	Kind     ObligationKind         `json:"kind"`
	Target   PolicyObligationTarget `json:"target,omitempty"`
	Selector *BlackboardSelector    `json:"selector,omitempty"`
}

type MessagePolicyChecker func(UserMessage) PolicyDecision
