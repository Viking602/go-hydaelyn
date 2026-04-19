package evaluation

import "sort"

type CitationResult struct {
	Citations         []string `json:"citations,omitempty"`
	ValidCitations    []string `json:"validCitations,omitempty"`
	InvalidCitations  []string `json:"invalidCitations,omitempty"`
	RelevantCitations []string `json:"relevantCitations,omitempty"`
	Precision         float64  `json:"precision,omitempty"`
	Recall            float64  `json:"recall,omitempty"`
}

func ValidateCitations(answer string, corpus Corpus) CitationResult {
	claims := ResolveTemporalConflicts(extractClaims(answer), corpus)
	docIDs := map[string]struct{}{}
	for _, doc := range corpus.Documents {
		docIDs[doc.ID] = struct{}{}
	}

	cited := map[string]struct{}{}
	valid := map[string]struct{}{}
	invalid := map[string]struct{}{}
	relevant := map[string]struct{}{}
	retrievedRelevant := map[string]struct{}{}

	for _, claim := range claims {
		for _, citation := range claim.Citations {
			cited[citation] = struct{}{}
			if _, ok := docIDs[citation]; ok {
				valid[citation] = struct{}{}
			} else {
				invalid[citation] = struct{}{}
			}
		}
		for _, docID := range claim.SupportDocIDs {
			relevant[docID] = struct{}{}
			for _, citation := range claim.Citations {
				if citation == docID {
					retrievedRelevant[docID] = struct{}{}
				}
			}
		}
	}

	result := CitationResult{
		Citations:         sortedKeys(cited),
		ValidCitations:    sortedKeys(valid),
		InvalidCitations:  sortedKeys(invalid),
		RelevantCitations: sortedKeys(relevant),
	}
	if len(result.Citations) > 0 {
		result.Precision = float64(len(retrievedRelevant)) / float64(len(result.Citations))
	}
	if len(result.RelevantCitations) > 0 {
		result.Recall = float64(len(retrievedRelevant)) / float64(len(result.RelevantCitations))
	}
	return result
}

func sortedKeys(items map[string]struct{}) []string {
	if len(items) == 0 {
		return nil
	}
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
