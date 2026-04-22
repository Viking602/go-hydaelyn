package blackboard

// ExchangeSelector describes a structured read request. It is designed to
// replace the flat Reads []string key list so that tasks can express
// verification requirements, namespace scoping, and confidence thresholds
// without the caller having to rely on implicit namespace-prefix heuristics.
//
// The zero value is a permissive match (no constraints) — callers typically
// construct one from LegacyReadKeyToSelector when migrating flat keys.
type ExchangeSelector struct {
	Keys              []string            `json:"keys,omitempty"`
	Namespaces        []string            `json:"namespaces,omitempty"`
	TaskIDs           []string            `json:"taskIds,omitempty"`
	ValueTypes        []ExchangeValueType `json:"valueTypes,omitempty"`
	ClaimIDs          []string            `json:"claimIds,omitempty"`
	FindingIDs        []string            `json:"findingIds,omitempty"`
	ArtifactIDs       []string            `json:"artifactIds,omitempty"`
	RequireVerified   bool                `json:"requireVerified,omitempty"`
	MinConfidence     float64             `json:"minConfidence,omitempty"`
	Limit             int                 `json:"limit,omitempty"`
	IncludeText       bool                `json:"includeText,omitempty"`
	IncludeStructured bool                `json:"includeStructured,omitempty"`
	IncludeArtifacts  bool                `json:"includeArtifacts,omitempty"`
	Required          bool                `json:"required,omitempty"`
	Label             string              `json:"label,omitempty"`
}

// LegacyReadKeyToSelector converts a flat Reads []string entry into an
// ExchangeSelector preserving the old behaviour: scope by key, permissive
// value types, and include all renderable payload fields.
func LegacyReadKeyToSelector(key string) ExchangeSelector {
	return ExchangeSelector{
		Keys:              []string{key},
		Limit:             50,
		IncludeText:       true,
		IncludeStructured: true,
		IncludeArtifacts:  true,
		Required:          true,
		Label:             key,
	}
}

// Matches reports whether a raw Exchange satisfies the non-verification
// portions of the selector. Verification and confidence filters live in
// SelectExchanges because they require cross-referencing the containing
// State's claim / verification tables.
func (sel ExchangeSelector) Matches(exchange Exchange) bool {
	if len(sel.Keys) > 0 && !containsString(sel.Keys, exchange.Key) {
		return false
	}
	if len(sel.Namespaces) > 0 && !containsString(sel.Namespaces, exchange.Namespace) {
		return false
	}
	if len(sel.TaskIDs) > 0 && !containsString(sel.TaskIDs, exchange.TaskID) {
		return false
	}
	if len(sel.ValueTypes) > 0 && !containsValueType(sel.ValueTypes, exchange.ValueType) {
		return false
	}
	if len(sel.ClaimIDs) > 0 && !overlapString(sel.ClaimIDs, exchange.ClaimIDs) {
		return false
	}
	if len(sel.FindingIDs) > 0 && !overlapString(sel.FindingIDs, exchange.FindingIDs) {
		return false
	}
	if len(sel.ArtifactIDs) > 0 && !overlapString(sel.ArtifactIDs, exchange.ArtifactIDs) {
		return false
	}
	return true
}

// SelectExchanges returns exchanges matching the selector, applying
// verification + confidence + finding-linkage gates. Returned exchanges are
// cloned so callers can mutate without corrupting state.
func (s State) SelectExchanges(sel ExchangeSelector) []Exchange {
	supportedClaims := map[string]struct{}{}
	if sel.RequireVerified {
		threshold := sel.MinConfidence
		if threshold <= 0 {
			threshold = DefaultVerificationConfidence
		}
		for _, result := range s.Verifications {
			if result.SupportsClaim(threshold) {
				supportedClaims[result.ClaimID] = struct{}{}
			}
		}
	}
	items := make([]Exchange, 0, len(s.Exchanges))
	for _, exchange := range s.Exchanges {
		if !sel.Matches(exchange) {
			continue
		}
		if sel.MinConfidence > 0 && !exchangeClearsConfidence(exchange, sel.MinConfidence) {
			continue
		}
		if sel.RequireVerified && !exchangeIsVerified(exchange, supportedClaims) {
			continue
		}
		items = append(items, cloneExchange(exchange))
		if sel.Limit > 0 && len(items) >= sel.Limit {
			break
		}
	}
	return items
}

// SelectFindings returns findings that satisfy the selector. When
// RequireVerified is set, every claim backing the finding must have a
// verification result that passes SupportsClaim at the effective threshold.
func (s State) SelectFindings(sel ExchangeSelector) []Finding {
	threshold := sel.MinConfidence
	if threshold <= 0 && sel.RequireVerified {
		threshold = DefaultVerificationConfidence
	}
	supported := map[string]struct{}{}
	if sel.RequireVerified {
		for _, result := range s.Verifications {
			if result.SupportsClaim(threshold) {
				supported[result.ClaimID] = struct{}{}
			}
		}
	}
	items := make([]Finding, 0, len(s.Findings))
	for _, finding := range s.Findings {
		if len(sel.TaskIDs) > 0 && !containsString(sel.TaskIDs, finding.TaskID) {
			continue
		}
		if len(sel.FindingIDs) > 0 && !containsString(sel.FindingIDs, finding.ID) {
			continue
		}
		if len(sel.ClaimIDs) > 0 && !overlapString(sel.ClaimIDs, finding.ClaimIDs) {
			continue
		}
		if sel.MinConfidence > 0 && finding.Confidence < sel.MinConfidence {
			continue
		}
		if sel.RequireVerified {
			if len(finding.ClaimIDs) == 0 {
				continue
			}
			allSupported := true
			for _, claimID := range finding.ClaimIDs {
				if _, ok := supported[claimID]; !ok {
					allSupported = false
					break
				}
			}
			if !allSupported {
				continue
			}
		}
		items = append(items, cloneFinding(finding))
		if sel.Limit > 0 && len(items) >= sel.Limit {
			break
		}
	}
	return items
}

func exchangeClearsConfidence(exchange Exchange, threshold float64) bool {
	if threshold <= 0 {
		return true
	}
	if raw, ok := exchange.Structured["confidence"]; ok {
		if value, ok := raw.(float64); ok {
			return value >= threshold
		}
	}
	return true
}

func exchangeIsVerified(exchange Exchange, supportedClaims map[string]struct{}) bool {
	if len(supportedClaims) == 0 {
		return false
	}
	if len(exchange.ClaimIDs) == 0 {
		return false
	}
	for _, claimID := range exchange.ClaimIDs {
		if _, ok := supportedClaims[claimID]; !ok {
			return false
		}
	}
	return true
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func overlapString(left, right []string) bool {
	if len(left) == 0 || len(right) == 0 {
		return false
	}
	set := make(map[string]struct{}, len(left))
	for _, value := range left {
		set[value] = struct{}{}
	}
	for _, value := range right {
		if _, ok := set[value]; ok {
			return true
		}
	}
	return false
}

func containsValueType(values []ExchangeValueType, target ExchangeValueType) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func cloneFinding(finding Finding) Finding {
	finding.ClaimIDs = append([]string{}, finding.ClaimIDs...)
	finding.EvidenceIDs = append([]string{}, finding.EvidenceIDs...)
	return finding
}
