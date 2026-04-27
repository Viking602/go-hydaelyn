package eval

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type ClaimStatus string

const (
	ClaimSupported   ClaimStatus = "supported"
	ClaimUnsupported ClaimStatus = "unsupported"
	ClaimBlocked     ClaimStatus = "blocked"
)

type Claim struct {
	Text           string      `json:"text,omitempty"`
	Status         ClaimStatus `json:"status,omitempty"`
	SupportDocIDs  []string    `json:"supportDocIds,omitempty"`
	ConflictDocIDs []string    `json:"conflictDocIds,omitempty"`
	Citations      []string    `json:"citations,omitempty"`
	Conflict       bool        `json:"conflict,omitempty"`
	Reason         string      `json:"reason,omitempty"`
}

type GroundednessResult struct {
	Claims             []Claim  `json:"claims,omitempty"`
	SupportedClaims    int      `json:"supportedClaims,omitempty"`
	UnsupportedClaims  int      `json:"unsupportedClaims,omitempty"`
	BlockedClaims      int      `json:"blockedClaims,omitempty"`
	GroundednessRatio  float64  `json:"groundednessRatio,omitempty"`
	ConflictedClaims   int      `json:"conflictedClaims,omitempty"`
	UnsupportedReasons []string `json:"unsupportedReasons,omitempty"`
}

func CheckGroundedness(answer string, corpus Corpus) GroundednessResult {
	claims := extractClaims(answer)
	resolved := ResolveTemporalConflicts(claims, corpus)

	result := GroundednessResult{Claims: resolved}
	for _, claim := range resolved {
		switch claim.Status {
		case ClaimSupported:
			result.SupportedClaims++
		case ClaimUnsupported:
			result.UnsupportedClaims++
			if claim.Reason != "" {
				result.UnsupportedReasons = append(result.UnsupportedReasons, claim.Reason)
			}
		case ClaimBlocked:
			result.BlockedClaims++
		}
		if claim.Conflict {
			result.ConflictedClaims++
		}
	}
	denominator := result.SupportedClaims + result.UnsupportedClaims
	if denominator > 0 {
		result.GroundednessRatio = float64(result.SupportedClaims) / float64(denominator)
	}
	return result
}

var citationPattern = regexp.MustCompile(`\[([^\]]+)\]`)
var sentenceSplitter = regexp.MustCompile(`[.!?\n]+`)
var tokenPattern = regexp.MustCompile(`[a-z0-9]+`)

var stopwords = map[string]struct{}{
	"a": {}, "an": {}, "and": {}, "are": {}, "as": {}, "at": {}, "be": {}, "by": {},
	"for": {}, "from": {}, "has": {}, "have": {}, "if": {}, "in": {}, "is": {}, "it": {},
	"of": {}, "on": {}, "or": {}, "that": {}, "the": {}, "their": {}, "there": {}, "to": {},
	"unless": {}, "was": {}, "were": {}, "will": {}, "with": {}, "without": {}, "after": {},
	"before": {}, "into": {}, "than": {}, "then": {}, "this": {}, "those": {}, "these": {},
	"under": {}, "until": {}, "up": {}, "via": {}, "can": {}, "cannot": {}, "not": {},
}

func extractClaims(answer string) []Claim {
	raw := sentenceSplitter.Split(answer, -1)
	claims := make([]Claim, 0, len(raw))
	for _, segment := range raw {
		text := normalizeWhitespace(citationPattern.ReplaceAllString(segment, ""))
		if text == "" {
			continue
		}
		claim := Claim{Text: text, Citations: extractCitationIDs(segment)}
		if refusalSignal(text) {
			claim.Status = ClaimBlocked
			claim.Reason = "insufficient evidence acknowledged"
		}
		claims = append(claims, claim)
	}
	return claims
}

