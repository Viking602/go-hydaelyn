package host

import (
	"github.com/Viking602/go-hydaelyn/internal/blackboard"
	"github.com/Viking602/go-hydaelyn/team"
)

// MaterializedContext is the selector-driven output that replaces the old
// flat slice of Exchanges the runtime used to assemble into a text block. We
// keep Exchanges for rendering but also surface Findings and per-selector
// bookkeeping so readiness checks can reason about required-vs-optional reads
// without re-executing the selectors.
type MaterializedContext struct {
	Exchanges []blackboard.Exchange
	Findings  []blackboard.Finding
	Misses    []SelectorMiss
}

type SelectorMiss struct {
	Selector blackboard.ExchangeSelector
	Reason   string
}

const (
	missReasonNoMatch           = "no_match"
	missReasonRequireVerifiedNo = "no_supported_finding"
)

// MaterializeSelectors runs a list of selectors over the blackboard and
// returns the deduplicated exchanges + findings that satisfy them. Selectors
// that match nothing are reported as SelectorMiss entries so the readiness
// resolver can block tasks whose required reads are unfulfilled.
//
// The selector pipeline is the only blessed way to feed task prompts in
// strict control mode — callers must never re-introduce the namespace-prefix
// heuristic that filterMaterializedExchanges historically relied on.
func MaterializeSelectors(board *blackboard.State, selectors []blackboard.ExchangeSelector) MaterializedContext {
	if board == nil || len(selectors) == 0 {
		return MaterializedContext{}
	}
	exchangeSeen := map[string]struct{}{}
	findingSeen := map[string]struct{}{}
	ctx := MaterializedContext{}
	for _, selector := range selectors {
		selectorHit := false
		for _, exchange := range board.SelectExchanges(selector) {
			key := exchangeDedupKey(exchange)
			if _, ok := exchangeSeen[key]; ok {
				continue
			}
			exchangeSeen[key] = struct{}{}
			ctx.Exchanges = append(ctx.Exchanges, exchange)
			selectorHit = true
		}
		if selectorAsksForFindings(selector) {
			for _, finding := range board.SelectFindings(selector) {
				if _, ok := findingSeen[finding.ID]; ok {
					continue
				}
				findingSeen[finding.ID] = struct{}{}
				ctx.Findings = append(ctx.Findings, finding)
				selectorHit = true
			}
		}
		if !selectorHit {
			reason := missReasonNoMatch
			if selector.RequireVerified {
				reason = missReasonRequireVerifiedNo
			}
			ctx.Misses = append(ctx.Misses, SelectorMiss{Selector: selector, Reason: reason})
		}
	}
	return ctx
}

// selectorAsksForFindings decides whether a selector is expressing "pull me
// the verified findings themselves" versus "pull me exchanges of a specific
// key that happen to need verification". The design.doc selector, lifted to
// RequireVerified for guarded synthesis, must not also enumerate every
// supported finding on the board — that would bleed unrelated findings into
// the read set. Only selectors that explicitly target findings (by pseudo-key
// "supported_findings" or by finding/claim id) should harvest findings.
func selectorAsksForFindings(sel blackboard.ExchangeSelector) bool {
	if !sel.RequireVerified {
		return false
	}
	if len(sel.FindingIDs) > 0 || len(sel.ClaimIDs) > 0 {
		return true
	}
	for _, key := range sel.Keys {
		if key == supportedFindingsReadKey {
			return true
		}
	}
	return false
}

// selectorsForTask derives the effective selector list for a task. Explicit
// ReadSelectors win; otherwise each Reads []string key is converted via
// LegacyReadKeyToSelector. The special "supported_findings" pseudo-key keeps
// its historical meaning but now flows through RequireVerified rather than
// the old namespace-prefix escape hatch.
func selectorsForTask(task team.Task) []blackboard.ExchangeSelector {
	if len(task.ReadSelectors) > 0 {
		return cloneSelectors(task.ReadSelectors)
	}
	if len(task.Reads) == 0 {
		return nil
	}
	out := make([]blackboard.ExchangeSelector, 0, len(task.Reads))
	for _, key := range task.Reads {
		if key == supportedFindingsReadKey {
			out = append(out, supportedFindingsSelector(task))
			continue
		}
		sel := blackboard.LegacyReadKeyToSelector(key)
		if task.Kind == team.TaskKindSynthesize && task.VerifierRequired {
			sel.RequireVerified = true
		}
		out = append(out, sel)
	}
	return out
}

const supportedFindingsReadKey = "supported_findings"

// supportedFindingsSelector replaces the old board.SupportedFindings() escape
// hatch with a selector that declares the same intent explicitly: only
// findings whose claims pass the default verification threshold.
func supportedFindingsSelector(task team.Task) blackboard.ExchangeSelector {
	return blackboard.ExchangeSelector{
		Keys:              []string{supportedFindingsReadKey},
		RequireVerified:   true,
		IncludeText:       true,
		IncludeStructured: true,
		IncludeArtifacts:  true,
		Required:          task.Kind == team.TaskKindSynthesize && task.VerifierRequired,
		Label:             supportedFindingsReadKey,
	}
}

func cloneSelectors(selectors []blackboard.ExchangeSelector) []blackboard.ExchangeSelector {
	if len(selectors) == 0 {
		return nil
	}
	out := make([]blackboard.ExchangeSelector, len(selectors))
	copy(out, selectors)
	return out
}

func exchangeDedupKey(exchange blackboard.Exchange) string {
	if exchange.ID != "" {
		return exchange.ID
	}
	return exchange.Key + "|" + exchange.Namespace + "|" + exchange.TaskID + "|" + exchange.Text
}
