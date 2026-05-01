// Package blackboard exposes only the stable item and selector contracts used
// by the Orchestrator runtime.
package blackboard

import "github.com/Viking602/go-hydaelyn/orchestrator"

type (
	Visibility     = orchestrator.BlackboardVisibility
	ItemType       = orchestrator.BlackboardItemType
	SourceType     = orchestrator.SourceType
	SourceIdentity = orchestrator.SourceIdentity
	Item           = orchestrator.BlackboardItem
	Selector       = orchestrator.BlackboardSelector
)

const (
	VisibilityInternal             = orchestrator.BlackboardVisibilityInternal
	VisibilityAgentVisible         = orchestrator.BlackboardVisibilityAgentVisible
	VisibilityUserVisibleCandidate = orchestrator.BlackboardVisibilityUserVisibleCandidate
	VisibilityUserVisible          = orchestrator.BlackboardVisibilityUserVisible

	ItemClaim          = orchestrator.BlackboardItemClaim
	ItemEvidence       = orchestrator.BlackboardItemEvidence
	ItemFinding        = orchestrator.BlackboardItemFinding
	ItemArtifactRef    = orchestrator.BlackboardItemArtifactRef
	ItemContext        = orchestrator.BlackboardItemContext
	ItemTaskOutput     = orchestrator.BlackboardItemTaskOutput
	ItemHandoffContext = orchestrator.BlackboardItemHandoffContext

	SourceAgent     = orchestrator.SourceAgent
	SourceComponent = orchestrator.SourceComponent
	SourceTool      = orchestrator.SourceTool
	SourceSystem    = orchestrator.SourceSystem
)