func extractCitationIDs(text string) []string {
	matches := citationPattern.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	ids := make([]string, 0, len(matches))
	for _, match := range matches {
		parts := strings.Split(match[1], ",")
		for _, part := range parts {
			id := strings.TrimSpace(part)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func assessClaimAgainstCorpus(claim Claim, corpus Corpus) Claim {
	if claim.Status == ClaimBlocked {
		return claim
	}

	supportDocs := []CorpusDocument{}
	conflictDocs := []CorpusDocument{}
	for _, doc := range candidateDocuments(claim, corpus) {
		overlap := tokenOverlapRatio(claim.Text, doc.Text)
		if overlap == 0 {
			continue
		}
		if contradicts(claim.Text, doc.Text) {
			conflictDocs = append(conflictDocs, doc)
			continue
		}
		if overlap >= 0.6 || strings.Contains(normalizeForMatch(doc.Text), normalizeForMatch(claim.Text)) {
			supportDocs = append(supportDocs, doc)
		}
	}

	claim.SupportDocIDs = documentIDs(supportDocs)
	claim.ConflictDocIDs = documentIDs(conflictDocs)
	if len(supportDocs) == 0 {
		claim.Status = ClaimUnsupported
		if len(conflictDocs) > 0 {
			claim.Conflict = true
			claim.Reason = "conflicting evidence found in corpus"
		} else {
			claim.Reason = "no supporting evidence found in corpus"
		}
		return claim
	}
	claim.Status = ClaimSupported
	if len(conflictDocs) > 0 {
		claim.Conflict = true
		claim.Reason = "supported with conflicting evidence resolved by recency"
		return claim
	}
	claim.Reason = "supported by corpus"
	return claim
}

func candidateDocuments(claim Claim, corpus Corpus) []CorpusDocument {
	if len(claim.Citations) == 0 {
		return corpus.Documents
	}
	allowed := map[string]struct{}{}
	for _, id := range claim.Citations {
		allowed[id] = struct{}{}
	}
	filtered := make([]CorpusDocument, 0, len(claim.Citations))
	for _, doc := range corpus.Documents {
		if _, ok := allowed[doc.ID]; ok {
			filtered = append(filtered, doc)
		}
	}
	if len(filtered) == 0 {
		return corpus.Documents
	}
	return filtered
}

func documentIDs(documents []CorpusDocument) []string {
	if len(documents) == 0 {
		return nil
	}
	ids := make([]string, 0, len(documents))
	for _, doc := range documents {
		ids = append(ids, doc.ID)
	}
	sort.Strings(ids)
	return ids
}

func tokenOverlapRatio(left, right string) float64 {
	leftTokens := meaningfulTokens(left)
	if len(leftTokens) == 0 {
		return 0
	}
	rightTokens := tokenSet(right)
	matched := 0
	for token := range leftTokens {
		if _, ok := rightTokens[token]; ok {
			matched++
		}
	}
	return float64(matched) / float64(len(leftTokens))
}

func meaningfulTokens(text string) map[string]struct{} {
	items := tokenSet(text)
	for token := range items {
		if _, ok := stopwords[token]; ok {
			delete(items, token)
		}
	}
	return items
}

func tokenSet(text string) map[string]struct{} {
	tokens := tokenPattern.FindAllString(strings.ToLower(text), -1)
	set := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		set[token] = struct{}{}
	}
	return set
}

func contradicts(claimText, docText string) bool {
	claimNumbers := numericTokens(claimText)
	docNumbers := numericTokens(docText)
	if len(claimNumbers) == 1 && len(docNumbers) == 1 && !sameStringSet(claimNumbers, docNumbers) {
		if tokenOverlapRatio(claimText, docText) >= 0.5 {
			return true
		}
	}
	claimNeg := negativeSignal(claimText)
	docNeg := negativeSignal(docText)
	return claimNeg != docNeg && tokenOverlapRatio(claimText, docText) >= 0.5
}

func numericTokens(text string) map[string]struct{} {
	set := map[string]struct{}{}
	for token := range tokenSet(text) {
		value, err := strconv.Atoi(token)
		if err == nil && value > 0 && value <= 999 && !(len(token) > 1 && token[0] == '0') {
			set[token] = struct{}{}
		}
	}
	return set
}

func sameStringSet(left, right map[string]struct{}) bool {
	if len(left) != len(right) {
		return false
	}
	for key := range left {
		if _, ok := right[key]; !ok {
			return false
		}
	}
	return true
}

func negativeSignal(text string) bool {
	normalized := normalizeForMatch(text)
	for _, marker := range []string{" no ", " not ", " false ", " without ", " blocked ", " denied ", " insufficient "} {
		if strings.Contains(" "+normalized+" ", marker) {
			return true
		}
	}
	return false
}

func normalizeWhitespace(text string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
}

func normalizeForMatch(text string) string {
	return " " + strings.Join(tokenPattern.FindAllString(strings.ToLower(text), -1), " ") + " "
}
