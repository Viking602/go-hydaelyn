// Package blackboard exposes only the stable item and selector contracts used
// by the Hydaelyn runtime.
package blackboard

import "github.com/Viking602/go-hydaelyn/api"

type (
	Visibility     = api.BlackboardVisibility
	ItemType       = api.BlackboardItemType
	SourceType     = api.SourceType
	SourceIdentity = api.SourceIdentity
	Item           = api.BlackboardItem
	Selector       = api.BlackboardSelector
)

const (
	VisibilityInternal             = api.BlackboardVisibilityInternal
	VisibilityAgentVisible         = api.BlackboardVisibilityAgentVisible
	VisibilityUserVisibleCandidate = api.BlackboardVisibilityUserVisibleCandidate
	VisibilityUserVisible          = api.BlackboardVisibilityUserVisible

	ItemClaim          = api.BlackboardItemClaim
	ItemEvidence       = api.BlackboardItemEvidence
	ItemFinding        = api.BlackboardItemFinding
	ItemArtifactRef    = api.BlackboardItemArtifactRef
	ItemContext        = api.BlackboardItemContext
	ItemTaskOutput     = api.BlackboardItemTaskOutput
	ItemHandoffContext = api.BlackboardItemHandoffContext

	SourceAgent     = api.SourceAgent
	SourceComponent = api.SourceComponent
	SourceTool      = api.SourceTool
	SourceSystem    = api.SourceSystem
)
