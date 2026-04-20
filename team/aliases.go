package team

import "github.com/Viking602/go-hydaelyn/internal/blackboard"

// Type aliases re-exporting internal/blackboard types that appear in the
// public [RunState] surface. These allow external callers to read and
// construct blackboard values without importing internal/.

type (
	BlackboardState              = blackboard.State
	BlackboardSource             = blackboard.Source
	BlackboardArtifact           = blackboard.Artifact
	BlackboardEvidence           = blackboard.Evidence
	BlackboardEvidenceInput      = blackboard.EvidenceInput
	BlackboardClaim              = blackboard.Claim
	BlackboardFinding            = blackboard.Finding
	BlackboardExchange           = blackboard.Exchange
	BlackboardExchangeValueType  = blackboard.ExchangeValueType
	BlackboardVerificationResult = blackboard.VerificationResult
	BlackboardVerificationStatus = blackboard.VerificationStatus
	BlackboardPublishRequest     = blackboard.PublishRequest
	BlackboardPublishResult      = blackboard.PublishResult
	BlackboardPipeline           = blackboard.Pipeline
)

var (
	ErrBlackboardExchangeConflict    = blackboard.ErrExchangeConflict
	DefaultVerificationConfidence    = blackboard.DefaultVerificationConfidence
	NewBlackboardPipeline            = blackboard.NewPipeline
	InferBlackboardVerificationStatus = blackboard.InferVerificationStatus
)
